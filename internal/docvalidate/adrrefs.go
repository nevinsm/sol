package docvalidate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// adrTableRow matches a row in the docs/decisions/README.md status table:
//
//	| 0027 | Forge as Deterministic Go Process | Superseded by ADR-0028 | ... |
//
// Group 1 = ADR number (4 digits), group 2 = status column.
var adrTableRow = regexp.MustCompile(`^\|\s*(\d{4})\s*\|[^|]*\|\s*([^|]+?)\s*\|`)

// adrSupersedeStatus matches "Superseded by ADR-NNNN" inside a status cell.
// Case-insensitive on the leading word so "superseded by" works too.
// Group 1 = the replacement ADR number.
var adrSupersedeStatus = regexp.MustCompile(`(?i)superseded\s+by\s+ADR-(\d{4})`)

// adrReferenceRe finds any "ADR-NNNN" reference in prose. Used to scan docs
// other than the index for citations.
var adrReferenceRe = regexp.MustCompile(`ADR-(\d{4})`)

// adrReadmePath returns the canonical path of the ADR index relative to
// repoRoot.
func adrReadmePath(repoRoot string) string {
	return filepath.Join(repoRoot, "docs", "decisions", "README.md")
}

// loadSupersededADRs parses docs/decisions/README.md and returns a map from
// superseded ADR number to its first canonical replacement.
//
// Only the table rows are consulted — the prose "Superseded ADRs" section at
// the bottom is informational and may chain across multiple supersessions; the
// table holds the authoritative most-recent status.
func loadSupersededADRs(repoRoot string) (map[string]string, error) {
	path := adrReadmePath(repoRoot)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open ADR index: %w", err)
	}
	defer f.Close()

	out := make(map[string]string)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		m := adrTableRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		adr := m[1]
		status := m[2]
		if rep := adrSupersedeStatus.FindStringSubmatch(status); rep != nil {
			out[adr] = rep[1]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan ADR index: %w", err)
	}
	return out, nil
}

// CheckADRReferences walks every Markdown file under docs/ (excluding the ADR
// index itself) plus CLAUDE.md and reports any citation of an ADR that has
// been superseded.
//
// Citations inside ADR files themselves are allowed — ADRs reference their
// predecessors and successors as part of their normal content. The check
// scans all other documentation files where a stale citation would mislead a
// reader.
//
// If the ADR index is missing entirely (e.g., when the validator is invoked
// against a synthetic repo with only docs/cli.md), the check is a no-op:
// there is no ground truth to enforce.
func CheckADRReferences(repoRoot string) ([]Finding, error) {
	if !fileExists(adrReadmePath(repoRoot)) {
		return nil, nil
	}
	superseded, err := loadSupersededADRs(repoRoot)
	if err != nil {
		return nil, err
	}

	var files []string
	docsDir := filepath.Join(repoRoot, "docs")
	if err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// Skip ADR files themselves and the ADR index — citations there are
		// expected.
		rel, _ := filepath.Rel(repoRoot, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "docs/decisions/") {
			return nil
		}
		files = append(files, path)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk docs/: %w", err)
	}

	// Append CLAUDE.md if present.
	if claudeMd := filepath.Join(repoRoot, "CLAUDE.md"); fileExists(claudeMd) {
		files = append(files, claudeMd)
	}

	var findings []Finding
	for _, path := range files {
		fileFindings, err := scanFileForSupersededADRs(repoRoot, path, superseded)
		if err != nil {
			return findings, err
		}
		findings = append(findings, fileFindings...)
	}
	return findings, nil
}

// scanFileForSupersededADRs reads a Markdown file line-by-line and emits one
// finding per ADR-NNNN reference whose target is in superseded.
//
// Each reference produces at most one finding per (file, line, adr) triple —
// repeated mentions of the same ADR on one line collapse into one entry.
func scanFileForSupersededADRs(repoRoot, path string, superseded map[string]string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	rel, _ := filepath.Rel(repoRoot, path)
	rel = filepath.ToSlash(rel)

	var findings []Finding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		matches := adrReferenceRe.FindAllStringSubmatch(line, -1)
		if len(matches) == 0 {
			continue
		}
		seen := make(map[string]bool)
		for _, m := range matches {
			adr := m[1]
			if seen[adr] {
				continue
			}
			seen[adr] = true
			replacement, ok := superseded[adr]
			if !ok {
				continue
			}
			findings = append(findings, Finding{
				Check:   "adr-refs",
				File:    rel,
				Line:    lineNo,
				Message: fmt.Sprintf("cites ADR-%s, which is superseded by ADR-%s", adr, replacement),
			})
		}
	}
	if err := sc.Err(); err != nil {
		return findings, fmt.Errorf("scan %s: %w", path, err)
	}
	return findings, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

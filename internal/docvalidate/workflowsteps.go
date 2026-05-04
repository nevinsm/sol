package docvalidate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// workflowStepsHeader matches "[[steps]]" at the start of a line in a TOML
// manifest. We don't need a TOML parser — `[[steps]]` only appears as an
// array-of-tables header, never inside a string or table value.
var workflowStepsHeader = regexp.MustCompile(`^\s*\[\[steps\]\]\s*$`)

// workflowSectionHeader matches a docs section header that introduces a
// workflow. Examples that should match:
//
//	### 1. rule-of-five
//	### 7. codebase-scan (project-tier)
//	### codebase-scan
//
// Group 1 = the workflow name (a slug — lowercase letters, digits, hyphens).
var workflowSectionHeader = regexp.MustCompile(`^#{2,4}\s+(?:\d+\.\s+)?([a-z][a-z0-9-]*)\b`)

// workflowStepCountClaim captures the doc's prose claim of step count for a
// workflow. Group 1 = step count.
var workflowStepCountClaim = regexp.MustCompile(`(?i)\bmanifest\s*\(\s*(\d+)\s+steps?\s*\)`)

// workflowSearchRoots are the directories under repoRoot that may contain
// workflow manifests we treat as ground truth.
func workflowSearchRoots() []string {
	return []string{
		filepath.Join(".sol", "workflows"),
		filepath.Join("internal", "workflow", "defaults"),
	}
}

// workflowDocs are the documentation files that may contain step-count claims.
// The docs/workflows.md catalog is the primary source; operations.md
// occasionally references workflows by name in monitoring runbooks.
func workflowDocs() []string {
	return []string{
		filepath.Join("docs", "workflows.md"),
		filepath.Join("docs", "operations.md"),
	}
}

// CheckWorkflowSteps verifies that every prose "(N steps)" claim in the
// workflow documentation matches the actual `[[steps]]` count of the
// corresponding manifest.toml.
//
// The check loads ground-truth step counts from .sol/workflows/<name>/ and
// internal/workflow/defaults/<name>/. It then walks docs/workflows.md and
// docs/operations.md, and for every "(N steps)" claim it cross-references the
// most recently introduced workflow section.
//
// Workflows mentioned only in prose with no corresponding manifest are
// reported so dead documentation doesn't escape detection.
func CheckWorkflowSteps(repoRoot string) ([]Finding, error) {
	manifests, err := loadWorkflowManifests(repoRoot)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, doc := range workflowDocs() {
		path := filepath.Join(repoRoot, doc)
		if !fileExists(path) {
			continue
		}
		f, err := scanDocForStepClaims(repoRoot, path, manifests)
		if err != nil {
			return findings, err
		}
		findings = append(findings, f...)
	}
	return findings, nil
}

// loadWorkflowManifests returns a map from workflow name → step count by
// scanning every manifest.toml under the configured search roots.
//
// If two manifests share a name (project-tier shadowing embedded-tier) the
// project tier wins, matching `sol workflow list`.
func loadWorkflowManifests(repoRoot string) (map[string]int, error) {
	out := make(map[string]int)
	// Iterate roots in priority order (project before embedded) so that the
	// project-tier copy wins on conflict.
	for _, rel := range workflowSearchRoots() {
		root := filepath.Join(repoRoot, rel)
		if !dirExists(root) {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if _, alreadySet := out[name]; alreadySet {
				continue
			}
			manifest := filepath.Join(root, name, "manifest.toml")
			if !fileExists(manifest) {
				continue
			}
			n, err := countStepsInManifest(manifest)
			if err != nil {
				return nil, err
			}
			out[name] = n
		}
	}
	return out, nil
}

// countStepsInManifest returns the number of `[[steps]]` headers in the
// given manifest TOML file.
func countStepsInManifest(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if workflowStepsHeader.MatchString(sc.Text()) {
			count++
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("scan %s: %w", path, err)
	}
	return count, nil
}

// scanDocForStepClaims reads a markdown doc, tracks the most recent workflow
// section, and emits findings for any (N steps) claim that disagrees with the
// manifest.
func scanDocForStepClaims(repoRoot, path string, manifests map[string]int) ([]Finding, error) {
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

	currentWorkflow := ""
	currentLine := 0
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()

		// Update the active workflow context whenever we cross a section
		// header. Only change it when the heading slug looks like a workflow
		// we know about, so unrelated headers (e.g., "## TOML Schema
		// Reference") don't poison the context.
		if m := workflowSectionHeader.FindStringSubmatch(line); m != nil {
			candidate := strings.ToLower(m[1])
			if _, known := manifests[candidate]; known {
				currentWorkflow = candidate
				currentLine = lineNo
			}
		}

		// Look for an "(N steps)" claim. We require the active workflow
		// context — orphan claims (no preceding workflow header) are flagged.
		mm := workflowStepCountClaim.FindStringSubmatch(line)
		if mm == nil {
			continue
		}
		var claimed int
		_, _ = fmt.Sscanf(mm[1], "%d", &claimed)

		if currentWorkflow == "" {
			findings = append(findings, Finding{
				Check:   "workflow-steps",
				File:    rel,
				Line:    lineNo,
				Message: fmt.Sprintf("found '(%d steps)' claim with no preceding workflow heading; cannot validate", claimed),
			})
			continue
		}
		actual, ok := manifests[currentWorkflow]
		if !ok {
			findings = append(findings, Finding{
				Check:   "workflow-steps",
				File:    rel,
				Line:    lineNo,
				Message: fmt.Sprintf("workflow %q has no manifest.toml under .sol/workflows/ or internal/workflow/defaults/ (heading at %s:%d)", currentWorkflow, rel, currentLine),
			})
			continue
		}
		if claimed != actual {
			findings = append(findings, Finding{
				Check:   "workflow-steps",
				File:    rel,
				Line:    lineNo,
				Message: fmt.Sprintf("workflow %q: doc claims %d steps, manifest has %d", currentWorkflow, claimed, actual),
			})
		}
	}
	if err := sc.Err(); err != nil {
		return findings, fmt.Errorf("scan %s: %w", path, err)
	}

	// Stable ordering for tests: findings emitted in line order are already
	// sorted, but sort defensively in case of future expansion.
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Line < findings[j].Line })

	return findings, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

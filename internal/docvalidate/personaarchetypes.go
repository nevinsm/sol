package docvalidate

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// personaDefaultsPath is the directory containing embedded persona templates.
// The registry is derived from .md files in this directory (see internal/persona/defaults.go).
const personaDefaultsPath = "internal/persona/defaults"

// personaArchetypeRow matches the built-in templates table in
// docs/personas.md:
//
//	| `planner`  | Polaris   | ... |
//
// Group 1 = the template name (the Name column), group 2 = the archetype
// label (the Archetype column).
var personaArchetypeRow = regexp.MustCompile("^\\|\\s*`([a-zA-Z0-9_-]+)`\\s*\\|\\s*([A-Za-z0-9_-]+)\\s*\\|")

// CheckPersonaArchetypes asserts that every persona template name listed in
// docs/personas.md (and surfaced in docs/naming.md) corresponds to an
// embedded template under internal/persona/defaults/.
//
// The registered set is derived by scanning the defaults directory for .md
// files (matching how internal/persona/defaults.go populates knownDefaults at
// init time). Only the template "Name" column is checked against the registry
// — the archetype labels in the second column (Polaris, Meridian, …) are
// operator-facing nicknames and may legitimately be richer than the registry
// slug. They are still reported alongside the template name for diagnostic
// clarity.
func CheckPersonaArchetypes(repoRoot string) ([]Finding, error) {
	defaultsDir := filepath.Join(repoRoot, personaDefaultsPath)
	if !dirExists(defaultsDir) {
		return nil, nil
	}
	registered, err := loadPersonaRegistry(defaultsDir)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	docs := []string{
		filepath.Join("docs", "personas.md"),
		filepath.Join("docs", "naming.md"),
	}
	for _, doc := range docs {
		path := filepath.Join(repoRoot, doc)
		if !fileExists(path) {
			continue
		}
		f, err := scanPersonaDoc(repoRoot, path, registered)
		if err != nil {
			return findings, err
		}
		findings = append(findings, f...)
	}
	return findings, nil
}

// loadPersonaRegistry scans the persona defaults directory for .md files and
// returns the set of registered template names (file basenames stripped of
// the .md suffix). This mirrors how internal/persona/defaults.go populates
// the knownDefaults map at init time from the embedded FS.
func loadPersonaRegistry(defaultsDir string) (map[string]bool, error) {
	entries, err := os.ReadDir(defaultsDir)
	if err != nil {
		return nil, fmt.Errorf("read persona defaults dir %s: %w", defaultsDir, err)
	}
	registered := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		registered[strings.TrimSuffix(name, ".md")] = true
	}
	return registered, nil
}

// scanPersonaDoc reads a markdown doc and emits a finding for every persona
// template name (the first column of the persona table) that has no
// matching entry in the registry.
func scanPersonaDoc(repoRoot, path string, registered map[string]bool) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	rel, _ := filepath.Rel(repoRoot, path)
	rel = filepath.ToSlash(rel)
	codeRel := filepath.ToSlash(personaDefaultsPath)

	var findings []Finding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		m := personaArchetypeRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		archetype := strings.TrimSpace(m[2])
		// Skip header/divider rows that happen to match the template syntax.
		if strings.EqualFold(name, "name") {
			continue
		}
		if registered[name] {
			continue
		}
		findings = append(findings, Finding{
			Check:   "persona-archetypes",
			File:    rel,
			Line:    lineNo,
			Message: fmt.Sprintf("persona template %q (archetype %q) not found in %s/", name, archetype, codeRel),
		})
	}
	if err := sc.Err(); err != nil {
		return findings, fmt.Errorf("scan %s: %w", path, err)
	}
	return findings, nil
}

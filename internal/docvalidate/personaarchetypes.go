package docvalidate

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// personaDefaultsPath is where the embedded persona registry lives.
const personaDefaultsPath = "internal/persona/defaults.go"

// personaArchetypeRow matches the built-in templates table in
// docs/personas.md:
//
//	| `planner`  | Polaris   | ... |
//
// Group 1 = the template name (the Name column), group 2 = the archetype
// label (the Archetype column).
var personaArchetypeRow = regexp.MustCompile("^\\|\\s*`([a-zA-Z0-9_-]+)`\\s*\\|\\s*([A-Za-z0-9_-]+)\\s*\\|")

// CheckPersonaArchetypes asserts that every persona template name listed in
// docs/personas.md (and surfaced in docs/naming.md) corresponds to a
// registered template in internal/persona/defaults.go.
//
// The defaults map is parsed with go/parser so that the registered names
// survive multi-line declarations and added comments. Only the template
// "Name" column is checked against the registry — the archetype labels in
// the second column (Polaris, Meridian, …) are operator-facing nicknames
// and may legitimately be richer than the registry slug. They are still
// reported alongside the template name for diagnostic clarity.
func CheckPersonaArchetypes(repoRoot string) ([]Finding, error) {
	if !fileExists(filepath.Join(repoRoot, personaDefaultsPath)) {
		return nil, nil
	}
	registered, declLine, err := loadPersonaRegistry(repoRoot)
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
		f, err := scanPersonaDoc(repoRoot, path, registered, declLine)
		if err != nil {
			return findings, err
		}
		findings = append(findings, f...)
	}
	return findings, nil
}

// loadPersonaRegistry parses internal/persona/defaults.go and extracts the
// keys of `var knownDefaults = map[string]bool{...}`. Returns the set of
// registered names plus the declaration line for diagnostic context.
func loadPersonaRegistry(repoRoot string) (map[string]bool, int, error) {
	path := filepath.Join(repoRoot, personaDefaultsPath)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
	if err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", path, err)
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "knownDefaults" {
					continue
				}
				if i >= len(vs.Values) {
					return nil, 0, fmt.Errorf("var knownDefaults has no initializer in %s", path)
				}
				comp, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					return nil, 0, fmt.Errorf("var knownDefaults is not a composite literal in %s", path)
				}
				registered := make(map[string]bool, len(comp.Elts))
				for _, elt := range comp.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						return nil, 0, fmt.Errorf("non-keyvalue element in knownDefaults at %s:%d", path, fset.Position(elt.Pos()).Line)
					}
					bl, ok := kv.Key.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						return nil, 0, fmt.Errorf("non-string key in knownDefaults at %s:%d", path, fset.Position(kv.Key.Pos()).Line)
					}
					s := bl.Value
					if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
						s = s[1 : len(s)-1]
					}
					registered[s] = true
				}
				return registered, fset.Position(name.Pos()).Line, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("var knownDefaults not found in %s", path)
}

// scanPersonaDoc reads a markdown doc and emits a finding for every persona
// template name (the first column of the persona table) that has no
// matching entry in the registry.
func scanPersonaDoc(repoRoot, path string, registered map[string]bool, declLine int) ([]Finding, error) {
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
			Message: fmt.Sprintf("persona template %q (archetype %q) not registered in %s:%d (knownDefaults)", name, archetype, codeRel, declLine),
		})
	}
	if err := sc.Err(); err != nil {
		return findings, fmt.Errorf("scan %s: %w", path, err)
	}
	return findings, nil
}

package docvalidate

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// servicePackagePath is the location of the components slice we treat as
// ground truth, relative to repoRoot.
const servicePackagePath = "internal/service/service.go"

// recoveryMatrixDoc is the markdown file holding the Recovery Matrix table.
const recoveryMatrixDoc = "docs/failure-modes.md"

// CheckRecoveryMatrix asserts that every entry in the service.Components
// slice (the canonical list of sphere daemons managed as system services)
// has a corresponding row in the "Recovery Matrix" table in
// docs/failure-modes.md.
//
// The slice is parsed with go/parser/go/ast — not regex — so that a
// multi-line declaration or future formatting changes don't break the
// check. The component name comparison is case-insensitive: code uses
// "broker", docs use "Broker".
//
// If either the components source file or the failure-modes doc is missing,
// the check is a no-op — there is no ground truth to enforce.
func CheckRecoveryMatrix(repoRoot string) ([]Finding, error) {
	servicePath := filepath.Join(repoRoot, servicePackagePath)
	docPath := filepath.Join(repoRoot, recoveryMatrixDoc)
	if !fileExists(servicePath) || !fileExists(docPath) {
		return nil, nil
	}

	components, declLine, err := loadServiceComponents(repoRoot)
	if err != nil {
		return nil, err
	}

	rows, err := loadRecoveryMatrixRows(docPath)
	if err != nil {
		return nil, err
	}

	docRel := filepath.ToSlash(recoveryMatrixDoc)
	codeRel := filepath.ToSlash(servicePackagePath)

	rowsLower := make(map[string]bool, len(rows))
	for r := range rows {
		rowsLower[strings.ToLower(r)] = true
	}

	var findings []Finding
	for _, comp := range components {
		if rowsLower[strings.ToLower(comp)] {
			continue
		}
		findings = append(findings, Finding{
			Check:   "recovery-matrix",
			File:    docRel,
			Line:    0,
			Message: fmt.Sprintf("missing Recovery Matrix row for service component %q (declared in %s:%d)", comp, codeRel, declLine),
		})
	}
	return findings, nil
}

// loadServiceComponents parses internal/service/service.go and returns the
// string elements of the package-level `Components` slice declaration, plus
// the line number of the declaration itself for diagnostic context.
func loadServiceComponents(repoRoot string) ([]string, int, error) {
	path := filepath.Join(repoRoot, servicePackagePath)
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
				if name.Name != "Components" {
					continue
				}
				if i >= len(vs.Values) {
					return nil, 0, fmt.Errorf("var Components has no initializer in %s", path)
				}
				comp, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					return nil, 0, fmt.Errorf("var Components is not a composite literal in %s", path)
				}
				out := make([]string, 0, len(comp.Elts))
				for _, elt := range comp.Elts {
					bl, ok := elt.(*ast.BasicLit)
					if !ok || bl.Kind != token.STRING {
						return nil, 0, fmt.Errorf("non-string element in Components literal at %s:%d", path, fset.Position(elt.Pos()).Line)
					}
					// Strip the surrounding quotes.
					s := bl.Value
					if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
						s = s[1 : len(s)-1]
					}
					out = append(out, s)
				}
				return out, fset.Position(name.Pos()).Line, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("var Components not found in %s", path)
}

// loadRecoveryMatrixRows scans the failure-modes doc for the "Recovery
// Matrix" table and returns the set of component names appearing in the
// first column.
//
// We anchor on the "## Recovery Matrix" heading and read until the next
// heading at the same level or above. Each row is a `|`-delimited line whose
// first cell is the component name; header and divider rows are skipped.
func loadRecoveryMatrixRows(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	rows := make(map[string]int)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	inSection := false
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trim := strings.TrimSpace(line)

		// Section detection.
		if rest, ok := strings.CutPrefix(trim, "## "); ok {
			inSection = strings.EqualFold(strings.TrimSpace(rest), "Recovery Matrix")
			continue
		}
		// A higher-level header ends the section.
		if strings.HasPrefix(trim, "# ") {
			inSection = false
			continue
		}
		if !inSection {
			continue
		}

		if !strings.HasPrefix(trim, "|") {
			continue
		}
		// Skip the divider row (cells consist solely of dashes/colons/spaces).
		if isMarkdownTableDivider(trim) {
			continue
		}
		cells := splitMarkdownRow(trim)
		if len(cells) == 0 {
			continue
		}
		first := strings.TrimSpace(cells[0])
		// Skip the header row: the first column heading is "Component".
		if strings.EqualFold(first, "Component") {
			continue
		}
		if first == "" {
			continue
		}
		rows[first] = lineNo
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return rows, nil
}

// splitMarkdownRow splits a markdown table row on '|'. The leading and
// trailing pipes produce empty strings, which the caller filters.
func splitMarkdownRow(row string) []string {
	parts := strings.Split(row, "|")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		// Drop the empty leading and trailing fragments produced by the
		// outer pipes.
		if (i == 0 || i == len(parts)-1) && strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// isMarkdownTableDivider reports whether a row is a "|---|---|" style
// divider (cells contain only dashes, colons, whitespace, or pipes).
func isMarkdownTableDivider(row string) bool {
	trim := strings.TrimSpace(row)
	if !strings.HasPrefix(trim, "|") {
		return false
	}
	for _, r := range trim {
		switch r {
		case '|', '-', ':', ' ', '\t':
			// allowed
		default:
			return false
		}
	}
	return true
}

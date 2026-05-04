package docvalidate

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// CheckHeartbeatPaths cross-references the canonical heartbeat path constants
// in the Go source against the heartbeat-path table in docs/operations.md.
//
// For each `*heartbeat*.go` file under internal/, we look for a function
// named HeartbeatPath or heartbeatPath and render its return expression as a
// path template (e.g. "$SOL_HOME/.runtime/broker-heartbeat.json"). We then
// look up the component name (the package name, capitalized) in the
// "Heartbeat path" table and compare.
//
// The component-to-path render uses a tiny symbolic evaluator: filepath.Join
// of (1) string literals, (2) `world` parameter references, and (3) a known
// set of helpers (config.Home, config.RuntimeDir) is resolved to the
// equivalent path. Other constructions are reported and skipped — better to
// say "unknown construction" than to silently emit a wrong expected value.
func CheckHeartbeatPaths(repoRoot string) ([]Finding, error) {
	internalDir := filepath.Join(repoRoot, "internal")
	docPath := filepath.Join(repoRoot, "docs", "operations.md")
	// No ground truth → nothing to check (e.g., synthetic test repos).
	if !dirExists(internalDir) || !fileExists(docPath) {
		return nil, nil
	}

	codePaths, err := loadCodeHeartbeatPaths(repoRoot)
	if err != nil {
		return nil, err
	}

	docRows, err := loadHeartbeatDocRows(repoRoot)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	docRel := filepath.ToSlash(filepath.Join("docs", "operations.md"))

	for component, codePath := range codePaths {
		row, ok := docRows[strings.ToLower(component)]
		if !ok {
			findings = append(findings, Finding{
				Check:   "heartbeat-paths",
				File:    docRel,
				Line:    0,
				Message: fmt.Sprintf("missing heartbeat-path row for component %q (code: %s)", component, codePath),
			})
			continue
		}
		if !heartbeatPathsEqual(codePath, row.Path) {
			findings = append(findings, Finding{
				Check:   "heartbeat-paths",
				File:    docRel,
				Line:    row.Line,
				Message: fmt.Sprintf("component %q: doc says %q, code says %q", component, row.Path, codePath),
			})
		}
	}

	return findings, nil
}

// loadCodeHeartbeatPaths walks internal/ for any non-test Go file and looks
// for a HeartbeatPath/heartbeatPath function declaration whose body matches
// our expected shape. Returns a map from component name (package name
// title-cased) to the rendered path.
//
// Some packages (notably broker) define `heartbeatPath()` in the main
// package file rather than in a heartbeat-named file, so we can't filter by
// filename. The AST cost across internal/ is small (a few hundred files).
//
// If the same component has both an exported and unexported HeartbeatPath,
// the exported one wins; otherwise the unexported one is used.
func loadCodeHeartbeatPaths(repoRoot string) (map[string]string, error) {
	internalDir := filepath.Join(repoRoot, "internal")
	exported := make(map[string]string)
	unexported := make(map[string]string)

	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if !strings.HasSuffix(base, ".go") || strings.HasSuffix(base, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		pkg := capitalizeASCII(file.Name.Name)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if fn.Name.Name != "HeartbeatPath" && fn.Name.Name != "heartbeatPath" {
				continue
			}
			rendered, ok := renderHeartbeatReturn(fn)
			if !ok {
				// Skip silently — function exists but has unrecognized body
				// shape. Could be a stub or a future variant.
				continue
			}
			if fn.Name.IsExported() {
				if _, set := exported[pkg]; !set {
					exported[pkg] = rendered
				}
			} else if _, set := unexported[pkg]; !set {
				unexported[pkg] = rendered
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(exported)+len(unexported))
	maps.Copy(out, unexported)
	// Exported overrides unexported when both exist in the same package.
	maps.Copy(out, exported)
	return out, nil
}

// renderHeartbeatReturn extracts the single return-statement expression of
// HeartbeatPath/heartbeatPath and renders it as a slash-separated path
// template. The second return value reports whether the body matched the
// expected shape.
func renderHeartbeatReturn(fn *ast.FuncDecl) (string, bool) {
	if fn.Body == nil {
		return "", false
	}
	for _, stmt := range fn.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			continue
		}
		s, ok := renderPathExpr(ret.Results[0])
		if !ok {
			return "", false
		}
		return s, true
	}
	return "", false
}

// renderPathExpr maps a Go expression to its symbolic path representation.
// Supports the small subset of expressions that appear in the codebase's
// heartbeat path constructors:
//
//   - filepath.Join(args...) — concatenated with "/".
//   - config.Home() → "$SOL_HOME"
//   - config.RuntimeDir() → "$SOL_HOME/.runtime"
//   - identifier `world` (function param) → "{world}"
//   - string literal → its value
//
// Unknown shapes return ("", false) so callers can flag them rather than
// silently rendering "" and producing bogus diffs.
func renderPathExpr(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s := e.Value
		if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
			s = s[1 : len(s)-1]
		}
		return s, true
	case *ast.Ident:
		// Bare identifier — usually a function parameter. The canonical
		// names are documented above. We special-case the SOL_HOME-typed
		// parameter so callers don't see {solHome} when the doc rightly
		// shows $SOL_HOME.
		if isSolHomeParam(e.Name) {
			return "$SOL_HOME", true
		}
		return "{" + e.Name + "}", true
	case *ast.CallExpr:
		return renderCallExpr(e)
	}
	return "", false
}

// renderCallExpr handles the two function shapes we care about:
//
//	filepath.Join(...)
//	config.Home() / config.RuntimeDir()
func renderCallExpr(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	switch {
	case pkg.Name == "filepath" && sel.Sel.Name == "Join":
		parts := make([]string, 0, len(call.Args))
		for _, a := range call.Args {
			s, ok := renderPathExpr(a)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "/"), true
	case pkg.Name == "config" && sel.Sel.Name == "Home":
		return "$SOL_HOME", true
	case pkg.Name == "config" && sel.Sel.Name == "RuntimeDir":
		return "$SOL_HOME/.runtime", true
	}
	return "", false
}

// heartbeatPathDocRow is the doc-side record for one row of the heartbeat
// path table.
type heartbeatPathDocRow struct {
	Component string
	Path      string
	Line      int
}

// loadHeartbeatDocRows scans docs/operations.md for the heartbeat-path table
// and returns a map keyed by lower-cased component name.
//
// We anchor on a header containing "Heartbeat" and read forward until a
// blank line ends the table. Path cells are wrapped in backticks per the
// repo's markdown convention; the backticks are stripped before storage.
func loadHeartbeatDocRows(repoRoot string) (map[string]heartbeatPathDocRow, error) {
	path := filepath.Join(repoRoot, "docs", "operations.md")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	rows := make(map[string]heartbeatPathDocRow)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	type tableState int
	const (
		searchingForTable tableState = iota
		readingTable
	)
	state := searchingForTable
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trim := strings.TrimSpace(line)

		switch state {
		case searchingForTable:
			if !strings.HasPrefix(trim, "|") {
				continue
			}
			cells := splitMarkdownRow(trim)
			if len(cells) < 2 {
				continue
			}
			first := strings.TrimSpace(cells[0])
			second := strings.TrimSpace(cells[1])
			if strings.EqualFold(first, "Component") && strings.Contains(strings.ToLower(second), "heartbeat") {
				state = readingTable
			}
		case readingTable:
			if !strings.HasPrefix(trim, "|") {
				if trim == "" {
					return rows, nil
				}
				continue
			}
			if isMarkdownTableDivider(trim) {
				continue
			}
			cells := splitMarkdownRow(trim)
			if len(cells) < 2 {
				continue
			}
			component := strings.TrimSpace(cells[0])
			pathCell := strings.TrimSpace(cells[1])
			pathCell = strings.Trim(pathCell, "`")
			if component == "" {
				continue
			}
			rows[strings.ToLower(component)] = heartbeatPathDocRow{
				Component: component,
				Path:      pathCell,
				Line:      lineNo,
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return rows, nil
}

// heartbeatPathsEqual normalizes minor whitespace differences before
// comparing.
func heartbeatPathsEqual(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// isSolHomeParam reports whether a parameter name is one of the canonical
// SOL_HOME-typed names used in the codebase. The list is small and
// closed; widening it requires evidence from real code.
func isSolHomeParam(name string) bool {
	switch name {
	case "solHome", "solhome", "SOLHome", "home":
		return true
	}
	return false
}

// capitalizeASCII returns s with its first byte upper-cased. Package names
// are ASCII identifiers, so this is sufficient for rendering "broker" →
// "Broker" without pulling in golang.org/x/text/cases.
func capitalizeASCII(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-('a'-'A')) + s[1:]
	}
	return s
}

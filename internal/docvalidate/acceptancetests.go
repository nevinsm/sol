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

// acceptanceCheckedItem matches a checked-off acceptance criterion that
// includes a Go test reference. Examples:
//
//	- [x] some criterion (`TestFoo`)
//	- [x] criterion `TestFooBar` covers it
//
// Group 1 = test function name (must start with Test followed by an
// upper-case ASCII letter).
var acceptanceCheckedItem = regexp.MustCompile("(?i)^[\\s>]*-\\s*\\[x\\][^`]*`(Test[A-Z][A-Za-z0-9_]*)`")

// CheckAcceptanceTests walks every test/integration/LOOP*_ACCEPTANCE.md and
// reports each `[x]` criterion that names a Go test function which doesn't
// exist anywhere in test/integration/.
//
// The set of existing tests is extracted with go/parser — every top-level
// `func TestX(t *testing.T)` is treated as registered. The doc side uses a
// regex match on the line, but the *existence* check is AST-driven so that
// renames, build tags, or type ambiguities don't produce false positives.
func CheckAcceptanceTests(repoRoot string) ([]Finding, error) {
	docsDir := filepath.Join(repoRoot, "test", "integration")
	// No test/integration/ tree → nothing to check (e.g., synthetic repos).
	if !dirExists(docsDir) {
		return nil, nil
	}
	tests, err := loadIntegrationTests(repoRoot)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(docsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", docsDir, err)
	}

	var findings []Finding
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "LOOP") || !strings.HasSuffix(name, "_ACCEPTANCE.md") {
			continue
		}
		path := filepath.Join(docsDir, name)
		f, err := scanAcceptanceDoc(repoRoot, path, tests)
		if err != nil {
			return findings, err
		}
		findings = append(findings, f...)
	}
	return findings, nil
}

// loadIntegrationTests parses every *_test.go file under test/integration/
// and returns the set of top-level test function names defined there.
func loadIntegrationTests(repoRoot string) (map[string]bool, error) {
	out := make(map[string]bool)
	root := filepath.Join(repoRoot, "test", "integration")

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			if strings.HasPrefix(name, "Test") && len(name) > 4 && isUpperASCII(name[4]) {
				out[name] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scanAcceptanceDoc reads a single LOOP*_ACCEPTANCE.md and emits a finding
// for every `[x]` line referencing an unknown Test function.
//
// One finding per (file, line, test) — repeated mentions of the same test
// on a single line collapse so the output stays readable.
func scanAcceptanceDoc(repoRoot, path string, tests map[string]bool) ([]Finding, error) {
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
		// Cheap pre-filter — most lines have no `[x]` and no backtick.
		if !strings.Contains(line, "[x]") && !strings.Contains(line, "[X]") {
			continue
		}
		// Walk every `Test...` reference on the line so a bullet that names
		// multiple tests is fully checked.
		seen := make(map[string]bool)
		for _, m := range allBacktickedTestNames(line) {
			if seen[m] {
				continue
			}
			seen[m] = true
			if tests[m] {
				continue
			}
			findings = append(findings, Finding{
				Check:   "acceptance-tests",
				File:    rel,
				Line:    lineNo,
				Message: fmt.Sprintf("references %s, but no such test function exists under test/integration/", m),
			})
		}
	}
	if err := sc.Err(); err != nil {
		return findings, fmt.Errorf("scan %s: %w", path, err)
	}
	return findings, nil
}

// backtickedTestRe finds every `TestFooBar`-style identifier wrapped in
// backticks anywhere on a line. It assumes the line has already been
// pre-filtered to contain `[x]` or `[X]`.
var backtickedTestRe = regexp.MustCompile("`(Test[A-Z][A-Za-z0-9_]*)`")

func allBacktickedTestNames(line string) []string {
	matches := backtickedTestRe.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		// Fall back to the stricter "[x]" prefix pattern so the simple
		// bullet form still matches when the test reference is at the head
		// of the line. (The general regex above already covers that case;
		// this path mostly exists for the test fixtures.)
		if m := acceptanceCheckedItem.FindStringSubmatch(line); m != nil {
			return []string{m[1]}
		}
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

func isUpperASCII(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

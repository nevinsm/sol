package envfile

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// parseString is a test helper that parses a .env string directly.
func parseString(s string) (map[string]string, error) {
	return parse(bufio.NewScanner(strings.NewReader(s)))
}

// --- parse tests ---

func TestParse_BasicKeyValue(t *testing.T) {
	got, err := parseString("KEY=value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_CommentsIgnored(t *testing.T) {
	got, err := parseString("# this is a comment\nKEY=value\n# another comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_BlankLinesIgnored(t *testing.T) {
	got, err := parseString("\nKEY=value\n\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_EmptyValue(t *testing.T) {
	got, err := parseString("KEY=")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_DoubleQuotedValue(t *testing.T) {
	got, err := parseString(`KEY="quoted value"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "quoted value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_SingleQuotedValue(t *testing.T) {
	got, err := parseString("KEY='quoted value'")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "quoted value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_MismatchedQuotesNotStripped(t *testing.T) {
	got, err := parseString(`KEY="value'`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": `"value'`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_ValueContainsEquals(t *testing.T) {
	// Value should include everything after the first '='.
	got, err := parseString("DATABASE_URL=postgres://localhost:5432/myproject?sslmode=disable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"DATABASE_URL": "postgres://localhost:5432/myproject?sslmode=disable"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_WhitespaceTrimmed(t *testing.T) {
	got, err := parseString("  KEY  =  value  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_MissingEquals_Skipped(t *testing.T) {
	got, err := parseString("NOTAKEYVALUE\nKEY=value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParse_MultipleKeys(t *testing.T) {
	got, err := parseString("ANTHROPIC_API_KEY=sk-ant-abc\nGITHUB_TOKEN=ghp_xyz\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-abc",
		"GITHUB_TOKEN":      "ghp_xyz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// --- LoadEnv tests ---

func TestLoadEnv_MissingBothFiles(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadEnv(dir, "myworld")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestLoadEnv_SphereOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "KEY=sphere")

	got, err := LoadEnv(dir, "myworld")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "sphere"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoadEnv_WorldOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "myworld"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "myworld", ".env"), "KEY=world")

	got, err := LoadEnv(dir, "myworld")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "world"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoadEnv_WorldOverridesSphere(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "myworld"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".env"), "KEY=sphere\nSPHERE_ONLY=sphere")
	writeFile(t, filepath.Join(dir, "myworld", ".env"), "KEY=world\nWORLD_ONLY=world")

	got, err := LoadEnv(dir, "myworld")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"KEY":         "world",
		"SPHERE_ONLY": "sphere",
		"WORLD_ONLY":  "world",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoadEnv_EmptyWorldName_SphereOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "KEY=sphere")

	got, err := LoadEnv(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"KEY": "sphere"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoadEnv_EmptyValuesPreserved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "KEY=\nOTHER=value")

	got, err := LoadEnv(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got["KEY"]; !ok || v != "" {
		t.Errorf("expected KEY to be empty string, got %q (ok=%v)", v, ok)
	}
	if got["OTHER"] != "value" {
		t.Errorf("expected OTHER=value, got %q", got["OTHER"])
	}
}

func TestLoadEnv_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	content := "# sphere secrets\n\nANTHROPIC_API_KEY=sk-ant-abc\n# end\n"
	writeFile(t, filepath.Join(dir, ".env"), content)

	got, err := LoadEnv(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-abc"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestLoadEnv_MissingSphereFile(t *testing.T) {
	// Only world file exists — sphere file missing is not an error.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "myworld"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "myworld", ".env"), "WORLD_KEY=wv")

	got, err := LoadEnv(dir, "myworld")
	if err != nil {
		t.Fatalf("unexpected error when sphere .env is missing: %v", err)
	}
	if got["WORLD_KEY"] != "wv" {
		t.Errorf("expected WORLD_KEY=wv, got %v", got)
	}
}

func TestLoadEnv_MissingWorldFile(t *testing.T) {
	// Only sphere file exists — world file missing is not an error.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SPHERE_KEY=sv")

	got, err := LoadEnv(dir, "noworld")
	if err != nil {
		t.Fatalf("unexpected error when world .env is missing: %v", err)
	}
	if got["SPHERE_KEY"] != "sv" {
		t.Errorf("expected SPHERE_KEY=sv, got %v", got)
	}
}

// --- ParseFile tests ---

// writeEnvFile is a test helper that writes a .env file and returns its path.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseFileBasic(t *testing.T) {
	path := writeEnvFile(t, "FOO=bar\nBAZ=qux\n")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["FOO"] != "bar" {
		t.Errorf("FOO: want %q, got %q", "bar", got["FOO"])
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ: want %q, got %q", "qux", got["BAZ"])
	}
}

func TestParseFileDoubleQuoted(t *testing.T) {
	path := writeEnvFile(t, `KEY="hello world"`)
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "hello world" {
		t.Errorf("KEY: want %q, got %q", "hello world", got["KEY"])
	}
}

func TestParseFileSingleQuoted(t *testing.T) {
	path := writeEnvFile(t, "KEY='single quoted'")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "single quoted" {
		t.Errorf("KEY: want %q, got %q", "single quoted", got["KEY"])
	}
}

func TestParseFileEmptyValue(t *testing.T) {
	path := writeEnvFile(t, "EMPTY=\nALSO_EMPTY=\"\"\n")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got["EMPTY"]; !ok || v != "" {
		t.Errorf("EMPTY: want empty string, got %q (ok=%v)", v, ok)
	}
	if v, ok := got["ALSO_EMPTY"]; !ok || v != "" {
		t.Errorf("ALSO_EMPTY: want empty string, got %q (ok=%v)", v, ok)
	}
}

func TestParseFileCommentsAndBlanks(t *testing.T) {
	content := `# this is a comment
FOO=bar

# another comment
BAZ=qux
`
	path := writeEnvFile(t, content)
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 pairs, got %d: %v", len(got), got)
	}
}

func TestParseFileExportPrefix(t *testing.T) {
	path := writeEnvFile(t, "export MY_VAR=myvalue\n")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["MY_VAR"] != "myvalue" {
		t.Errorf("MY_VAR: want %q, got %q", "myvalue", got["MY_VAR"])
	}
}

func TestParseFileMissingEquals(t *testing.T) {
	path := writeEnvFile(t, "NOEQUALS\n")
	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for missing '=', got nil")
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Errorf("expected *ParseError, got %T: %v", err, err)
	}
	if parseErr != nil && parseErr.Line != 1 {
		t.Errorf("expected line 1, got %d", parseErr.Line)
	}
}

func TestParseFileParseErrorLineNumber(t *testing.T) {
	content := "GOOD=value\n# comment\n\nBAD_LINE\n"
	path := writeEnvFile(t, content)
	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if parseErr.Line != 4 {
		t.Errorf("expected line 4, got %d", parseErr.Line)
	}
}

func TestParseFileNotExist(t *testing.T) {
	_, err := ParseFile("/nonexistent/.env")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestParseFileEmpty(t *testing.T) {
	path := writeEnvFile(t, "")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseFileValueWithEquals(t *testing.T) {
	// Value containing '=' should be preserved.
	path := writeEnvFile(t, "URL=http://example.com?foo=bar\n")
	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["URL"] != "http://example.com?foo=bar" {
		t.Errorf("URL: want %q, got %q", "http://example.com?foo=bar", got["URL"])
	}
}

func TestParseErrorFormat(t *testing.T) {
	e := &ParseError{Path: "/some/.env", Line: 3, Msg: "missing '=' separator"}
	got := e.Error()
	want := "/some/.env:3: missing '=' separator"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %q: %v", path, err)
	}
}

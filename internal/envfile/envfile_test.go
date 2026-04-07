package envfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeEnvFile is a test helper that writes a .env file at dir/name and
// returns its path.
func writeEnvFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write env file: %v", err)
	}
	return path
}

// writeFile is a test helper that writes content to path.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeFile %q: %v", path, err)
	}
}

// --- ParseFile tests ---

func TestParseFileBasic(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "KEY=value\nFOO=bar\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "value" {
		t.Errorf("KEY: got %q, want %q", got["KEY"], "value")
	}
	if got["FOO"] != "bar" {
		t.Errorf("FOO: got %q, want %q", got["FOO"], "bar")
	}
}

func TestParseFileExportPrefix(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "export KEY=value\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "value" {
		t.Errorf("KEY: got %q, want %q", got["KEY"], "value")
	}
	// Ensure "export KEY" is not present as a raw key.
	if _, ok := got["export KEY"]; ok {
		t.Error("expected 'export KEY' to be stripped, but it was kept as a key")
	}
}

func TestParseFileExportPrefixQuotedValue(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", `export KEY="quoted value"`+"\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "quoted value" {
		t.Errorf("KEY: got %q, want %q", got["KEY"], "quoted value")
	}
}

func TestParseFileSingleQuotes(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "KEY='single quoted'\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "single quoted" {
		t.Errorf("KEY: got %q, want %q", got["KEY"], "single quoted")
	}
}

func TestParseFileDoubleQuoted(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", `KEY="hello world"`)

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "hello world" {
		t.Errorf("KEY: want %q, got %q", "hello world", got["KEY"])
	}
}

func TestParseFileSkipsComments(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "# comment\nKEY=value\n# another\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 key, got %d: %v", len(got), got)
	}
	if got["KEY"] != "value" {
		t.Errorf("KEY: got %q, want %q", got["KEY"], "value")
	}
}

func TestParseFileSkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "\nKEY=value\n\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 key, got %d", len(got))
	}
}

func TestParseFileEmptyValue(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "EMPTY=\nALSO_EMPTY=\"\"\n")

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

func TestParseFileMissingEquals(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "KEYNOEQUALS\n")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for missing '=', got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Errorf("expected *ParseError, got %T: %v", err, err)
	}
	if pe != nil && pe.Line != 1 {
		t.Errorf("expected line 1, got %d", pe.Line)
	}
}

func TestParseFileExportMissingEquals(t *testing.T) {
	dir := t.TempDir()
	// "export KEY" with no "=" should still be a parse error.
	path := writeEnvFile(t, dir, ".env", "export KEYNOEQUALS\n")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for missing '=' after export prefix, got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Errorf("expected *ParseError, got %T: %v", err, err)
	}
}

// TestParseFileEmptyKey verifies that a line like "=value" (empty key) is
// rejected with a *ParseError. An empty environment variable name is invalid
// and should not be silently inserted into the result map as {"": "value"}.
func TestParseFileEmptyKey(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "=value\n")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for empty key '=value', got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Errorf("expected *ParseError, got %T: %v", err, err)
	}
	if pe != nil && pe.Line != 1 {
		t.Errorf("expected line 1, got %d", pe.Line)
	}
}

// TestParseFileEmptyKeyWithGoodLines verifies that the empty-key error is
// returned even when valid lines precede the bad line, and that the reported
// line number is correct.
func TestParseFileEmptyKeyWithGoodLines(t *testing.T) {
	dir := t.TempDir()
	// Line 1: valid, line 2: blank (skipped), line 3: empty key.
	path := writeEnvFile(t, dir, ".env", "GOOD=ok\n\n=bad\n")

	_, err := ParseFile(path)
	if err == nil {
		t.Fatal("expected error for empty key on line 3, got nil")
	}
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParseError, got %T: %v", err, err)
	}
	if pe.Line != 3 {
		t.Errorf("expected line 3, got %d", pe.Line)
	}
}

func TestParseFileParseErrorLineNumber(t *testing.T) {
	dir := t.TempDir()
	content := "GOOD=value\n# comment\n\nBAD_LINE\n"
	path := writeEnvFile(t, dir, ".env", content)

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

func TestParseFileNotFound(t *testing.T) {
	_, err := ParseFile("/nonexistent/path/.env")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist wrapped in error, got: %v", err)
	}
}

func TestParseFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

func TestParseFileValueWithEquals(t *testing.T) {
	// Value may contain '=' characters — only the first '=' is the separator.
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "KEY=a=b=c\n")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["KEY"] != "a=b=c" {
		t.Errorf("KEY: got %q, want %q", got["KEY"], "a=b=c")
	}
}

func TestParseFileURLValue(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, ".env", "DATABASE_URL=postgres://localhost:5432/myproject?sslmode=disable")

	got, err := ParseFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"DATABASE_URL": "postgres://localhost:5432/myproject?sslmode=disable"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestParseFileInlineComments verifies that inline '#' comments are stripped
// from unquoted values, while '#' inside quoted values or without preceding
// whitespace is preserved as a literal.
func TestParseFileInlineComments(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
		want    string
	}{
		{
			name:    "unquoted with space-hash comment",
			content: "API_KEY=sk-abc123 # production key\n",
			key:     "API_KEY",
			want:    "sk-abc123",
		},
		{
			name:    "unquoted with tab-hash comment",
			content: "API_KEY=sk-abc123\t# production key\n",
			key:     "API_KEY",
			want:    "sk-abc123",
		},
		{
			name:    "double-quoted preserves hash",
			content: "KEY=\"value # not a comment\"\n",
			key:     "KEY",
			want:    "value # not a comment",
		},
		{
			name:    "single-quoted preserves hash",
			content: "KEY='value # not a comment'\n",
			key:     "KEY",
			want:    "value # not a comment",
		},
		{
			name:    "no space before hash is literal",
			content: "KEY=value#nocomment\n",
			key:     "KEY",
			want:    "value#nocomment",
		},
		{
			name:    "hash at start of value (no preceding ws) is literal",
			content: "KEY=#literal\n",
			key:     "KEY",
			want:    "#literal",
		},
		{
			name:    "trailing whitespace before comment is trimmed",
			content: "KEY=value   # comment\n",
			key:     "KEY",
			want:    "value",
		},
		{
			name:    "url with fragment-like #anchor is literal",
			content: "URL=https://example.com/page#section\n",
			key:     "URL",
			want:    "https://example.com/page#section",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeEnvFile(t, dir, ".env", tt.content)
			got, err := ParseFile(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got[tt.key] != tt.want {
				t.Errorf("%s: got %q, want %q", tt.key, got[tt.key], tt.want)
			}
		})
	}
}

func TestParseErrorFormat(t *testing.T) {
	e := &ParseError{Path: "/some/.env", Line: 3, Msg: "missing '=' in \"BAD\""}
	got := e.Error()
	want := `env file "/some/.env" line 3: missing '=' in "BAD"`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
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

// TestLoadEnvExportRoundTrip verifies that "export KEY=value" is correctly
// loaded by LoadEnv (round-trip test for the export prefix bug).
func TestLoadEnvExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "export API_KEY=secret123\n")

	got, err := LoadEnv(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["API_KEY"] != "secret123" {
		t.Errorf("API_KEY: got %q, want %q", got["API_KEY"], "secret123")
	}
	if _, ok := got["export API_KEY"]; ok {
		t.Error("LoadEnv kept 'export API_KEY' as raw key — export prefix not stripped")
	}
}

// TestLoadEnvExportQuotedRoundTrip verifies export + quoted value round-trip.
func TestLoadEnvExportQuotedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), `export TOKEN="my secret token"`+"\n")

	got, err := LoadEnv(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["TOKEN"] != "my secret token" {
		t.Errorf("TOKEN: got %q, want %q", got["TOKEN"], "my secret token")
	}
}

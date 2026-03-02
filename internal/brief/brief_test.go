package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInjectFileNotFound(t *testing.T) {
	result, err := Inject("/nonexistent/path/brief.md", 200)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got: %q", result)
	}
}

func TestInjectEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.md")
	os.WriteFile(path, []byte(""), 0o644)

	result, err := Inject(path, 200)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got: %q", result)
	}
}

func TestInjectWithinLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.md")
	content := "line 1\nline 2\nline 3"
	os.WriteFile(path, []byte(content), 0o644)

	result, err := Inject(path, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(result, "<brief>\n") {
		t.Fatal("expected result to start with <brief> tag")
	}
	if !strings.HasSuffix(result, "\n</brief>") {
		t.Fatal("expected result to end with </brief> tag")
	}
	if !strings.Contains(result, "line 1\nline 2\nline 3") {
		t.Fatal("expected full content in result")
	}
	if strings.Contains(result, "TRUNCATED") {
		t.Fatal("should not contain truncation notice")
	}
}

func TestInjectExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.md")

	// Create 10 lines, limit to 5.
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "line content")
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)

	result, err := Inject(path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "TRUNCATED") {
		t.Fatal("expected truncation notice")
	}
	if !strings.Contains(result, "exceeded 5 lines") {
		t.Fatal("expected truncation notice to mention line limit")
	}
	if !strings.Contains(result, path) {
		t.Fatal("expected truncation notice to mention file path")
	}
	// Count actual content lines (between <brief> and truncation notice).
	inner := strings.TrimPrefix(result, "<brief>\n")
	inner = strings.Split(inner, "\n---\n")[0]
	contentLines := strings.Split(inner, "\n")
	if len(contentLines) != 5 {
		t.Fatalf("expected 5 content lines, got %d", len(contentLines))
	}
}

func TestInjectExactLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.md")

	// Create exactly 5 lines, limit to 5.
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, "exact line")
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)

	result, err := Inject(path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result, "TRUNCATED") {
		t.Fatal("should not be truncated at exact limit")
	}
	if !strings.HasPrefix(result, "<brief>\n") {
		t.Fatal("expected <brief> frame")
	}
}

func TestInjectTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")

	// Write exactly maxLines lines WITH trailing newline.
	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i+1))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Inject(path, 5)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(result, "TRUNCATED") {
		t.Error("Inject should not truncate a file with exactly maxLines lines + trailing newline")
	}
}

func TestInjectExceedsLimitWithTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brief.md")

	// Create 10 lines with trailing newline, limit to 5.
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "line content")
	}
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	result, err := Inject(path, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "TRUNCATED") {
		t.Fatal("expected truncation notice")
	}
}

func TestWriteSessionStart(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")

	err := WriteSessionStart(briefDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(briefDir, ".session_start"))
	if err != nil {
		t.Fatalf("failed to read session_start: %v", err)
	}

	ts := strings.TrimSpace(string(data))
	_, err = time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("session_start is not valid RFC3339: %q, error: %v", ts, err)
	}
}

func TestWriteSessionStartOverwrite(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")

	// Write twice.
	WriteSessionStart(briefDir)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps.
	WriteSessionStart(briefDir)

	data, err := os.ReadFile(filepath.Join(briefDir, ".session_start"))
	if err != nil {
		t.Fatalf("failed to read session_start: %v", err)
	}

	ts := strings.TrimSpace(string(data))
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("session_start not valid RFC3339: %q", ts)
	}

	// Should be recent (within last second).
	if time.Since(parsed) > 2*time.Second {
		t.Fatal("session_start timestamp is not recent — overwrite may have failed")
	}
}

func TestCheckSaveUpdated(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")

	// Write session start.
	WriteSessionStart(briefDir)
	time.Sleep(50 * time.Millisecond)

	// Write brief AFTER session start.
	os.WriteFile(briefPath, []byte("updated content"), 0o644)

	saved, err := CheckSave(briefPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !saved {
		t.Fatal("expected true — brief was modified after session start")
	}
}

func TestCheckSaveNotUpdated(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")

	// Write session start first.
	WriteSessionStart(briefDir)

	// Write brief but set its mtime to well before session start.
	os.WriteFile(briefPath, []byte("old content"), 0o644)
	past := time.Now().Add(-10 * time.Second)
	os.Chtimes(briefPath, past, past)

	saved, err := CheckSave(briefPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved {
		t.Fatal("expected false — brief was NOT modified after session start")
	}
}

func TestCheckSaveNoSessionStart(t *testing.T) {
	dir := t.TempDir()
	briefPath := filepath.Join(dir, "memory.md")
	os.WriteFile(briefPath, []byte("content"), 0o644)

	saved, err := CheckSave(briefPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !saved {
		t.Fatal("expected true — no session start means we can't enforce")
	}
}

func TestCheckSaveNoBrief(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	os.MkdirAll(briefDir, 0o755)
	briefPath := filepath.Join(briefDir, "memory.md")

	WriteSessionStart(briefDir)

	saved, err := CheckSave(briefPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saved {
		t.Fatal("expected false — brief file doesn't exist")
	}
}

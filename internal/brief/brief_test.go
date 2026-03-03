package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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


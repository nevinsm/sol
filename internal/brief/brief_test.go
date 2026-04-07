package brief

import (
	"bufio"
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

// TestInjectHugeBriefStillTruncates verifies that a multi-megabyte brief no
// longer wedges the SessionStart hook (CF-M7 regression). Inject must stream
// the file and emit the LAST maxLines lines as a non-empty injection.
func TestInjectHugeBriefStillTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Write ~5 MB of brief content. Each line is ~50 bytes, so ~100k lines.
	const totalLines = 100_000
	bw := bufio.NewWriter(f)
	for i := 0; i < totalLines; i++ {
		fmt.Fprintf(bw, "line %06d: padding padding padding padding padd\n", i)
	}
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 4*1024*1024 {
		t.Fatalf("test setup: brief should be >4 MB, got %d bytes", info.Size())
	}

	result, err := Inject(path, 200)
	if err != nil {
		t.Fatalf("Inject must not refuse a huge brief, got error: %v", err)
	}
	if result == "" {
		t.Fatal("Inject must return non-empty content for a huge brief (truncated)")
	}
	if !strings.HasPrefix(result, "<brief>\n") || !strings.HasSuffix(result, "\n</brief>") {
		t.Fatal("expected <brief>...</brief> framing")
	}
	if !strings.Contains(result, "TRUNCATED") {
		t.Fatal("expected truncation notice")
	}
	// Must contain the LAST line, not the first.
	lastLine := fmt.Sprintf("line %06d:", totalLines-1)
	if !strings.Contains(result, lastLine) {
		t.Errorf("expected result to contain final line %q (truncation should keep the tail)", lastLine)
	}
	// First line should be gone.
	firstLine := "line 000000:"
	// Count instances; the truncation notice may contain "line", so just
	// check that the early-numbered line isn't anywhere in the body.
	if strings.Contains(result, firstLine) {
		t.Errorf("expected first line %q to be truncated away", firstLine)
	}

	// Verify exactly 200 content lines between <brief> and the truncation notice.
	inner := strings.TrimPrefix(result, "<brief>\n")
	inner = strings.Split(inner, "\n---\n")[0]
	contentLines := strings.Split(inner, "\n")
	if len(contentLines) != 200 {
		t.Errorf("expected 200 content lines, got %d", len(contentLines))
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


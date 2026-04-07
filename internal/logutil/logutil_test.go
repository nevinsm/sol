package logutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncateIfNeeded_OverMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Create a file with 100 lines, each ~100 bytes, well over 1KB.
	var buf bytes.Buffer
	for i := 0; i < 100; i++ {
		buf.WriteString(strings.Repeat("x", 95))
		buf.WriteString("\n")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	originalSize := int64(buf.Len())
	maxBytes := originalSize / 2 // Set max to half, so truncation triggers.

	truncated, _, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	// Read back and verify it contains only tail content.
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if int64(len(result)) >= originalSize {
		t.Errorf("result size %d should be less than original %d", len(result), originalSize)
	}

	// The result should end with a newline (last line intact).
	if len(result) > 0 && result[len(result)-1] != '\n' {
		t.Error("result should end with a newline")
	}
}

func TestTruncateIfNeeded_SnapsToNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Create content with lines of varying length.
	lines := []string{
		"line-1-short",
		"line-2-medium-length-content",
		"line-3-another-medium-line",
		"line-4-this-is-a-longer-line-with-more-content",
		"line-5-final-line",
	}
	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set maxBytes small enough to trigger truncation.
	maxBytes := int64(len(content)) / 2

	truncated, _, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Verify no partial lines — every line should be complete.
	resultStr := string(result)
	if resultStr == "" {
		t.Fatal("result should not be empty")
	}

	// The result must start at a line boundary (not mid-line).
	// Check that each line in the result matches one of the original lines.
	resultLines := strings.Split(strings.TrimSuffix(resultStr, "\n"), "\n")
	for _, rl := range resultLines {
		found := false
		for _, ol := range lines {
			if rl == ol {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("result contains partial or unknown line: %q", rl)
		}
	}
}

func TestTruncateIfNeeded_UnderMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	content := "small log content\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	truncated, _, err := TruncateIfNeeded(path, 1024*1024) // 1MB max, file is tiny.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("expected no truncation for small file")
	}

	// Verify content is unmodified.
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != content {
		t.Errorf("content modified: got %q, want %q", string(result), content)
	}
}

func TestTruncateIfNeeded_MissingFile(t *testing.T) {
	truncated, _, err := TruncateIfNeeded("/nonexistent/path/file.log", 1024)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if truncated {
		t.Fatal("expected false for missing file")
	}
}

func TestTruncateIfNeeded_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	truncated, _, err := TruncateIfNeeded(path, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("expected no truncation for empty file")
	}
}

func TestTruncateIfNeeded_KeepsApproximatelyMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Create a file with many uniform lines so size assertions are predictable.
	lineContent := strings.Repeat("a", 97) + "\n" // 98 bytes per line.
	var buf bytes.Buffer
	lineCount := 1000
	for i := 0; i < lineCount; i++ {
		buf.WriteString(lineContent)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	originalSize := int64(buf.Len())
	maxBytes := originalSize / 2

	truncated, _, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// The kept portion must be ≤ maxBytes (snapping to a newline boundary
	// can only shrink it). It must also be at least maxBytes minus one
	// full line, since we snap forward by at most one line.
	resultSize := int64(len(result))
	if resultSize > maxBytes {
		t.Errorf("kept portion %d bytes exceeds maxBytes %d", resultSize, maxBytes)
	}
	lineLen := int64(len(lineContent))
	if resultSize < maxBytes-lineLen {
		t.Errorf("kept portion %d bytes is less than maxBytes-lineLen %d (lost too much data)",
			resultSize, maxBytes-lineLen)
	}
}

// TestTruncateIfNeeded_LargeOverflowConvergesInOneCall verifies that a single
// call against an input far larger than maxBytes produces output ≤ maxBytes.
// The previous implementation kept 75% of the input regardless of maxBytes,
// requiring O(log) calls to converge (CF-M4).
func TestTruncateIfNeeded_LargeOverflowConvergesInOneCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 100MB convergence test in short mode")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.log")

	// 100 MB of synthetic content, 100 bytes per line.
	const totalSize = 100 * 1024 * 1024
	const lineLen = 100
	line := strings.Repeat("a", lineLen-1) + "\n"
	var buf bytes.Buffer
	buf.Grow(totalSize)
	for buf.Len() < totalSize {
		buf.WriteString(line)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	const maxBytes int64 = 10 * 1024 * 1024 // 10 MB cap.

	truncated, _, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxBytes {
		t.Errorf("after one call, file size %d exceeds maxBytes %d (did not converge)",
			info.Size(), maxBytes)
	}
	// Sanity: shouldn't have lost everything either — must keep most of maxBytes.
	if info.Size() < maxBytes-int64(lineLen) {
		t.Errorf("after one call, file size %d is much smaller than maxBytes %d (lost too much data)",
			info.Size(), maxBytes)
	}

}

func TestTruncateIfNeeded_NoNewlines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Single long line with no newlines.
	content := strings.Repeat("x", 10000)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	truncated, _, err := TruncateIfNeeded(path, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// With no newlines, the cutoff is len(data)-maxBytes and there is no
	// newline to snap forward to, so exactly the last maxBytes bytes are
	// retained: 10000 - 1000 = 9000, keeping bytes [9000:] = 1000 bytes.
	if len(result) == 0 {
		t.Fatal("result should not be empty for single long line")
	}

	expectedSize := 1000
	if len(result) != expectedSize {
		t.Errorf("expected %d bytes, got %d", expectedSize, len(result))
	}
}

func TestTruncateIfNeeded_ExactlyAtMax(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	content := strings.Repeat("x", 1000) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// maxBytes exactly equals file size — should NOT truncate.
	truncated, _, err := TruncateIfNeeded(path, int64(len(content)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("expected no truncation when file size equals maxBytes")
	}
}

func TestTruncateIfNeeded_TailStartIsCorrect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Write 100 lines of known content.
	var buf bytes.Buffer
	for i := range 100 {
		buf.WriteString(strings.Repeat("x", 97))
		buf.WriteString("\n")
		_ = i
	}
	originalContent := buf.Bytes()
	if err := os.WriteFile(path, originalContent, 0o644); err != nil {
		t.Fatal(err)
	}

	originalSize := int64(buf.Len())
	maxBytes := originalSize / 2

	truncated, tailStart, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	// tailStart must be a valid offset in the original file.
	if tailStart <= 0 || tailStart >= originalSize {
		t.Errorf("tailStart %d out of expected range (0, %d)", tailStart, originalSize)
	}

	// The content of the new file must equal originalContent[tailStart:].
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := originalContent[tailStart:]
	if !bytes.Equal(result, expected) {
		t.Errorf("file content after truncation does not match originalContent[tailStart:]\n"+
			"got len=%d, want len=%d", len(result), len(expected))
	}
}

func TestTruncateIfNeeded_PreservesNewBytesWrittenDuringWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Write initial content that exceeds maxBytes.
	var buf bytes.Buffer
	for range 50 {
		buf.WriteString(strings.Repeat("y", 97))
		buf.WriteString("\n")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	maxBytes := int64(buf.Len()) / 2

	// Simulate the truncation window: append a line AFTER creating the file
	// but BEFORE TruncateIfNeeded would have completed the rename in real usage.
	// We do it here by appending before calling TruncateIfNeeded, which means
	// it will be captured in the post-ReadFile window simulation (our append
	// lands in the os.ReadFile call itself), but the key invariant is that
	// TruncateIfNeeded must not lose bytes present in the file at any point.
	windowLine := strings.Repeat("z", 97) + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(windowLine); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	truncated, tailStart, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}
	if tailStart <= 0 {
		t.Errorf("tailStart should be positive, got %d", tailStart)
	}

	// The window line must be present in the truncated file.
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result, []byte("zz")) {
		t.Error("truncated file is missing the line appended during the window")
	}
}

func TestTruncateIfNeeded_PreservesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Create a file with 0o755 permissions (unusual, makes it easy to detect loss).
	var buf bytes.Buffer
	for range 100 {
		buf.WriteString(strings.Repeat("p", 97))
		buf.WriteString("\n")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}

	maxBytes := int64(buf.Len()) / 2

	truncated, _, err := TruncateIfNeeded(path, maxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncation to occur")
	}

	// Verify the file permissions are preserved after truncation.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Errorf("permissions after truncation = %04o, want %04o", got, 0o755)
	}
}

func TestDefaultMaxLogSize(t *testing.T) {
	expected := int64(10 * 1024 * 1024)
	if DefaultMaxLogSize != expected {
		t.Errorf("DefaultMaxLogSize = %d, want %d", DefaultMaxLogSize, expected)
	}
}

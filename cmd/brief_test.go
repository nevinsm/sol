package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBriefInjectWithContent(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("some context\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point SOL_HOME to temp dir so EnsureDirs succeeds.
	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)

	// Capture stdout.
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"brief", "inject", "--path", briefPath})

	// Redirect os.Stdout to capture fmt.Println output.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "<brief>") {
		t.Errorf("expected <brief> tag in output, got: %s", output)
	}
	if !strings.Contains(output, "some context") {
		t.Errorf("expected brief content in output, got: %s", output)
	}
	if !strings.Contains(output, "</brief>") {
		t.Errorf("expected </brief> tag in output, got: %s", output)
	}

	// Check .session_start was created.
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	data, err := os.ReadFile(sessionStartPath)
	if err != nil {
		t.Fatalf("expected .session_start to exist: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err != nil {
		t.Errorf("expected RFC3339 timestamp in .session_start, got: %s", string(data))
	}
}

func TestBriefInjectMissingFile(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	briefPath := filepath.Join(briefDir, "memory.md")

	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)

	rootCmd.SetArgs([]string{"brief", "inject", "--path", briefPath})

	// Capture stdout.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("expected no error for missing brief, got: %v", err)
	}

	output := captured.String()
	if strings.Contains(output, "<brief>") {
		t.Errorf("expected no brief output for missing file, got: %s", output)
	}

	// .session_start should still be written.
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	if _, err := os.Stat(sessionStartPath); err != nil {
		t.Fatalf("expected .session_start to exist even with missing brief: %v", err)
	}
}

func TestBriefInjectTruncation(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a 300-line brief.
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "line content")
	}
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)

	rootCmd.SetArgs([]string{"brief", "inject", "--path", briefPath, "--max-lines", "200"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "TRUNCATED") {
		t.Errorf("expected truncation notice in output, got: %s", output)
	}
}

func TestBriefCheckSaveUpdated(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write .session_start in the past.
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(sessionStartPath, []byte(past), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write brief file (will have mtime of now, which is after session_start).
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("updated content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)

	rootCmd.SetArgs([]string{"brief", "check-save", briefPath})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected exit 0 for updated brief, got: %v", err)
	}
}

func TestBriefCheckSaveNotUpdated(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write brief file first.
	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write .session_start after brief (future timestamp ensures brief is "not updated").
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	future := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(sessionStartPath, []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}

	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)

	// Capture stderr to verify nudge message.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	rootCmd.SetArgs([]string{"brief", "check-save", briefPath})
	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stderr = oldStderr

	if ExitCode(err) != 2 {
		t.Fatalf("expected exit code 2, got error: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "brief has not been updated") {
		t.Errorf("expected nudge message in stderr, got: %s", output)
	}
}

func TestBriefCheckSaveStopHookActive(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write .session_start in the future (would normally cause exit 2).
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	future := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(sessionStartPath, []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}

	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("SOL_STOP_HOOK_ACTIVE", "true")

	rootCmd.SetArgs([]string{"brief", "check-save", briefPath})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("expected exit 0 with SOL_STOP_HOOK_ACTIVE=true, got: %v", err)
	}
}

func TestBriefInjectSkipSessionStart(t *testing.T) {
	dir := t.TempDir()
	briefDir := filepath.Join(dir, ".brief")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}

	briefPath := filepath.Join(briefDir, "memory.md")
	if err := os.WriteFile(briefPath, []byte("some context\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a known .session_start timestamp.
	sessionStartPath := filepath.Join(briefDir, ".session_start")
	original := "2026-01-01T00:00:00Z"
	if err := os.WriteFile(sessionStartPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	solHome := filepath.Join(dir, "sol-home")
	t.Setenv("SOL_HOME", solHome)

	// Redirect stdout to discard.
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"brief", "inject", "--path", briefPath, "--skip-session-start"})
	err := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify .session_start was NOT overwritten.
	data, err := os.ReadFile(sessionStartPath)
	if err != nil {
		t.Fatalf("expected .session_start to still exist: %v", err)
	}
	if strings.TrimSpace(string(data)) != original {
		t.Errorf("expected .session_start to be %q, got %q", original, strings.TrimSpace(string(data)))
	}
}

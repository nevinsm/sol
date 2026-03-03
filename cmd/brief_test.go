package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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


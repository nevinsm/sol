package cmd

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/governor"
)

func TestWorldQueryGovernorNotRunning(t *testing.T) {
	initTestWorld(t, "myworld")

	rootCmd.SetArgs([]string{"world", "query", "myworld", "What is the architecture?"})

	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stderr = oldStderr

	if err == nil {
		t.Fatal("expected error when governor not running")
	}
	if !strings.Contains(err.Error(), "governor not running") {
		t.Errorf("error = %q, want contains \"governor not running\"", err.Error())
	}
}

func TestWorldQueryMissingWorld(t *testing.T) {
	initTestWorld(t, "myworld")

	rootCmd.SetArgs([]string{"world", "query", "nonexistent", "question"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent world")
	}
}

func TestWorldSummaryCommand(t *testing.T) {
	initTestWorld(t, "myworld")

	// Create world-summary.md file.
	briefDir := governor.BriefDir("myworld")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summaryPath := governor.WorldSummaryPath("myworld")
	summaryContent := "# World Summary: myworld\n## Project\nA test project.\n"
	if err := os.WriteFile(summaryPath, []byte(summaryContent), 0o644); err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"world", "summary", "myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("world summary failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "World Summary: myworld") {
		t.Errorf("expected summary content in output, got: %s", output)
	}
	if !strings.Contains(output, "A test project") {
		t.Errorf("expected project description in output, got: %s", output)
	}
}

func TestWorldSummaryCommandNotFound(t *testing.T) {
	initTestWorld(t, "myworld")

	rootCmd.SetArgs([]string{"world", "summary", "myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	// Expect exit code 1 (not found) per convention.
	if err == nil {
		t.Fatal("world summary should return exit error 1 for missing summary")
	}
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.code != 1 {
		t.Fatalf("expected exitError{code:1}, got: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "No world summary found") {
		t.Errorf("expected 'No world summary found' message, got: %s", output)
	}
}

func TestWorldSummaryMissingWorld(t *testing.T) {
	initTestWorld(t, "myworld")

	rootCmd.SetArgs([]string{"world", "summary", "nonexistent"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent world")
	}
}

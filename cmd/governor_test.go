package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/governor"
	"github.com/nevinsm/sol/internal/store"
)

func TestGovernorStartCommand(t *testing.T) {
	solHome := initTestWorld(t, "myworld")

	// Create a source repo.
	sourceRepo := filepath.Join(t.TempDir(), "repo")
	initGitRepo(t, sourceRepo)

	// Reset flags.
	governorStartWorld = "myworld"
	governorStartSourceRepo = sourceRepo
	defer func() {
		governorStartWorld = ""
		governorStartSourceRepo = ""
	}()

	rootCmd.SetArgs([]string{"governor", "start", "--world=myworld", "--source-repo=" + sourceRepo})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor start failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "Started governor") {
		t.Errorf("expected 'Started governor' in output, got: %s", output)
	}

	// Verify governor directory created.
	govDir := governor.GovernorDir("myworld")
	if _, err := os.Stat(govDir); os.IsNotExist(err) {
		t.Error("governor directory not created")
	}

	// Verify CLAUDE.md installed (by CLI, not placeholder).
	claudeMDPath := filepath.Join(govDir, ".claude", "CLAUDE.md")
	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("CLAUDE.md not written: %v", err)
	}
	if strings.Contains(string(data), "Placeholder") {
		t.Error("CLAUDE.md should not be placeholder — protocol generator should have been used")
	}
	if !strings.Contains(string(data), "work coordinator") {
		t.Error("CLAUDE.md should contain governor identity from protocol generator")
	}

	// Verify mirror was cloned.
	mirrorPath := governor.MirrorPath("myworld")
	if _, err := os.Stat(mirrorPath); os.IsNotExist(err) {
		t.Error("mirror not cloned")
	}

	// Verify agent record in sphere store.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()

	agent, err := ss.GetAgent("myworld/governor")
	if err != nil {
		t.Fatalf("agent not found in sphere store: %v", err)
	}
	if agent.Role != "governor" {
		t.Errorf("agent role = %q, want \"governor\"", agent.Role)
	}

	_ = solHome
}

func TestGovernorStopCommand(t *testing.T) {
	solHome := initTestWorld(t, "myworld")

	// Create agent in sphere store.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	ss.EnsureAgent("governor", "myworld", "governor")
	ss.Close()

	governorStopWorld = "myworld"
	defer func() { governorStopWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "stop", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor stop failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "Stopped governor") {
		t.Errorf("expected 'Stopped governor' in output, got: %s", output)
	}

	_ = solHome
}

func TestGovernorBriefCommand(t *testing.T) {
	initTestWorld(t, "myworld")

	// Create brief file.
	briefDir := governor.BriefDir("myworld")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	briefPath := governor.BriefPath("myworld")
	if err := os.WriteFile(briefPath, []byte("# Governor Brief\nWorld knowledge here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	governorBriefWorld = "myworld"
	defer func() { governorBriefWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "brief", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor brief failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "Governor Brief") {
		t.Errorf("expected brief content in output, got: %s", output)
	}
	if !strings.Contains(output, "World knowledge here") {
		t.Errorf("expected brief content in output, got: %s", output)
	}
}

func TestGovernorBriefCommandNotFound(t *testing.T) {
	initTestWorld(t, "myworld")

	governorBriefWorld = "myworld"
	defer func() { governorBriefWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "brief", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor brief should not error for missing brief: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "No brief found for governor") {
		t.Errorf("expected 'No brief found for governor' message, got: %s", output)
	}
}

func TestGovernorDebriefCommand(t *testing.T) {
	initTestWorld(t, "myworld")

	// Create brief file.
	briefDir := governor.BriefDir("myworld")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	briefPath := governor.BriefPath("myworld")
	if err := os.WriteFile(briefPath, []byte("# Governor Brief\nImportant world context.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	governorDebriefWorld = "myworld"
	defer func() { governorDebriefWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "debrief", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor debrief failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "Archived brief to .brief/archive/") {
		t.Errorf("expected archive message in output, got: %s", output)
	}
	if !strings.Contains(output, "ready for fresh engagement") {
		t.Errorf("expected 'ready for fresh engagement' message, got: %s", output)
	}

	// Verify original brief file removed.
	if _, err := os.Stat(briefPath); !os.IsNotExist(err) {
		t.Error("original brief file should have been removed")
	}

	// Verify archive file exists.
	archiveDir := filepath.Join(briefDir, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("failed to read archive directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 archive file, got %d", len(entries))
	}

	// Verify archive file has .md extension and no colons.
	archiveFile := entries[0].Name()
	if !strings.HasSuffix(archiveFile, ".md") {
		t.Errorf("archive file should have .md extension, got: %s", archiveFile)
	}
	if strings.Contains(archiveFile, ":") {
		t.Errorf("archive filename should not contain colons, got: %s", archiveFile)
	}

	// Verify archive content.
	archiveData, err := os.ReadFile(filepath.Join(archiveDir, archiveFile))
	if err != nil {
		t.Fatalf("failed to read archive file: %v", err)
	}
	if !strings.Contains(string(archiveData), "Important world context") {
		t.Error("archive file should contain original brief content")
	}
}

func TestGovernorSummaryCommand(t *testing.T) {
	initTestWorld(t, "myworld")

	// Create world-summary.md file.
	briefDir := governor.BriefDir("myworld")
	if err := os.MkdirAll(briefDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summaryPath := governor.WorldSummaryPath("myworld")
	summaryContent := "# World Summary: myworld\n## Project\nA test project.\n## Architecture\nGo monorepo.\n"
	if err := os.WriteFile(summaryPath, []byte(summaryContent), 0o644); err != nil {
		t.Fatal(err)
	}

	governorSummaryWorld = "myworld"
	defer func() { governorSummaryWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "summary", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor summary failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "World Summary: myworld") {
		t.Errorf("expected summary content in output, got: %s", output)
	}
	if !strings.Contains(output, "A test project") {
		t.Errorf("expected project description in output, got: %s", output)
	}
}

func TestGovernorSummaryCommandNotFound(t *testing.T) {
	initTestWorld(t, "myworld")

	governorSummaryWorld = "myworld"
	defer func() { governorSummaryWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "summary", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor summary should not error for missing summary: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "No world summary found") {
		t.Errorf("expected 'No world summary found' message, got: %s", output)
	}
}

func TestGovernorRefreshMirrorCommand(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Create world directory and world.toml.
	worldDir := filepath.Join(solHome, "myworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldToml := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldToml, []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a source repo and clone it as a mirror.
	sourceRepo := filepath.Join(t.TempDir(), "repo")
	initGitRepo(t, sourceRepo)

	mirrorPath := governor.MirrorPath("myworld")
	cmd := exec.Command("git", "clone", sourceRepo, mirrorPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone failed: %s: %v", out, err)
	}

	// Add a new commit to the source repo.
	newFile := filepath.Join(sourceRepo, "refresh-test.txt")
	if err := os.WriteFile(newFile, []byte("refresh content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}
	cmd = exec.Command("git", "-C", sourceRepo, "commit", "-m", "refresh test commit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}

	governorRefreshMirrorWorld = "myworld"
	defer func() { governorRefreshMirrorWorld = "" }()

	rootCmd.SetArgs([]string{"governor", "refresh-mirror", "--world=myworld"})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("governor refresh-mirror failed: %v", err)
	}

	output := captured.String()
	if !strings.Contains(output, "Refreshed mirror") {
		t.Errorf("expected 'Refreshed mirror' in output, got: %s", output)
	}

	// Verify new commit is in the mirror.
	cmd = exec.Command("git", "-C", mirrorPath, "log", "--oneline")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %s: %v", out, err)
	}
	if !strings.Contains(string(out), "refresh test commit") {
		t.Errorf("mirror should contain new commit, got: %s", out)
	}
}

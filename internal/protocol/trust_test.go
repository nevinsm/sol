package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTrustDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(t.TempDir(), "sol", "myworld", "outposts", "Agent1", "worktree")

	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("TrustDirectory failed: %v", err)
	}

	// Read back and verify.
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}

	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid projects key")
	}

	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for %q", dir)
	}

	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestTrustDirectoryIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := "/some/worktree"

	// Trust twice.
	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("first TrustDirectory failed: %v", err)
	}
	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("second TrustDirectory failed: %v", err)
	}

	// Should still have one entry.
	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var state map[string]any
	json.Unmarshal(data, &state)
	projects := state["projects"].(map[string]any)

	if len(projects) != 1 {
		t.Errorf("expected 1 project entry, got %d", len(projects))
	}
}

func TestTrustDirectoryPreservesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a pre-existing .claude.json with other data.
	existing := map[string]any{
		"numStartups": float64(42),
		"projects": map[string]any{
			"/other/project": map[string]any{
				"hasTrustDialogAccepted": true,
				"lastCost":               1.5,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o600)

	// Trust a new directory.
	if err := TrustDirectory("/new/worktree"); err != nil {
		t.Fatalf("TrustDirectory failed: %v", err)
	}

	// Verify existing data preserved.
	data, _ = os.ReadFile(filepath.Join(home, ".claude.json"))
	var state map[string]any
	json.Unmarshal(data, &state)

	if state["numStartups"] != float64(42) {
		t.Error("numStartups was clobbered")
	}

	projects := state["projects"].(map[string]any)
	if len(projects) != 2 {
		t.Errorf("expected 2 project entries, got %d", len(projects))
	}

	other := projects["/other/project"].(map[string]any)
	if other["lastCost"] != 1.5 {
		t.Error("existing project data was clobbered")
	}
}

func TestTrustDirectoryConcurrent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dir := fmt.Sprintf("/worktree/%d", i)
			errs[i] = TrustDirectory(dir)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	// Verify all entries are present.
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}
	projects := state["projects"].(map[string]any)
	if len(projects) != n {
		t.Errorf("expected %d project entries, got %d", n, len(projects))
	}
	for i := 0; i < n; i++ {
		dir := fmt.Sprintf("/worktree/%d", i)
		entry, ok := projects[dir].(map[string]any)
		if !ok {
			t.Errorf("missing entry for %s", dir)
			continue
		}
		if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
			t.Errorf("entry %s not trusted", dir)
		}
	}
}

func TestTrustDirectoryAtomicWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := TrustDirectory("/test/atomic"); err != nil {
		t.Fatalf("TrustDirectory failed: %v", err)
	}

	// No .tmp file should linger.
	tmpPath := filepath.Join(home, ".claude.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf(".tmp file should not exist, got err=%v", err)
	}

	// Result must be valid JSON.
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf(".claude.json is not valid JSON: %v", err)
	}
}

func TestTrustDirectoryIn(t *testing.T) {
	configDir := t.TempDir()

	dir := "/test/worktree/agent1"

	if err := TrustDirectoryIn(dir, configDir); err != nil {
		t.Fatalf("TrustDirectoryIn failed: %v", err)
	}

	// Read back and verify.
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read config dir .claude.json: %v", err)
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}

	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid projects key")
	}

	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for %q", dir)
	}

	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

func TestTrustDirectoryInIdempotent(t *testing.T) {
	configDir := t.TempDir()

	dir := "/test/worktree"

	if err := TrustDirectoryIn(dir, configDir); err != nil {
		t.Fatalf("first TrustDirectoryIn failed: %v", err)
	}
	if err := TrustDirectoryIn(dir, configDir); err != nil {
		t.Fatalf("second TrustDirectoryIn failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	var state map[string]any
	json.Unmarshal(data, &state)
	projects := state["projects"].(map[string]any)

	if len(projects) != 1 {
		t.Errorf("expected 1 project entry, got %d", len(projects))
	}
}

func TestTrustDirectoryInPreservesExisting(t *testing.T) {
	configDir := t.TempDir()

	// Write pre-existing state to config dir .claude.json.
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"projects": map[string]any{
			"/other/project": map[string]any{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(configDir, ".claude.json"), data, 0o600)

	// Trust a new directory in the config dir.
	if err := TrustDirectoryIn("/new/worktree", configDir); err != nil {
		t.Fatalf("TrustDirectoryIn failed: %v", err)
	}

	// Verify existing data preserved.
	data, _ = os.ReadFile(filepath.Join(configDir, ".claude.json"))
	var state map[string]any
	json.Unmarshal(data, &state)

	if v, ok := state["hasCompletedOnboarding"].(bool); !ok || !v {
		t.Error("hasCompletedOnboarding was clobbered")
	}

	projects := state["projects"].(map[string]any)
	if len(projects) != 2 {
		t.Errorf("expected 2 project entries, got %d", len(projects))
	}

	if _, ok := projects["/other/project"].(map[string]any); !ok {
		t.Error("existing project entry was lost")
	}
}

func TestTrustDirectoryInDoesNotAffectGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configDir := t.TempDir()

	// Trust in config dir only.
	if err := TrustDirectoryIn("/agent/worktree", configDir); err != nil {
		t.Fatalf("TrustDirectoryIn failed: %v", err)
	}

	// Global ~/.claude.json should NOT exist (was not written to).
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); !os.IsNotExist(err) {
		t.Error("TrustDirectoryIn should not write to ~/.claude.json")
	}

	// Config dir .claude.json should exist with the trust entry.
	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("config dir .claude.json not created: %v", err)
	}

	var state map[string]any
	json.Unmarshal(data, &state)
	projects := state["projects"].(map[string]any)
	if _, ok := projects["/agent/worktree"]; !ok {
		t.Error("trust entry missing from config dir .claude.json")
	}
}

func TestTrustDirectoryConcurrentPreservesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed with pre-existing data.
	existing := map[string]any{
		"numStartups": float64(99),
		"projects": map[string]any{
			"/pre/existing": map[string]any{
				"hasTrustDialogAccepted": true,
				"customField":            "preserve-me",
			},
		},
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o600)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dir := fmt.Sprintf("/concurrent/%d", i)
			errs[i] = TrustDirectory(dir)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	// Verify pre-existing data survived.
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}

	if state["numStartups"] != float64(99) {
		t.Error("numStartups was clobbered")
	}

	projects := state["projects"].(map[string]any)
	// n concurrent + 1 pre-existing
	if len(projects) != n+1 {
		t.Errorf("expected %d project entries, got %d", n+1, len(projects))
	}

	pre, ok := projects["/pre/existing"].(map[string]any)
	if !ok {
		t.Fatal("pre-existing entry missing")
	}
	if pre["customField"] != "preserve-me" {
		t.Error("pre-existing customField was clobbered")
	}
}

// TestTrustDirectoryUnexpectedEntryTypeLogsAndRecovers verifies that a
// malformed projects[dir] entry (string instead of object) is logged via
// softfail and overwritten with a sane default, rather than silently
// leaving the session without a trusted project. (CF-L3 / pattern P1.)
func TestTrustDirectoryUnexpectedEntryTypeLogsAndRecovers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := "/weird/worktree"
	claudeJSON := filepath.Join(home, ".claude.json")

	// Seed .claude.json with a projects entry whose value is a string
	// (not a map) — the shape the old code silently ignored.
	seed := map[string]any{
		"projects": map[string]any{
			dir: "definitely not an object",
		},
	}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Capture slog output.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := TrustDirectory(dir); err != nil {
		t.Fatalf("TrustDirectory failed: %v", err)
	}

	// Verify the entry was replaced with a sane default.
	readBack, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("read .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(readBack, &state); err != nil {
		t.Fatalf("parse .claude.json: %v", err)
	}
	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("projects not a map")
	}
	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("entry was not rewritten into an object, got %T", projects[dir])
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("hasTrustDialogAccepted should be true after recovery")
	}

	out := buf.String()
	if !strings.Contains(out, "soft failure") {
		t.Errorf("expected soft-failure log, got: %s", out)
	}
	if !strings.Contains(out, "trust.Update") {
		t.Errorf("expected op identifier in log, got: %s", out)
	}
}

// runGit runs a git command in dir, failing the test on error. It passes
// -c user.* config on the command line so it works without relying on any
// ambient git identity configuration.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// TestTrustDirectoryInWorktreeTrustsMainCheckoutRoot verifies that, per
// Claude Code v2.1.211+ trust keying, TrustDirectoryIn on a linked git
// worktree trusts both the worktree path and the main checkout's root.
func TestTrustDirectoryInWorktreeTrustsMainCheckoutRoot(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "commit", "--allow-empty", "-q", "-m", "init")

	worktree := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-q", worktree)

	configDir := t.TempDir()
	if err := TrustDirectoryIn(worktree, configDir); err != nil {
		t.Fatalf("TrustDirectoryIn failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read config dir .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}
	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid projects key")
	}

	wantWorktree, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		t.Fatalf("EvalSymlinks(worktree) failed: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks(repo) failed: %v", err)
	}

	if len(projects) != 2 {
		t.Errorf("expected 2 project entries (worktree + main checkout root), got %d: %v", len(projects), projects)
	}

	worktreeEntry, ok := projects[wantWorktree].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for worktree %q; projects=%v", wantWorktree, projects)
	}
	if trusted, _ := worktreeEntry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("worktree entry should be trusted")
	}

	rootEntry, ok := projects[wantRoot].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for main checkout root %q; projects=%v", wantRoot, projects)
	}
	if trusted, _ := rootEntry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("main checkout root entry should be trusted")
	}
}

// TestTrustDirectoryInMainCheckoutSingleEntry verifies that TrustDirectoryIn
// on an ordinary (non-worktree) git checkout trusts only that one directory
// — there is no separate "main checkout root" to add.
func TestTrustDirectoryInMainCheckoutSingleEntry(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "commit", "--allow-empty", "-q", "-m", "init")

	configDir := t.TempDir()
	if err := TrustDirectoryIn(repo, configDir); err != nil {
		t.Fatalf("TrustDirectoryIn failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read config dir .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}
	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid projects key")
	}
	if len(projects) != 1 {
		t.Errorf("expected exactly 1 project entry for a non-worktree repo, got %d: %v", len(projects), projects)
	}

	wantRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks(repo) failed: %v", err)
	}
	entry, ok := projects[wantRepo].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for %q; projects=%v", wantRepo, projects)
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

// TestTrustDirectoryInNonGitDirSingleEntry verifies that TrustDirectoryIn on
// a directory that isn't a git repository at all falls back to trusting
// just that directory, without error.
func TestTrustDirectoryInNonGitDirSingleEntry(t *testing.T) {
	dir := t.TempDir()
	configDir := t.TempDir()

	if err := TrustDirectoryIn(dir, configDir); err != nil {
		t.Fatalf("TrustDirectoryIn failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read config dir .claude.json: %v", err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}
	projects, ok := state["projects"].(map[string]any)
	if !ok {
		t.Fatal("missing or invalid projects key")
	}
	if len(projects) != 1 {
		t.Errorf("expected exactly 1 project entry for a non-git dir, got %d: %v", len(projects), projects)
	}

	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("missing project entry for %q; projects=%v", dir, projects)
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

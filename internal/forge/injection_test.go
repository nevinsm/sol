package forge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestBuildInjection(t *testing.T) {
	mr := &store.MergeRequest{
		ID:       "mr-abc123",
		WritID:   "sol-def456",
		Branch:   "outpost/Toast/sol-def456",
		Phase:    "claimed",
		Attempts: 1,
	}
	writ := &store.Writ{
		ID:          "sol-def456",
		Title:       "feat: add widget support",
		Description: "Add support for widgets in the dashboard.",
	}
	cfg := InjectionConfig{
		MaxAttempts:  3,
		GateCommands: []string{"make build", "make test"},
		WorktreeDir:  "/home/user/sol/myworld/forge/worktree",
		TargetBranch: "main",
		World:        "myworld",
	}

	t.Run("first attempt produces complete injection", func(t *testing.T) {
		result := BuildInjection(mr, writ, cfg)

		// Check MR metadata section.
		mustContain(t, result, "MR: mr-abc123")
		mustContain(t, result, "Branch: origin/outpost/Toast/sol-def456")
		mustContain(t, result, "feat: add widget support (sol-def456)")
		mustContain(t, result, "Attempt: 1 of 3")
		mustContain(t, result, "Target: origin/main")

		// Check writ context — should instruct agent to fetch via CLI, not embed description.
		mustContain(t, result, "### Writ Context")
		mustContain(t, result, "sol writ status sol-def456 --world=myworld")
		mustNotContain(t, result, "Add support for widgets in the dashboard.")

		// Check first attempt notice.
		mustContain(t, result, "First attempt.")

		// Check gate commands.
		mustContain(t, result, "`make build`")
		mustContain(t, result, "`make test`")

		// Check instructions.
		mustContain(t, result, "git fetch origin && git reset --hard origin/main")
		mustContain(t, result, "git merge --squash origin/outpost/Toast/sol-def456")
		mustContain(t, result, `git commit --no-edit --author="Toast <outpost.toast@sol.local>" -m "feat: add widget support (sol-def456)"`)
		mustContain(t, result, "make build && make test")
		mustContain(t, result, "git push origin HEAD:main")
		mustContain(t, result, ".forge-result.json")
	})

	t.Run("includes attempt history when present", func(t *testing.T) {
		cfgWithHistory := cfg
		cfgWithHistory.AttemptHistory = []string{
			"Gate failure: test_auth.go:42 nil pointer",
			"Conflict in internal/store/writs.go",
		}

		// Simulate attempt 3.
		mr3 := *mr
		mr3.Attempts = 3

		result := BuildInjection(&mr3, writ, cfgWithHistory)

		mustContain(t, result, "Attempt 1: Gate failure: test_auth.go:42 nil pointer")
		mustContain(t, result, "Attempt 2: Conflict in internal/store/writs.go")
		mustNotContain(t, result, "First attempt.")
	})

	t.Run("handles empty description — still uses CLI fetch", func(t *testing.T) {
		writNoDesc := &store.Writ{
			ID:    "sol-111",
			Title: "fix: something",
		}
		result := BuildInjection(mr, writNoDesc, cfg)
		// Even with no description, the injection should direct the agent to fetch via CLI.
		mustContain(t, result, "sol writ status sol-111 --world=myworld")
		mustNotContain(t, result, "No description provided.")
	})

	t.Run("handles no gate commands", func(t *testing.T) {
		cfgNoGates := cfg
		cfgNoGates.GateCommands = nil

		result := BuildInjection(mr, writ, cfgNoGates)
		mustContain(t, result, "No quality gates configured.")
		mustContain(t, result, "No gates to run")
	})

	t.Run("respects custom target branch", func(t *testing.T) {
		cfgCustom := cfg
		cfgCustom.TargetBranch = "develop"

		result := BuildInjection(mr, writ, cfgCustom)
		mustContain(t, result, "Target: origin/develop")
		mustContain(t, result, "git reset --hard origin/develop")
		mustContain(t, result, "git push origin HEAD:develop")
	})

	t.Run("escapes quotes in commit message", func(t *testing.T) {
		writQuotes := &store.Writ{
			ID:    "sol-222",
			Title: `feat: handle "special" cases`,
		}
		result := BuildInjection(mr, writQuotes, cfg)
		mustContain(t, result, `git commit --no-edit --author="Toast <outpost.toast@sol.local>" -m "feat: handle \"special\" cases (sol-222)"`)
	})

	t.Run("contains empty squash handling instructions", func(t *testing.T) {
		result := BuildInjection(mr, writ, cfg)
		mustContain(t, result, "no changes")
		mustContain(t, result, "no_op")
		mustContain(t, result, "do not commit or push")
		mustContain(t, result, "acceptance criteria")
		mustContain(t, result, "No-op: work already present on target branch")
		mustContain(t, result, "Empty merge but acceptance criteria not met")
	})

	t.Run("includes writ ID in commit instruction", func(t *testing.T) {
		result := BuildInjection(mr, writ, cfg)
		// Commit instruction should include both title and writ ID.
		mustContain(t, result, "(sol-def456)")
	})

	t.Run("derives author from outpost branch", func(t *testing.T) {
		result := BuildInjection(mr, writ, cfg)
		mustContain(t, result, `--author="Toast <outpost.toast@sol.local>"`)
	})

	t.Run("derives author from envoy branch", func(t *testing.T) {
		envoyMR := &store.MergeRequest{
			ID:       "mr-envoy1",
			WritID:   "sol-envoy1",
			Branch:   "envoy/myworld/Polaris/sol-envoy1",
			Phase:    "claimed",
			Attempts: 1,
		}
		result := BuildInjection(envoyMR, writ, cfg)
		mustContain(t, result, `--author="Polaris <envoy.polaris@sol.local>"`)
	})
}

func TestWriteInjectionFile(t *testing.T) {
	dir := t.TempDir()
	content := "## Merge Task\n\nSome injection context here."

	if err := WriteInjectionFile(dir, content); err != nil {
		t.Fatalf("WriteInjectionFile() error: %v", err)
	}

	// Verify file was written.
	data, err := os.ReadFile(filepath.Join(dir, injectionFileName))
	if err != nil {
		t.Fatalf("failed to read injection file: %v", err)
	}
	if string(data) != content {
		t.Errorf("injection file content = %q, want %q", string(data), content)
	}
}

func TestCleanInjectionFile(t *testing.T) {
	t.Run("removes existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, injectionFileName)
		os.WriteFile(path, []byte("test"), 0o644)

		if err := CleanInjectionFile(dir); err != nil {
			t.Fatalf("CleanInjectionFile() error: %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("injection file should have been removed")
		}
	})

	t.Run("idempotent — no error if file missing", func(t *testing.T) {
		dir := t.TempDir()
		if err := CleanInjectionFile(dir); err != nil {
			t.Fatalf("CleanInjectionFile() should not error on missing file: %v", err)
		}
	})
}

func mustContain(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("output does not contain %q\n--- output ---\n%s", substr, s)
	}
}

func mustNotContain(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("output should not contain %q", substr)
	}
}

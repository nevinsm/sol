package forge

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/setup"
	"github.com/nevinsm/sol/internal/store"
)

// --- matchesSolManagedPath / filterSolManagedPaths ---

func TestMatchesSolManagedPath(t *testing.T) {
	patterns := setup.SolManagedPaths()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"file entry exact match", ".resolution.md", true},
		{"file entry nested path does not match", "docs/.resolution.md", false},
		{"directory entry matches directory itself", ".brief", true},
		{"directory entry matches file under directory", ".brief/arch-simp-review.md", true},
		{"directory entry matches nested file under directory", ".brief/notes/deep.md", true},
		{"codex directory matches", ".codex/config.toml", true},
		{"clean file does not match", "internal/forge/patrol.go", false},
		{"clean file with similar prefix does not match", ".briefly-unrelated.md", false},
		{"CLAUDE.local.md file entry matches", "CLAUDE.local.md", true},
		{"CLAUDE.md (shared, tracked) does not match", "CLAUDE.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSolManagedPath(tt.path, patterns)
			if got != tt.want {
				t.Errorf("matchesSolManagedPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestFilterSolManagedPaths(t *testing.T) {
	files := []string{
		"internal/forge/patrol.go",
		".resolution.md",
		"docs/decisions/0028-event-driven-forge-orchestrator.md",
		".brief/arch-simp-review.md",
		".codex/config.toml",
		"",
		"  ",
	}

	got := filterSolManagedPaths(files)
	want := []string{".resolution.md", ".brief/arch-simp-review.md", ".codex/config.toml"}

	if len(got) != len(want) {
		t.Fatalf("filterSolManagedPaths() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterSolManagedPaths()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterSolManagedPathsCleanBranch(t *testing.T) {
	files := []string{
		"internal/forge/patrol.go",
		"internal/forge/patrol_test.go",
		"docs/decisions/0029-something.md",
	}

	got := filterSolManagedPaths(files)
	if len(got) != 0 {
		t.Errorf("filterSolManagedPaths() = %v, want empty", got)
	}
}

// --- checkPathGate ---

func TestCheckPathGateDetectsOffendingPaths(t *testing.T) {
	state, _, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-gate-001",
		WritID: "sol-gate1111111",
		Branch: "outpost/Toast/sol-gate1111111",
	}

	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult(
		"git diff --name-only origin/main...origin/outpost/Toast/sol-gate1111111",
		[]byte("internal/forge/patrol.go\n.resolution.md\n.brief/notes.md\n"),
		nil,
	)

	offending, err := state.checkPathGate(context.Background(), mr)
	if err != nil {
		t.Fatalf("checkPathGate error: %v", err)
	}

	want := []string{".resolution.md", ".brief/notes.md"}
	if len(offending) != len(want) {
		t.Fatalf("checkPathGate() = %v, want %v", offending, want)
	}
	for i := range want {
		if offending[i] != want[i] {
			t.Errorf("checkPathGate()[%d] = %q, want %q", i, offending[i], want[i])
		}
	}
}

func TestCheckPathGateCleanBranch(t *testing.T) {
	state, _, cmdRunner := setupPatrolTest(t)
	defer state.fl.Close()

	mr := &store.MergeRequest{
		ID:     "mr-gate-002",
		WritID: "sol-gate2222222",
		Branch: "outpost/Toast/sol-gate2222222",
	}

	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult(
		"git diff --name-only origin/main...origin/outpost/Toast/sol-gate2222222",
		[]byte("internal/forge/patrol.go\ninternal/forge/patrol_test.go\n"),
		nil,
	)

	offending, err := state.checkPathGate(context.Background(), mr)
	if err != nil {
		t.Fatalf("checkPathGate error: %v", err)
	}
	if len(offending) != 0 {
		t.Errorf("checkPathGate() = %v, want empty for clean branch", offending)
	}
}

// --- executeMergeSession integration with the path gate ---

// TestExecuteMergeSessionPathGateRejectsOffendingBranch verifies the
// end-to-end forge gate flow: an MR whose branch touches a sol-managed path
// is marked failed with the offending paths named in the reason, without
// ever starting a merge session (never lose work — branch persists, writ
// reopens for the next attempt per ADR-0028's gate-failure flow).
func TestExecuteMergeSessionPathGateRejectsOffendingBranch(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-reject-001", Phase: store.MRClaimed, WritID: "sol-reject111111", Branch: "outpost/Toast/sol-reject111111"},
	}
	worldStore.items["sol-reject111111"] = &store.Writ{
		ID:     "sol-reject111111",
		Title:  "Path gate rejection test writ",
		Status: store.WritDone,
	}

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult(
		"git diff --name-only origin/main...origin/outpost/Toast/sol-reject111111",
		[]byte(".resolution.md\n.brief/notes.md\n"),
		nil,
	)

	mr := worldStore.mrs[0]
	state.executeMergeSession(context.Background(), &mr, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-reject-001"]
	worldStore.mu.Unlock()

	if phase != store.MRFailed {
		t.Fatalf("MR phase = %q, want %q", phase, store.MRFailed)
	}

	if state.lastError == "" || !strings.Contains(state.lastError, ".resolution.md") || !strings.Contains(state.lastError, ".brief/notes.md") {
		t.Errorf("lastError = %q, want it to name both offending paths", state.lastError)
	}

	// The merge session must never have been started — the gate runs before
	// any session launch.
	sessMgr.mu.Lock()
	sessionCount := len(sessMgr.sessions)
	sessMgr.mu.Unlock()
	if sessionCount != 0 {
		t.Errorf("expected no merge session to be started, got %d active session(s)", sessionCount)
	}

	// Branch must persist — never lose work. The writ reopens for the next
	// attempt (MarkFailed's existing contract; verified indirectly via the
	// writ status below since the mock store applies UpdateWrit).
	writ := worldStore.items["sol-reject111111"]
	if writ.Status != store.WritOpen {
		t.Errorf("writ status = %q, want %q (reopened for re-dispatch)", writ.Status, store.WritOpen)
	}
}

// TestExecuteMergeSessionPathGateAllowsCleanBranch verifies a clean MR (no
// sol-managed paths touched) is unaffected by the path gate: it proceeds to
// the merge session launch. We force a distinguishable session-launch error
// so the resulting failure reason proves the gate was passed rather than
// tripped (a gate rejection would name offending paths and never launch a
// session).
func TestExecuteMergeSessionPathGateAllowsCleanBranch(t *testing.T) {
	state, worldStore, sessMgr := setupOrchestratorTest(t)
	defer state.fl.Close()

	const maxAttempts = 1
	state.forge.cfg.MaxAttempts = maxAttempts

	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-clean-001", Phase: store.MRClaimed, WritID: "sol-clean1111111", Branch: "outpost/Toast/sol-clean1111111", Attempts: maxAttempts},
	}
	worldStore.items["sol-clean1111111"] = &store.Writ{
		ID:     "sol-clean1111111",
		Title:  "Path gate clean-branch test writ",
		Status: store.WritDone,
	}

	cmdRunner := state.cmd.(*mockCmdRunner)
	cmdRunner.SetResult("git fetch origin", nil, nil)
	cmdRunner.SetResult(
		"git diff --name-only origin/main...origin/outpost/Toast/sol-clean1111111",
		[]byte("internal/forge/patrol.go\n"),
		nil,
	)

	// Force a distinguishable failure past the gate: session launch fails.
	sessMgr.startErr = fmt.Errorf("simulated launch failure")

	mr := worldStore.mrs[0]
	state.executeMergeSession(context.Background(), &mr, 1)

	worldStore.mu.Lock()
	phase := worldStore.phaseUpdates["mr-clean-001"]
	worldStore.mu.Unlock()

	if phase != store.MRFailed {
		t.Fatalf("MR phase = %q, want %q (failure past the gate, at max attempts)", phase, store.MRFailed)
	}

	if strings.Contains(state.lastError, "sol-managed") {
		t.Errorf("lastError = %q, must not mention the path gate for a clean branch", state.lastError)
	}
	if !strings.Contains(state.lastError, "merge session failed") {
		t.Errorf("lastError = %q, want it to reflect the session-launch failure past the gate", state.lastError)
	}
}

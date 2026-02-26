package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/store"
)

// --- Mock session manager ---

type mockSessionManager struct {
	started map[string]bool
	stopped map[string]bool
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		started: make(map[string]bool),
		stopped: make(map[string]bool),
	}
}

func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, rig string) error {
	m.started[name] = true
	return nil
}

func (m *mockSessionManager) Stop(name string, force bool) error {
	m.stopped[name] = true
	return nil
}

func (m *mockSessionManager) Exists(name string) bool {
	return m.started[name] && !m.stopped[name]
}

// --- Helper to set up real stores in temp dirs ---

func setupStores(t *testing.T) (*store.Store, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)

	if err := os.MkdirAll(dir+"/.store", 0o755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}

	rigStore, err := store.OpenRig("testrig")
	if err != nil {
		t.Fatalf("failed to open rig store: %v", err)
	}
	t.Cleanup(func() { rigStore.Close() })

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("failed to open town store: %v", err)
	}
	t.Cleanup(func() { townStore.Close() })

	return rigStore, townStore
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
	}
}

// --- Sling tests ---

func TestSlingHappyPath(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := townStore.CreateAgent("Toast", "testrig", "polecat"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create a temporary git repo to use as source.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Sling(SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, rigStore, townStore, mgr)

	if err != nil {
		t.Fatalf("Sling failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}
	if result.SessionName != "gt-testrig-Toast" {
		t.Errorf("expected session name gt-testrig-Toast, got %q", result.SessionName)
	}

	// Verify hook was written.
	hookID, err := hook.Read("testrig", "Toast")
	if err != nil {
		t.Fatalf("failed to read hook: %v", err)
	}
	if hookID != itemID {
		t.Errorf("hook has %q, expected %q", hookID, itemID)
	}

	// Verify work item was updated.
	item, err := rigStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "hooked" {
		t.Errorf("expected work item status 'hooked', got %q", item.Status)
	}
	if item.Assignee != "testrig/Toast" {
		t.Errorf("expected assignee 'testrig/Toast', got %q", item.Assignee)
	}

	// Verify agent was updated.
	agent, err := townStore.GetAgent("testrig/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.HookItem != itemID {
		t.Errorf("expected agent hook_item %q, got %q", itemID, agent.HookItem)
	}

	// Verify session was started.
	if !mgr.started["gt-testrig-Toast"] {
		t.Error("expected session to be started")
	}

	// Verify CLAUDE.md was installed.
	claudeMD := result.WorktreeDir + "/.claude/CLAUDE.md"
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(data), "Toast") {
		t.Error("CLAUDE.md missing agent name")
	}
}

func TestSlingAutoAgent(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := townStore.CreateAgent("Alpha", "testrig", "polecat"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Sling(SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: repoDir,
	}, rigStore, townStore, mgr)

	if err != nil {
		t.Fatalf("Sling failed: %v", err)
	}
	if result.AgentName != "Alpha" {
		t.Errorf("expected auto-selected agent 'Alpha', got %q", result.AgentName)
	}
}

func TestSlingAutoProvision(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// No agent exists — Sling should auto-provision from the name pool.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Sling(SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: repoDir,
	}, rigStore, townStore, mgr)

	if err != nil {
		t.Fatalf("Sling failed: %v", err)
	}

	// First name in the default pool is "Toast".
	if result.AgentName != "Toast" {
		t.Errorf("expected auto-provisioned agent 'Toast', got %q", result.AgentName)
	}

	// Verify the agent was created in the store.
	agent, err := townStore.GetAgent("testrig/Toast")
	if err != nil {
		t.Fatalf("failed to get auto-provisioned agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.HookItem != itemID {
		t.Errorf("expected agent hook_item %q, got %q", itemID, agent.HookItem)
	}
}

func TestSlingAutoProvisionSkipsUsed(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create agents with the first 3 pool names and set them to "working".
	poolNames := []string{"Toast", "Jasper", "Sage"}
	for _, name := range poolNames {
		if _, err := townStore.CreateAgent(name, "testrig", "polecat"); err != nil {
			t.Fatalf("failed to create agent %q: %v", name, err)
		}
		if err := townStore.UpdateAgentState("testrig/"+name, "working", "gt-other"); err != nil {
			t.Fatalf("failed to update agent %q: %v", name, err)
		}
	}

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Sling(SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: repoDir,
	}, rigStore, townStore, mgr)

	if err != nil {
		t.Fatalf("Sling failed: %v", err)
	}

	// Auto-provisioned name must not be any of the already-used names.
	for _, used := range poolNames {
		if result.AgentName == used {
			t.Errorf("auto-provisioned agent got already-used name %q", used)
		}
	}
	if result.AgentName == "" {
		t.Error("auto-provisioned agent has empty name")
	}
}

func TestSlingFlockPreventsDoubleDispatch(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// Acquire the lock manually before calling Sling.
	lock, err := AcquireWorkItemLock(itemID)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	defer lock.Release()

	_, err = Sling(SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		SourceRepo: "/tmp",
	}, rigStore, townStore, mgr)

	if err == nil {
		t.Fatal("expected contention error")
	}
	if !strings.Contains(err.Error(), "being dispatched by another process") {
		t.Errorf("expected contention error, got: %v", err)
	}
}

func TestSlingItemNotOpen(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if err := rigStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := townStore.CreateAgent("Toast", "testrig", "polecat"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Sling(SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "Toast",
		SourceRepo: "/tmp",
	}, rigStore, townStore, mgr)

	if err == nil {
		t.Fatal("expected error for non-open work item")
	}
	if !strings.Contains(err.Error(), "expected \"open\"") {
		t.Errorf("expected 'expected open' error, got: %v", err)
	}
}

// --- Prime tests ---

func TestPrimeWithHook(t *testing.T) {
	rigStore, _ := setupStores(t)

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if err := hook.Write("testrig", "Toast", itemID); err != nil {
		t.Fatalf("failed to write hook: %v", err)
	}

	result, err := Prime("testrig", "Toast", rigStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("output missing WORK CONTEXT header")
	}
	if !strings.Contains(result.Output, "Toast") {
		t.Error("output missing agent name")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("output missing work item ID")
	}
	if !strings.Contains(result.Output, "Add README") {
		t.Error("output missing title")
	}
	if !strings.Contains(result.Output, "gt done") {
		t.Error("output missing gt done instruction")
	}
}

func TestPrimeWithoutHook(t *testing.T) {
	rigStore, _ := setupStores(t)

	result, err := Prime("testrig", "Toast", rigStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if result.Output != "No work hooked" {
		t.Errorf("expected 'No work hooked', got %q", result.Output)
	}
}

// --- Done tests ---

func TestDoneHappyPath(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := rigStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := rigStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "hooked", Assignee: "testrig/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := townStore.CreateAgent("Toast", "testrig", "polecat"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := townStore.UpdateAgentState("testrig/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := hook.Write("testrig", "Toast", itemID); err != nil {
		t.Fatalf("failed to write hook: %v", err)
	}

	// Create a worktree directory with a git repo (simulating a worktree).
	worktreeDir := WorktreePath("testrig", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")

	sessName := SessionName("testrig", "Toast")
	mgr.started[sessName] = true

	result, err := Done(DoneOpts{
		Rig:       "testrig",
		AgentName: "Toast",
	}, rigStore, townStore, mgr)

	if err != nil {
		t.Fatalf("Done failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}
	expectedBranch := fmt.Sprintf("polecat/Toast/%s", itemID)
	if result.BranchName != expectedBranch {
		t.Errorf("expected branch %q, got %q", expectedBranch, result.BranchName)
	}

	// Verify work item was updated to done.
	item, err := rigStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected work item status 'done', got %q", item.Status)
	}

	// Verify agent is idle.
	agent, err := townStore.GetAgent("testrig/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Verify hook is cleared.
	hookID, err := hook.Read("testrig", "Toast")
	if err != nil {
		t.Fatalf("failed to read hook: %v", err)
	}
	if hookID != "" {
		t.Errorf("expected empty hook, got %q", hookID)
	}
}

func TestDoneNoHook(t *testing.T) {
	rigStore, townStore := setupStores(t)
	mgr := newMockSessionManager()

	_, err := Done(DoneOpts{
		Rig:       "testrig",
		AgentName: "Toast",
	}, rigStore, townStore, mgr)

	if err == nil {
		t.Fatal("expected error when no hook exists")
	}
	if !strings.Contains(err.Error(), "no work hooked") {
		t.Errorf("expected 'no work hooked' error, got: %v", err)
	}
}

package dispatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
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

func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
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
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(dir+"/.store", 0o755); err != nil {
		t.Fatalf("failed to create store dir: %v", err)
	}

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	t.Cleanup(func() { worldStore.Close() })

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	t.Cleanup(func() { sphereStore.Close() })

	return worldStore, sphereStore
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	allArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", allArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %s: %v", strings.Join(args, " "), string(out), err)
	}
}

// addBareRemote creates a bare git repo and adds it as "origin" to repoDir
// so that git push succeeds in tests.
func addBareRemote(t *testing.T, repoDir string) {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), "origin.git")
	runGit(t, repoDir, "clone", "--bare", ".", bareDir)
	runGit(t, repoDir, "remote", "add", "origin", bareDir)
}

// --- Cast tests ---

func TestCastHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Create a temporary git repo to use as source.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}
	if result.SessionName != "sol-ember-Toast" {
		t.Errorf("expected session name sol-ember-Toast, got %q", result.SessionName)
	}

	// Verify tether was written.
	tetherID, err := tether.Read("ember", "Toast")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether has %q, expected %q", tetherID, itemID)
	}

	// Verify work item was updated.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected work item status 'tethered', got %q", item.Status)
	}
	if item.Assignee != "ember/Toast" {
		t.Errorf("expected assignee 'ember/Toast', got %q", item.Assignee)
	}

	// Verify agent was updated.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.TetherItem != itemID {
		t.Errorf("expected agent tether_item %q, got %q", itemID, agent.TetherItem)
	}

	// Verify session was started.
	if !mgr.started["sol-ember-Toast"] {
		t.Error("expected session to be started")
	}

	// Verify CLAUDE.local.md was installed.
	claudeMD := result.WorktreeDir + "/.claude/CLAUDE.local.md"
	data, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	if !strings.Contains(string(data), "Toast") {
		t.Error("CLAUDE.local.md missing agent name")
	}
}

func TestCastAutoAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Alpha", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}
	if result.AgentName != "Alpha" {
		t.Errorf("expected auto-selected agent 'Alpha', got %q", result.AgentName)
	}
}

func TestCastAutoProvision(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// No agent exists — Cast should auto-provision from the name pool.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// First name in the default pool is "Nova".
	if result.AgentName != "Nova" {
		t.Errorf("expected auto-provisioned agent 'Nova', got %q", result.AgentName)
	}

	// Verify the agent was created in the store.
	agent, err := sphereStore.GetAgent("ember/Nova")
	if err != nil {
		t.Fatalf("failed to get auto-provisioned agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.TetherItem != itemID {
		t.Errorf("expected agent tether_item %q, got %q", itemID, agent.TetherItem)
	}
}

func TestCastAutoProvisionCapacityEnforced(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Set capacity = 1 via world config.
	solHome := os.Getenv("SOL_HOME")
	worldDir := solHome + "/ember"
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(worldDir+"/world.toml", []byte("[agents]\ncapacity = 1\n"), 0o644)

	// Create first work item and cast — should auto-provision one agent.
	item1, err := worldStore.CreateWorkItem("Item 1", "First item", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	_, err = Cast(CastOpts{
		WorkItemID: item1,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("first Cast failed: %v", err)
	}

	// Create second work item and cast — should fail with capacity error.
	item2, err := worldStore.CreateWorkItem("Item 2", "Second item", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	_, err = Cast(CastOpts{
		WorkItemID: item2,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err == nil {
		t.Fatal("expected capacity error on second cast")
	}
	if !strings.Contains(err.Error(), "reached agent capacity") {
		t.Errorf("expected 'reached agent capacity' error, got: %v", err)
	}
}

func TestCastAutoProvisionCapacityZeroUnlimited(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Default capacity = 0 (unlimited). No world.toml needed.
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	// Create and cast multiple work items — all should succeed.
	for i := 0; i < 3; i++ {
		itemID, err := worldStore.CreateWorkItem(
			fmt.Sprintf("Item %d", i), "desc", "operator", 2, nil)
		if err != nil {
			t.Fatalf("failed to create work item %d: %v", i, err)
		}

		_, err = Cast(CastOpts{
			WorkItemID: itemID,
			World:      "ember",
			SourceRepo: repoDir,
		}, worldStore, sphereStore, mgr, nil)
		if err != nil {
			t.Fatalf("Cast %d failed: %v", i, err)
		}
	}

	// Verify 3 agents exist.
	agents, err := sphereStore.ListAgents("ember", "")
	if err != nil {
		t.Fatalf("failed to list agents: %v", err)
	}
	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
	}
}

func TestCastAutoProvisionCustomNamePool(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create a custom name pool file.
	solHome := os.Getenv("SOL_HOME")
	customPoolPath := solHome + "/custom-names.txt"
	os.WriteFile(customPoolPath, []byte("Mercury\nVenus\nEarth\n"), 0o644)

	// Write world config pointing to custom pool.
	worldDir := solHome + "/ember"
	os.MkdirAll(worldDir, 0o755)
	toml := fmt.Sprintf("[agents]\nname_pool_path = %q\n", customPoolPath)
	os.WriteFile(worldDir+"/world.toml", []byte(toml), 0o644)

	itemID, err := worldStore.CreateWorkItem("Test item", "desc", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(CastOpts{
		WorkItemID: itemID,
		World:      "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Cast failed: %v", err)
	}

	// Agent name should come from the custom pool.
	if result.AgentName != "Mercury" {
		t.Errorf("expected agent name 'Mercury' from custom pool, got %q", result.AgentName)
	}
}

func TestCastAutoProvisionSkipsUsed(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create agents with the first 3 pool names and set them to "working".
	poolNames := []string{"Nova", "Vega", "Lyra"}
	for _, name := range poolNames {
		if _, err := sphereStore.CreateAgent(name, "ember", "agent"); err != nil {
			t.Fatalf("failed to create agent %q: %v", name, err)
		}
		if err := sphereStore.UpdateAgentState("ember/"+name, "working", "sol-other"); err != nil {
			t.Fatalf("failed to update agent %q: %v", name, err)
		}
	}

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast failed: %v", err)
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

func TestCastFlockPreventsDoubleDispatch(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// Acquire the lock manually before calling Cast.
	lock, err := AcquireWorkItemLock(itemID)
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
	defer lock.Release()

	_, err = Cast(CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		SourceRepo: "/tmp",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected contention error")
	}
	if !strings.Contains(err.Error(), "being dispatched by another process") {
		t.Errorf("expected contention error, got: %v", err)
	}
}

func TestCastItemNotOpen(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "done"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Cast(CastOpts{
		WorkItemID: itemID,
		World:        "ember",
		AgentName:  "Toast",
		SourceRepo: "/tmp",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error for non-open work item")
	}
	if !strings.Contains(err.Error(), "expected \"open\"") {
		t.Errorf("expected 'expected open' error, got: %v", err)
	}
}

// --- Prime tests ---

func TestPrimeWithTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	result, err := Prime("ember", "Toast", worldStore)
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
	if !strings.Contains(result.Output, "sol resolve") {
		t.Error("output missing sol resolve instruction")
	}
}

func TestPrimeWithoutTether(t *testing.T) {
	worldStore, _ := setupStores(t)

	result, err := Prime("ember", "Toast", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if result.Output != "No work tethered" {
		t.Errorf("expected 'No work tethered', got %q", result.Output)
	}
}

// --- Resolve tests ---

func TestResolveHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create a worktree directory with a git repo (simulating a worktree).
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}
	expectedBranch := fmt.Sprintf("outpost/Toast/%s", itemID)
	if result.BranchName != expectedBranch {
		t.Errorf("expected branch %q, got %q", expectedBranch, result.BranchName)
	}

	// Verify merge request was created.
	if result.MergeRequestID == "" {
		t.Error("expected MergeRequestID to be set")
	}

	// Verify work item was updated to done.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected work item status 'done', got %q", item.Status)
	}

	// Verify agent is idle.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Verify tether is cleared.
	tetherID, err := tether.Read("ember", "Toast")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}
}

func TestResolveNoTether(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	_, err := Resolve(ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error when no tether exists")
	}
	if !strings.Contains(err.Error(), "no work tethered") {
		t.Errorf("expected 'no work tethered' error, got: %v", err)
	}
}

func TestResolveConflictResolution(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	// Create the original work item.
	origItemID, err := worldStore.CreateWorkItem("Add feature X", "Implement feature X", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// Create a merge request for the original work item.
	mrID, err := worldStore.CreateMergeRequest(origItemID, "outpost/Alpha/"+origItemID, 2)
	if err != nil {
		t.Fatalf("failed to create merge request: %v", err)
	}

	// Create the conflict-resolution task.
	resolutionID, err := worldStore.CreateWorkItemWithOpts(store.CreateWorkItemOpts{
		Title:       "Resolve merge conflicts: Add feature X",
		Description: "Resolve merge conflicts",
		CreatedBy:   "ember/forge",
		Priority:    1,
		Labels:      []string{"conflict-resolution", "source-mr:" + mrID},
		ParentID:    origItemID,
	})
	if err != nil {
		t.Fatalf("failed to create resolution task: %v", err)
	}

	// Block the MR with the resolution task.
	if err := worldStore.BlockMergeRequest(mrID, resolutionID); err != nil {
		t.Fatalf("failed to block MR: %v", err)
	}

	// Set up agent and tether the resolution task.
	if err := worldStore.UpdateWorkItem(resolutionID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}
	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", resolutionID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}
	if err := tether.Write("ember", "Toast", resolutionID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree dir with git repo.
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve (conflict-resolution) failed: %v", err)
	}

	// Verify NO new merge request was created.
	if result.MergeRequestID != "" {
		t.Errorf("expected empty MergeRequestID for conflict-resolution, got %q", result.MergeRequestID)
	}

	// Verify the resolution work item is closed.
	resItem, err := worldStore.GetWorkItem(resolutionID)
	if err != nil {
		t.Fatalf("failed to get resolution item: %v", err)
	}
	if resItem.Status != "closed" {
		t.Errorf("expected resolution item status 'closed', got %q", resItem.Status)
	}

	// Verify the original MR is unblocked.
	mr, err := worldStore.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("failed to get MR: %v", err)
	}
	if mr.BlockedBy != "" {
		t.Errorf("expected MR blocked_by to be empty (unblocked), got %q", mr.BlockedBy)
	}
	if mr.Phase != "ready" {
		t.Errorf("expected MR phase 'ready' after unblock, got %q", mr.Phase)
	}

	// Verify agent is idle.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Verify tether is cleared.
	tetherID, err := tether.Read("ember", "Toast")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}
}

func TestResolveCreatesMergeRequest(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Implement login page", "Build the login page", "operator", 1, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:       "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify MergeRequestID is set.
	if result.MergeRequestID == "" {
		t.Fatal("expected MergeRequestID to be set")
	}
	if !strings.HasPrefix(result.MergeRequestID, "mr-") {
		t.Errorf("expected MergeRequestID to start with 'mr-', got %q", result.MergeRequestID)
	}

	// Verify merge request exists in store with correct fields.
	mr, err := worldStore.GetMergeRequest(result.MergeRequestID)
	if err != nil {
		t.Fatalf("failed to get merge request: %v", err)
	}
	if mr.Phase != "ready" {
		t.Errorf("expected MR phase 'ready', got %q", mr.Phase)
	}
	if mr.WorkItemID != itemID {
		t.Errorf("expected MR work_item_id %q, got %q", itemID, mr.WorkItemID)
	}
	expectedBranch := fmt.Sprintf("outpost/Toast/%s", itemID)
	if mr.Branch != expectedBranch {
		t.Errorf("expected MR branch %q, got %q", expectedBranch, mr.Branch)
	}
	if mr.Priority != 1 {
		t.Errorf("expected MR priority 1, got %d", mr.Priority)
	}

	// Verify agent is idle and work item is done (existing behavior).
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected work item status 'done', got %q", item.Status)
	}

	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}
}

// --- Prime with handoff tests ---

func TestPrimeWithHandoff(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file.
	state := &handoff.State{
		WorkItemID:      itemID,
		AgentName:       "Toast",
		World:             "ember",
		PreviousSession: "sol-ember-Toast",
		Summary:         "Implemented login form. Tests passing.",
		RecentCommits:   []string{"abc1234 feat: add login form"},
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	result, err := Prime("ember", "Toast", worldStore)
	if err != nil {
		t.Fatalf("Prime with handoff failed: %v", err)
	}

	if !strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("output missing HANDOFF CONTEXT header")
	}
	if !strings.Contains(result.Output, "Toast") {
		t.Error("output missing agent name")
	}
	if !strings.Contains(result.Output, itemID) {
		t.Error("output missing work item ID")
	}
	if !strings.Contains(result.Output, "Implemented login form") {
		t.Error("output missing summary")
	}
	if !strings.Contains(result.Output, "abc1234 feat: add login form") {
		t.Error("output missing recent commits")
	}
	if !strings.Contains(result.Output, "sol handoff") {
		t.Error("output missing handoff instruction")
	}

	// Handoff file should be deleted after prime.
	if handoff.HasHandoff("ember", "Toast") {
		t.Error("expected handoff file to be removed after prime")
	}
}

func TestPrimeHandoffTakesPriority(t *testing.T) {
	worldStore, _ := setupStores(t)
	solHome := os.Getenv("SOL_HOME")

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Write handoff file.
	state := &handoff.State{
		WorkItemID:       itemID,
		AgentName:        "Toast",
		World:              "ember",
		PreviousSession:  "sol-ember-Toast",
		Summary:          "Handoff summary here.",
		RecentCommits:    []string{"abc1234 feat: work"},
		WorkflowStep:     "implement",
		WorkflowProgress: "1/3 complete",
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("failed to write handoff: %v", err)
	}

	// Also set up workflow state (should be ignored in favor of handoff).
	wfDir := fmt.Sprintf("%s/ember/outposts/Toast/.workflow", solHome)
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("failed to create workflow dir: %v", err)
	}
	stateJSON := `{"current_step":"implement","completed":["plan"],"status":"running","started_at":"2026-02-27T10:00:00Z"}`
	os.WriteFile(wfDir+"/state.json", []byte(stateJSON), 0o644)

	result, err := Prime("ember", "Toast", worldStore)
	if err != nil {
		t.Fatalf("Prime with handoff+workflow failed: %v", err)
	}

	// Should have handoff context, not workflow context.
	if !strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("output missing HANDOFF CONTEXT — handoff should take priority")
	}
	if strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("output contains WORK CONTEXT — handoff should take priority over workflow")
	}

	// Handoff file should be deleted.
	if handoff.HasHandoff("ember", "Toast") {
		t.Error("expected handoff file to be removed after prime")
	}
}

func TestPrimeNoHandoff(t *testing.T) {
	worldStore, _ := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Add README", "Create a README file", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// No handoff file — should use standard prime.
	result, err := Prime("ember", "Toast", worldStore)
	if err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	if !strings.Contains(result.Output, "WORK CONTEXT") {
		t.Error("expected standard WORK CONTEXT output when no handoff")
	}
	if strings.Contains(result.Output, "HANDOFF CONTEXT") {
		t.Error("unexpected HANDOFF CONTEXT when no handoff file exists")
	}
}

// --- Mock world store that wraps a real store but can inject errors ---

type mockWorldStore struct {
	*store.Store
	createMRErr error // if set, CreateMergeRequest returns this error
}

func (m *mockWorldStore) CreateMergeRequest(workItemID, branch string, priority int) (string, error) {
	if m.createMRErr != nil {
		return "", m.createMRErr
	}
	return m.Store.CreateMergeRequest(workItemID, branch, priority)
}

// --- Resolve rollback/safety tests ---

func TestResolveRollbackOnMRFailure(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add feature", "Build the feature", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree with git repo and remote.
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	// Use mock world store that fails on CreateMergeRequest.
	mock := &mockWorldStore{
		Store:       worldStore,
		createMRErr: fmt.Errorf("simulated MR creation failure"),
	}

	_, err = Resolve(ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, mock, sphereStore, mgr, nil)

	if err == nil {
		t.Fatal("expected error from failed CreateMergeRequest")
	}
	if !strings.Contains(err.Error(), "simulated MR creation failure") {
		t.Errorf("expected simulated error, got: %v", err)
	}

	// Verify: work item status is rolled back to "tethered" (not stuck at "done").
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected work item status rolled back to 'tethered', got %q", item.Status)
	}
}

func TestResolvePushFailureCreatesMR(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add feature", "Build the feature", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree with git repo but NO remote (so push fails).
	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	// Intentionally NO addBareRemote — push will fail.

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Verify: MR is created with phase "failed".
	if result.MergeRequestID == "" {
		t.Fatal("expected MergeRequestID to be set even with push failure")
	}

	mr, err := worldStore.GetMergeRequest(result.MergeRequestID)
	if err != nil {
		t.Fatalf("failed to get merge request: %v", err)
	}
	if mr.Phase != "failed" {
		t.Errorf("expected MR phase 'failed', got %q", mr.Phase)
	}

	// Verify: work item is "done", agent is "idle".
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected work item status 'done', got %q", item.Status)
	}

	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}
}

func TestReCastPartialFailureRecovery(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Add feature", "Build the feature", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	// Set up partial failure state: item tethered to agent, but agent still "idle".
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	// Agent state is "idle" with no tether_item — simulates crash after work item
	// update but before agent state update.

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "commit", "--allow-empty", "-m", "initial")

	result, err := Cast(CastOpts{
		WorkItemID: itemID,
		World:      "ember",
		AgentName:  "Toast",
		SourceRepo: repoDir,
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Cast (partial failure recovery) failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Toast" {
		t.Errorf("expected agent name Toast, got %q", result.AgentName)
	}

	// Verify: agent state is now "working", session started.
	agent, err := sphereStore.GetAgent("ember/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if !mgr.started["sol-ember-Toast"] {
		t.Error("expected session to be started")
	}
}

// --- Envoy resolve tests ---

func TestResolveEnvoyKeepsSession(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Envoy task", "An envoy work item", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Scout"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	// Create an envoy agent.
	if _, err := sphereStore.CreateAgent("Scout", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Scout", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Scout", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Create worktree dir at envoy path with git repo.
	worktreeDir := envoy.WorktreePath("ember", "Scout")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := SessionName("ember", "Scout")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:     "ember",
		AgentName: "Scout",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Session should NOT have been stopped.
	if mgr.stopped[sessName] {
		t.Error("expected session to NOT be stopped for envoy resolve")
	}

	// SessionKept should be true.
	if !result.SessionKept {
		t.Error("expected SessionKept to be true for envoy resolve")
	}

	// Branch name should have envoy/{world}/{agentName} format.
	expectedBranch := "envoy/ember/Scout"
	if result.BranchName != expectedBranch {
		t.Errorf("expected branch %q, got %q", expectedBranch, result.BranchName)
	}

	// Work item should still be done.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "done" {
		t.Errorf("expected work item status 'done', got %q", item.Status)
	}

	// Agent should be idle.
	agent, err := sphereStore.GetAgent("ember/Scout")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Tether should be cleared.
	tetherID, err := tether.Read("ember", "Scout")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}

	// MR should be created.
	if result.MergeRequestID == "" {
		t.Error("expected MergeRequestID to be set")
	}
}

func TestResolveAgentKillsSession(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Agent task", "A regular work item", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	// Create a regular agent.
	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	worktreeDir := WorktreePath("ember", "Toast")
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}
	runGit(t, worktreeDir, "init")
	runGit(t, worktreeDir, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, worktreeDir)

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)

	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// SessionKept should be false for regular agents.
	if result.SessionKept {
		t.Error("expected SessionKept to be false for regular agent resolve")
	}
}

func TestResolveRemovesWorktreeForOutpostAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)
	mgr := newMockSessionManager()

	itemID, err := worldStore.CreateWorkItem("Cleanup test", "Test worktree cleanup", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered", Assignee: "ember/Toast"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Toast", "working", itemID); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	if err := tether.Write("ember", "Toast", itemID); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set up a real managed repo and create a worktree from it.
	repoPath := config.RepoPath("ember")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "commit", "--allow-empty", "-m", "initial")
	addBareRemote(t, repoPath)

	worktreeDir := WorktreePath("ember", "Toast")
	branchName := fmt.Sprintf("outpost/Toast/%s", itemID)
	runGit(t, repoPath, "worktree", "add", worktreeDir, "-b", branchName, "HEAD")

	// Verify worktree exists before resolve.
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worktree should exist before resolve: %v", err)
	}

	sessName := SessionName("ember", "Toast")
	mgr.started[sessName] = true

	result, err := Resolve(ResolveOpts{
		World:     "ember",
		AgentName: "Toast",
	}, worldStore, sphereStore, mgr, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if result.SessionKept {
		t.Error("expected SessionKept to be false for outpost agent")
	}

	// Wait for the async cleanup goroutine (1s delay + execution time).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(worktreeDir); os.IsNotExist(err) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify worktree directory was removed.
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Errorf("worktree directory should be removed after resolve, but still exists: %s", worktreeDir)
	}
}

// --- ResolveSourceRepo tests ---

func TestResolveSourceRepoManagedClone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create managed repo directory.
	repoPath := config.RepoPath("testworld")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	result, err := ResolveSourceRepo("testworld", config.WorldConfig{})
	if err != nil {
		t.Fatalf("ResolveSourceRepo failed: %v", err)
	}
	if result != repoPath {
		t.Errorf("expected %q, got %q", repoPath, result)
	}
}

func TestResolveSourceRepoConfigFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// No managed clone exists — should fall back to config value.
	cfg := config.WorldConfig{}
	cfg.World.SourceRepo = "/some/legacy/path"

	result, err := ResolveSourceRepo("testworld", cfg)
	if err != nil {
		t.Fatalf("ResolveSourceRepo failed: %v", err)
	}
	if result != "/some/legacy/path" {
		t.Errorf("expected /some/legacy/path, got %q", result)
	}
}

func TestResolveSourceRepoManagedCloneTakesPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create managed repo directory.
	repoPath := config.RepoPath("testworld")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Config also has a source_repo — managed clone should take priority.
	cfg := config.WorldConfig{}
	cfg.World.SourceRepo = "/some/other/path"

	result, err := ResolveSourceRepo("testworld", cfg)
	if err != nil {
		t.Fatalf("ResolveSourceRepo failed: %v", err)
	}
	if result != repoPath {
		t.Errorf("expected managed clone %q, got %q", repoPath, result)
	}
}

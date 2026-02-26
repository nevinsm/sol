package refinery

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/store"
)

// --- Mock stores ---

type mockRigStore struct {
	mu            sync.Mutex
	mrs           []store.MergeRequest
	items         map[string]*store.WorkItem
	claims        []string // IDs of claimed MRs
	phaseUpdates  map[string]string
	staleReleased int
}

func newMockRigStore() *mockRigStore {
	return &mockRigStore{
		items:        make(map[string]*store.WorkItem),
		phaseUpdates: make(map[string]string),
	}
}

func (m *mockRigStore) GetMergeRequest(id string) (*store.MergeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.mrs {
		if m.mrs[i].ID == id {
			mr := m.mrs[i]
			return &mr, nil
		}
	}
	return nil, fmt.Errorf("merge request %q not found", id)
}

func (m *mockRigStore) ClaimMergeRequest(claimerID string) (*store.MergeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.mrs {
		if m.mrs[i].Phase == "ready" {
			m.mrs[i].Phase = "claimed"
			m.mrs[i].ClaimedBy = claimerID
			m.mrs[i].Attempts++
			m.claims = append(m.claims, m.mrs[i].ID)
			mr := m.mrs[i]
			return &mr, nil
		}
	}
	return nil, nil
}

func (m *mockRigStore) UpdateMergeRequestPhase(id, phase string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phaseUpdates[id] = phase
	for i := range m.mrs {
		if m.mrs[i].ID == id {
			m.mrs[i].Phase = phase
			return nil
		}
	}
	// Still track the update even if MR not in the list (claimed MRs
	// are copied out, so the original may not be tracked).
	return nil
}

func (m *mockRigStore) ReleaseStaleClaims(ttl time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := m.staleReleased
	m.staleReleased = 0
	return count, nil
}

func (m *mockRigStore) GetWorkItem(id string) (*store.WorkItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("work item %q not found", id)
	}
	return item, nil
}

func (m *mockRigStore) UpdateWorkItem(id string, updates store.WorkItemUpdates) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return fmt.Errorf("work item %q not found", id)
	}
	if updates.Status != "" {
		item.Status = updates.Status
	}
	return nil
}

func (m *mockRigStore) ListMergeRequests(phase string) ([]store.MergeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []store.MergeRequest
	for _, mr := range m.mrs {
		if phase == "" || mr.Phase == phase {
			result = append(result, mr)
		}
	}
	return result, nil
}

func (m *mockRigStore) BlockMergeRequest(mrID, blockerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.mrs {
		if m.mrs[i].ID == mrID {
			m.mrs[i].BlockedBy = blockerID
			m.mrs[i].Phase = "ready"
			return nil
		}
	}
	return fmt.Errorf("merge request %q not found", mrID)
}

func (m *mockRigStore) UnblockMergeRequest(mrID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.mrs {
		if m.mrs[i].ID == mrID {
			m.mrs[i].BlockedBy = ""
			m.mrs[i].Phase = "ready"
			return nil
		}
	}
	return fmt.Errorf("merge request %q not found", mrID)
}

func (m *mockRigStore) FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mr := range m.mrs {
		if mr.BlockedBy == blockerID {
			mrCopy := mr
			return &mrCopy, nil
		}
	}
	return nil, nil
}

func (m *mockRigStore) CreateWorkItemWithOpts(opts store.CreateWorkItemOpts) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := fmt.Sprintf("gt-%08x", len(m.items))
	m.items[id] = &store.WorkItem{
		ID:        id,
		Title:     opts.Title,
		Status:    "open",
		Priority:  opts.Priority,
		ParentID:  opts.ParentID,
		CreatedBy: opts.CreatedBy,
		Labels:    opts.Labels,
	}
	return id, nil
}

func (m *mockRigStore) CloseWorkItem(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return fmt.Errorf("work item %q not found", id)
	}
	item.Status = "closed"
	return nil
}

func (m *mockRigStore) Close() error { return nil }

type mockTownStore struct {
	mu     sync.Mutex
	agents map[string]*store.Agent
}

func newMockTownStore() *mockTownStore {
	return &mockTownStore{agents: make(map[string]*store.Agent)}
}

func (m *mockTownStore) CreateAgent(name, rig, role string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := rig + "/" + name
	m.agents[id] = &store.Agent{
		ID:    id,
		Name:  name,
		Rig:   rig,
		Role:  role,
		State: "idle",
	}
	return id, nil
}

func (m *mockTownStore) GetAgent(id string) (*store.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %q not found", id)
	}
	return agent, nil
}

func (m *mockTownStore) UpdateAgentState(id, state, hookItem string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	agent, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %q not found", id)
	}
	agent.State = state
	agent.HookItem = hookItem
	return nil
}

func (m *mockTownStore) Close() error { return nil }

// --- Helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- Unit Tests ---

func TestPollClaimsAndProcesses(t *testing.T) {
	rigStore := newMockRigStore()
	rigStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WorkItemID: "gt-aaa11111", Branch: "polecat/Toast/gt-aaa11111", Phase: "ready", Attempts: 0},
	}
	rigStore.items["gt-aaa11111"] = &store.WorkItem{ID: "gt-aaa11111", Title: "Test", Status: "done"}

	townStore := newMockTownStore()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	cfg := DefaultConfig()
	cfg.QualityGates = []string{} // No gates for this test.

	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: "/dev/null",
		worktree:   "/dev/null",
		rigStore:   rigStore,
		townStore:  townStore,
		logger:     testLogger(),
		cfg:        cfg,
	}

	r.poll()

	rigStore.mu.Lock()
	defer rigStore.mu.Unlock()
	if len(rigStore.claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(rigStore.claims))
	}
	if rigStore.claims[0] != "mr-00000001" {
		t.Errorf("claimed MR = %q, want %q", rigStore.claims[0], "mr-00000001")
	}
}

func TestPollReleasesStale(t *testing.T) {
	rigStore := newMockRigStore()
	rigStore.staleReleased = 2

	townStore := newMockTownStore()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	cfg := DefaultConfig()
	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: "/dev/null",
		worktree:   "/dev/null",
		rigStore:   rigStore,
		townStore:  townStore,
		logger:     testLogger(),
		cfg:        cfg,
	}

	r.poll()
	// If staleReleased was consumed without error, test passes.
}

func TestPollSkipsWhenEmpty(t *testing.T) {
	rigStore := newMockRigStore()
	// No MRs.

	townStore := newMockTownStore()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	cfg := DefaultConfig()
	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: "/dev/null",
		worktree:   "/dev/null",
		rigStore:   rigStore,
		townStore:  townStore,
		logger:     testLogger(),
		cfg:        cfg,
	}

	r.poll()

	rigStore.mu.Lock()
	defer rigStore.mu.Unlock()
	if len(rigStore.claims) != 0 {
		t.Fatalf("expected 0 claims, got %d", len(rigStore.claims))
	}
}

func TestMaxAttemptsExceeded(t *testing.T) {
	rigStore := newMockRigStore()
	rigStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WorkItemID: "gt-aaa11111", Branch: "polecat/Toast/gt-aaa11111", Phase: "ready", Attempts: 3},
	}

	townStore := newMockTownStore()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	cfg := DefaultConfig()
	cfg.MaxAttempts = 3 // Attempts will be 4 after claim (3+1), which > 3.

	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: "/dev/null",
		worktree:   "/dev/null",
		rigStore:   rigStore,
		townStore:  townStore,
		logger:     testLogger(),
		cfg:        cfg,
	}

	r.poll()

	rigStore.mu.Lock()
	defer rigStore.mu.Unlock()
	if phase, ok := rigStore.phaseUpdates["mr-00000001"]; !ok || phase != "failed" {
		t.Errorf("expected MR phase to be 'failed', got %q (ok=%v)", phase, ok)
	}
}

func TestRegisterAgent(t *testing.T) {
	townStore := newMockTownStore()

	r := &Refinery{
		rig:       "testrig",
		agentID:   "testrig/refinery",
		townStore: townStore,
		logger:    testLogger(),
	}

	if err := r.registerAgent(); err != nil {
		t.Fatalf("registerAgent() error: %v", err)
	}

	townStore.mu.Lock()
	defer townStore.mu.Unlock()
	agent, ok := townStore.agents["testrig/refinery"]
	if !ok {
		t.Fatal("agent not created")
	}
	if agent.Role != "refinery" {
		t.Errorf("agent role = %q, want %q", agent.Role, "refinery")
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestRegisterAgentIdempotent(t *testing.T) {
	townStore := newMockTownStore()
	townStore.agents["testrig/refinery"] = &store.Agent{
		ID:    "testrig/refinery",
		Name:  "refinery",
		Rig:   "testrig",
		Role:  "refinery",
		State: "idle",
	}

	r := &Refinery{
		rig:       "testrig",
		agentID:   "testrig/refinery",
		townStore: townStore,
		logger:    testLogger(),
	}

	if err := r.registerAgent(); err != nil {
		t.Fatalf("registerAgent() error: %v", err)
	}

	townStore.mu.Lock()
	defer townStore.mu.Unlock()
	agent := townStore.agents["testrig/refinery"]
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}
}

func TestShutdown(t *testing.T) {
	townStore := newMockTownStore()
	townStore.agents["testrig/refinery"] = &store.Agent{
		ID:    "testrig/refinery",
		Name:  "refinery",
		Rig:   "testrig",
		Role:  "refinery",
		State: "working",
	}

	r := &Refinery{
		rig:       "testrig",
		agentID:   "testrig/refinery",
		townStore: townStore,
		logger:    testLogger(),
	}

	if err := r.shutdown(); err != nil {
		t.Fatalf("shutdown() error: %v", err)
	}

	townStore.mu.Lock()
	defer townStore.mu.Unlock()
	agent := townStore.agents["testrig/refinery"]
	if agent.State != "idle" {
		t.Errorf("agent state = %q, want %q", agent.State, "idle")
	}
}

func TestLoadQualityGates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quality-gates.txt")
	defaults := []string{"go test ./..."}

	// Write a file with commands, comments, and blanks.
	content := `# Quality gates for this rig
go test ./...

go vet ./...
# Another comment
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	gates, err := LoadQualityGates(path, defaults)
	if err != nil {
		t.Fatalf("LoadQualityGates() error: %v", err)
	}
	if len(gates) != 2 {
		t.Fatalf("expected 2 gates, got %d: %v", len(gates), gates)
	}
	if gates[0] != "go test ./..." {
		t.Errorf("gate[0] = %q, want %q", gates[0], "go test ./...")
	}
	if gates[1] != "go vet ./..." {
		t.Errorf("gate[1] = %q, want %q", gates[1], "go vet ./...")
	}
}

func TestLoadQualityGatesDefaults(t *testing.T) {
	defaults := []string{"go test ./..."}

	gates, err := LoadQualityGates("/nonexistent/path", defaults)
	if err != nil {
		t.Fatalf("LoadQualityGates() error: %v", err)
	}
	if len(gates) != 1 || gates[0] != "go test ./..." {
		t.Errorf("expected defaults, got %v", gates)
	}
}

// --- Git Operations Tests ---

func setupGitTest(t *testing.T) (sourceRepo, worktree string) {
	t.Helper()

	dir := t.TempDir()

	// Create a bare repo.
	bareDir := filepath.Join(dir, "origin.git")
	run(t, "git", "init", "--bare", bareDir)

	// Clone it.
	workDir := filepath.Join(dir, "work")
	run(t, "git", "clone", bareDir, workDir)

	// Make initial commit on main.
	run(t, "git", "-C", workDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", workDir, "config", "user.name", "Test")
	mainFile := filepath.Join(workDir, "main.go")
	os.WriteFile(mainFile, []byte("package main\n"), 0o644)
	run(t, "git", "-C", workDir, "add", ".")
	run(t, "git", "-C", workDir, "commit", "-m", "init")
	run(t, "git", "-C", workDir, "push", "origin", "main")

	return workDir, filepath.Join(dir, "refinery-worktree")
}

func createBranchWithChanges(t *testing.T, repoDir, branch, filename, content string) {
	t.Helper()
	run(t, "git", "-C", repoDir, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(repoDir, filename), []byte(content), 0o644)
	run(t, "git", "-C", repoDir, "add", ".")
	run(t, "git", "-C", repoDir, "commit", "-m", "changes on "+branch)
	run(t, "git", "-C", repoDir, "push", "origin", branch)
	run(t, "git", "-C", repoDir, "checkout", "main")
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %s: %v", name, strings.Join(args, " "),
			strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}

func TestProcessMergeSuccess(t *testing.T) {
	sourceRepo, worktreeDir := setupGitTest(t)

	// Create a feature branch with changes.
	createBranchWithChanges(t, sourceRepo, "polecat/Toast/gt-aaa11111", "feature.go", "package main\n// feature\n")

	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	rigStore := newMockRigStore()
	rigStore.items["gt-aaa11111"] = &store.WorkItem{ID: "gt-aaa11111", Title: "Test", Status: "done"}

	// Create refinery worktree.
	branch := "refinery/testrig"
	run(t, "git", "-C", sourceRepo, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	run(t, "git", "-C", worktreeDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", worktreeDir, "config", "user.name", "Test")

	cfg := DefaultConfig()
	cfg.QualityGates = []string{"true"} // Always passes.

	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: sourceRepo,
		worktree:   worktreeDir,
		rigStore:   rigStore,
		townStore:  newMockTownStore(),
		logger:     testLogger(),
		cfg:        cfg,
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000001",
		WorkItemID: "gt-aaa11111",
		Branch:     "polecat/Toast/gt-aaa11111",
		Phase:      "claimed",
		Attempts:   1,
	}

	if err := r.processMerge(mr); err != nil {
		t.Fatalf("processMerge() error: %v", err)
	}

	// Verify MR phase is merged.
	rigStore.mu.Lock()
	phase := rigStore.phaseUpdates["mr-00000001"]
	rigStore.mu.Unlock()
	if phase != "merged" {
		t.Errorf("MR phase = %q, want %q", phase, "merged")
	}

	// Verify work item is closed.
	rigStore.mu.Lock()
	item := rigStore.items["gt-aaa11111"]
	rigStore.mu.Unlock()
	if item.Status != "closed" {
		t.Errorf("work item status = %q, want %q", item.Status, "closed")
	}

	// Verify the commit is on main.
	out := run(t, "git", "-C", sourceRepo, "log", "--oneline", "origin/main")
	if !strings.Contains(out, "changes on polecat/Toast/gt-aaa11111") {
		t.Errorf("main branch should contain the merged commit, got:\n%s", out)
	}
}

func TestProcessMergeConflict(t *testing.T) {
	sourceRepo, worktreeDir := setupGitTest(t)

	// Create two branches that modify the same file differently.
	createBranchWithChanges(t, sourceRepo, "polecat/Toast/gt-aaa11111", "conflict.go", "package main\n// version A\n")

	// Merge first branch to main directly.
	run(t, "git", "-C", sourceRepo, "merge", "polecat/Toast/gt-aaa11111")
	run(t, "git", "-C", sourceRepo, "push", "origin", "main")

	// Create second branch from old main (before merge) with conflicting changes.
	run(t, "git", "-C", sourceRepo, "checkout", "-b", "polecat/Jasper/gt-bbb22222", "HEAD~1")
	os.WriteFile(filepath.Join(sourceRepo, "conflict.go"), []byte("package main\n// version B\n"), 0o644)
	run(t, "git", "-C", sourceRepo, "add", ".")
	run(t, "git", "-C", sourceRepo, "commit", "-m", "conflicting change")
	run(t, "git", "-C", sourceRepo, "push", "origin", "polecat/Jasper/gt-bbb22222")
	run(t, "git", "-C", sourceRepo, "checkout", "main")

	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	rigStore := newMockRigStore()

	// Create refinery worktree.
	branch := "refinery/testrig"
	run(t, "git", "-C", sourceRepo, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	run(t, "git", "-C", worktreeDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", worktreeDir, "config", "user.name", "Test")

	cfg := DefaultConfig()
	cfg.QualityGates = []string{"true"}

	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: sourceRepo,
		worktree:   worktreeDir,
		rigStore:   rigStore,
		townStore:  newMockTownStore(),
		logger:     testLogger(),
		cfg:        cfg,
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000002",
		WorkItemID: "gt-bbb22222",
		Branch:     "polecat/Jasper/gt-bbb22222",
		Phase:      "claimed",
		Attempts:   1,
	}

	if err := r.processMerge(mr); err != nil {
		t.Fatalf("processMerge() error: %v", err)
	}

	// Verify MR phase is failed (conflict).
	rigStore.mu.Lock()
	phase := rigStore.phaseUpdates["mr-00000002"]
	rigStore.mu.Unlock()
	if phase != "failed" {
		t.Errorf("MR phase = %q, want %q", phase, "failed")
	}
}

func TestProcessMergeQualityGateFail(t *testing.T) {
	sourceRepo, worktreeDir := setupGitTest(t)

	createBranchWithChanges(t, sourceRepo, "polecat/Toast/gt-ccc33333", "feature2.go", "package main\n// feature2\n")

	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	rigStore := newMockRigStore()

	branch := "refinery/testrig"
	run(t, "git", "-C", sourceRepo, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	run(t, "git", "-C", worktreeDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", worktreeDir, "config", "user.name", "Test")

	cfg := DefaultConfig()
	cfg.QualityGates = []string{"exit 1"} // Always fails.

	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: sourceRepo,
		worktree:   worktreeDir,
		rigStore:   rigStore,
		townStore:  newMockTownStore(),
		logger:     testLogger(),
		cfg:        cfg,
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000003",
		WorkItemID: "gt-ccc33333",
		Branch:     "polecat/Toast/gt-ccc33333",
		Phase:      "claimed",
		Attempts:   1,
	}

	if err := r.processMerge(mr); err != nil {
		t.Fatalf("processMerge() error: %v", err)
	}

	// Verify MR phase is ready (will retry).
	rigStore.mu.Lock()
	phase := rigStore.phaseUpdates["mr-00000003"]
	rigStore.mu.Unlock()
	if phase != "ready" {
		t.Errorf("MR phase = %q, want %q", phase, "ready")
	}
}

func TestProcessMergePushRejected(t *testing.T) {
	sourceRepo, worktreeDir := setupGitTest(t)

	createBranchWithChanges(t, sourceRepo, "polecat/Toast/gt-ddd44444", "feature3.go", "package main\n// feature3\n")

	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	rigStore := newMockRigStore()

	branch := "refinery/testrig"
	run(t, "git", "-C", sourceRepo, "worktree", "add", "-b", branch, worktreeDir, "HEAD")
	run(t, "git", "-C", worktreeDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", worktreeDir, "config", "user.name", "Test")

	cfg := DefaultConfig()
	// Quality gate that pushes a competing commit to main before returning success.
	// This simulates someone pushing to main between test and push.
	// We use a separate clone to avoid worktree conflicts.
	competingDir := filepath.Join(dir, "competing-clone")
	// Get the bare repo URL from the source repo's remote.
	originURL := run(t, "git", "-C", sourceRepo, "remote", "get-url", "origin")
	run(t, "git", "clone", originURL, competingDir)
	run(t, "git", "-C", competingDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", competingDir, "config", "user.name", "Test")
	gateScript := fmt.Sprintf(
		`cd %s && echo "// competing" >> main.go && git add . && git commit -m "competing" && git push origin main`,
		competingDir,
	)
	cfg.QualityGates = []string{gateScript}

	r := &Refinery{
		rig:        "testrig",
		agentID:    "testrig/refinery",
		sourceRepo: sourceRepo,
		worktree:   worktreeDir,
		rigStore:   rigStore,
		townStore:  newMockTownStore(),
		logger:     testLogger(),
		cfg:        cfg,
	}

	mr := &store.MergeRequest{
		ID:         "mr-00000004",
		WorkItemID: "gt-ddd44444",
		Branch:     "polecat/Toast/gt-ddd44444",
		Phase:      "claimed",
		Attempts:   1,
	}

	if err := r.processMerge(mr); err != nil {
		t.Fatalf("processMerge() error: %v", err)
	}

	// Verify MR phase is ready (push rejected, will retry).
	rigStore.mu.Lock()
	phase := rigStore.phaseUpdates["mr-00000004"]
	rigStore.mu.Unlock()
	if phase != "ready" {
		t.Errorf("MR phase = %q, want %q", phase, "ready")
	}
}

func TestEnsureWorktreeCreatesNew(t *testing.T) {
	sourceRepo, worktreeDir := setupGitTest(t)

	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)

	// Set worktree to a path inside GT_HOME.
	wtPath := filepath.Join(dir, "testrig", "refinery", "rig")

	r := &Refinery{
		rig:        "testrig",
		sourceRepo: sourceRepo,
		worktree:   wtPath,
		logger:     testLogger(),
	}

	if err := r.EnsureWorktree(); err != nil {
		t.Fatalf("ensureWorktree() error: %v", err)
	}

	// Verify worktree is valid.
	out := run(t, "git", "-C", wtPath, "rev-parse", "--is-inside-work-tree")
	if out != "true" {
		t.Errorf("expected worktree to be valid, got: %s", out)
	}

	// Calling again should be idempotent.
	if err := r.EnsureWorktree(); err != nil {
		t.Fatalf("ensureWorktree() second call error: %v", err)
	}

	_ = worktreeDir // unused in this variant
}

func TestRunLifecycle(t *testing.T) {
	sourceRepo, _ := setupGitTest(t)

	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".runtime", "locks"), 0o755)

	rigStore := newMockRigStore()
	townStore := newMockTownStore()

	wtPath := filepath.Join(dir, "testrig", "refinery", "rig")

	cfg := DefaultConfig()
	cfg.PollInterval = 50 * time.Millisecond

	r := New("testrig", sourceRepo, rigStore, townStore, cfg, testLogger())
	r.worktree = wtPath

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	if err := <-done; err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Agent should be idle after shutdown.
	townStore.mu.Lock()
	agent := townStore.agents["testrig/refinery"]
	townStore.mu.Unlock()
	if agent == nil {
		t.Fatal("refinery agent not found in town store")
	}
	if agent.State != "idle" {
		t.Errorf("agent state after shutdown = %q, want %q", agent.State, "idle")
	}
}

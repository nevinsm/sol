package forge

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// --- Mock stores ---

type mockWorldStore struct {
	mu              sync.Mutex
	mrs             []store.MergeRequest
	items           map[string]*store.Writ
	claims          []string // IDs of claimed MRs
	phaseUpdates    map[string]string
	staleReleased   int
	closeWritErr    error // inject CloseWrit failure
	updateWritErr   error // inject UpdateWrit failure
}

func newMockWorldStore() *mockWorldStore {
	return &mockWorldStore{
		items:        make(map[string]*store.Writ),
		phaseUpdates: make(map[string]string),
	}
}

func (m *mockWorldStore) GetMergeRequest(id string) (*store.MergeRequest, error) {
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

func (m *mockWorldStore) ClaimMergeRequest(claimerID string) (*store.MergeRequest, error) {
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

func (m *mockWorldStore) UpdateMergeRequestPhase(id, phase string) error {
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

func (m *mockWorldStore) ReleaseStaleClaims(ttl time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := m.staleReleased
	m.staleReleased = 0
	return count, nil
}

func (m *mockWorldStore) GetWrit(id string) (*store.Writ, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return nil, fmt.Errorf("writ %q not found", id)
	}
	return item, nil
}

func (m *mockWorldStore) UpdateWrit(id string, updates store.WritUpdates) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateWritErr != nil {
		return m.updateWritErr
	}
	item, ok := m.items[id]
	if !ok {
		return fmt.Errorf("writ %q not found", id)
	}
	if updates.Status != "" {
		item.Status = updates.Status
	}
	if updates.Assignee == "-" {
		item.Assignee = ""
	}
	return nil
}

func (m *mockWorldStore) ListMergeRequests(phase string) ([]store.MergeRequest, error) {
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

func (m *mockWorldStore) ListMergeRequestsByWrit(writID, phase string) ([]store.MergeRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []store.MergeRequest
	for _, mr := range m.mrs {
		if mr.WritID != writID {
			continue
		}
		if phase != "" && mr.Phase != phase {
			continue
		}
		result = append(result, mr)
	}
	return result, nil
}

func (m *mockWorldStore) BlockMergeRequest(mrID, blockerID string) error {
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

func (m *mockWorldStore) UnblockMergeRequest(mrID string) error {
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

func (m *mockWorldStore) FindMergeRequestByBlocker(blockerID string) (*store.MergeRequest, error) {
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

func (m *mockWorldStore) CreateWritWithOpts(opts store.CreateWritOpts) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := fmt.Sprintf("sol-%08x", len(m.items))
	m.items[id] = &store.Writ{
		ID:          id,
		Title:       opts.Title,
		Description: opts.Description,
		Status:      "open",
		Priority:    opts.Priority,
		ParentID:    opts.ParentID,
		CreatedBy:   opts.CreatedBy,
		Labels:      opts.Labels,
	}
	return id, nil
}

func (m *mockWorldStore) CloseWrit(id string, closeReason ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closeWritErr != nil {
		return m.closeWritErr
	}
	item, ok := m.items[id]
	if !ok {
		return fmt.Errorf("writ %q not found", id)
	}
	item.Status = "closed"
	if len(closeReason) > 0 {
		item.CloseReason = closeReason[0]
	}
	return nil
}

func (m *mockWorldStore) Close() error { return nil }

type mockEscalation struct {
	id          string
	severity    string
	source      string
	description string
	sourceRef   string
	status      string
}

type mockSphereStore struct {
	mu          sync.Mutex
	escalations []mockEscalation
}

func newMockSphereStore() *mockSphereStore {
	return &mockSphereStore{}
}

func (m *mockSphereStore) CreateEscalation(severity, source, description string, sourceRef ...string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := fmt.Sprintf("esc-%08x", len(m.escalations)+1)
	ref := ""
	if len(sourceRef) > 0 {
		ref = sourceRef[0]
	}
	m.escalations = append(m.escalations, mockEscalation{id: id, severity: severity, source: source, description: description, sourceRef: ref, status: "open"})
	return id, nil
}

func (m *mockSphereStore) ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []store.Escalation
	for _, e := range m.escalations {
		if e.status == "resolved" {
			continue
		}
		if e.sourceRef == sourceRef {
			result = append(result, store.Escalation{
				ID:          e.id,
				Severity:    e.severity,
				Source:      e.source,
				Description: e.description,
				SourceRef:   e.sourceRef,
				Status:      e.status,
			})
		}
	}
	return result, nil
}

func (m *mockSphereStore) ResolveEscalation(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.escalations {
		if m.escalations[i].id == id {
			m.escalations[i].status = "resolved"
			return nil
		}
	}
	return fmt.Errorf("escalation %q not found", id)
}

func (m *mockSphereStore) IsWritBlockedByCaravanDeps(writID string) (bool, []string, error) {
	return false, nil, nil
}

func (m *mockSphereStore) Close() error { return nil }

// --- Helpers ---

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- Git Helpers ---

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

	return workDir, filepath.Join(dir, "forge-worktree")
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

// --- Unit Tests ---

func TestLoadQualityGates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quality-gates.txt")
	defaults := []string{"go test ./..."}

	// Write a file with commands, comments, and blanks.
	content := `# Quality gates for this world
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

func TestEnsureWorktreeCreatesNew(t *testing.T) {
	sourceRepo, _ := setupGitTest(t)

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Set worktree to a path inside SOL_HOME.
	wtPath := filepath.Join(dir, "ember", "forge", "world")

	r := &Forge{
		world:      "ember",
		sourceRepo: sourceRepo,
		worktree:   wtPath,
		logger:     testLogger(),
		cfg:        DefaultConfig(),
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
}

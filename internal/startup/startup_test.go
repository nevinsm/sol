package startup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/workflow"
)

// mockSessionStarter records Start calls.
type mockSessionStarter struct {
	started []startCall
}

type startCall struct {
	Name    string
	Workdir string
	Cmd     string
	Env     map[string]string
	Role    string
	World   string
}

func (m *mockSessionStarter) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started = append(m.started, startCall{name, workdir, cmd, env, role, world})
	return nil
}

// setupTestEnv creates a minimal SOL_HOME with a world config and sphere DB.
func setupTestEnv(t *testing.T, world string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")

	// Create required dirs.
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755)

	// Create world config.
	worldDir := filepath.Join(dir, world)
	os.MkdirAll(worldDir, 0o755)
	worldToml := filepath.Join(worldDir, "world.toml")
	os.WriteFile(worldToml, []byte(`[world]
source_repo = "/tmp/fakerepo"
`), 0o644)

	return dir
}

func TestRegisterAndConfigFor(t *testing.T) {
	// Reset registry for test isolation.
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	cfg := RoleConfig{
		Role:    "testrole",
		Formula: "test-formula",
	}
	Register("testrole", cfg)

	got := ConfigFor("testrole")
	if got == nil {
		t.Fatal("ConfigFor returned nil for registered role")
	}
	if got.Role != "testrole" {
		t.Errorf("Role = %q, want %q", got.Role, "testrole")
	}
	if got.Formula != "test-formula" {
		t.Errorf("Formula = %q, want %q", got.Formula, "test-formula")
	}

	// Unregistered role returns nil.
	if got := ConfigFor("nonexistent"); got != nil {
		t.Errorf("ConfigFor(nonexistent) = %v, want nil", got)
	}
}

func TestLaunchBasic(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Open sphere store for the test.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		Persona: func(w, _ string) ([]byte, error) {
			return []byte("# Test Forge Persona"), nil
		},
		Hooks: func(w, a string) HookSet {
			return protocol.HookConfig{
				Hooks: map[string][]protocol.HookMatcherGroup{
					"SessionStart": {
						{
							Hooks: []protocol.HookHandler{
								{Type: "command", Command: "echo test"},
							},
						},
					},
				},
			}
		},
		PrimeBuilder: func(w, a string) string {
			return "Execute your formula."
		},
	}

	opts := LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	}

	sessName, err := Launch(cfg, world, "forge", opts)
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	if sessName != "sol-haven-forge" {
		t.Errorf("session name = %q, want %q", sessName, "sol-haven-forge")
	}

	// Verify session was started.
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
	call := mock.started[0]
	if call.Name != "sol-haven-forge" {
		t.Errorf("started session name = %q, want %q", call.Name, "sol-haven-forge")
	}
	if call.Role != "forge" {
		t.Errorf("started role = %q, want %q", call.Role, "forge")
	}
	if call.World != "haven" {
		t.Errorf("started world = %q, want %q", call.World, "haven")
	}

	// Verify persona was written.
	personaPath := filepath.Join(worktreeDir, "CLAUDE.local.md")
	data, err := os.ReadFile(personaPath)
	if err != nil {
		t.Fatalf("persona not written: %v", err)
	}
	if string(data) != "# Test Forge Persona" {
		t.Errorf("persona content = %q, want %q", string(data), "# Test Forge Persona")
	}

	// Verify hooks were written.
	hooksPath := filepath.Join(worktreeDir, ".claude", "settings.local.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Fatalf("hooks not written: %v", err)
	}

	// Verify agent was registered.
	agent, err := sphereStore.GetAgent("haven/forge")
	if err != nil {
		t.Fatalf("agent not registered: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("agent state = %q, want %q", agent.State, "working")
	}

	// Verify env includes CLAUDE_CONFIG_DIR.
	if call.Env["CLAUDE_CONFIG_DIR"] == "" {
		t.Error("CLAUDE_CONFIG_DIR not set in env")
	}
	if call.Env["SOL_HOME"] != solHome {
		t.Errorf("SOL_HOME = %q, want %q", call.Env["SOL_HOME"], solHome)
	}
}

func TestLaunchMissingWorktree(t *testing.T) {
	setupTestEnv(t, "haven")

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return "/nonexistent/path" },
	}

	_, err := Launch(cfg, "haven", "forge", LaunchOpts{})
	if err == nil {
		t.Fatal("expected error for missing worktree")
	}
	if !strings.Contains(err.Error(), "worktree directory does not exist") {
		t.Errorf("error = %q, want it to mention worktree", err.Error())
	}
}

func TestLaunchNilWorktreeDir(t *testing.T) {
	cfg := RoleConfig{
		Role: "forge",
	}

	_, err := Launch(cfg, "haven", "forge", LaunchOpts{})
	if err == nil {
		t.Fatal("expected error for nil WorktreeDir")
	}
	if !strings.Contains(err.Error(), "worktree dir is required") {
		t.Errorf("error = %q, want it to mention worktree dir", err.Error())
	}
}

func TestBuildCommand(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")

	cfg := RoleConfig{
		Role: "forge",
	}

	cmd := buildCommand(cfg, "/tmp/worktree", "Hello forge", false)
	if !strings.Contains(cmd, "claude --dangerously-skip-permissions") {
		t.Errorf("command missing claude: %q", cmd)
	}
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("command missing --settings: %q", cmd)
	}
	if !strings.Contains(cmd, "Hello forge") {
		t.Errorf("command missing prompt: %q", cmd)
	}
}

func TestBuildCommandWithSystemPrompt(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")

	cfg := RoleConfig{
		Role:             "forge",
		SystemPromptFile: "prompts/forge.md",
		ReplacePrompt:    true,
	}

	cmd := buildCommand(cfg, "/tmp/worktree", "Hello forge", false)
	if !strings.Contains(cmd, "--system-prompt-file") {
		t.Errorf("command missing --system-prompt-file: %q", cmd)
	}
	if !strings.Contains(cmd, "prompts/forge.md") {
		t.Errorf("command missing prompt file path: %q", cmd)
	}
}

func TestBuildCommandAppendSystemPrompt(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")

	cfg := RoleConfig{
		Role:             "agent",
		SystemPromptFile: "prompts/agent.md",
		ReplacePrompt:    false,
	}

	cmd := buildCommand(cfg, "/tmp/worktree", "Hello agent", false)
	if !strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("command missing --append-system-prompt-file: %q", cmd)
	}
}

func TestBuildCommandContinue(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")

	cfg := RoleConfig{Role: "forge"}

	cmd := buildCommand(cfg, "/tmp/worktree", "Hello", true)
	if !strings.Contains(cmd, "--continue") {
		t.Errorf("command missing --continue: %q", cmd)
	}
}

func TestBuildCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")

	cfg := RoleConfig{
		Role:             "forge",
		SystemPromptFile: "prompts/forge.md",
		ReplacePrompt:    true,
	}

	cmd := buildCommand(cfg, "/tmp/worktree", "Hello", false)
	if cmd != "sleep 300" {
		t.Errorf("expected override, got %q", cmd)
	}
}

func TestResumeSetsContinue(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		PrimeBuilder: func(w, a string) string {
			return "Execute your formula."
		},
	}

	state := ResumeState{
		CurrentStep:     "gates",
		StepDescription: "Quality Gates",
		ClaimedResource: "sol-abc123",
		Reason:          "compact",
	}

	sessName, err := Resume(cfg, world, "forge", state, LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if sessName != "sol-haven-forge" {
		t.Errorf("session name = %q, want %q", sessName, "sol-haven-forge")
	}

	// Resume always uses SOL_SESSION_COMMAND in tests, so we verify
	// the session was started. The --continue flag is set on LaunchOpts.Continue
	// which is verified by TestBuildCommandContinue.
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
}

func TestResumeWithWorkflowStep(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")

	state := ResumeState{
		CurrentStep:     "gates",
		StepDescription: "Quality Gates",
		Reason:          "compact",
	}

	prime := buildResumePrime("", state)
	if !strings.Contains(prime, "[RESUME]") {
		t.Errorf("resume prime missing [RESUME] tag: %q", prime)
	}
	if !strings.Contains(prime, "reason: compact") {
		t.Errorf("resume prime missing reason: %q", prime)
	}
	if !strings.Contains(prime, "step gates (Quality Gates)") {
		t.Errorf("resume prime missing step info: %q", prime)
	}
}

func TestResumeWithClaimedResource(t *testing.T) {
	state := ResumeState{
		ClaimedResource: "sol-abc123def456",
		Reason:          "compact",
	}

	prime := buildResumePrime("", state)
	if !strings.Contains(prime, "sol-abc123def456") {
		t.Errorf("resume prime missing claimed resource: %q", prime)
	}
	if !strings.Contains(prime, "claimed and in-progress") {
		t.Errorf("resume prime missing in-progress indicator: %q", prime)
	}
}

func TestResumePreservesBasePrime(t *testing.T) {
	state := ResumeState{
		CurrentStep: "scan",
		Reason:      "compact",
	}

	base := "Execute your formula."
	prime := buildResumePrime(base, state)
	if !strings.Contains(prime, "[RESUME]") {
		t.Errorf("resume prime missing [RESUME]: %q", prime)
	}
	if !strings.Contains(prime, "Execute your formula.") {
		t.Errorf("resume prime missing base prime: %q", prime)
	}
}

func TestResumeNilPrimeBuilder(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		// PrimeBuilder intentionally nil.
	}

	state := ResumeState{
		CurrentStep: "scan",
		Reason:      "compact",
	}

	_, err = Resume(cfg, world, "forge", state, LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Resume() with nil PrimeBuilder error: %v", err)
	}
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
}

func TestBuildResumePrimeStepOnly(t *testing.T) {
	state := ResumeState{
		CurrentStep: "isolate",
		Reason:      "manual",
	}

	prime := buildResumePrime("", state)
	if !strings.Contains(prime, "step isolate. Resume from there.") {
		t.Errorf("step-only prime missing step without description: %q", prime)
	}
	// Should not contain step description parentheses (reason parens are expected).
	if strings.Contains(prime, "step isolate (") {
		t.Errorf("step-only prime should not have step description parens: %q", prime)
	}
}

func TestBuildResumePrimeEmpty(t *testing.T) {
	state := ResumeState{Reason: "compact"}
	prime := buildResumePrime("", state)
	if !strings.Contains(prime, "[RESUME] Session recovery (reason: compact).") {
		t.Errorf("empty resume prime = %q", prime)
	}
	// Should not contain step or resource lines.
	if strings.Contains(prime, "step") {
		t.Errorf("empty resume prime should not mention step: %q", prime)
	}
	if strings.Contains(prime, "Claimed") {
		t.Errorf("empty resume prime should not mention claimed: %q", prime)
	}
}

func TestLaunchPreservesTetherItem(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, world, "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Open sphere store and pre-create agent with tether item.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	sphereStore.CreateAgent("Toast", "haven", "agent")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "agent",
		WorktreeDir: func(w, a string) string { return filepath.Join(solHome, w, "outposts", a, "worktree") },
		Persona:     func(w, a string) ([]byte, error) { return []byte("# Test Agent"), nil },
		PrimeBuilder: func(w, a string) string { return "Agent " + a },
	}

	_, err = Launch(cfg, world, "Toast", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	// Verify tether item was preserved.
	agent, err := sphereStore.GetAgent("haven/Toast")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.TetherItem != "sol-abc12345" {
		t.Errorf("tether_item = %q, want %q (not preserved)", agent.TetherItem, "sol-abc12345")
	}
	if agent.State != "working" {
		t.Errorf("state = %q, want %q", agent.State, "working")
	}
}

func TestLaunchSystemPromptFullReplace(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	t.Setenv("SOL_SESSION_COMMAND", "") // override setupTestEnv's sleep 300
	world := "haven"

	worktreeDir := filepath.Join(solHome, world, "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:                "agent",
		WorktreeDir:         func(w, a string) string { return filepath.Join(solHome, w, "outposts", a, "worktree") },
		Persona:             func(w, a string) ([]byte, error) { return []byte("# Test Agent"), nil },
		SystemPromptContent: "# Outpost System Prompt\nYou are an outpost agent.",
		ReplacePrompt:       true,
		PrimeBuilder:        func(w, a string) string { return "Agent " + a },
	}

	_, err = Launch(cfg, world, "Toast", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	// Verify system prompt file was written.
	promptPath := filepath.Join(worktreeDir, ".claude", "system-prompt.md")
	data, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("system prompt not written: %v", err)
	}
	if !strings.Contains(string(data), "Outpost System Prompt") {
		t.Errorf("system prompt content = %q, missing expected content", string(data))
	}

	// Verify command uses --system-prompt-file (not --append-system-prompt-file).
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 start, got %d", len(mock.started))
	}
	cmd := mock.started[0].Cmd
	if !strings.Contains(cmd, "--system-prompt-file") {
		t.Errorf("command missing --system-prompt-file: %q", cmd)
	}
	if strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("command should use --system-prompt-file not --append: %q", cmd)
	}
}

func TestWriteReadClearResumeState(t *testing.T) {
	solHome := setupTestEnv(t, "haven")

	// Create agent dir for the forge role.
	agentDir := filepath.Join(solHome, "haven", "forge")
	os.MkdirAll(agentDir, 0o755)

	state := ResumeState{
		CurrentStep:     "gates",
		StepDescription: "Quality Gates",
		ClaimedResource: "sol-abc123",
		Reason:          "compact",
	}

	// Write.
	if err := WriteResumeState("haven", "forge", "forge", state); err != nil {
		t.Fatalf("WriteResumeState() error: %v", err)
	}

	// Verify file exists.
	p := resumeStatePath("haven", "forge", "forge")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("resume state file not created: %v", err)
	}

	// Read.
	got, err := ReadResumeState("haven", "forge", "forge")
	if err != nil {
		t.Fatalf("ReadResumeState() error: %v", err)
	}
	if got == nil {
		t.Fatal("ReadResumeState() returned nil")
	}
	if got.CurrentStep != "gates" {
		t.Errorf("CurrentStep = %q, want %q", got.CurrentStep, "gates")
	}
	if got.StepDescription != "Quality Gates" {
		t.Errorf("StepDescription = %q, want %q", got.StepDescription, "Quality Gates")
	}
	if got.ClaimedResource != "sol-abc123" {
		t.Errorf("ClaimedResource = %q, want %q", got.ClaimedResource, "sol-abc123")
	}
	if got.Reason != "compact" {
		t.Errorf("Reason = %q, want %q", got.Reason, "compact")
	}

	// Clear.
	if err := ClearResumeState("haven", "forge", "forge"); err != nil {
		t.Fatalf("ClearResumeState() error: %v", err)
	}

	// Read after clear should return nil.
	got, err = ReadResumeState("haven", "forge", "forge")
	if err != nil {
		t.Fatalf("ReadResumeState() after clear error: %v", err)
	}
	if got != nil {
		t.Error("ReadResumeState() after clear should return nil")
	}
}

func TestReadResumeStateNotFound(t *testing.T) {
	setupTestEnv(t, "haven")

	got, err := ReadResumeState("haven", "forge", "forge")
	if err != nil {
		t.Fatalf("ReadResumeState() error: %v", err)
	}
	if got != nil {
		t.Error("ReadResumeState() for missing file should return nil")
	}
}

func TestClearResumeStateIdempotent(t *testing.T) {
	setupTestEnv(t, "haven")

	// Clearing a non-existent file should not error.
	if err := ClearResumeState("haven", "forge", "forge"); err != nil {
		t.Fatalf("ClearResumeState() for missing file error: %v", err)
	}
}

func TestLaunchReinstantiatesDoneWorkflow(t *testing.T) {
	solHome := setupTestEnv(t, "haven")

	// Create worktree.
	worktreeDir := filepath.Join(solHome, "haven", "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Create an existing workflow with status "done" (simulate completed patrol).
	wfDir := filepath.Join(solHome, "haven", "forge", ".workflow")
	os.MkdirAll(wfDir, 0o755)
	os.WriteFile(filepath.Join(wfDir, "state.json"), []byte(`{"current_step":"","completed":["scan","claim"],"status":"done","started_at":"2025-01-01T00:00:00Z"}`), 0o644)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		Formula:     "forge-patrol", // Real embedded formula; requires only "world" var.
	}

	_, err = Launch(cfg, "haven", "forge", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	// Verify workflow was re-instantiated (state.json should have status "running").
	state, err := workflow.ReadState("haven", "forge", "forge")
	if err != nil {
		t.Fatalf("ReadState() error: %v", err)
	}
	if state == nil {
		t.Fatal("workflow state should exist after re-instantiation")
	}
	if state.Status != "running" {
		t.Errorf("workflow status = %q, want %q", state.Status, "running")
	}
	if state.CurrentStep == "" {
		t.Error("workflow should have a current step after re-instantiation")
	}
}

func TestLaunchSkipsWorkflowIfActive(t *testing.T) {
	solHome := setupTestEnv(t, "haven")

	// Create worktree.
	worktreeDir := filepath.Join(solHome, "haven", "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Create an existing workflow directory (simulate active workflow).
	wfDir := filepath.Join(solHome, "haven", "forge", ".workflow")
	os.MkdirAll(wfDir, 0o755)
	os.WriteFile(filepath.Join(wfDir, "state.json"), []byte(`{"current_step":"scan","completed":[],"status":"running","started_at":"2025-01-01T00:00:00Z"}`), 0o644)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		Formula:     "nonexistent-formula", // Would fail if instantiation were attempted.
	}

	_, err = Launch(cfg, "haven", "forge", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	// If we got here without error, workflow instantiation was skipped.
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
}

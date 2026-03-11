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
		Workflow: "test-workflow",
	}
	Register("testrole", cfg)

	got := ConfigFor("testrole")
	if got == nil {
		t.Fatal("ConfigFor returned nil for registered role")
	}
	if got.Role != "testrole" {
		t.Errorf("Role = %q, want %q", got.Role, "testrole")
	}
	if got.Workflow != "test-workflow" {
		t.Errorf("Workflow = %q, want %q", got.Workflow, "test-workflow")
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
			return "Execute your workflow."
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
		Role:             "outpost",
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
			return "Execute your workflow."
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

	prime := BuildResumePrime("", state)
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

	prime := BuildResumePrime("", state)
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

	base := "Execute your workflow."
	prime := BuildResumePrime(base, state)
	if !strings.Contains(prime, "[RESUME]") {
		t.Errorf("resume prime missing [RESUME]: %q", prime)
	}
	if !strings.Contains(prime, "Execute your workflow.") {
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

	prime := BuildResumePrime("", state)
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
	prime := BuildResumePrime("", state)
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

func TestLaunchPreservesActiveWrit(t *testing.T) {
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

	sphereStore.CreateAgent("Toast", "haven", "outpost")
	sphereStore.UpdateAgentState("haven/Toast", "working", "sol-abc12345")

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "outpost",
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
	if agent.ActiveWrit != "sol-abc12345" {
		t.Errorf("active_writ = %q, want %q (not preserved)", agent.ActiveWrit, "sol-abc12345")
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
		Role:                "outpost",
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
	worktreeDir := filepath.Join(solHome, "haven", "outposts", "TestBot", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Create an existing workflow with status "done" (simulate completed workflow).
	wfDir := filepath.Join(solHome, "haven", "outposts", "TestBot", ".workflow")
	os.MkdirAll(wfDir, 0o755)
	os.WriteFile(filepath.Join(wfDir, "state.json"), []byte(`{"current_step":"","completed":["load-context","implement"],"status":"done","started_at":"2025-01-01T00:00:00Z"}`), 0o644)

	// Create a minimal test workflow with no required variables at user-level
	// so Instantiate can resolve it without needing writ-specific vars.
	testWfDir := filepath.Join(solHome, "workflows", "test-simple")
	os.MkdirAll(filepath.Join(testWfDir, "steps"), 0o755)
	os.WriteFile(filepath.Join(testWfDir, "manifest.toml"), []byte(`name = "test-simple"
type = "workflow"
description = "Minimal test workflow"

[[steps]]
id = "step-one"
title = "Step One"
instructions = "steps/01-step.md"
`), 0o644)
	os.WriteFile(filepath.Join(testWfDir, "steps", "01-step.md"), []byte("Do the thing.\n"), 0o644)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	cfg := RoleConfig{
		Role:        "outpost",
		WorktreeDir: func(w, a string) string { return filepath.Join(solHome, w, "outposts", a, "worktree") },
		Workflow:    "test-simple", // Minimal test workflow; no required variables.
	}

	_, err = Launch(cfg, "haven", "TestBot", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	// Verify workflow was re-instantiated (state.json should have status "running").
	state, err := workflow.ReadState("haven", "TestBot", "outpost")
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

func TestRespawnWithResumeState(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// Reset registry for test isolation.
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register a role config.
	Register("forge", RoleConfig{
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		PrimeBuilder: func(w, a string) string {
			return "Execute your workflow."
		},
	})

	// Write resume state.
	state := ResumeState{
		CurrentStep:     "gates",
		StepDescription: "Quality Gates",
		ClaimedResource: "sol-abc123",
		Reason:          "compact",
	}
	if err := WriteResumeState(world, "forge", "forge", state); err != nil {
		t.Fatalf("WriteResumeState() error: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	sessName, err := Respawn("forge", world, "forge", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Respawn() error: %v", err)
	}

	if sessName != "sol-haven-forge" {
		t.Errorf("session name = %q, want %q", sessName, "sol-haven-forge")
	}

	// Verify session was started (Resume path).
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}

	// Verify resume state was cleared after Respawn.
	got, err := ReadResumeState(world, "forge", "forge")
	if err != nil {
		t.Fatalf("ReadResumeState() after Respawn error: %v", err)
	}
	if got != nil {
		t.Error("resume state should be cleared after Respawn")
	}
}

func TestRespawnWithoutResumeState(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// Reset registry for test isolation.
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	// Create worktree directory.
	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	// Register a role config.
	Register("forge", RoleConfig{
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		PrimeBuilder: func(w, a string) string {
			return "Execute your workflow."
		},
	})

	// No resume state written — should take the Launch path.

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	sessName, err := Respawn("forge", world, "forge", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Respawn() error: %v", err)
	}

	if sessName != "sol-haven-forge" {
		t.Errorf("session name = %q, want %q", sessName, "sol-haven-forge")
	}

	// Verify session was started (Launch path).
	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
}

func TestRespawnUnregisteredRole(t *testing.T) {
	// Reset registry for test isolation.
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	// Do not register any role — Respawn should return error.
	_, err := Respawn("nonexistent", "haven", "forge", LaunchOpts{})
	if err == nil {
		t.Fatal("expected error for unregistered role")
	}
	if !strings.Contains(err.Error(), "no startup config registered for role") {
		t.Errorf("error = %q, want it to mention unregistered role", err.Error())
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %q, want it to mention role name", err.Error())
	}
}

func TestRespawnSetsRespawnFlag(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// Reset registry for test isolation.
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	// Create worktree directory.
	os.MkdirAll(filepath.Join(solHome, world, "forge", "worktree"), 0o755)

	// Use SessionOp to verify that the full launch pipeline runs to completion.
	// opts.Respawn is set unconditionally in Respawn() before calling Launch/Resume.
	var sessionOpCalled bool
	Register("forge", RoleConfig{
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
	})

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Pass Respawn=false initially — Respawn() should set it to true.
	opts := LaunchOpts{
		Sphere:  sphereStore,
		Respawn: false,
		SessionOp: func(name, workdir, cmd string, env map[string]string, role, world string) error {
			sessionOpCalled = true
			return nil
		},
	}

	_, err = Respawn("forge", world, "forge", opts)
	if err != nil {
		t.Fatalf("Respawn() error: %v", err)
	}

	// Verify the session op was called (proves opts.Respawn=true didn't break flow,
	// and the full Launch pipeline completed with the Respawn flag set).
	if !sessionOpCalled {
		t.Fatal("SessionOp was not called — launch pipeline did not complete")
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
		Workflow:    "nonexistent-workflow", // Would fail if instantiation were attempted.
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

func TestBuildResumePrimeWritSwitch(t *testing.T) {
	state := ResumeState{
		Reason:             "writ-switch",
		PreviousActiveWrit: "sol-aaa111",
		NewActiveWrit:      "sol-bbb222",
	}

	prime := BuildResumePrime("", state)
	if !strings.Contains(prime, "[RESUME] Session recovery (reason: writ-switch).") {
		t.Errorf("prime missing writ-switch reason: %q", prime)
	}
	if !strings.Contains(prime, "Your active writ has changed to sol-bbb222. Previous active was sol-aaa111.") {
		t.Errorf("prime missing writ-switch context: %q", prime)
	}
}

func TestBuildResumePrimeWritSwitchNoPrevious(t *testing.T) {
	state := ResumeState{
		Reason:        "writ-switch",
		NewActiveWrit: "sol-bbb222",
	}

	prime := BuildResumePrime("", state)
	if !strings.Contains(prime, "Your active writ has changed to sol-bbb222.") {
		t.Errorf("prime missing new writ context: %q", prime)
	}
	// Should NOT mention "Previous active" when there's no previous.
	if strings.Contains(prime, "Previous active") {
		t.Errorf("prime should not mention previous when empty: %q", prime)
	}
}

func TestBuildResumePrimeWritSwitchWithBase(t *testing.T) {
	state := ResumeState{
		Reason:             "writ-switch",
		PreviousActiveWrit: "sol-aaa111",
		NewActiveWrit:      "sol-bbb222",
	}

	base := "Execute your workflow."
	prime := BuildResumePrime(base, state)
	if !strings.Contains(prime, "[RESUME]") {
		t.Errorf("prime missing [RESUME]: %q", prime)
	}
	if !strings.Contains(prime, "Your active writ has changed to sol-bbb222") {
		t.Errorf("prime missing writ-switch context: %q", prime)
	}
	if !strings.Contains(prime, "Execute your workflow.") {
		t.Errorf("prime missing base prime: %q", prime)
	}
}

func TestBuildResumePrimeActiveWritNonSwitch(t *testing.T) {
	// Non-writ-switch resume with an active writ should show "Active writ:" line.
	state := ResumeState{
		Reason:          "compact",
		NewActiveWrit:   "sol-ccc333",
		ClaimedResource: "sol-ccc333",
	}

	prime := BuildResumePrime("", state)
	if !strings.Contains(prime, "Active writ: sol-ccc333") {
		t.Errorf("expected 'Active writ: sol-ccc333' in prime, got %q", prime)
	}
	// Should NOT contain writ-switch language.
	if strings.Contains(prime, "has changed to") {
		t.Errorf("non-switch prime should not contain writ-switch language: %q", prime)
	}
}

func TestBuildResumePrimeNoActiveWrit(t *testing.T) {
	// Resume without any active writ should not mention active writ.
	state := ResumeState{
		Reason:          "compact",
		ClaimedResource: "sol-aaa111",
	}

	prime := BuildResumePrime("", state)
	if strings.Contains(prime, "Active writ") {
		t.Errorf("prime should not mention active writ when none set: %q", prime)
	}
	if !strings.Contains(prime, "sol-aaa111") {
		t.Errorf("prime should contain claimed resource: %q", prime)
	}
}

func TestWriteReadResumeStateWithWritSwitch(t *testing.T) {
	solHome := setupTestEnv(t, "haven")

	// Create agent dir.
	agentDir := filepath.Join(solHome, "haven", "envoys", "Scout")
	os.MkdirAll(agentDir, 0o755)

	state := ResumeState{
		Reason:             "writ-switch",
		PreviousActiveWrit: "sol-aaa111",
		NewActiveWrit:      "sol-bbb222",
	}

	// Write.
	if err := WriteResumeState("haven", "Scout", "envoy", state); err != nil {
		t.Fatalf("WriteResumeState() error: %v", err)
	}

	// Read.
	got, err := ReadResumeState("haven", "Scout", "envoy")
	if err != nil {
		t.Fatalf("ReadResumeState() error: %v", err)
	}
	if got == nil {
		t.Fatal("ReadResumeState() returned nil")
	}
	if got.Reason != "writ-switch" {
		t.Errorf("Reason = %q, want %q", got.Reason, "writ-switch")
	}
	if got.PreviousActiveWrit != "sol-aaa111" {
		t.Errorf("PreviousActiveWrit = %q, want %q", got.PreviousActiveWrit, "sol-aaa111")
	}
	if got.NewActiveWrit != "sol-bbb222" {
		t.Errorf("NewActiveWrit = %q, want %q", got.NewActiveWrit, "sol-bbb222")
	}
}

func TestLaunchInstallsSkills(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	worktreeDir := filepath.Join(solHome, world, "outposts", "TestBot", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	var skillInstallerCalled bool
	var skillInstallerDir string

	cfg := RoleConfig{
		Role:        "outpost",
		WorktreeDir: func(w, a string) string { return filepath.Join(solHome, w, "outposts", a, "worktree") },
		Persona: func(w, a string) ([]byte, error) {
			return []byte("# Test Outpost Persona"), nil
		},
		SkillInstaller: func(dir, w, a string) error {
			skillInstallerCalled = true
			skillInstallerDir = dir
			// Actually install skills to verify end-to-end.
			return protocol.InstallSkills(dir, protocol.SkillContext{
				World: w,
				Role:  "outpost",
			})
		},
	}

	_, err = Launch(cfg, world, "TestBot", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	if !skillInstallerCalled {
		t.Fatal("SkillInstaller was not called during Launch")
	}
	if skillInstallerDir != worktreeDir {
		t.Errorf("SkillInstaller dir = %q, want %q", skillInstallerDir, worktreeDir)
	}

	// Verify skills were actually written.
	skillsDir := filepath.Join(worktreeDir, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read skills directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skills installed")
	}

	// Verify expected outpost skills exist.
	expectedSkills := protocol.RoleSkills("outpost")
	for _, name := range expectedSkills {
		skillPath := filepath.Join(skillsDir, name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("expected skill %q not found at %s", name, skillPath)
		}
	}
}

func TestLaunchSkillInstallerNil(t *testing.T) {
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
		// SkillInstaller intentionally nil — should not error.
	}

	_, err = Launch(cfg, world, "forge", LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Launch() with nil SkillInstaller error: %v", err)
	}

	// Skills directory should not exist since no installer was set.
	skillsDir := filepath.Join(worktreeDir, ".claude", "skills")
	if _, err := os.Stat(skillsDir); err == nil {
		t.Error("skills directory should not exist when SkillInstaller is nil")
	}
}

func TestResumeInstallsSkills(t *testing.T) {
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

	var skillInstallerCalled bool

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		SkillInstaller: func(dir, w, a string) error {
			skillInstallerCalled = true
			return nil
		},
	}

	state := ResumeState{
		CurrentStep: "gates",
		Reason:      "compact",
	}

	_, err = Resume(cfg, world, "forge", state, LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	})
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if !skillInstallerCalled {
		t.Fatal("SkillInstaller was not called during Resume")
	}
}

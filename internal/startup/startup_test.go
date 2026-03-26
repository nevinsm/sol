package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/store"
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

func (m *mockSessionStarter) Exists(name string) bool {
	return false
}

func (m *mockSessionStarter) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.started = append(m.started, startCall{name, workdir, cmd, env, role, world})
	return nil
}

// mockRuntimeAdapter records adapter method calls.
type mockRuntimeAdapter struct {
	calls          []string
	personaWritten []byte
	skillsWritten  []adapter.Skill
	promptFile     string
	hookSet        HookSet
	configResult   adapter.ConfigResult
	buildCmdResult string
}

func newMockAdapter(t *testing.T) *mockRuntimeAdapter {
	t.Helper()
	return &mockRuntimeAdapter{
		configResult:   adapter.ConfigResult{Dir: "/tmp/fake-config", EnvVar: map[string]string{"CLAUDE_CONFIG_DIR": "/tmp/fake-config"}},
		buildCmdResult: "sleep 300",
	}
}

func (m *mockRuntimeAdapter) InjectPersona(worktreeDir string, content []byte) error {
	m.calls = append(m.calls, "InjectPersona")
	m.personaWritten = content
	// Actually write the file so tests that check persona content still work.
	path := filepath.Join(worktreeDir, "CLAUDE.local.md")
	return os.WriteFile(path, content, 0o644)
}

func (m *mockRuntimeAdapter) InstallSkills(worktreeDir string, skills []adapter.Skill) error {
	m.calls = append(m.calls, "InstallSkills")
	m.skillsWritten = skills
	return nil
}

func (m *mockRuntimeAdapter) InjectSystemPrompt(worktreeDir, content string, replace bool) (string, error) {
	m.calls = append(m.calls, "InjectSystemPrompt")
	// Write file so tests that check file existence still pass.
	promptDir := filepath.Join(worktreeDir, ".claude")
	os.MkdirAll(promptDir, 0o755)
	promptPath := filepath.Join(promptDir, "system-prompt.md")
	os.WriteFile(promptPath, []byte(content), 0o644)
	m.promptFile = ".claude/system-prompt.md"
	return ".claude/system-prompt.md", nil
}

func (m *mockRuntimeAdapter) InstallHooks(worktreeDir string, hooks HookSet) error {
	m.calls = append(m.calls, "InstallHooks")
	m.hookSet = hooks
	// Write a minimal settings.local.json so tests that check file existence pass.
	claudeDir := filepath.Join(worktreeDir, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(`{"hooks":{}}`), 0o644)
	return nil
}

func (m *mockRuntimeAdapter) EnsureConfigDir(worldDir, role, agent, worktreeDir string) (adapter.ConfigResult, error) {
	m.calls = append(m.calls, "EnsureConfigDir")
	return m.configResult, nil
}

func (m *mockRuntimeAdapter) BuildCommand(ctx adapter.CommandContext) string {
	m.calls = append(m.calls, "BuildCommand")
	return m.buildCmdResult
}

func (m *mockRuntimeAdapter) CredentialEnv(cred adapter.Credential) (map[string]string, error) {
	m.calls = append(m.calls, "CredentialEnv")
	return map[string]string{"ANTHROPIC_API_KEY": "test-key"}, nil
}

func (m *mockRuntimeAdapter) TelemetryEnv(port int, agent, world, activeWrit, account string) map[string]string {
	m.calls = append(m.calls, "TelemetryEnv")
	return map[string]string{}
}

func (m *mockRuntimeAdapter) Name() string {
	return "mock"
}

func (m *mockRuntimeAdapter) ExtractTelemetry(eventName string, attrs map[string]string) *adapter.TelemetryRecord {
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

	// Write a fake token so startup.Launch can inject credentials.
	writeTestToken(t, dir)

	// Create world config.
	worldDir := filepath.Join(dir, world)
	os.MkdirAll(worldDir, 0o755)
	worldToml := filepath.Join(worldDir, "world.toml")
	os.WriteFile(worldToml, []byte(`[world]
source_repo = "/tmp/fakerepo"
`), 0o644)

	return dir
}

// writeTestToken writes a minimal api_key token to $SOL_HOME/.accounts/token.json
// so startup.Launch can inject credentials in tests (empty account handle).
func writeTestToken(t *testing.T, solHome string) {
	t.Helper()
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatalf("failed to create .accounts dir: %v", err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatalf("failed to write test token: %v", err)
	}
}

func TestRegisterAndConfigFor(t *testing.T) {
	// Reset registry for test isolation.
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	cfg := RoleConfig{
		Role: "testrole",
	}
	Register("testrole", cfg)

	got := ConfigFor("testrole")
	if got == nil {
		t.Fatal("ConfigFor returned nil for registered role")
	}
	if got.Role != "testrole" {
		t.Errorf("Role = %q, want %q", got.Role, "testrole")
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
	mockA := newMockAdapter(t)

	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		Persona: func(w, _ string) ([]byte, error) {
			return []byte("# Test Forge Persona"), nil
		},
		Hooks: func(w, a string) HookSet {
			return HookSet{
				SessionStart: []HookCommand{{Command: "echo test"}},
			}
		},
		PrimeBuilder: func(w, a string) string {
			return "Execute your workflow."
		},
		Adapter: mockA,
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

	// Verify env includes CLAUDE_CONFIG_DIR (from mock adapter's configResult).
	if call.Env["CLAUDE_CONFIG_DIR"] == "" {
		t.Error("CLAUDE_CONFIG_DIR not set in env")
	}
	if call.Env["SOL_HOME"] != solHome {
		t.Errorf("SOL_HOME = %q, want %q", call.Env["SOL_HOME"], solHome)
	}

	// Verify adapter methods were called in order.
	wantCalls := []string{"InjectPersona", "InstallHooks", "EnsureConfigDir", "BuildCommand", "CredentialEnv", "TelemetryEnv"}
	for _, want := range wantCalls {
		found := false
		for _, got := range mockA.calls {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected adapter call %q, calls were: %v", want, mockA.calls)
		}
	}
}

func TestLaunchAdapterMethodOrder(t *testing.T) {
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

	var callOrder []string
	orderedAdapter := &mockRuntimeAdapter{
		configResult:   adapter.ConfigResult{Dir: os.TempDir(), EnvVar: map[string]string{"CLAUDE_CONFIG_DIR": os.TempDir()}},
		buildCmdResult: "sleep 300",
	}
	// Override methods to record order.
	// We rely on the mockRuntimeAdapter.calls slice which records all calls.

	cfg := RoleConfig{
		Role:                "forge",
		WorktreeDir:         func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		Persona:             func(w, _ string) ([]byte, error) { return []byte("# persona"), nil },
		Hooks:               func(w, a string) HookSet { return HookSet{} },
		SystemPromptContent: "# System Prompt",
		ReplacePrompt:       true,
		SkillInstaller:      func(w, a string) []adapter.Skill { return nil },
		Adapter:             orderedAdapter,
	}

	_, err = Launch(cfg, world, "forge", LaunchOpts{Sessions: mock, Sphere: sphereStore})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	callOrder = orderedAdapter.calls

	// Verify InjectPersona comes before InstallSkills, which comes before
	// InjectSystemPrompt, which comes before InstallHooks, etc.
	findIdx := func(name string) int {
		for i, c := range callOrder {
			if c == name {
				return i
			}
		}
		return -1
	}

	checks := []struct{ first, second string }{
		{"InjectPersona", "InstallSkills"},
		{"InstallSkills", "InjectSystemPrompt"},
		{"InjectSystemPrompt", "InstallHooks"},
		{"InstallHooks", "EnsureConfigDir"},
		{"EnsureConfigDir", "BuildCommand"},
	}
	for _, c := range checks {
		i, j := findIdx(c.first), findIdx(c.second)
		if i < 0 {
			t.Errorf("%q not called", c.first)
		} else if j < 0 {
			t.Errorf("%q not called", c.second)
		} else if i >= j {
			t.Errorf("expected %q (idx %d) before %q (idx %d)", c.first, i, c.second, j)
		}
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

func TestSessionCommandOverrideBypassesAdapter(t *testing.T) {
	// SOL_SESSION_COMMAND override: adapter.BuildCommand is called but returns the env override.
	// The test verifies the session still starts with the override command.
	solHome := setupTestEnv(t, "haven")
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	world := "haven"
	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := &mockSessionStarter{}

	// Use the real claude adapter (registered via init), which respects SOL_SESSION_COMMAND.
	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
	}

	_, err = Launch(cfg, world, "forge", LaunchOpts{Sessions: mock, Sphere: sphereStore})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
	if mock.started[0].Cmd != "sleep 300" {
		t.Errorf("expected SOL_SESSION_COMMAND override, got %q", mock.started[0].Cmd)
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
	t.Setenv("SOL_SESSION_COMMAND", "") // need real command to check --system-prompt-file flag
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

	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	Register("forge", RoleConfig{
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
		PrimeBuilder: func(w, a string) string {
			return "Execute your workflow."
		},
	})

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

	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
}

func TestRespawnUnregisteredRole(t *testing.T) {
	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

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

	origRegistry := registry
	registry = map[string]*RoleConfig{}
	t.Cleanup(func() { registry = origRegistry })

	os.MkdirAll(filepath.Join(solHome, world, "forge", "worktree"), 0o755)

	var sessionOpCalled bool
	Register("forge", RoleConfig{
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
	})

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

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

	if !sessionOpCalled {
		t.Fatal("SessionOp was not called — launch pipeline did not complete")
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
	state := ResumeState{
		Reason:          "compact",
		NewActiveWrit:   "sol-ccc333",
		ClaimedResource: "sol-ccc333",
	}

	prime := BuildResumePrime("", state)
	if !strings.Contains(prime, "Active writ: sol-ccc333") {
		t.Errorf("expected 'Active writ: sol-ccc333' in prime, got %q", prime)
	}
	if strings.Contains(prime, "has changed to") {
		t.Errorf("non-switch prime should not contain writ-switch language: %q", prime)
	}
}

func TestBuildResumePrimeNoActiveWrit(t *testing.T) {
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

	agentDir := filepath.Join(solHome, "haven", "envoys", "Scout")
	os.MkdirAll(agentDir, 0o755)

	state := ResumeState{
		Reason:             "writ-switch",
		PreviousActiveWrit: "sol-aaa111",
		NewActiveWrit:      "sol-bbb222",
	}

	if err := WriteResumeState("haven", "Scout", "envoy", state); err != nil {
		t.Fatalf("WriteResumeState() error: %v", err)
	}

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
	var skillsReturned []adapter.Skill

	cfg := RoleConfig{
		Role:        "outpost",
		WorktreeDir: func(w, a string) string { return filepath.Join(solHome, w, "outposts", a, "worktree") },
		Persona: func(w, a string) ([]byte, error) {
			return []byte("# Test Outpost Persona"), nil
		},
		// Return real skills so the adapter can write them to disk.
		SkillInstaller: func(w, a string) []adapter.Skill {
			skillInstallerCalled = true
			skills, err := protocol.BuildSkills(protocol.SkillContext{
				World: w,
				Role:  "outpost",
			})
			if err != nil {
				t.Fatalf("BuildSkills error: %v", err)
			}
			skillsReturned = skills
			return skillsReturned
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
	if len(skillsReturned) == 0 {
		t.Fatal("SkillInstaller returned no skills")
	}

	// Verify skills were actually written to disk.
	skillsDir := filepath.Join(worktreeDir, ".claude", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("failed to read skills directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no skills installed")
	}

	// Verify expected outpost skills exist.
	expectedSkills, _ := protocol.RoleSkills("outpost")
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
		SkillInstaller: func(w, a string) []adapter.Skill {
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

// TestLaunchInjectsDotEnv verifies that Launch merges world .env vars into the
// session environment, with system-managed vars taking precedence over .env.
func TestLaunchInjectsDotEnv(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// Write a .env file in the world directory.
	dotEnvContent := `# injected secrets
MY_SECRET=supersecret
API_ENDPOINT=https://api.example.com
# SOL_HOME should be overridden by the system value
SOL_HOME=/should/not/win
`
	if err := os.WriteFile(filepath.Join(solHome, world, ".env"), []byte(dotEnvContent), 0o600); err != nil {
		t.Fatalf("failed to write .env: %v", err)
	}

	// Create worktree directory.
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
	}

	opts := LaunchOpts{
		Sessions: mock,
		Sphere:   sphereStore,
	}

	if _, err := Launch(cfg, world, "forge", opts); err != nil {
		t.Fatalf("Launch() error: %v", err)
	}

	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
	call := mock.started[0]

	// .env vars should be present.
	if got := call.Env["MY_SECRET"]; got != "supersecret" {
		t.Errorf("MY_SECRET = %q, want %q", got, "supersecret")
	}
	if got := call.Env["API_ENDPOINT"]; got != "https://api.example.com" {
		t.Errorf("API_ENDPOINT = %q, want %q", got, "https://api.example.com")
	}

	// System-managed SOL_HOME must win over .env.
	if got := call.Env["SOL_HOME"]; got != solHome {
		t.Errorf("SOL_HOME = %q, want system value %q (not .env value)", got, solHome)
	}
}

// TestLaunchDotEnvMissing verifies that Launch succeeds when no .env file exists.
func TestLaunchDotEnvMissing(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	// No .env file written.

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
	}

	if _, err := Launch(cfg, world, "forge", LaunchOpts{Sessions: mock, Sphere: sphereStore}); err != nil {
		t.Fatalf("Launch() should succeed without .env: %v", err)
	}

	if len(mock.started) != 1 {
		t.Fatalf("expected 1 session start, got %d", len(mock.started))
	}
	// System vars should still be present.
	if call := mock.started[0]; call.Env["SOL_HOME"] != solHome {
		t.Errorf("SOL_HOME = %q, want %q", call.Env["SOL_HOME"], solHome)
	}
}

// errSessionStarter is a SessionStarter that always returns an error.
type errSessionStarter struct{ err error }

func (e *errSessionStarter) Exists(name string) bool { return false }

func (e *errSessionStarter) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	return e.err
}

// TestLaunchRollsBackAgentStateOnSessionFailure verifies that if session start
// fails after UpdateAgentState("working"), the agent state is rolled back to its
// previous value so it is not stuck in "working" with no live session.
func TestLaunchRollsBackAgentStateOnSessionFailure(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	worktreeDir := filepath.Join(solHome, world, "forge", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Pre-create agent in idle state.
	if _, err := sphereStore.CreateAgent("forge", world, "forge"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	failSess := &errSessionStarter{err: fmt.Errorf("session already exists")}
	cfg := RoleConfig{
		Role:        "forge",
		WorktreeDir: func(w, _ string) string { return filepath.Join(solHome, w, "forge", "worktree") },
	}

	_, launchErr := Launch(cfg, world, "forge", LaunchOpts{Sessions: failSess, Sphere: sphereStore})
	if launchErr == nil {
		t.Fatal("Launch() should have returned an error")
	}

	// Agent state must be rolled back to idle (not stuck at "working").
	agent, err := sphereStore.GetAgent(world + "/forge")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.State != store.AgentIdle {
		t.Errorf("agent state = %q after failed launch, want %q (rollback failed)", agent.State, store.AgentIdle)
	}
}

// TestLaunchRollsBackToPreviousStateOnSessionFailure verifies that when an agent
// is already in a non-idle state, a failed Launch rolls back to that prior state.
func TestLaunchRollsBackToPreviousStateOnSessionFailure(t *testing.T) {
	solHome := setupTestEnv(t, "haven")
	world := "haven"

	worktreeDir := filepath.Join(solHome, world, "outposts", "Toast", "worktree")
	os.MkdirAll(worktreeDir, 0o755)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Pre-create agent that already has a tethered writ.
	if _, err := sphereStore.CreateAgent("Toast", world, "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState(world+"/Toast", store.AgentWorking, "sol-existingwrit"); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	failSess := &errSessionStarter{err: fmt.Errorf("session already exists")}
	cfg := RoleConfig{
		Role:        "outpost",
		WorktreeDir: func(w, a string) string { return filepath.Join(solHome, w, "outposts", a, "worktree") },
	}

	_, launchErr := Launch(cfg, world, "Toast", LaunchOpts{Sessions: failSess, Sphere: sphereStore})
	if launchErr == nil {
		t.Fatal("Launch() should have returned an error")
	}

	// Agent state must be rolled back to its previous working state with the
	// original active writ preserved.
	agent, err := sphereStore.GetAgent(world + "/Toast")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentWorking)
	}
	if agent.ActiveWrit != "sol-existingwrit" {
		t.Errorf("active_writ = %q, want %q", agent.ActiveWrit, "sol-existingwrit")
	}
}

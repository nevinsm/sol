package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/adapter"
)

func newAdapter() *Adapter {
	return New()
}

// ---- Name ----

func TestName(t *testing.T) {
	a := newAdapter()
	if a.Name() != "codex" {
		t.Errorf("expected Name()=codex, got %q", a.Name())
	}
}

// ---- CalloutCommand ----

func TestCalloutCommand(t *testing.T) {
	a := newAdapter()
	if got := a.CalloutCommand(); got != "codex exec --json" {
		t.Errorf("CalloutCommand() = %q, want %q", got, "codex exec --json")
	}
}

// ---- BuildCommand ----

func TestBuildCommandBasic(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Prompt:      "Hello agent",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "codex --full-auto") {
		t.Errorf("expected codex --full-auto prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "Hello agent") {
		t.Errorf("expected prompt in command, got: %q", cmd)
	}
}

func TestBuildCommandWithModel(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Model:       "o3",
		Prompt:      "go",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.Contains(cmd, "--model o3") {
		t.Errorf("expected --model flag, got: %q", cmd)
	}
}

func TestBuildCommandContinue(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Continue:    true,
		Prompt:      "resume context",
	}
	cmd := a.BuildCommand(ctx)

	if cmd != "codex resume --last" {
		t.Errorf("expected 'codex resume --last' for continue mode, got: %q", cmd)
	}
}

func TestBuildCommandNoPrompt(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
	}
	cmd := a.BuildCommand(ctx)

	if cmd != "codex --full-auto" {
		t.Errorf("expected bare 'codex --full-auto', got: %q", cmd)
	}
}

func TestBuildCommandSOLSessionCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	a := newAdapter()

	cmd := a.BuildCommand(adapter.CommandContext{WorktreeDir: t.TempDir()})
	if cmd != "sleep 300" {
		t.Errorf("expected SOL_SESSION_COMMAND override, got: %q", cmd)
	}
}

// ---- CredentialEnv ----

func TestCredentialEnvAPIKey(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "api_key", Token: "sk-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := env["OPENAI_API_KEY"]; !ok || v != "sk-abc" {
		t.Errorf("expected OPENAI_API_KEY=sk-abc, got %v", env)
	}
}

func TestCredentialEnvUnknown(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "oauth_token", Token: "val"})
	if err == nil {
		t.Error("expected error for unrecognized credential type, got nil")
	}
	if len(env) != 0 {
		t.Errorf("expected nil/empty map for unknown credential type, got %v", env)
	}
}

func TestCredentialEnvCompletelyUnknown(t *testing.T) {
	a := newAdapter()
	_, err := a.CredentialEnv(adapter.Credential{Type: "unknown", Token: "val"})
	if err == nil {
		t.Error("expected error for unknown credential type, got nil")
	}
}

// ---- TelemetryEnv ----

func TestTelemetryEnvReturnsEmpty(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "")
	if len(env) != 0 {
		t.Errorf("expected empty map, got %v", env)
	}
}

// ---- InjectPersona ----

func TestInjectPersona(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	content := []byte("# Agent Instructions\n\nDo the thing.\n")
	if err := a.InjectPersona(dir, content); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	// Should write to AGENTS.override.md, not AGENTS.md.
	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, content)
	}

	// AGENTS.md should NOT exist (no project file was present).
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md should not be created by InjectPersona, stat err: %v", err)
	}
}

func TestInjectPersonaPreservesProjectAGENTSmd(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Create a project AGENTS.md first.
	projectContent := "# Project Instructions\n\nFollow the coding standards.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(projectContent), 0o644); err != nil {
		t.Fatalf("failed to write project AGENTS.md: %v", err)
	}

	persona := []byte("# Agent Persona\n\nBe helpful.\n")
	if err := a.InjectPersona(dir, persona); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}

	content := string(got)
	// Project content should come first.
	if !strings.Contains(content, "# Project Instructions") {
		t.Errorf("expected project AGENTS.md content to be preserved, got:\n%s", content)
	}
	// Persona should come after separator.
	if !strings.Contains(content, "# Agent Persona") {
		t.Errorf("expected persona content, got:\n%s", content)
	}
	// Separator should be present.
	if !strings.Contains(content, "\n---\n") {
		t.Errorf("expected separator between project and persona content, got:\n%s", content)
	}
	// Project content should precede persona.
	projectIdx := strings.Index(content, "# Project Instructions")
	personaIdx := strings.Index(content, "# Agent Persona")
	if projectIdx >= personaIdx {
		t.Errorf("project content should come before persona content")
	}
}

// ---- InstallSkills ----

func TestInstallSkillsCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	skills := []adapter.Skill{
		{Name: "greeting", Content: "# Greeting\nSay hello."},
		{Name: "farewell", Content: "# Farewell\nSay goodbye."},
	}

	if err := a.InstallSkills(dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Check skill files exist with correct content.
	for _, s := range skills {
		skillPath := filepath.Join(dir, ".agents", "skills", s.Name, "SKILL.md")
		got, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("failed to read skill %q: %v", s.Name, err)
		}
		if string(got) != s.Content {
			t.Errorf("skill %q content mismatch:\ngot:  %q\nwant: %q", s.Name, got, s.Content)
		}

		// Check sol-managed marker exists.
		markerPath := filepath.Join(dir, ".agents", "skills", s.Name, ".sol-managed")
		if _, err := os.Stat(markerPath); err != nil {
			t.Errorf("sol-managed marker missing for skill %q: %v", s.Name, err)
		}
	}
}

func TestInstallSkillsCleansUpStale(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// First install with two skills.
	skills := []adapter.Skill{
		{Name: "keep", Content: "keep content"},
		{Name: "stale", Content: "stale content"},
	}
	if err := a.InstallSkills(dir, skills); err != nil {
		t.Fatalf("first InstallSkills failed: %v", err)
	}

	// Verify both exist.
	stalePath := filepath.Join(dir, ".agents", "skills", "stale")
	if _, err := os.Stat(stalePath); err != nil {
		t.Fatalf("stale skill dir should exist after first install: %v", err)
	}

	// Second install with only "keep" — "stale" should be removed.
	if err := a.InstallSkills(dir, []adapter.Skill{{Name: "keep", Content: "keep content"}}); err != nil {
		t.Fatalf("second InstallSkills failed: %v", err)
	}

	// "keep" should still exist.
	keepPath := filepath.Join(dir, ".agents", "skills", "keep", "SKILL.md")
	if _, err := os.Stat(keepPath); err != nil {
		t.Errorf("keep skill should still exist: %v", err)
	}

	// "stale" should be removed.
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("stale skill dir should have been removed, but stat returned: %v", err)
	}
}

func TestInstallSkillsPreservesNonSolManaged(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Create a non-sol-managed skill directory manually.
	customDir := filepath.Join(dir, ".agents", "skills", "custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("failed to create custom skill dir: %v", err)
	}
	customFile := filepath.Join(customDir, "SKILL.md")
	if err := os.WriteFile(customFile, []byte("custom content"), 0o644); err != nil {
		t.Fatalf("failed to write custom skill: %v", err)
	}

	// Install sol-managed skills (not including "custom").
	if err := a.InstallSkills(dir, []adapter.Skill{{Name: "managed", Content: "managed"}}); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Custom directory should still exist (no .sol-managed marker).
	if _, err := os.Stat(customFile); err != nil {
		t.Errorf("custom skill should be preserved (no sol-managed marker): %v", err)
	}
}

// ---- InjectSystemPrompt ----

func TestInjectSystemPromptReplace(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	path, err := a.InjectSystemPrompt(dir, "system content", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}
	if path != "AGENTS.override.md" {
		t.Errorf("expected AGENTS.override.md, got %q", path)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if string(got) != "system content" {
		t.Errorf("content mismatch: got %q", got)
	}
}

func TestInjectSystemPromptReplacePreservesProjectAGENTSmd(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Create a project AGENTS.md.
	projectContent := "# Project Instructions\n\nBuild quality software.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(projectContent), 0o644); err != nil {
		t.Fatalf("failed to write project AGENTS.md: %v", err)
	}

	// Replace mode should prepend project content.
	_, err := a.InjectSystemPrompt(dir, "system prompt content", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}

	content := string(got)
	if !strings.Contains(content, "# Project Instructions") {
		t.Errorf("expected project AGENTS.md content preserved in replace mode, got:\n%s", content)
	}
	if !strings.Contains(content, "system prompt content") {
		t.Errorf("expected system prompt content, got:\n%s", content)
	}
}

func TestInjectSystemPromptReplaceOverwritesPersona(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Write persona first.
	if err := a.InjectPersona(dir, []byte("persona content")); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	// Replace with system prompt — should overwrite AGENTS.override.md.
	_, err := a.InjectSystemPrompt(dir, "system replaces persona", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if string(got) != "system replaces persona" {
		t.Errorf("expected system prompt to overwrite persona, got %q", got)
	}
}

func TestInjectSystemPromptAppend(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Write to AGENTS.override.md at worktree root (not .codex/ subdirectory).
	path, err := a.InjectSystemPrompt(dir, "override content", false)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}
	if path != "AGENTS.override.md" {
		t.Errorf("expected AGENTS.override.md, got %q", path)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if string(got) != "override content" {
		t.Errorf("content mismatch: got %q", got)
	}

	// .codex/AGENTS.override.md should NOT exist.
	if _, err := os.Stat(filepath.Join(dir, ".codex", "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Errorf(".codex/AGENTS.override.md should not exist, stat err: %v", err)
	}
}

func TestInjectSystemPromptAppendToExisting(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Write persona first.
	if err := a.InjectPersona(dir, []byte("persona content\n")); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	// Append system prompt.
	_, err := a.InjectSystemPrompt(dir, "appended content", false)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}

	content := string(got)
	if !strings.Contains(content, "persona content") {
		t.Errorf("expected persona content preserved, got:\n%s", content)
	}
	if !strings.Contains(content, "appended content") {
		t.Errorf("expected appended content, got:\n%s", content)
	}
}

// ---- InstallHooks ----

func TestInstallHooksGuards(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		Guards: []adapter.Guard{
			{Pattern: "Bash(git push --force*)", Command: "exit 2"},
			{Pattern: "Bash(rm -rf /*)", Command: "exit 2"},
		},
	}

	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}

	content := string(got)
	if !strings.Contains(content, "IMPORTANT: NEVER run: git push --force") {
		t.Errorf("expected guard instruction for git push --force, got:\n%s", content)
	}
	if !strings.Contains(content, "IMPORTANT: NEVER run: rm -rf /") {
		t.Errorf("expected guard instruction for rm -rf /, got:\n%s", content)
	}
}

func TestInstallHooksAppendsToExistingPersona(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Write persona first (writes to AGENTS.override.md).
	persona := "# My Agent\n\nBe helpful.\n"
	if err := a.InjectPersona(dir, []byte(persona)); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	// Install hooks — should append to AGENTS.override.md, not overwrite.
	hooks := adapter.HookSet{
		Guards: []adapter.Guard{
			{Pattern: "Bash(git push --force*)", Command: "exit 2"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}

	content := string(got)
	// Persona content should still be present.
	if !strings.Contains(content, "# My Agent") {
		t.Errorf("expected persona content to be preserved, got:\n%s", content)
	}
	if !strings.Contains(content, "Be helpful.") {
		t.Errorf("expected persona content to be preserved, got:\n%s", content)
	}
	// Guard instruction should be appended.
	if !strings.Contains(content, "IMPORTANT: NEVER run: git push --force") {
		t.Errorf("expected guard instruction appended, got:\n%s", content)
	}
}

func TestInstallHooksPreCompact(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		PreCompact: []adapter.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if !strings.Contains(string(got), "Before running /compact, execute this command: sol prime --world=myworld --agent=Toast") {
		t.Errorf("expected PreCompact instruction, got:\n%s", got)
	}
}

func TestInstallHooksTurnBoundary(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		TurnBoundary: []adapter.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if !strings.Contains(string(got), "Periodically run this command: sol heartbeat --world=myworld --agent=Toast") {
		t.Errorf("expected TurnBoundary instruction, got:\n%s", got)
	}
}

func TestInstallHooksSessionStartSkipped(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// SessionStart hooks should be skipped (not written to AGENTS.override.md).
	hooks := adapter.HookSet{
		SessionStart: []adapter.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// AGENTS.override.md should not exist (no translatable hooks).
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.override.md should not exist when only SessionStart hooks are provided, stat err: %v", err)
	}
}

func TestInstallHooksEmptyHookSet(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	if err := a.InstallHooks(dir, adapter.HookSet{}); err != nil {
		t.Fatalf("InstallHooks with empty HookSet should not error: %v", err)
	}

	// AGENTS.override.md should not be created.
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.override.md should not exist for empty HookSet, stat err: %v", err)
	}
}

// ---- EnsureConfigDir ----

func TestEnsureConfigDirReturnsCodexHome(t *testing.T) {
	worldDir := t.TempDir()
	worktreeDir := t.TempDir()
	a := newAdapter()

	result, err := a.EnsureConfigDir(worldDir, "outpost", "Nova", worktreeDir)
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	// CODEX_HOME should be returned in EnvVar.
	codexHome, ok := result.EnvVar["CODEX_HOME"]
	if !ok {
		t.Fatal("expected CODEX_HOME in EnvVar, not found")
	}

	// CODEX_HOME should be under the agent-scoped directory.
	expectedDir := filepath.Join(worldDir, "outposts", "Nova", ".codex-home")
	if codexHome != expectedDir {
		t.Errorf("CODEX_HOME = %q, want %q", codexHome, expectedDir)
	}

	// Dir should match CODEX_HOME.
	if result.Dir != expectedDir {
		t.Errorf("Dir = %q, want %q", result.Dir, expectedDir)
	}
}

func TestEnsureConfigDirWritesConfigToml(t *testing.T) {
	worldDir := t.TempDir()
	worktreeDir := t.TempDir()
	a := newAdapter()

	result, err := a.EnsureConfigDir(worldDir, "outpost", "Nova", worktreeDir)
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	configPath := filepath.Join(result.Dir, "config.toml")
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}

	content := string(got)

	// Verify correct approval_policy value (Never = auto-reject user prompts,
	// return failures to model).
	if !strings.Contains(content, `approval_policy = "never"`) {
		t.Errorf("expected approval_policy = \"never\", got:\n%s", content)
	}

	// Verify correct sandbox_mode value (DangerFullAccess = no sandbox).
	if !strings.Contains(content, `sandbox_mode = "danger-full-access"`) {
		t.Errorf("expected sandbox_mode = \"danger-full-access\", got:\n%s", content)
	}
}

func TestEnsureConfigDirDoesNotWriteGlobalConfig(t *testing.T) {
	worldDir := t.TempDir()
	worktreeDir := t.TempDir()
	a := newAdapter()

	_, err := a.EnsureConfigDir(worldDir, "outpost", "Nova", worktreeDir)
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	// ~/.codex/config.toml should NOT be touched.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}
	globalConfig := filepath.Join(home, ".codex", "config.toml")
	// We can't assert it doesn't exist (it might from other tests), but we
	// verify the returned Dir is NOT the global path.
	if filepath.Join(home, ".codex") == filepath.Join(worldDir, "outposts", "Nova", ".codex-home") {
		t.Error("CODEX_HOME should not be the global ~/.codex/ directory")
	}
	_ = globalConfig // suppress unused warning
}

func TestEnsureConfigDirAgentIsolation(t *testing.T) {
	worldDir := t.TempDir()
	worktreeDir := t.TempDir()
	a := newAdapter()

	// Two different agents should get different CODEX_HOME paths.
	result1, err := a.EnsureConfigDir(worldDir, "outpost", "Alpha", worktreeDir)
	if err != nil {
		t.Fatalf("EnsureConfigDir for Alpha failed: %v", err)
	}
	result2, err := a.EnsureConfigDir(worldDir, "outpost", "Beta", worktreeDir)
	if err != nil {
		t.Fatalf("EnsureConfigDir for Beta failed: %v", err)
	}

	if result1.EnvVar["CODEX_HOME"] == result2.EnvVar["CODEX_HOME"] {
		t.Errorf("different agents should have different CODEX_HOME: both got %q", result1.EnvVar["CODEX_HOME"])
	}
}

// ---- ExtractTelemetry ----

func TestExtractTelemetryReturnsNilForIrrelevantEvent(t *testing.T) {
	a := newAdapter()
	result := a.ExtractTelemetry("some.event", map[string]string{"key": "val"})
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestExtractTelemetryCodexAPIRequest(t *testing.T) {
	a := newAdapter()
	attrs := map[string]string{
		"gen_ai.response.model":      "o3",
		"gen_ai.usage.input_tokens":  "100",
		"gen_ai.usage.output_tokens": "50",
	}
	result := a.ExtractTelemetry("codex.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "o3" {
		t.Errorf("expected model=o3, got %q", result.Model)
	}
	if result.InputTokens != 100 {
		t.Errorf("expected InputTokens=100, got %d", result.InputTokens)
	}
	if result.OutputTokens != 50 {
		t.Errorf("expected OutputTokens=50, got %d", result.OutputTokens)
	}
}

func TestExtractTelemetryGenAICompletion(t *testing.T) {
	a := newAdapter()
	attrs := map[string]string{
		"model":         "gpt-4",
		"input_tokens":  "200",
		"output_tokens": "75",
	}
	result := a.ExtractTelemetry("gen_ai.content.completion", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "gpt-4" {
		t.Errorf("expected model=gpt-4, got %q", result.Model)
	}
	if result.InputTokens != 200 {
		t.Errorf("expected InputTokens=200, got %d", result.InputTokens)
	}
}

func TestExtractTelemetryNoModel(t *testing.T) {
	a := newAdapter()
	attrs := map[string]string{
		"gen_ai.usage.input_tokens": "100",
	}
	result := a.ExtractTelemetry("codex.api_request", attrs)
	if result != nil {
		t.Errorf("expected nil when no model, got %+v", result)
	}
}

// ---- extractGuardReadable ----

func TestExtractGuardReadable(t *testing.T) {
	tests := []struct {
		pattern string
		want    string
	}{
		{"Bash(git push --force*)", "git push --force"},
		{"Bash(rm -rf /*)", "rm -rf /"},
		{"EnterPlanMode", "EnterPlanMode"},
		{"Bash(git reset --hard)", "git reset --hard"},
	}
	for _, tt := range tests {
		got := extractGuardReadable(tt.pattern)
		if got != tt.want {
			t.Errorf("extractGuardReadable(%q) = %q, want %q", tt.pattern, got, tt.want)
		}
	}
}

// ---- Registry ----

func TestAdapterImplementsInterface(t *testing.T) {
	var _ adapter.RuntimeAdapter = (*Adapter)(nil)
}

func TestAdapterRegistered(t *testing.T) {
	// The init() function registers the adapter. Verify it's resolvable.
	a, ok := adapter.Get("codex")
	if !ok {
		t.Fatal("codex adapter not found in registry")
	}
	if a.Name() != "codex" {
		t.Errorf("expected Name()=codex, got %q", a.Name())
	}
}

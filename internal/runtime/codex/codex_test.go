package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/runtime"
)

func newRuntime() *CodexRuntime {
	return New()
}

// ---- Descriptor ----

func TestDescriptorName(t *testing.T) {
	r := newRuntime()
	if r.Descriptor().Name != "codex" {
		t.Errorf("Descriptor().Name = %q, want %q", r.Descriptor().Name, "codex")
	}
}

func TestDescriptorPersonaFile(t *testing.T) {
	r := newRuntime()
	// Codex uses AGENTS.override.md for persona injection.
	if r.Descriptor().PersonaFile != "AGENTS.override.md" {
		t.Errorf("Descriptor().PersonaFile = %q, want %q", r.Descriptor().PersonaFile, "AGENTS.override.md")
	}
}

func TestDescriptorSkillsDir(t *testing.T) {
	r := newRuntime()
	if r.Descriptor().SkillsDir != ".agents/skills" {
		t.Errorf("Descriptor().SkillsDir = %q, want %q", r.Descriptor().SkillsDir, ".agents/skills")
	}
}

func TestDescriptorConfigDirEnv(t *testing.T) {
	r := newRuntime()
	if r.Descriptor().ConfigDirEnv != "CODEX_HOME" {
		t.Errorf("Descriptor().ConfigDirEnv = %q, want %q", r.Descriptor().ConfigDirEnv, "CODEX_HOME")
	}
}

func TestDescriptorCredentialFile(t *testing.T) {
	r := newRuntime()
	if r.Descriptor().CredentialFile != "auth.json" {
		t.Errorf("Descriptor().CredentialFile = %q, want %q", r.Descriptor().CredentialFile, "auth.json")
	}
}

func TestDescriptorGlobalCredsPath(t *testing.T) {
	r := newRuntime()
	if r.Descriptor().GlobalCredsPath != "~/.codex/auth.json" {
		t.Errorf("Descriptor().GlobalCredsPath = %q, want %q", r.Descriptor().GlobalCredsPath, "~/.codex/auth.json")
	}
}

func TestDescriptorDefaultModel(t *testing.T) {
	r := newRuntime()
	// Match the adapter's DefaultModel() return value.
	if r.Descriptor().DefaultModel != "gpt-5.4" {
		t.Errorf("Descriptor().DefaultModel = %q, want %q", r.Descriptor().DefaultModel, "gpt-5.4")
	}
}

func TestDescriptorCalloutCommand(t *testing.T) {
	r := newRuntime()
	if r.Descriptor().CalloutCommand != "codex exec" {
		t.Errorf("Descriptor().CalloutCommand = %q, want %q", r.Descriptor().CalloutCommand, "codex exec")
	}
}

func TestDescriptorSupportedHooks(t *testing.T) {
	r := newRuntime()
	d := r.Descriptor()

	// TurnBoundary and Guard are natively supported.
	if !d.HasHookSupport("TurnBoundary") {
		t.Error("HasHookSupport(TurnBoundary) = false, want true")
	}
	if !d.HasHookSupport("Guard") {
		t.Error("HasHookSupport(Guard) = false, want true")
	}

	// SessionStart and PreCompact are instruction-text only.
	if d.HasHookSupport("SessionStart") {
		t.Error("HasHookSupport(SessionStart) = true, want false")
	}
	if d.HasHookSupport("PreCompact") {
		t.Error("HasHookSupport(PreCompact) = true, want false")
	}
}

func TestDescriptorCredentialEnvKeys(t *testing.T) {
	r := newRuntime()
	keys := r.Descriptor().CredentialEnvKeys
	if v, ok := keys["api_key"]; !ok || v != "OPENAI_API_KEY" {
		t.Errorf("CredentialEnvKeys[api_key] = %q, want %q", v, "OPENAI_API_KEY")
	}
}

func TestRuntimeImplementsInterface(t *testing.T) {
	// Compile-time: var _ runtime.Runtime = (*CodexRuntime)(nil) above.
	// Runtime check: verify we can call all interface methods without panic.
	r := newRuntime()
	d := r.Descriptor()
	if d.Name != "codex" {
		t.Errorf("Descriptor().Name = %q, want %q", d.Name, "codex")
	}

	ctx := runtime.CommandContext{Prompt: "hello"}
	cmd := r.BuildCommand(ctx)
	if cmd == "" {
		t.Error("BuildCommand returned empty string")
	}

	if err := r.InstallHooks(t.TempDir(), runtime.HookSet{}); err != nil {
		t.Errorf("InstallHooks(empty) returned error: %v", err)
	}
}

// ---- BuildCommand ----

func TestBuildCommandBasic(t *testing.T) {
	r := newRuntime()

	ctx := runtime.CommandContext{
		WorktreeDir: t.TempDir(),
		Prompt:      "Hello agent",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "codex") {
		t.Errorf("expected codex prefix, got: %q", cmd)
	}
	if strings.Contains(cmd, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("expected no --dangerously-bypass-approvals-and-sandbox flag (policy via config.toml), got: %q", cmd)
	}
	if !strings.Contains(cmd, "Hello agent") {
		t.Errorf("expected prompt in command, got: %q", cmd)
	}
}

func TestBuildCommandWithModel(t *testing.T) {
	r := newRuntime()

	ctx := runtime.CommandContext{
		WorktreeDir: t.TempDir(),
		Model:       "o3",
		Prompt:      "go",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.Contains(cmd, "--model o3") {
		t.Errorf("expected --model flag, got: %q", cmd)
	}
}

func TestBuildCommandContinue(t *testing.T) {
	r := newRuntime()

	ctx := runtime.CommandContext{
		WorktreeDir: t.TempDir(),
		Continue:    true,
	}
	cmd := r.BuildCommand(ctx)

	if cmd != "codex resume --last" {
		t.Errorf("expected 'codex resume --last' for continue mode without prompt, got: %q", cmd)
	}
}

func TestBuildCommandContinueWithPrompt(t *testing.T) {
	r := newRuntime()

	ctx := runtime.CommandContext{
		WorktreeDir: t.TempDir(),
		Continue:    true,
		Prompt:      "resume context",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "codex resume --last") {
		t.Errorf("expected 'codex resume --last' prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "resume context") {
		t.Errorf("expected prompt in resume command, got: %q", cmd)
	}
}

func TestBuildCommandNoPrompt(t *testing.T) {
	r := newRuntime()

	ctx := runtime.CommandContext{
		WorktreeDir: t.TempDir(),
	}
	cmd := r.BuildCommand(ctx)

	if cmd != "codex" {
		t.Errorf("expected bare 'codex', got: %q", cmd)
	}
}

func TestBuildCommandSOLSessionCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	r := newRuntime()

	cmd := r.BuildCommand(runtime.CommandContext{WorktreeDir: t.TempDir()})
	if cmd != "sleep 300" {
		t.Errorf("expected SOL_SESSION_COMMAND override, got: %q", cmd)
	}
}

// ---- InstallHooks ----

func TestInstallHooksEmptyHookSet(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	if err := r.InstallHooks(dir, runtime.HookSet{}); err != nil {
		t.Fatalf("InstallHooks with empty HookSet should not error: %v", err)
	}

	// AGENTS.override.md should not be created.
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.override.md should not exist for empty HookSet, stat err: %v", err)
	}
}

func TestInstallHooksGuards(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		Guards: []runtime.Guard{
			{Pattern: "Bash(git push --force*)", Command: "exit 2"},
			{Pattern: "Bash(rm -rf /*)", Command: "exit 2"},
		},
	}

	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// Verify instruction text (defense-in-depth) in AGENTS.override.md.
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

	// Verify exec policy rules file was written.
	rulesPath := filepath.Join(dir, ".codex", "rules", solGuardRulesFile)
	rulesContent, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", solGuardRulesFile, err)
	}
	rules := string(rulesContent)
	if !strings.Contains(rules, `prefix_rule(["git", "push", "--force"], decision="forbidden")`) {
		t.Errorf("expected exec policy rule for git push --force, got:\n%s", rules)
	}
	if !strings.Contains(rules, `prefix_rule(["rm", "-rf", "/"], decision="forbidden")`) {
		t.Errorf("expected exec policy rule for rm -rf /, got:\n%s", rules)
	}
}

func TestInstallHooksGuardsNonBashFallsBack(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		Guards: []runtime.Guard{
			{Pattern: "EnterPlanMode", Command: "exit 2"},
			{Pattern: "Bash(git push*)", Command: "exit 2"},
		},
	}

	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// Exec policy rules should only contain the Bash guard.
	rulesPath := filepath.Join(dir, ".codex", "rules", solGuardRulesFile)
	rulesContent, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", solGuardRulesFile, err)
	}
	rules := string(rulesContent)
	if !strings.Contains(rules, `prefix_rule(["git", "push"], decision="forbidden")`) {
		t.Errorf("expected exec policy rule for git push, got:\n%s", rules)
	}
	// EnterPlanMode should NOT be in exec policy rules.
	if strings.Contains(rules, "EnterPlanMode") {
		t.Errorf("non-Bash guard should not appear in exec policy rules, got:\n%s", rules)
	}

	// Both should appear as instruction text.
	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, "IMPORTANT: NEVER run: EnterPlanMode") {
		t.Errorf("expected instruction for EnterPlanMode, got:\n%s", content)
	}
	if !strings.Contains(content, "IMPORTANT: NEVER run: git push") {
		t.Errorf("expected instruction for git push, got:\n%s", content)
	}
}

func TestInstallHooksGuardsAllNonBashNoRulesFile(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		Guards: []runtime.Guard{
			{Pattern: "EnterPlanMode", Command: "exit 2"},
			{Pattern: "Write(/etc/passwd*)", Command: "exit 2"},
		},
	}

	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// No exec policy rules file should exist when all guards are non-Bash.
	rulesPath := filepath.Join(dir, ".codex", "rules", solGuardRulesFile)
	if _, err := os.Stat(rulesPath); !os.IsNotExist(err) {
		t.Errorf("expected no rules file when all guards are non-Bash, stat err: %v", err)
	}

	// Instruction text should still be present.
	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, "IMPORTANT: NEVER run: EnterPlanMode") {
		t.Errorf("expected instruction for EnterPlanMode, got:\n%s", content)
	}
}

func TestInstallHooksPreCompact(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		PreCompact: []runtime.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(dir, hooks); err != nil {
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

func TestInstallHooksTurnBoundaryWritesNotify(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// First TurnBoundary hook should be written as notify in .codex/config.toml.
	configPath := filepath.Join(dir, ".codex", "config.toml")
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, `notify = ["sol", "heartbeat", "--world=myworld", "--agent=Toast"]`) {
		t.Errorf("expected notify config in .codex/config.toml, got:\n%s", content)
	}

	// AGENTS.override.md should NOT exist (no remaining instruction hooks).
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.override.md should not exist when only one TurnBoundary hook, stat err: %v", err)
	}
}

func TestInstallHooksTurnBoundaryMultiple(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
			{Command: "sol extra-hook --world=myworld"},
		},
	}
	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// First hook should be in .codex/config.toml as notify.
	configPath := filepath.Join(dir, ".codex", "config.toml")
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	if !strings.Contains(string(got), "notify =") {
		t.Errorf("expected notify in .codex/config.toml, got:\n%s", got)
	}

	// Second hook should be in AGENTS.override.md as instruction text.
	overrideGot, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(overrideGot)
	if !strings.Contains(content, "Periodically run this command: sol extra-hook --world=myworld") {
		t.Errorf("expected second TurnBoundary as instruction text, got:\n%s", content)
	}
	// First hook should NOT appear as instruction text.
	if strings.Contains(content, "sol heartbeat") {
		t.Errorf("first TurnBoundary hook should not be in instruction text, got:\n%s", content)
	}
}

func TestInstallHooksSessionStartSkipped(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		SessionStart: []runtime.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// AGENTS.override.md should not exist (no translatable hooks).
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.override.md should not exist when only SessionStart hooks are provided, stat err: %v", err)
	}
}

func TestInstallHooksMultiTurnBoundarySucceedsWithDegradation(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
			{Command: "sol second-hook --world=myworld"},
			{Command: "sol third-hook --world=myworld"},
		},
	}
	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks should succeed for multi-TurnBoundary degradation, got: %v", err)
	}

	// First hook in notify.
	cfg, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	if !strings.Contains(string(cfg), `notify = ["sol", "heartbeat", "--world=myworld", "--agent=Toast"]`) {
		t.Errorf("expected first hook in notify, got:\n%s", cfg)
	}

	// Remaining hooks in instruction text.
	override, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(override)
	if !strings.Contains(content, "Periodically run this command: sol second-hook --world=myworld") {
		t.Errorf("expected second hook as instruction text, got:\n%s", content)
	}
	if !strings.Contains(content, "Periodically run this command: sol third-hook --world=myworld") {
		t.Errorf("expected third hook as instruction text, got:\n%s", content)
	}
}

func TestInstallHooksTurnBoundaryIdempotent(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}

	// Call InstallHooks twice with same hooks.
	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("first InstallHooks failed: %v", err)
	}
	configPath := filepath.Join(dir, ".codex", "config.toml")
	first, _ := os.ReadFile(configPath)

	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("second InstallHooks failed: %v", err)
	}
	second, _ := os.ReadFile(configPath)

	if string(first) != string(second) {
		t.Errorf("InstallHooks not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInstallHooksTurnBoundaryReplacesOnChange(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	// Install with first hook set.
	hooks1 := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(dir, hooks1); err != nil {
		t.Fatalf("first InstallHooks failed: %v", err)
	}

	// Install with different hook set — should replace, not append.
	hooks2 := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=other --agent=Nova"},
		},
	}
	if err := r.InstallHooks(dir, hooks2); err != nil {
		t.Fatalf("second InstallHooks failed: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	content := string(got)

	if strings.Contains(content, "Toast") {
		t.Errorf("old notify should be replaced, got:\n%s", content)
	}
	if !strings.Contains(content, "Nova") {
		t.Errorf("expected new notify, got:\n%s", content)
	}
}

func TestInstallHooksNotifyPreservesExistingProjectConfig(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	// Create existing .codex/config.toml.
	codexDir := filepath.Join(dir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("failed to create .codex dir: %v", err)
	}
	existingContent := "instructions = \"Be helpful\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(existingContent), 0o644); err != nil {
		t.Fatalf("failed to write existing config: %v", err)
	}

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	content := string(got)

	// Existing content should be preserved.
	if !strings.Contains(content, "instructions = \"Be helpful\"") {
		t.Errorf("expected existing content preserved, got:\n%s", content)
	}
	// Notify should be appended.
	if !strings.Contains(content, "notify =") {
		t.Errorf("expected notify appended, got:\n%s", content)
	}
}

// ---- InstallHooks error propagation ----

// blockPath replaces a path with a regular file so that os.MkdirAll on that
// path (or a subpath) fails with ENOTDIR.
func blockPath(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("blockPath: failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("blocker"), 0o644); err != nil {
		t.Fatalf("blockPath: failed to write blocker file: %v", err)
	}
}

func TestInstallHooksPropagatesWriteGuardRulesFailure(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	// Block .codex/rules so writeGuardRules cannot mkdir.
	blockPath(t, filepath.Join(dir, ".codex", "rules"))

	hooks := runtime.HookSet{
		Guards: []runtime.Guard{
			{Pattern: "Bash(rm -rf /*)", Command: "exit 2"},
		},
	}
	err := r.InstallHooks(dir, hooks)
	if err == nil {
		t.Fatalf("InstallHooks expected error when guard rules cannot be written, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write guard exec policy rules") {
		t.Errorf("expected wrapped guard error, got: %v", err)
	}
}

func TestInstallHooksPropagatesWriteProjectConfigBlockFailure(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	// Block .codex by pre-creating it as a regular file.
	blockPath(t, filepath.Join(dir, ".codex"))

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}
	err := r.InstallHooks(dir, hooks)
	if err == nil {
		t.Fatalf("InstallHooks expected error when project config cannot be written, got nil")
	}
	if !strings.Contains(err.Error(), "failed to install notify hook") {
		t.Errorf("expected wrapped notify error, got: %v", err)
	}
}

func TestInstallHooksPropagatesUpdateSectionFailure(t *testing.T) {
	dir := t.TempDir()
	r := newRuntime()

	// Block AGENTS.override.md by pre-creating it as a directory.
	if err := os.MkdirAll(filepath.Join(dir, "AGENTS.override.md"), 0o755); err != nil {
		t.Fatalf("failed to create blocking directory: %v", err)
	}

	hooks := runtime.HookSet{
		PreCompact: []runtime.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	err := r.InstallHooks(dir, hooks)
	if err == nil {
		t.Fatalf("InstallHooks expected error when hooks section cannot be written, got nil")
	}
	if !strings.Contains(err.Error(), "failed to write hooks section") {
		t.Errorf("expected wrapped hooks section error, got: %v", err)
	}
}

// ---- ExtractTelemetry ----

func TestExtractTelemetryReturnsNilForIrrelevantEvent(t *testing.T) {
	r := newRuntime()
	// Completely unknown event.
	result := r.ExtractTelemetry("some.event", map[string]string{"key": "val"})
	if result != nil {
		t.Errorf("expected nil for unknown event, got %+v", result)
	}
	// Old event names should no longer be accepted.
	result = r.ExtractTelemetry("codex.api_request", map[string]string{"model": "o3"})
	if result != nil {
		t.Errorf("expected nil for old codex.api_request event, got %+v", result)
	}
	result = r.ExtractTelemetry("gen_ai.content.completion", map[string]string{"model": "o3"})
	if result != nil {
		t.Errorf("expected nil for old gen_ai.content.completion event, got %+v", result)
	}
}

func TestExtractTelemetryAPIRequestInitiated(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"model":              "o3",
		"input_token_count":  "100",
		"output_token_count": "50",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
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

func TestExtractTelemetryTokenUsageMetric(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"model":              "o3",
		"input_token_count":  "200",
		"output_token_count": "75",
		"cached_token_count": "50",
	}
	result := r.ExtractTelemetry("codex.turn.token_usage", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "o3" {
		t.Errorf("expected model=o3, got %q", result.Model)
	}
	if result.InputTokens != 200 {
		t.Errorf("expected InputTokens=200, got %d", result.InputTokens)
	}
	if result.OutputTokens != 75 {
		t.Errorf("expected OutputTokens=75, got %d", result.OutputTokens)
	}
	if result.CacheReadTokens != 50 {
		t.Errorf("expected CacheReadTokens=50, got %d", result.CacheReadTokens)
	}
}

func TestExtractTelemetrySSEEvent(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"model":              "o3",
		"input_token_count":  "300",
		"output_token_count": "100",
	}
	result := r.ExtractTelemetry("codex.sse_event", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "o3" {
		t.Errorf("expected model=o3, got %q", result.Model)
	}
	if result.InputTokens != 300 {
		t.Errorf("expected InputTokens=300, got %d", result.InputTokens)
	}
}

func TestExtractTelemetryReasoningTokens(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"model":                 "o3",
		"output_token_count":    "50",
		"reasoning_token_count": "30",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.OutputTokens != 50 {
		t.Errorf("expected OutputTokens=50, got %d", result.OutputTokens)
	}
	if result.ReasoningTokens != 30 {
		t.Errorf("expected ReasoningTokens=30, got %d", result.ReasoningTokens)
	}
}

func TestExtractTelemetryFallbackAttributes(t *testing.T) {
	r := newRuntime()
	// Use gen_ai.* fallback keys for forward compatibility.
	attrs := map[string]string{
		"gen_ai.response.model":                "gpt-4",
		"gen_ai.usage.input_tokens":            "100",
		"gen_ai.usage.output_tokens":           "50",
		"gen_ai.usage.cache_read_input_tokens": "25",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord with fallback attrs")
	}
	if result.Model != "gpt-4" {
		t.Errorf("expected model=gpt-4, got %q", result.Model)
	}
	if result.InputTokens != 100 {
		t.Errorf("expected InputTokens=100, got %d", result.InputTokens)
	}
	if result.OutputTokens != 50 {
		t.Errorf("expected OutputTokens=50, got %d", result.OutputTokens)
	}
	if result.CacheReadTokens != 25 {
		t.Errorf("expected CacheReadTokens=25, got %d", result.CacheReadTokens)
	}
}

func TestExtractTelemetryCostAndDuration(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"model":       "o3",
		"cost_usd":    "0.0042",
		"duration_ms": "1500",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.CostUSD == nil || *result.CostUSD != 0.0042 {
		t.Errorf("expected CostUSD=0.0042, got %v", result.CostUSD)
	}
	if result.DurationMS == nil || *result.DurationMS != 1500 {
		t.Errorf("expected DurationMS=1500, got %v", result.DurationMS)
	}
}

func TestExtractTelemetryCacheCreationTokens(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"model":                        "o3",
		"cache_creation_token_count":   "40",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.CacheCreationTokens != 40 {
		t.Errorf("expected CacheCreationTokens=40, got %d", result.CacheCreationTokens)
	}
}

func TestExtractTelemetryCacheCreationFallback(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"gen_ai.response.model":                          "gpt-4",
		"gen_ai.usage.cache_creation_input_tokens":       "55",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.CacheCreationTokens != 55 {
		t.Errorf("expected CacheCreationTokens=55 via fallback, got %d", result.CacheCreationTokens)
	}
}

func TestExtractTelemetryNoModel(t *testing.T) {
	r := newRuntime()
	attrs := map[string]string{
		"input_token_count": "100",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result != nil {
		t.Errorf("expected nil when no model, got %+v", result)
	}
}

// ---- X-Sol-* header attribution path ----

// TestExtractTelemetryHeaderAttribution verifies that ExtractTelemetry can
// process events that arrive via the X-Sol-* header path. Attribution context
// (agent name, world) is injected as HTTP headers in CODEX_HOME/config.toml
// by EnsureConfigDir and forwarded by the ledger's OTLP receiver into the
// attrs map. This test confirms that ExtractTelemetry correctly handles events
// regardless of how attribution context is delivered.
func TestExtractTelemetryHeaderAttribution(t *testing.T) {
	r := newRuntime()
	// Simulate attrs as they arrive from the ledger after X-Sol-* header forwarding.
	// The ledger remaps X-Sol-Agent → agent.name, X-Sol-World → world, etc.
	attrs := map[string]string{
		"model":              "gpt-5.4",
		"input_token_count":  "150",
		"output_token_count": "60",
		// Attribution context from X-Sol-* headers (remapped by ledger).
		"agent.name": "Toast",
		"world":      "sol-dev",
	}
	result := r.ExtractTelemetry("codex.api_request_initiated", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord for event with X-Sol-* attribution")
	}
	if result.Model != "gpt-5.4" {
		t.Errorf("expected model=gpt-5.4, got %q", result.Model)
	}
	if result.InputTokens != 150 {
		t.Errorf("expected InputTokens=150, got %d", result.InputTokens)
	}
	if result.OutputTokens != 60 {
		t.Errorf("expected OutputTokens=60, got %d", result.OutputTokens)
	}
}

// ---- Helper function tests ----

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

func TestGuardToExecPolicyRule(t *testing.T) {
	tests := []struct {
		pattern  string
		wantRule string
		wantOK   bool
	}{
		{
			pattern:  "Bash(git push --force*)",
			wantRule: `prefix_rule(["git", "push", "--force"], decision="forbidden")`,
			wantOK:   true,
		},
		{
			pattern:  "Bash(rm -rf /*)",
			wantRule: `prefix_rule(["rm", "-rf", "/"], decision="forbidden")`,
			wantOK:   true,
		},
		{
			pattern:  "Bash(git reset --hard)",
			wantRule: `prefix_rule(["git", "reset", "--hard"], decision="forbidden")`,
			wantOK:   true,
		},
		{
			// Non-Bash tool guard — cannot express as exec policy rule.
			pattern: "EnterPlanMode",
			wantOK:  false,
		},
		{
			// Non-Bash tool guard with args — cannot express as exec policy rule.
			pattern: "Write(/etc/passwd*)",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		rule, ok := guardToExecPolicyRule(tt.pattern)
		if ok != tt.wantOK {
			t.Errorf("guardToExecPolicyRule(%q): ok=%v, want ok=%v", tt.pattern, ok, tt.wantOK)
			continue
		}
		if ok && rule != tt.wantRule {
			t.Errorf("guardToExecPolicyRule(%q) = %q, want %q", tt.pattern, rule, tt.wantRule)
		}
	}
}

func TestToTOMLStringArray(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{[]string{"sol", "heartbeat"}, `["sol", "heartbeat"]`},
		{[]string{"notify-send", "Codex"}, `["notify-send", "Codex"]`},
		{[]string{"single"}, `["single"]`},
	}
	for _, tt := range tests {
		got := toTOMLStringArray(tt.input)
		if got != tt.want {
			t.Errorf("toTOMLStringArray(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestWriteProjectConfigBlockCreatesFile(t *testing.T) {
	dir := t.TempDir()

	if err := writeProjectConfigBlock(dir, "notify = [\"sol\"]\n"); err != nil {
		t.Fatalf("writeProjectConfigBlock failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, projectConfigBeginMarker) {
		t.Errorf("expected BEGIN marker, got:\n%s", content)
	}
	if !strings.Contains(content, projectConfigEndMarker) {
		t.Errorf("expected END marker, got:\n%s", content)
	}
	if !strings.Contains(content, `notify = ["sol"]`) {
		t.Errorf("expected notify content, got:\n%s", content)
	}
}

func TestWriteProjectConfigBlockIdempotent(t *testing.T) {
	dir := t.TempDir()

	content := "notify = [\"sol\", \"heartbeat\"]\n"

	if err := writeProjectConfigBlock(dir, content); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))

	if err := writeProjectConfigBlock(dir, content); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))

	if string(first) != string(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestWriteProjectConfigBlockReplacesOnChange(t *testing.T) {
	dir := t.TempDir()

	if err := writeProjectConfigBlock(dir, "notify = [\"old\"]\n"); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	if err := writeProjectConfigBlock(dir, "notify = [\"new\"]\n"); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	content := string(got)
	if strings.Contains(content, "old") {
		t.Errorf("old content should be replaced, got:\n%s", content)
	}
	if !strings.Contains(content, `notify = ["new"]`) {
		t.Errorf("expected new content, got:\n%s", content)
	}
	if strings.Count(content, projectConfigBeginMarker) != 1 {
		t.Errorf("expected exactly one BEGIN marker, got:\n%s", content)
	}
}

func TestWriteGuardRulesPropagatesMkdirError(t *testing.T) {
	dir := t.TempDir()

	// Block .codex/rules by pre-creating it as a regular file.
	blockPath(t, filepath.Join(dir, ".codex", "rules"))

	guards := []runtime.Guard{
		{Pattern: "Bash(rm -rf /*)", Command: "exit 2"},
	}
	err := writeGuardRules(dir, guards)
	if err == nil {
		t.Fatalf("writeGuardRules expected error when .codex/rules is not a directory, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create .codex/rules dir") {
		t.Errorf("expected mkdir error wrapping, got: %v", err)
	}
}

// ---- parseSections / renderSections round-trip ----

func TestSectionsRoundTrip(t *testing.T) {
	// Build a well-formed AGENTS.override.md with known sections.
	input := fmt.Sprintf("<!-- %s -->\nproject content\n\n<!-- %s -->\npersona content\n", sectionProject, sectionPersona)

	sections := parseSections(input)
	if !strings.Contains(sections[sectionProject], "project content") {
		t.Errorf("expected project content in PROJECT section, got %q", sections[sectionProject])
	}
	if !strings.Contains(sections[sectionPersona], "persona content") {
		t.Errorf("expected persona content in PERSONA section, got %q", sections[sectionPersona])
	}

	rendered := renderSections(sections)
	if !strings.Contains(rendered, "project content") {
		t.Errorf("expected project content in rendered output, got %q", rendered)
	}
	if !strings.Contains(rendered, "persona content") {
		t.Errorf("expected persona content in rendered output, got %q", rendered)
	}
	// PROJECT section should come before PERSONA.
	projectIdx := strings.Index(rendered, "<!-- "+sectionProject+" -->")
	personaIdx := strings.Index(rendered, "<!-- "+sectionPersona+" -->")
	if projectIdx >= personaIdx {
		t.Errorf("PROJECT section should come before PERSONA in rendered output")
	}
}

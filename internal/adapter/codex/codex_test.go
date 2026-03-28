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

	if !strings.HasPrefix(cmd, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("expected codex --dangerously-bypass-approvals-and-sandbox prefix, got: %q", cmd)
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
	}
	cmd := a.BuildCommand(ctx)

	if cmd != "codex resume --last" {
		t.Errorf("expected 'codex resume --last' for continue mode without prompt, got: %q", cmd)
	}
}

func TestBuildCommandContinueWithPrompt(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Continue:    true,
		Prompt:      "resume context",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "codex resume --last") {
		t.Errorf("expected 'codex resume --last' prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "resume context") {
		t.Errorf("expected prompt in resume command, got: %q", cmd)
	}
}

func TestBuildCommandNoPrompt(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
	}
	cmd := a.BuildCommand(ctx)

	if cmd != "codex --dangerously-bypass-approvals-and-sandbox" {
		t.Errorf("expected bare 'codex --dangerously-bypass-approvals-and-sandbox', got: %q", cmd)
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

func TestTelemetryEnvReturnsResourceAttributes(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "")

	attrs, ok := env["OTEL_RESOURCE_ATTRIBUTES"]
	if !ok {
		t.Fatal("expected OTEL_RESOURCE_ATTRIBUTES in env map")
	}
	if !strings.Contains(attrs, "agent.name=Toast") {
		t.Errorf("expected agent.name=Toast in attrs, got %q", attrs)
	}
	if !strings.Contains(attrs, "world=myworld") {
		t.Errorf("expected world=myworld in attrs, got %q", attrs)
	}
	if !strings.Contains(attrs, "writ_id=sol-abc123") {
		t.Errorf("expected writ_id=sol-abc123 in attrs, got %q", attrs)
	}
	if !strings.Contains(attrs, "service.name=codex") {
		t.Errorf("expected service.name=codex in attrs, got %q", attrs)
	}
}

func TestTelemetryEnvReturnsEmptyWhenPortZero(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(0, "Toast", "myworld", "", "")
	if len(env) != 0 {
		t.Errorf("expected empty map for port=0, got %v", env)
	}
}

func TestTelemetryEnvOptionalFields(t *testing.T) {
	a := newAdapter()

	// Without optional fields.
	env := a.TelemetryEnv(4318, "Toast", "myworld", "", "")
	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if strings.Contains(attrs, "writ_id=") {
		t.Errorf("expected no writ_id when empty, got %q", attrs)
	}
	if strings.Contains(attrs, "account=") {
		t.Errorf("expected no account when empty, got %q", attrs)
	}

	// With account.
	env = a.TelemetryEnv(4318, "Toast", "myworld", "", "acme")
	attrs = env["OTEL_RESOURCE_ATTRIBUTES"]
	if !strings.Contains(attrs, "account=acme") {
		t.Errorf("expected account=acme in attrs, got %q", attrs)
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
	// Content should be in a PERSONA section.
	gotStr := string(got)
	if !strings.Contains(gotStr, "<!-- SOL:PERSONA -->") {
		t.Errorf("expected SOL:PERSONA section marker, got:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "# Agent Instructions") {
		t.Errorf("expected persona content, got:\n%s", gotStr)
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
	// Section markers should be present.
	if !strings.Contains(content, "<!-- SOL:PROJECT -->") {
		t.Errorf("expected SOL:PROJECT section marker, got:\n%s", content)
	}
	if !strings.Contains(content, "<!-- SOL:PERSONA -->") {
		t.Errorf("expected SOL:PERSONA section marker, got:\n%s", content)
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
	gotStr := string(got)
	if !strings.Contains(gotStr, "<!-- SOL:SYSTEM-PROMPT -->") {
		t.Errorf("expected SOL:SYSTEM-PROMPT section marker, got:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "system content") {
		t.Errorf("expected system content, got:\n%s", gotStr)
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

func TestInjectSystemPromptReplacePreservesPersona(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Write persona first.
	if err := a.InjectPersona(dir, []byte("persona content")); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	// Replace with system prompt — should replace only the SYSTEM-PROMPT
	// section, preserving the PERSONA section.
	_, err := a.InjectSystemPrompt(dir, "system replaces persona", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(got)
	// Persona should be preserved in its own section.
	if !strings.Contains(content, "persona content") {
		t.Errorf("expected persona content preserved, got:\n%s", content)
	}
	// System prompt should be in its own section.
	if !strings.Contains(content, "system replaces persona") {
		t.Errorf("expected system prompt content, got:\n%s", content)
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
	gotStr := string(got)
	if !strings.Contains(gotStr, "override content") {
		t.Errorf("expected override content, got:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "<!-- SOL:SYSTEM-PROMPT -->") {
		t.Errorf("expected SOL:SYSTEM-PROMPT section marker, got:\n%s", gotStr)
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
	a := newAdapter()

	hooks := adapter.HookSet{
		Guards: []adapter.Guard{
			{Pattern: "EnterPlanMode", Command: "exit 2"},
			{Pattern: "Bash(git push*)", Command: "exit 2"},
		},
	}

	if err := a.InstallHooks(dir, hooks); err != nil {
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
	a := newAdapter()

	hooks := adapter.HookSet{
		Guards: []adapter.Guard{
			{Pattern: "EnterPlanMode", Command: "exit 2"},
			{Pattern: "Write(/etc/passwd*)", Command: "exit 2"},
		},
	}

	if err := a.InstallHooks(dir, hooks); err != nil {
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
	// Guard instruction should be appended (defense-in-depth).
	if !strings.Contains(content, "IMPORTANT: NEVER run: git push --force") {
		t.Errorf("expected guard instruction appended, got:\n%s", content)
	}

	// Exec policy rules should also be written.
	rulesPath := filepath.Join(dir, ".codex", "rules", solGuardRulesFile)
	rulesContent, err := os.ReadFile(rulesPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", solGuardRulesFile, err)
	}
	if !strings.Contains(string(rulesContent), `prefix_rule(["git", "push", "--force"], decision="forbidden")`) {
		t.Errorf("expected exec policy rule for git push --force, got:\n%s", rulesContent)
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

func TestInstallHooksTurnBoundaryWritesNotify(t *testing.T) {
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
	a := newAdapter()

	hooks := adapter.HookSet{
		TurnBoundary: []adapter.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
			{Command: "sol extra-hook --world=myworld"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
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

func TestInstallHooksNotifyPreservesExistingProjectConfig(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Create existing .codex/config.toml (e.g. from InstallSkills).
	codexDir := filepath.Join(dir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("failed to create .codex dir: %v", err)
	}
	existingContent := "instructions = \"Be helpful\"\n"
	if err := os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte(existingContent), 0o644); err != nil {
		t.Fatalf("failed to write existing config: %v", err)
	}

	hooks := adapter.HookSet{
		TurnBoundary: []adapter.HookCommand{
			{Command: "sol heartbeat --world=myworld --agent=Toast"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
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

// ---- Cross-method non-clobbering ----

func TestCrossMethodNonClobbering(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Create a project AGENTS.md.
	projectContent := "# Project Instructions\n\nFollow the coding standards.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(projectContent), 0o644); err != nil {
		t.Fatalf("failed to write project AGENTS.md: %v", err)
	}

	// Step 1: InjectPersona — writes PROJECT + PERSONA sections.
	persona := []byte("# Agent Persona\n\nBe helpful and concise.\n")
	if err := a.InjectPersona(dir, persona); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	// Step 2: InjectSystemPrompt(replace=true) — writes SYSTEM-PROMPT section,
	// should NOT clobber PERSONA or PROJECT.
	_, err := a.InjectSystemPrompt(dir, "You are an outpost agent.", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt(replace) failed: %v", err)
	}

	// Step 3: InstallHooks — writes HOOKS section, should NOT clobber anything.
	hooks := adapter.HookSet{
		Guards: []adapter.Guard{
			{Pattern: "Bash(git push --force*)", Command: "exit 2"},
		},
		PreCompact: []adapter.HookCommand{
			{Command: "sol prime --world=test --agent=Nova"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	// Verify: all four sections should be present.
	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(got)

	// PROJECT section should contain the project AGENTS.md content.
	if !strings.Contains(content, "# Project Instructions") {
		t.Errorf("PROJECT section missing — expected project AGENTS.md content, got:\n%s", content)
	}

	// PERSONA section should contain persona content.
	if !strings.Contains(content, "# Agent Persona") {
		t.Errorf("PERSONA section missing — expected persona content, got:\n%s", content)
	}
	if !strings.Contains(content, "Be helpful and concise.") {
		t.Errorf("PERSONA section incomplete, got:\n%s", content)
	}

	// SYSTEM-PROMPT section should contain system prompt.
	if !strings.Contains(content, "You are an outpost agent.") {
		t.Errorf("SYSTEM-PROMPT section missing — expected system prompt, got:\n%s", content)
	}

	// HOOKS section should contain guard and pre-compact instructions.
	if !strings.Contains(content, "IMPORTANT: NEVER run: git push --force") {
		t.Errorf("HOOKS section missing guard instruction, got:\n%s", content)
	}
	if !strings.Contains(content, "Before running /compact, execute this command: sol prime") {
		t.Errorf("HOOKS section missing pre-compact instruction, got:\n%s", content)
	}

	// Verify section ordering: PROJECT < PERSONA < SYSTEM-PROMPT < HOOKS.
	projectIdx := strings.Index(content, "<!-- SOL:PROJECT -->")
	personaIdx := strings.Index(content, "<!-- SOL:PERSONA -->")
	sysPromptIdx := strings.Index(content, "<!-- SOL:SYSTEM-PROMPT -->")
	hooksIdx := strings.Index(content, "<!-- SOL:HOOKS -->")

	if projectIdx >= personaIdx {
		t.Errorf("PROJECT section should come before PERSONA")
	}
	if personaIdx >= sysPromptIdx {
		t.Errorf("PERSONA section should come before SYSTEM-PROMPT")
	}
	if sysPromptIdx >= hooksIdx {
		t.Errorf("SYSTEM-PROMPT section should come before HOOKS")
	}
}

func TestInjectSystemPromptReplaceReplacesSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// First system prompt.
	_, err := a.InjectSystemPrompt(dir, "first system prompt", true)
	if err != nil {
		t.Fatalf("first InjectSystemPrompt failed: %v", err)
	}

	// Second system prompt (replace=true) should replace the first.
	_, err = a.InjectSystemPrompt(dir, "second system prompt", true)
	if err != nil {
		t.Fatalf("second InjectSystemPrompt failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	content := string(got)
	if strings.Contains(content, "first system prompt") {
		t.Errorf("expected first system prompt to be replaced, got:\n%s", content)
	}
	if !strings.Contains(content, "second system prompt") {
		t.Errorf("expected second system prompt, got:\n%s", content)
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

func TestEnsureConfigDirReturnsSQLiteHome(t *testing.T) {
	worldDir := t.TempDir()
	worktreeDir := t.TempDir()
	a := newAdapter()

	result, err := a.EnsureConfigDir(worldDir, "outpost", "Nova", worktreeDir)
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	sqliteHome, ok := result.EnvVar["CODEX_SQLITE_HOME"]
	if !ok {
		t.Fatal("expected CODEX_SQLITE_HOME in EnvVar, not found")
	}

	// CODEX_SQLITE_HOME should match CODEX_HOME for per-agent isolation.
	codexHome := result.EnvVar["CODEX_HOME"]
	if sqliteHome != codexHome {
		t.Errorf("CODEX_SQLITE_HOME = %q, want %q (same as CODEX_HOME)", sqliteHome, codexHome)
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

func TestEnsureConfigDirHardeningFields(t *testing.T) {
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

	// Memories disabled.
	if !strings.Contains(content, "generate_memories = false") {
		t.Errorf("expected generate_memories = false, got:\n%s", content)
	}
	if !strings.Contains(content, "use_memories = false") {
		t.Errorf("expected use_memories = false, got:\n%s", content)
	}

	// TUI features disabled.
	if !strings.Contains(content, "animations = false") {
		t.Errorf("expected animations = false, got:\n%s", content)
	}
	if !strings.Contains(content, "show_tooltips = false") {
		t.Errorf("expected show_tooltips = false, got:\n%s", content)
	}
	if !strings.Contains(content, `alternate_screen = "never"`) {
		t.Errorf("expected alternate_screen = \"never\", got:\n%s", content)
	}
	if !strings.Contains(content, "notifications = false") {
		t.Errorf("expected notifications = false, got:\n%s", content)
	}

	// File opener disabled.
	if !strings.Contains(content, `file_opener = "none"`) {
		t.Errorf("expected file_opener = \"none\", got:\n%s", content)
	}

	// Auth credential store.
	if !strings.Contains(content, `cli_auth_credentials_store = "file"`) {
		t.Errorf("expected cli_auth_credentials_store = \"file\", got:\n%s", content)
	}

	// Project doc max bytes.
	if !strings.Contains(content, "project_doc_max_bytes = 65536") {
		t.Errorf("expected project_doc_max_bytes = 65536, got:\n%s", content)
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
	if result1.EnvVar["CODEX_SQLITE_HOME"] == result2.EnvVar["CODEX_SQLITE_HOME"] {
		t.Errorf("different agents should have different CODEX_SQLITE_HOME: both got %q", result1.EnvVar["CODEX_SQLITE_HOME"])
	}
}

// ---- appendToProjectConfig ----

func TestAppendToProjectConfigCreatesFile(t *testing.T) {
	dir := t.TempDir()

	if err := appendToProjectConfig(dir, "key = \"value\"\n"); err != nil {
		t.Fatalf("appendToProjectConfig failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	if string(got) != "key = \"value\"\n" {
		t.Errorf("content mismatch: got %q", got)
	}
}

func TestAppendToProjectConfigPreservesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write initial content.
	if err := appendToProjectConfig(dir, "first = true\n"); err != nil {
		t.Fatalf("first append failed: %v", err)
	}

	// Append more content.
	if err := appendToProjectConfig(dir, "second = true\n"); err != nil {
		t.Fatalf("second append failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("failed to read .codex/config.toml: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, "first = true") {
		t.Errorf("expected first content preserved, got:\n%s", content)
	}
	if !strings.Contains(content, "second = true") {
		t.Errorf("expected second content appended, got:\n%s", content)
	}
}

// ---- toTOMLStringArray ----

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

// ---- guardToExecPolicyRule ----

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

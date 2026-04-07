package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/adapter"
)

func newAdapter() *Adapter {
	return New()
}

// ---- InjectPersona ----

func TestInjectPersona(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	content := []byte("# Outpost Agent: Toast\n\nHello world.\n")
	if err := a.InjectPersona(dir, content); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	path := filepath.Join(dir, "CLAUDE.local.md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestInjectPersonaOverwrites(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	_ = a.InjectPersona(dir, []byte("old content"))
	newContent := []byte("new content")
	if err := a.InjectPersona(dir, newContent); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.local.md"))
	if string(got) != string(newContent) {
		t.Errorf("expected overwrite, got %q", got)
	}
}

// ---- InstallSkills ----

func TestInstallSkillsCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	skills := []adapter.Skill{
		{Name: "resolve-and-handoff", Content: "# Resolve & Handoff\n"},
		{Name: "test-skill", Content: "# Test Skill\n"},
	}

	if err := a.InstallSkills(dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	for _, s := range skills {
		p := filepath.Join(dir, ".claude", "skills", s.Name, "SKILL.md")
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("skill %q not found: %v", s.Name, err)
			continue
		}
		if string(got) != s.Content {
			t.Errorf("skill %q content mismatch:\ngot:  %q\nwant: %q", s.Name, got, s.Content)
		}
		// Verify sol-managed marker exists.
		marker := filepath.Join(dir, ".claude", "skills", s.Name, ".sol-managed")
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("skill %q missing .sol-managed marker: %v", s.Name, err)
		}
	}
}

func TestInstallSkillsRemovesStale(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Pre-seed a stale sol-managed skill.
	staleDir := filepath.Join(dir, ".claude", "skills", "stale-skill")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "SKILL.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, ".sol-managed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Install one current skill.
	skills := []adapter.Skill{
		{Name: "test-skill", Content: "# Test Skill\n"},
	}
	if err := a.InstallSkills(dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Stale should be gone.
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale sol-managed skill directory should have been removed")
	}

	// Current should exist.
	p := filepath.Join(dir, ".claude", "skills", "test-skill", "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected memories skill to exist: %v", err)
	}
}

func TestInstallSkillsEmptyList(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Pre-seed a sol-managed skill.
	skillDir := filepath.Join(dir, ".claude", "skills", "old-skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, ".sol-managed"), nil, 0o644)

	// Install empty list — all stale sol-managed skills should be removed.
	if err := a.InstallSkills(dir, []adapter.Skill{}); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("old sol-managed skill should have been removed with empty input")
	}
}

func TestInstallSkillsPreservesNonSolSkills(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Pre-seed a custom project skill (no .sol-managed marker).
	customDir := filepath.Join(dir, ".claude", "skills", "custom-tool")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "SKILL.md"), []byte("# Custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install sol skills — custom skill should be preserved.
	skills := []adapter.Skill{
		{Name: "test-skill", Content: "# Test Skill\n"},
	}
	if err := a.InstallSkills(dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Custom skill should still exist.
	got, err := os.ReadFile(filepath.Join(customDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("custom skill was deleted: %v", err)
	}
	if string(got) != "# Custom\n" {
		t.Errorf("custom skill content changed: got %q", got)
	}

	// Sol skill should exist.
	p := filepath.Join(dir, ".claude", "skills", "test-skill", "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected memories skill to exist: %v", err)
	}
}

func TestInstallSkillsEmptyListPreservesNonSolSkills(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Pre-seed a custom project skill (no marker).
	customDir := filepath.Join(dir, ".claude", "skills", "custom-tool")
	_ = os.MkdirAll(customDir, 0o755)
	_ = os.WriteFile(filepath.Join(customDir, "SKILL.md"), []byte("custom"), 0o644)

	// Install empty list — custom skill should be preserved.
	if err := a.InstallSkills(dir, []adapter.Skill{}); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	if _, err := os.Stat(customDir); err != nil {
		t.Errorf("custom skill should have been preserved: %v", err)
	}
}

// ---- InjectSystemPrompt ----

func TestInjectSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	content := "You are a helpful agent."
	if _, err := a.InjectSystemPrompt(dir, content, false); err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	p := filepath.Join(dir, ".claude", "system-prompt.md")
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read system-prompt.md: %v", err)
	}
	if string(got) != content {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestInjectSystemPromptCreatesDotClaude(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// .claude doesn't exist yet.
	if _, err := a.InjectSystemPrompt(dir, "prompt", false); err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude")); err != nil {
		t.Errorf(".claude directory should exist: %v", err)
	}
}

func TestInjectSystemPromptReturnsPath(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	path, err := a.InjectSystemPrompt(dir, "content", false)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}
	if path != ".claude/system-prompt.md" {
		t.Errorf("expected .claude/system-prompt.md, got %q", path)
	}
}

// ---- InstallHooks ----

func readHookSettings(t *testing.T, dir string) map[string][]hookMatcherGroup {
	t.Helper()
	p := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}
	var s hookSettings
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("failed to parse settings.local.json: %v", err)
	}
	return s.Hooks
}

func TestInstallHooksSessionStart(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		SessionStart: []adapter.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	hooksMap := readHookSettings(t, dir)
	groups, ok := hooksMap["SessionStart"]
	if !ok {
		t.Fatal("expected SessionStart hooks")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 SessionStart group, got %d", len(groups))
	}
	if groups[0].Hooks[0].Command != "sol prime --world=myworld --agent=Toast" {
		t.Errorf("unexpected command: %q", groups[0].Hooks[0].Command)
	}
}

func TestInstallHooksPreCompact(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		PreCompact: []adapter.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast --compact"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	hooksMap := readHookSettings(t, dir)
	groups, ok := hooksMap["PreCompact"]
	if !ok {
		t.Fatal("expected PreCompact hooks")
	}
	if groups[0].Hooks[0].Command != "sol prime --world=myworld --agent=Toast --compact" {
		t.Errorf("unexpected command: %q", groups[0].Hooks[0].Command)
	}
}

func TestInstallHooksGuards(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		Guards: []adapter.Guard{
			{Pattern: "EnterPlanMode", Command: `echo "BLOCKED" >&2; exit 2`},
			{Pattern: "Bash(git push --force*)", Command: "sol guard dangerous-command"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	hooksMap := readHookSettings(t, dir)
	groups, ok := hooksMap["PreToolUse"]
	if !ok {
		t.Fatal("expected PreToolUse hooks")
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 PreToolUse groups, got %d", len(groups))
	}
	if groups[0].Matcher != "EnterPlanMode" {
		t.Errorf("expected EnterPlanMode matcher, got %q", groups[0].Matcher)
	}
	if groups[1].Matcher != "Bash(git push --force*)" {
		t.Errorf("expected force-push matcher, got %q", groups[1].Matcher)
	}
}

func TestInstallHooksTurnBoundary(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		TurnBoundary: []adapter.HookCommand{
			{Command: "sol nudge drain --world=myworld --agent=Toast"},
		},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	hooksMap := readHookSettings(t, dir)
	groups, ok := hooksMap["UserPromptSubmit"]
	if !ok {
		t.Fatal("expected UserPromptSubmit hooks")
	}
	if groups[0].Hooks[0].Command != "sol nudge drain --world=myworld --agent=Toast" {
		t.Errorf("unexpected command: %q", groups[0].Hooks[0].Command)
	}
}

func TestInstallHooksFullHookSet(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	hooks := adapter.HookSet{
		SessionStart: []adapter.HookCommand{{Command: "sol prime --world=w --agent=A"}},
		PreCompact:   []adapter.HookCommand{{Command: "sol prime --world=w --agent=A --compact"}},
		Guards: []adapter.Guard{
			{Pattern: "EnterPlanMode", Command: "exit 2"},
		},
		TurnBoundary: []adapter.HookCommand{{Command: "sol nudge drain --world=w --agent=A"}},
	}
	if err := a.InstallHooks(dir, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	hooksMap := readHookSettings(t, dir)
	for _, key := range []string{"SessionStart", "PreCompact", "PreToolUse", "UserPromptSubmit"} {
		if _, ok := hooksMap[key]; !ok {
			t.Errorf("expected hook key %q", key)
		}
	}
}

func TestInstallHooksEmptyHookSet(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// Empty hook set should produce valid JSON with empty hooks map.
	if err := a.InstallHooks(dir, adapter.HookSet{}); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	p := filepath.Join(dir, ".claude", "settings.local.json")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read settings.local.json: %v", err)
	}
	if !strings.Contains(string(data), `"hooks"`) {
		t.Error("expected hooks key in output")
	}
}

// ---- BuildCommand ----

func TestBuildCommandBasic(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: dir,
		Prompt:      "Hello agent",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "claude --dangerously-skip-permissions") {
		t.Errorf("expected claude prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings flag, got: %q", cmd)
	}
	if !strings.Contains(cmd, "Hello agent") {
		t.Errorf("expected prime in command, got: %q", cmd)
	}
}

func TestBuildCommandWithResume(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: dir,
		Continue:    true,
		Prompt:      "resume",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.Contains(cmd, "--continue") {
		t.Errorf("expected --continue flag, got: %q", cmd)
	}
}

func TestBuildCommandWithModel(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: dir,
		Model:       "claude-opus-4-5",
		Prompt:      "go",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.Contains(cmd, "--model claude-opus-4-5") {
		t.Errorf("expected --model flag, got: %q", cmd)
	}
}

func TestBuildCommandReplacePrompt(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir:      dir,
		ReplacePrompt:    true,
		SystemPromptFile: ".claude/system-prompt.md",
		Prompt:           "go",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.Contains(cmd, "--system-prompt-file") {
		t.Errorf("expected --system-prompt-file, got: %q", cmd)
	}
	if strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("should not have --append-system-prompt-file, got: %q", cmd)
	}
}

func TestBuildCommandAppendPrompt(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir:      dir,
		ReplacePrompt:    false,
		SystemPromptFile: ".claude/system-prompt.md",
		Prompt:           "go",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("expected --append-system-prompt-file, got: %q", cmd)
	}
}

func TestBuildCommandNoSystemPromptFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	// No SystemPromptFile in context — flag should be absent.
	ctx := adapter.CommandContext{
		WorktreeDir:   dir,
		ReplacePrompt: true,
		Prompt:        "go",
	}
	cmd := a.BuildCommand(ctx)

	if strings.Contains(cmd, "system-prompt") {
		t.Errorf("should not have system-prompt flag when SystemPromptFile is empty, got: %q", cmd)
	}
}

func TestBuildCommandSOLSessionCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	dir := t.TempDir()
	a := newAdapter()

	cmd := a.BuildCommand(adapter.CommandContext{WorktreeDir: dir})
	if cmd != "sleep 300" {
		t.Errorf("expected SOL_SESSION_COMMAND override, got: %q", cmd)
	}
}

// ---- CredentialEnv ----

func TestCredentialEnvOAuthToken(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "oauth_token", Token: "my-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := env["CLAUDE_CODE_OAUTH_TOKEN"]; !ok || v != "my-token" {
		t.Errorf("expected CLAUDE_CODE_OAUTH_TOKEN=my-token, got %v", env)
	}
	if _, ok := env["ANTHROPIC_API_KEY"]; ok {
		t.Error("should not set ANTHROPIC_API_KEY for oauth_token")
	}
}

func TestCredentialEnvAPIKey(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "api_key", Token: "sk-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := env["ANTHROPIC_API_KEY"]; !ok || v != "sk-abc" {
		t.Errorf("expected ANTHROPIC_API_KEY=sk-abc, got %v", env)
	}
	if _, ok := env["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Error("should not set CLAUDE_CODE_OAUTH_TOKEN for api_key")
	}
}

func TestCredentialEnvUnknown(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "unknown", Token: "val"})
	if err == nil {
		t.Error("expected error for unknown credential type, got nil")
	}
	if len(env) != 0 {
		t.Errorf("expected nil/empty map for unknown credential type, got %v", env)
	}
}

// ---- TelemetryEnv ----

func TestTelemetryEnvAllVarsPresent(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "")

	expected := []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_RESOURCE_ATTRIBUTES",
	}
	for _, k := range expected {
		if _, ok := env[k]; !ok {
			t.Errorf("expected env var %q to be set", k)
		}
	}
}

func TestTelemetryEnvValues(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "")

	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("expected CLAUDE_CODE_ENABLE_TELEMETRY=1, got %q", env["CLAUDE_CODE_ENABLE_TELEMETRY"])
	}
	if env["OTEL_LOGS_EXPORTER"] != "otlp" {
		t.Errorf("expected OTEL_LOGS_EXPORTER=otlp, got %q", env["OTEL_LOGS_EXPORTER"])
	}
	if env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != "http://localhost:4318/v1/logs" {
		t.Errorf("unexpected endpoint: %q", env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"])
	}
	if env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"] != "http/json" {
		t.Errorf("unexpected protocol: %q", env["OTEL_EXPORTER_OTLP_LOGS_PROTOCOL"])
	}

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if !strings.Contains(attrs, "agent.name=Toast") {
		t.Errorf("expected agent.name in attributes: %q", attrs)
	}
	if !strings.Contains(attrs, "world=myworld") {
		t.Errorf("expected world in attributes: %q", attrs)
	}
	if !strings.Contains(attrs, "writ_id=sol-abc123") {
		t.Errorf("expected writ_id in attributes: %q", attrs)
	}
}

func TestTelemetryEnvNoWritID(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "", "")

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if strings.Contains(attrs, "writ_id") {
		t.Errorf("should not include writ_id when empty: %q", attrs)
	}
}

func TestTelemetryEnvCustomPort(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(9999, "Toast", "myworld", "", "")

	if env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != "http://localhost:9999/v1/logs" {
		t.Errorf("unexpected endpoint: %q", env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"])
	}
}

func TestTelemetryEnvDisabledWhenPortZero(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(0, "Toast", "myworld", "sol-abc123", "")
	if len(env) != 0 {
		t.Errorf("expected empty map for port=0, got %v", env)
	}
}

func TestTelemetryEnvWithAccount(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "personal")

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if !strings.Contains(attrs, "account=personal") {
		t.Errorf("expected account in attributes: %q", attrs)
	}
}

func TestTelemetryEnvNoAccount(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "")

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if strings.Contains(attrs, "account") {
		t.Errorf("should not include account when empty: %q", attrs)
	}
}

// ---- CalloutCommand ----

func TestCalloutCommand(t *testing.T) {
	a := newAdapter()
	if got := a.CalloutCommand(); got != "claude -p" {
		t.Errorf("CalloutCommand() = %q, want %q", got, "claude -p")
	}
}

// ---- Name ----

func TestName(t *testing.T) {
	a := newAdapter()
	if a.Name() != "claude" {
		t.Errorf("expected Name()=claude, got %q", a.Name())
	}
}

// ---- SupportsHook ----

func TestSupportsHookAllTrue(t *testing.T) {
	a := newAdapter()
	for _, hookType := range []string{"SessionStart", "PreCompact", "TurnBoundary", "Guard"} {
		if !a.SupportsHook(hookType) {
			t.Errorf("SupportsHook(%q) = false, want true", hookType)
		}
	}
}

func TestSupportsHookUnknownType(t *testing.T) {
	a := newAdapter()
	if !a.SupportsHook("UnknownHookType") {
		t.Error("SupportsHook(unknown) = false, want true (Claude supports all)")
	}
}

// ---- ExtractTelemetry ----

func TestExtractTelemetryBasic(t *testing.T) {
	a := newAdapter()
	attrs := map[string]string{
		"model":                 "claude-sonnet-4",
		"input_tokens":          "100",
		"output_tokens":         "200",
		"cache_read_tokens":     "50",
		"cache_creation_tokens": "25",
	}
	result := a.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "claude-sonnet-4" {
		t.Errorf("expected model=claude-sonnet-4, got %q", result.Model)
	}
	if result.InputTokens != 100 {
		t.Errorf("expected InputTokens=100, got %d", result.InputTokens)
	}
	if result.OutputTokens != 200 {
		t.Errorf("expected OutputTokens=200, got %d", result.OutputTokens)
	}
	if result.CacheReadTokens != 50 {
		t.Errorf("expected CacheReadTokens=50, got %d", result.CacheReadTokens)
	}
	if result.CacheCreationTokens != 25 {
		t.Errorf("expected CacheCreationTokens=25, got %d", result.CacheCreationTokens)
	}
	if result.ReasoningTokens != 0 {
		t.Errorf("expected ReasoningTokens=0, got %d", result.ReasoningTokens)
	}
}

func TestExtractTelemetryReasoningTokens(t *testing.T) {
	a := newAdapter()
	attrs := map[string]string{
		"model":            "claude-sonnet-4",
		"input_tokens":     "100",
		"output_tokens":    "200",
		"reasoning_tokens": "75",
	}
	result := a.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.ReasoningTokens != 75 {
		t.Errorf("expected ReasoningTokens=75, got %d", result.ReasoningTokens)
	}
	if result.OutputTokens != 200 {
		t.Errorf("expected OutputTokens=200, got %d", result.OutputTokens)
	}
}

func TestExtractTelemetryReasoningTokensFallbackCodexName(t *testing.T) {
	// Forward-compatible fallback: accept the codex-style attribute name.
	a := newAdapter()
	attrs := map[string]string{
		"model":                 "claude-sonnet-4",
		"output_tokens":         "200",
		"reasoning_token_count": "42",
	}
	result := a.ExtractTelemetry("api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.ReasoningTokens != 42 {
		t.Errorf("expected ReasoningTokens=42, got %d", result.ReasoningTokens)
	}
}

func TestExtractTelemetryReasoningTokensGenAIFallback(t *testing.T) {
	// Forward-compatible fallback: accept the OTel gen_ai.* attribute name.
	a := newAdapter()
	attrs := map[string]string{
		"gen_ai.response.model":        "claude-sonnet-4",
		"gen_ai.usage.input_tokens":    "100",
		"gen_ai.usage.output_tokens":   "200",
		"gen_ai.usage.reasoning_tokens": "33",
	}
	result := a.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.ReasoningTokens != 33 {
		t.Errorf("expected ReasoningTokens=33, got %d", result.ReasoningTokens)
	}
}

// ---- DefaultModel ----

func TestDefaultModel(t *testing.T) {
	a := New()
	if got := a.DefaultModel(); got != "sonnet" {
		t.Errorf("DefaultModel() = %q, want %q", got, "sonnet")
	}
}

// ---- Registry ----

func TestAdapterImplementsInterface(t *testing.T) {
	// Compile-time check that *Adapter implements adapter.RuntimeAdapter.
	var _ adapter.RuntimeAdapter = (*Adapter)(nil)
}

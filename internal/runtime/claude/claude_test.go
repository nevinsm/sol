package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/channelplugin"
	"github.com/nevinsm/sol/internal/runtime"
)

// ---- Descriptor ----

func TestDescriptorValues(t *testing.T) {
	r := New()
	d := r.Descriptor()

	if d.Name != "claude" {
		t.Errorf("Name = %q, want %q", d.Name, "claude")
	}
	if d.PersonaFile != "CLAUDE.local.md" {
		t.Errorf("PersonaFile = %q, want %q", d.PersonaFile, "CLAUDE.local.md")
	}
	if d.SkillsDir != ".claude/skills" {
		t.Errorf("SkillsDir = %q, want %q", d.SkillsDir, ".claude/skills")
	}
	if d.ConfigDirEnv != "CLAUDE_CONFIG_DIR" {
		t.Errorf("ConfigDirEnv = %q, want %q", d.ConfigDirEnv, "CLAUDE_CONFIG_DIR")
	}
	if d.CredentialFile != ".credentials.json" {
		t.Errorf("CredentialFile = %q, want %q", d.CredentialFile, ".credentials.json")
	}
	if d.GlobalCredsPath != "~/.claude/.credentials.json" {
		t.Errorf("GlobalCredsPath = %q, want %q", d.GlobalCredsPath, "~/.claude/.credentials.json")
	}
	if d.DefaultModel != "sonnet" {
		t.Errorf("DefaultModel = %q, want %q", d.DefaultModel, "sonnet")
	}
	if d.CalloutCommand != "claude -p" {
		t.Errorf("CalloutCommand = %q, want %q", d.CalloutCommand, "claude -p")
	}
}

func TestDescriptorStaticEnv(t *testing.T) {
	r := New()
	d := r.Descriptor()

	if v, ok := d.StaticEnv["CLAUDE_CODE_ENABLE_TELEMETRY"]; !ok || v != "1" {
		t.Errorf("StaticEnv[CLAUDE_CODE_ENABLE_TELEMETRY] = %q, want %q", v, "1")
	}
}

func TestDescriptorCredentialEnvKeys(t *testing.T) {
	r := New()
	d := r.Descriptor()

	if v, ok := d.CredentialEnvKeys["oauth_token"]; !ok || v != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("CredentialEnvKeys[oauth_token] = %q, want CLAUDE_CODE_OAUTH_TOKEN", v)
	}
	if v, ok := d.CredentialEnvKeys["api_key"]; !ok || v != "ANTHROPIC_API_KEY" {
		t.Errorf("CredentialEnvKeys[api_key] = %q, want ANTHROPIC_API_KEY", v)
	}
}

func TestDescriptorSupportedHooks(t *testing.T) {
	r := New()
	d := r.Descriptor()

	// Claude Code supports all known hook types natively.
	for _, hookType := range []string{"SessionStart", "PreCompact", "TurnBoundary", "Guard"} {
		if !d.HasHookSupport(hookType) {
			t.Errorf("HasHookSupport(%q) = false, want true", hookType)
		}
	}
}

func TestImplementsRuntimeInterface(t *testing.T) {
	// Compile-time check via var _ declaration at top of claude.go.
	// Runtime assertion: New() must return a non-nil value.
	r := New()
	if r == nil {
		t.Fatal("New() returned nil")
	}
}

// ---- Seed ----

// TestSeedWritesOnboardingMarkers is the regression guard for the ADR-0041
// port that orphaned config seeding from the spawn path. ClaudeRuntime.Seed
// MUST produce .claude.json with hasCompletedOnboarding so Claude Code
// doesn't block fresh spawns at the welcome wizard.
func TestSeedWritesOnboardingMarkers(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	ctx := runtime.SpawnContext{
		WorktreeDir: t.TempDir(),
		ConfigDir:   t.TempDir(),
		Role:        "outpost",
		Agent:       "Toast",
	}
	if err := New().Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(ctx.ConfigDir, ".claude.json"))
	if err != nil {
		t.Fatalf(".claude.json not written: %v", err)
	}
	if !strings.Contains(string(data), `"hasCompletedOnboarding": true`) {
		t.Errorf(".claude.json missing onboarding marker: %s", data)
	}
	if _, err := os.Stat(filepath.Join(ctx.ConfigDir, "settings.json")); err != nil {
		t.Errorf("settings.json not written: %v", err)
	}
}

// TestSeedPreTrustsWorktree is the regression guard for the trust-dialog
// blocker: without TrustDirectoryIn, Claude Code prompts "Do you trust this
// directory?" on first run and blocks the session.
func TestSeedPreTrustsWorktree(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	worktreeDir := t.TempDir()
	configDir := t.TempDir()
	ctx := runtime.SpawnContext{
		WorktreeDir: worktreeDir,
		ConfigDir:   configDir,
		Role:        "outpost",
		Agent:       "Toast",
	}
	if err := New().Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf(".claude.json not written: %v", err)
	}
	// The exact JSON shape (projects.<path>.hasTrustDialogAccepted: true) is
	// produced by protocol.TrustDirectoryIn. A substring match is enough to
	// guard against the regression — full structural assertions live in the
	// protocol package's tests.
	if !strings.Contains(string(data), `"hasTrustDialogAccepted": true`) {
		t.Errorf(".claude.json missing hasTrustDialogAccepted=true: %s", data)
	}
}

// TestSeedCreatesEnvoyMemoryDir is the regression guard for the missing
// memory dir mkdir. Old claude adapter EnsureConfigDir did this; without it
// Claude Code's autoMemoryDirectory points at a non-existent path.
func TestSeedCreatesEnvoyMemoryDir(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	worldDir := filepath.Join(solHome, "myworld")
	ctx := runtime.SpawnContext{
		WorktreeDir: t.TempDir(),
		WorldDir:    worldDir,
		ConfigDir:   t.TempDir(),
		Role:        "envoy",
		Agent:       "Polaris",
	}
	if err := New().Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	memDir := runtime.MemoryDir(worldDir, "envoy", "Polaris")
	if memDir == "" {
		t.Fatal("runtime.MemoryDir returned empty for envoy")
	}
	if _, err := os.Stat(memDir); err != nil {
		t.Errorf("envoy memory dir %q not created by Seed: %v", memDir, err)
	}
}

// TestSeedDoesNotCreateOutpostMemoryDir verifies the envoy-only invariant:
// outposts are per-writ and have no persistent memory; the mkdir must be
// skipped for non-envoy roles.
func TestSeedDoesNotCreateOutpostMemoryDir(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	worldDir := filepath.Join(solHome, "myworld")
	ctx := runtime.SpawnContext{
		WorktreeDir: t.TempDir(),
		WorldDir:    worldDir,
		ConfigDir:   t.TempDir(),
		Role:        "outpost",
		Agent:       "Toast",
	}
	if err := New().Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	memDir := runtime.MemoryDir(worldDir, "outpost", "Toast")
	if _, err := os.Stat(memDir); err == nil {
		t.Errorf("outpost memory dir %q was created — should be envoy-only", memDir)
	}
}

// ---- Channels (ADR-0044) ----

// TestSeedChannelsDisabledWritesNoPluginState is the "flag off" half of the
// acceptance contract: byte-identical BuildCommand output is tested
// separately (TestBuildCommandChannelsFlagOff); this proves Seed touches no
// new state either when the flag is off — the default, and the common case
// for every world that hasn't opted in to the research-preview feature.
func TestSeedChannelsDisabledWritesNoPluginState(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	configDir := t.TempDir()
	ctx := runtime.SpawnContext{
		WorktreeDir:     t.TempDir(),
		ConfigDir:       configDir,
		Role:            "outpost",
		Agent:           "Toast",
		ChannelsEnabled: false,
	}
	if err := New().Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(solHome, ".claude-defaults", "plugins", "marketplaces")); !os.IsNotExist(err) {
		t.Errorf("expected no channel marketplace materialized when channels disabled, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "plugins", "installed_plugins.json")); !os.IsNotExist(err) {
		t.Errorf("expected no installed_plugins.json written when channels disabled, stat err = %v", err)
	}
}

// TestSeedChannelsEnabledWritesFreshAgentPluginState is the "flag on" half:
// a fresh agent config dir gets sol's channel plugin's installation record.
func TestSeedChannelsEnabledWritesFreshAgentPluginState(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	configDir := t.TempDir()
	ctx := runtime.SpawnContext{
		WorktreeDir:     t.TempDir(),
		ConfigDir:       configDir,
		Role:            "outpost",
		Agent:           "Toast",
		ChannelsEnabled: true,
	}
	if err := New().Seed(ctx); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	installedData, err := os.ReadFile(filepath.Join(configDir, "plugins", "installed_plugins.json"))
	if err != nil {
		t.Fatalf("installed_plugins.json not written: %v", err)
	}
	if !strings.Contains(string(installedData), channelplugin.PluginKey()) {
		t.Errorf("installed_plugins.json missing sol channel plugin key %q: %s", channelplugin.PluginKey(), installedData)
	}

	marketplacesData, err := os.ReadFile(filepath.Join(configDir, "plugins", "known_marketplaces.json"))
	if err != nil {
		t.Fatalf("known_marketplaces.json not written: %v", err)
	}
	if !strings.Contains(string(marketplacesData), channelplugin.MarketplaceName) {
		t.Errorf("known_marketplaces.json missing sol marketplace %q: %s", channelplugin.MarketplaceName, marketplacesData)
	}

	settingsData, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("settings.json not valid JSON: %v", err)
	}
	enabled, _ := settings["enabledPlugins"].(map[string]any)
	if enabled[channelplugin.PluginKey()] != true {
		t.Errorf("settings.json enabledPlugins missing sol channel plugin: %+v", enabled)
	}

	// EnsureMarketplace's sphere-wide content must also exist — this is
	// what the agent's installLocation/installPath entries point at.
	if _, err := os.Stat(channelplugin.MarketplaceDir(solHome)); err != nil {
		t.Errorf("channel marketplace content not materialized: %v", err)
	}
}

// ---- BuildCommand ----

func TestBuildCommandBasic(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir: dir,
		Prompt:      "Hello agent",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "claude --dangerously-skip-permissions") {
		t.Errorf("expected claude prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings flag, got: %q", cmd)
	}
	if !strings.Contains(cmd, "Hello agent") {
		t.Errorf("expected prompt in command, got: %q", cmd)
	}
}

func TestBuildCommandWithContinue(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir: dir,
		Continue:    true,
		Prompt:      "resume",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.Contains(cmd, "--continue") {
		t.Errorf("expected --continue flag, got: %q", cmd)
	}
}

func TestBuildCommandWithModel(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir: dir,
		Model:       "claude-opus-4-5",
		Prompt:      "go",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.Contains(cmd, "--model claude-opus-4-5") {
		t.Errorf("expected --model flag, got: %q", cmd)
	}
}

func TestBuildCommandNoModelFlag(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir: dir,
		Prompt:      "go",
	}
	cmd := r.BuildCommand(ctx)

	if strings.Contains(cmd, "--model") {
		t.Errorf("should not have --model flag when Model is empty, got: %q", cmd)
	}
}

func TestBuildCommandReplaceSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir:      dir,
		ReplacePrompt:    true,
		SystemPromptFile: ".claude/system-prompt.md",
		Prompt:           "go",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.Contains(cmd, "--system-prompt-file") {
		t.Errorf("expected --system-prompt-file, got: %q", cmd)
	}
	if strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("should not have --append-system-prompt-file, got: %q", cmd)
	}
}

func TestBuildCommandAppendSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir:      dir,
		ReplacePrompt:    false,
		SystemPromptFile: ".claude/system-prompt.md",
		Prompt:           "go",
	}
	cmd := r.BuildCommand(ctx)

	if !strings.Contains(cmd, "--append-system-prompt-file") {
		t.Errorf("expected --append-system-prompt-file, got: %q", cmd)
	}
}

func TestBuildCommandNoSystemPromptWhenFileEmpty(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.CommandContext{
		WorktreeDir:   dir,
		ReplacePrompt: true,
		Prompt:        "go",
	}
	cmd := r.BuildCommand(ctx)

	if strings.Contains(cmd, "system-prompt") {
		t.Errorf("should not have system-prompt flag when SystemPromptFile is empty, got: %q", cmd)
	}
}

// TestBuildCommandChannelsFlagOff is the acceptance-criteria regression
// guard: with ChannelsEnabled left at its zero value (false, the default),
// BuildCommand's output must be byte-identical to a CommandContext that
// never mentions channels at all.
func TestBuildCommandChannelsFlagOff(t *testing.T) {
	dir := t.TempDir()
	r := New()

	withFlag := r.BuildCommand(runtime.CommandContext{WorktreeDir: dir, Prompt: "go", ChannelsEnabled: false})
	withoutField := r.BuildCommand(runtime.CommandContext{WorktreeDir: dir, Prompt: "go"})

	if withFlag != withoutField {
		t.Errorf("ChannelsEnabled=false changed BuildCommand output:\n  got:  %q\n  want: %q", withFlag, withoutField)
	}
	if strings.Contains(withFlag, "--channels") {
		t.Errorf("expected no --channels flag when ChannelsEnabled is false, got: %q", withFlag)
	}
}

func TestBuildCommandChannelsFlagOn(t *testing.T) {
	dir := t.TempDir()
	r := New()

	cmd := r.BuildCommand(runtime.CommandContext{WorktreeDir: dir, Prompt: "go", ChannelsEnabled: true})

	want := "--channels " + channelplugin.ChannelsArg()
	if !strings.Contains(cmd, want) {
		t.Errorf("expected %q in command, got: %q", want, cmd)
	}
}

func TestBuildCommandSOLSessionCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	dir := t.TempDir()
	r := New()

	cmd := r.BuildCommand(runtime.CommandContext{WorktreeDir: dir})
	if cmd != "sleep 300" {
		t.Errorf("expected SOL_SESSION_COMMAND override, got: %q", cmd)
	}
}

func TestBuildCommandSOLSessionCommandTakesPrecedence(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "echo test")
	dir := t.TempDir()
	r := New()

	// Even with model, continue, system prompt — override wins.
	ctx := runtime.CommandContext{
		WorktreeDir:      dir,
		Continue:         true,
		Model:            "claude-opus-4-5",
		SystemPromptFile: ".claude/system-prompt.md",
		Prompt:           "go",
	}
	cmd := r.BuildCommand(ctx)
	if cmd != "echo test" {
		t.Errorf("expected SOL_SESSION_COMMAND to take precedence, got: %q", cmd)
	}
}

// ---- InstallHooks ----

func readHookSettings(t *testing.T, dir string) (hookSettings, string) {
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
	return s, string(data)
}

func TestInstallHooksSessionStart(t *testing.T) {
	dir := t.TempDir()
	r := New()

	hooks := runtime.HookSet{
		SessionStart: []runtime.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	s, _ := readHookSettings(t, dir)
	groups, ok := s.Hooks["SessionStart"]
	if !ok {
		t.Fatal("expected SessionStart hooks")
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 SessionStart group, got %d", len(groups))
	}
	if groups[0].Hooks[0].Command != "sol prime --world=myworld --agent=Toast" {
		t.Errorf("unexpected command: %q", groups[0].Hooks[0].Command)
	}
	if groups[0].Hooks[0].Type != "command" {
		t.Errorf("expected type=command, got %q", groups[0].Hooks[0].Type)
	}
}

func TestInstallHooksPreCompact(t *testing.T) {
	dir := t.TempDir()
	r := New()

	hooks := runtime.HookSet{
		PreCompact: []runtime.HookCommand{
			{Command: "sol prime --world=myworld --agent=Toast --compact"},
		},
	}
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	s, _ := readHookSettings(t, dir)
	groups, ok := s.Hooks["PreCompact"]
	if !ok {
		t.Fatal("expected PreCompact hooks")
	}
	if groups[0].Hooks[0].Command != "sol prime --world=myworld --agent=Toast --compact" {
		t.Errorf("unexpected command: %q", groups[0].Hooks[0].Command)
	}
}

func TestInstallHooksGuards(t *testing.T) {
	dir := t.TempDir()
	r := New()

	hooks := runtime.HookSet{
		Guards: []runtime.Guard{
			{Pattern: "EnterPlanMode", Command: `echo "BLOCKED" >&2; exit 2`},
			{Pattern: "Bash(git push --force*)", Command: "sol guard dangerous-command"},
		},
	}
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	s, _ := readHookSettings(t, dir)
	groups, ok := s.Hooks["PreToolUse"]
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
	r := New()

	hooks := runtime.HookSet{
		TurnBoundary: []runtime.HookCommand{
			{Command: "sol nudge drain --world=myworld --agent=Toast"},
		},
	}
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	s, _ := readHookSettings(t, dir)
	groups, ok := s.Hooks["UserPromptSubmit"]
	if !ok {
		t.Fatal("expected UserPromptSubmit hooks")
	}
	if groups[0].Hooks[0].Command != "sol nudge drain --world=myworld --agent=Toast" {
		t.Errorf("unexpected command: %q", groups[0].Hooks[0].Command)
	}
}

func TestInstallHooksFullHookSet(t *testing.T) {
	dir := t.TempDir()
	r := New()

	hooks := runtime.HookSet{
		SessionStart: []runtime.HookCommand{{Command: "sol prime --world=w --agent=A"}},
		PreCompact:   []runtime.HookCommand{{Command: "sol prime --world=w --agent=A --compact"}},
		Guards: []runtime.Guard{
			{Pattern: "EnterPlanMode", Command: "exit 2"},
		},
		TurnBoundary: []runtime.HookCommand{{Command: "sol nudge drain --world=w --agent=A"}},
	}
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	s, _ := readHookSettings(t, dir)
	for _, key := range []string{"SessionStart", "PreCompact", "PreToolUse", "UserPromptSubmit"} {
		if _, ok := s.Hooks[key]; !ok {
			t.Errorf("expected hook key %q", key)
		}
	}
}

func TestInstallHooksEmptyHookSet(t *testing.T) {
	dir := t.TempDir()
	r := New()

	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, runtime.HookSet{}); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	_, raw := readHookSettings(t, dir)
	if !strings.Contains(raw, `"hooks"`) {
		t.Error("expected hooks key in output")
	}
}

func TestInstallHooksWithMatcher(t *testing.T) {
	dir := t.TempDir()
	r := New()

	hooks := runtime.HookSet{
		SessionStart: []runtime.HookCommand{
			{Command: "sol prime --world=w --agent=A", Matcher: "startup|resume"},
		},
	}
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, hooks); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	s, _ := readHookSettings(t, dir)
	groups := s.Hooks["SessionStart"]
	if len(groups) == 0 {
		t.Fatal("expected SessionStart group")
	}
	if groups[0].Matcher != "startup|resume" {
		t.Errorf("expected matcher %q, got %q", "startup|resume", groups[0].Matcher)
	}
}

// TestInstallHooksEnvoyWritesAutoMemoryDirectory is the regression guard for
// the missing autoMemoryDirectory write. Old claude adapter InstallHooks
// wrote this for envoy sessions so Claude Code's native auto-memory finds
// the per-agent MEMORY.md; the port dropped it.
func TestInstallHooksEnvoyWritesAutoMemoryDirectory(t *testing.T) {
	dir := t.TempDir()
	worldDir := t.TempDir()
	r := New()

	ctx := runtime.SpawnContext{
		WorktreeDir: dir,
		WorldDir:    worldDir,
		Role:        "envoy",
		Agent:       "Polaris",
	}
	if err := r.InstallHooks(ctx, runtime.HookSet{}); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	expected := runtime.MemoryDir(worldDir, "envoy", "Polaris")
	if !strings.Contains(string(data), `"autoMemoryDirectory"`) {
		t.Errorf("expected autoMemoryDirectory key for envoy, got: %s", data)
	}
	if !strings.Contains(string(data), expected) {
		t.Errorf("expected autoMemoryDirectory value %q, got: %s", expected, data)
	}
}

// TestInstallHooksOutpostOmitsAutoMemoryDirectory verifies the envoy-only
// invariant: non-envoy roles must not carry autoMemoryDirectory.
func TestInstallHooksOutpostOmitsAutoMemoryDirectory(t *testing.T) {
	dir := t.TempDir()
	r := New()

	ctx := runtime.SpawnContext{
		WorktreeDir: dir,
		WorldDir:    t.TempDir(),
		Role:        "outpost",
		Agent:       "Toast",
	}
	if err := r.InstallHooks(ctx, runtime.HookSet{}); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if strings.Contains(string(data), "autoMemoryDirectory") {
		t.Errorf("outpost settings.local.json should not include autoMemoryDirectory, got: %s", data)
	}
}

func TestInstallHooksCreatesClaudeDir(t *testing.T) {
	dir := t.TempDir()
	r := New()

	// .claude does not exist yet — InstallHooks must create it.
	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, runtime.HookSet{}); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude")); err != nil {
		t.Errorf(".claude directory should exist: %v", err)
	}
}

func TestInstallHooksWritesSettingsLocalJSON(t *testing.T) {
	dir := t.TempDir()
	r := New()

	if err := r.InstallHooks(runtime.SpawnContext{WorktreeDir: dir}, runtime.HookSet{}); err != nil {
		t.Fatalf("InstallHooks failed: %v", err)
	}

	p := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("settings.local.json should exist: %v", err)
	}
}

// ---- ExtractTelemetry ----

func TestExtractTelemetryBasic(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"model":                 "claude-sonnet-4",
		"input_tokens":          "100",
		"output_tokens":         "200",
		"cache_read_tokens":     "50",
		"cache_creation_tokens": "25",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "claude-sonnet-4" {
		t.Errorf("Model = %q, want %q", result.Model, "claude-sonnet-4")
	}
	if result.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", result.InputTokens)
	}
	if result.OutputTokens != 200 {
		t.Errorf("OutputTokens = %d, want 200", result.OutputTokens)
	}
	if result.CacheReadTokens != 50 {
		t.Errorf("CacheReadTokens = %d, want 50", result.CacheReadTokens)
	}
	if result.CacheCreationTokens != 25 {
		t.Errorf("CacheCreationTokens = %d, want 25", result.CacheCreationTokens)
	}
	if result.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens = %d, want 0", result.ReasoningTokens)
	}
}

func TestExtractTelemetryApiRequestEventName(t *testing.T) {
	r := New()
	// "api_request" (without prefix) is also accepted.
	attrs := map[string]string{
		"model":         "claude-sonnet-4",
		"input_tokens":  "10",
		"output_tokens": "20",
	}
	result := r.ExtractTelemetry("api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil result for 'api_request' event name")
	}
}

func TestExtractTelemetryIgnoresIrrelevantEvent(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"model":         "claude-sonnet-4",
		"input_tokens":  "100",
		"output_tokens": "200",
	}
	result := r.ExtractTelemetry("some.other.event", attrs)
	if result != nil {
		t.Errorf("expected nil for irrelevant event, got %+v", result)
	}
}

func TestExtractTelemetryReturnsNilWithoutModel(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"input_tokens":  "100",
		"output_tokens": "200",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result != nil {
		t.Errorf("expected nil when model is absent, got %+v", result)
	}
}

func TestExtractTelemetryGenAIFallback(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"gen_ai.response.model":                    "claude-sonnet-4",
		"gen_ai.usage.input_tokens":                "150",
		"gen_ai.usage.output_tokens":               "250",
		"gen_ai.usage.cache_read_input_tokens":     "30",
		"gen_ai.usage.cache_creation_input_tokens": "15",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord with gen_ai.* attrs")
	}
	if result.Model != "claude-sonnet-4" {
		t.Errorf("Model = %q, want %q", result.Model, "claude-sonnet-4")
	}
	if result.InputTokens != 150 {
		t.Errorf("InputTokens = %d, want 150", result.InputTokens)
	}
	if result.OutputTokens != 250 {
		t.Errorf("OutputTokens = %d, want 250", result.OutputTokens)
	}
	if result.CacheReadTokens != 30 {
		t.Errorf("CacheReadTokens = %d, want 30", result.CacheReadTokens)
	}
	if result.CacheCreationTokens != 15 {
		t.Errorf("CacheCreationTokens = %d, want 15", result.CacheCreationTokens)
	}
}

func TestExtractTelemetryReasoningTokens(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"model":            "claude-sonnet-4",
		"input_tokens":     "100",
		"output_tokens":    "200",
		"reasoning_tokens": "75",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.ReasoningTokens != 75 {
		t.Errorf("ReasoningTokens = %d, want 75", result.ReasoningTokens)
	}
}

func TestExtractTelemetryReasoningTokensCodexStyleFallback(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"model":                 "claude-sonnet-4",
		"output_tokens":         "200",
		"reasoning_token_count": "42",
	}
	result := r.ExtractTelemetry("api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.ReasoningTokens != 42 {
		t.Errorf("ReasoningTokens = %d, want 42", result.ReasoningTokens)
	}
}

func TestExtractTelemetryReasoningTokensGenAIFallback(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"gen_ai.response.model":         "claude-sonnet-4",
		"gen_ai.usage.input_tokens":     "100",
		"gen_ai.usage.output_tokens":    "200",
		"gen_ai.usage.reasoning_tokens": "33",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.ReasoningTokens != 33 {
		t.Errorf("ReasoningTokens = %d, want 33", result.ReasoningTokens)
	}
}

func TestExtractTelemetryCostAndDuration(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"model":         "claude-sonnet-4",
		"input_tokens":  "100",
		"output_tokens": "200",
		"cost_usd":      "0.0042",
		"duration_ms":   "1500",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.CostUSD == nil {
		t.Fatal("CostUSD should not be nil")
	}
	if *result.CostUSD != 0.0042 {
		t.Errorf("CostUSD = %v, want 0.0042", *result.CostUSD)
	}
	if result.DurationMS == nil {
		t.Fatal("DurationMS should not be nil")
	}
	if *result.DurationMS != 1500 {
		t.Errorf("DurationMS = %d, want 1500", *result.DurationMS)
	}
}

func TestExtractTelemetryCostAndDurationAbsent(t *testing.T) {
	r := New()
	attrs := map[string]string{
		"model":         "claude-sonnet-4",
		"input_tokens":  "100",
		"output_tokens": "200",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.CostUSD != nil {
		t.Errorf("CostUSD should be nil when absent, got %v", *result.CostUSD)
	}
	if result.DurationMS != nil {
		t.Errorf("DurationMS should be nil when absent, got %d", *result.DurationMS)
	}
}

func TestExtractTelemetryShortNameTakesPrecedenceOverGenAI(t *testing.T) {
	r := New()
	// Short-name attrs take priority when both are present.
	attrs := map[string]string{
		"model":                     "short-model",
		"gen_ai.response.model":     "genai-model",
		"input_tokens":              "10",
		"gen_ai.usage.input_tokens": "99",
	}
	result := r.ExtractTelemetry("claude_code.api_request", attrs)
	if result == nil {
		t.Fatal("expected non-nil TelemetryRecord")
	}
	if result.Model != "short-model" {
		t.Errorf("Model = %q, want short-name to take precedence", result.Model)
	}
	if result.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want short-name attr to take precedence", result.InputTokens)
	}
}

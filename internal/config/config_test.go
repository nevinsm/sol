package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeFromEnv(t *testing.T) {
	t.Setenv("SOL_HOME", "/custom/sol/home")
	got := Home()
	if got != "/custom/sol/home" {
		t.Fatalf("expected /custom/sol/home, got %q", got)
	}
}

func TestHomeDefault(t *testing.T) {
	t.Setenv("SOL_HOME", "")
	got := Home()
	if !strings.HasSuffix(got, "/sol") {
		t.Fatalf("expected path ending with /sol, got %q", got)
	}
}

func TestStoreDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := StoreDir()
	if got != "/tmp/test-sol/.store" {
		t.Fatalf("expected /tmp/test-sol/.store, got %q", got)
	}
}

func TestRuntimeDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := RuntimeDir()
	if got != "/tmp/test-sol/.runtime" {
		t.Fatalf("expected /tmp/test-sol/.runtime, got %q", got)
	}
}

func TestWorldDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := WorldDir("myworld")
	if got != "/tmp/test-sol/myworld" {
		t.Fatalf("expected /tmp/test-sol/myworld, got %q", got)
	}
}

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error: %v", err)
	}

	// Verify .store was created.
	if info, err := os.Stat(filepath.Join(dir, ".store")); err != nil {
		t.Fatalf("expected .store dir to exist: %v", err)
	} else if !info.IsDir() {
		t.Fatal("expected .store to be a directory")
	}

	// Verify .runtime was created.
	if info, err := os.Stat(filepath.Join(dir, ".runtime")); err != nil {
		t.Fatalf("expected .runtime dir to exist: %v", err)
	} else if !info.IsDir() {
		t.Fatal("expected .runtime to be a directory")
	}
}

func TestEnsureDirsAlreadyExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create subdirs manually.
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)
	os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755)

	// Should be idempotent.
	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error on existing dirs: %v", err)
	}
}

func TestValidateAgentNameEmpty(t *testing.T) {
	err := ValidateAgentName("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestValidateAgentNameValid(t *testing.T) {
	valid := []string{"Nova", "Vega", "agent-1", "Toast_v2", "R2.D2"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAgentName(name); err != nil {
				t.Fatalf("expected name %q to be valid, got: %v", name, err)
			}
		})
	}
}

func TestValidateAgentNameInvalid(t *testing.T) {
	invalid := []string{"../evil", "foo/bar", "1starts-digit", ".hidden", " space", "semi;colon"}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateAgentName(name); err == nil {
				t.Fatalf("expected error for invalid name %q", name)
			}
		})
	}
}

func TestValidateAgentNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 65)
	if err := ValidateAgentName(long); err == nil {
		t.Fatal("expected error for name exceeding max length")
	}
}

func TestValidateWorldNameEmpty(t *testing.T) {
	err := ValidateWorldName("")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("expected 'must not be empty' in error, got: %v", err)
	}
}

func TestValidateWorldNameInvalid(t *testing.T) {
	invalid := []string{".hidden", "has spaces", "-starts-dash", "foo/bar", "foo.bar"}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			err := ValidateWorldName(name)
			if err == nil {
				t.Fatalf("expected error for invalid name %q", name)
			}
			if !strings.Contains(err.Error(), "invalid world name") {
				t.Fatalf("expected 'invalid world name' in error for %q, got: %v", name, err)
			}
		})
	}
}

func TestValidateWorldNameValid(t *testing.T) {
	valid := []string{"myworld", "test-world", "World_01", "a1"}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ValidateWorldName(name); err != nil {
				t.Fatalf("expected name %q to be valid, got: %v", name, err)
			}
		})
	}
}

func TestSettingsPath(t *testing.T) {
	got := SettingsPath("/tmp/worktree")
	want := "/tmp/worktree/.claude/settings.local.json"
	if got != want {
		t.Fatalf("SettingsPath() = %q, want %q", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", `"simple"`},
		{`has "quotes"`, `"has \"quotes\""`},
		{"has $var", `"has \$var"`},
		{"has `backtick`", "\"has \\`backtick\\`\""},
		{`has \backslash`, `"has \\backslash"`},
		{"has !bang", `"has \!bang"`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellQuote(tt.input)
			if got != tt.want {
				t.Fatalf("ShellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildSessionCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	got := BuildSessionCommand("/some/path", "hello")
	if got != "sleep 300" {
		t.Fatalf("expected override, got %q", got)
	}
}

func TestBuildSessionCommandDefault(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")
	got := BuildSessionCommand("/tmp/wt/.claude/settings.local.json", "Hello agent")
	if !strings.Contains(got, "claude --dangerously-skip-permissions") {
		t.Fatalf("expected claude command, got %q", got)
	}
	if !strings.Contains(got, "--settings") {
		t.Fatalf("expected --settings flag, got %q", got)
	}
	if !strings.Contains(got, "settings.local.json") {
		t.Fatalf("expected settings path, got %q", got)
	}
	if !strings.Contains(got, "Hello agent") {
		t.Fatalf("expected prompt, got %q", got)
	}
}

func TestBuildSessionCommandContinueDefault(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "")
	got := BuildSessionCommandContinue("/tmp/wt/.claude/settings.local.json", "Hello agent")
	if !strings.Contains(got, "--continue") {
		t.Fatalf("expected --continue flag, got %q", got)
	}
	if !strings.Contains(got, "--dangerously-skip-permissions") {
		t.Fatalf("expected --dangerously-skip-permissions, got %q", got)
	}
	if !strings.Contains(got, "--settings") {
		t.Fatalf("expected --settings flag, got %q", got)
	}
}

func TestBuildSessionCommandContinueOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	got := BuildSessionCommandContinue("/some/path", "hello")
	if got != "sleep 300" {
		t.Fatalf("override should ignore --continue, got %q", got)
	}
}

func TestEnsureClaudeConfigDirNamedAccount(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	worldDir := filepath.Join(solHome, "testworld")

	dir, err := EnsureClaudeConfigDir(worldDir, "outpost", "Toast", "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the config dir was created.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("expected config dir to exist: %s", dir)
	}

	// Verify no .account file written (credentials via env vars now).
	if _, err := os.ReadFile(filepath.Join(dir, ".account")); err == nil {
		t.Error("expected no .account file (credentials are injected via env vars)")
	}

	// Verify no .credentials.json written (credentials via env vars now).
	if _, err := os.Lstat(filepath.Join(dir, ".credentials.json")); err == nil {
		t.Error("expected no .credentials.json (credentials are injected via env vars)")
	}
}

func TestResolveAgentFlagSet(t *testing.T) {
	t.Setenv("SOL_AGENT", "EnvAgent")
	agent, err := ResolveAgent("FlagAgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent != "FlagAgent" {
		t.Fatalf("expected FlagAgent, got %q", agent)
	}
}

func TestResolveAgentEnvFallback(t *testing.T) {
	t.Setenv("SOL_AGENT", "EnvAgent")
	agent, err := ResolveAgent("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent != "EnvAgent" {
		t.Fatalf("expected EnvAgent, got %q", agent)
	}
}

func TestResolveAgentNeitherSet(t *testing.T) {
	t.Setenv("SOL_AGENT", "")
	_, err := ResolveAgent("")
	if err == nil {
		t.Fatal("expected error when neither flag nor env is set")
	}
	if !strings.Contains(err.Error(), "--agent is required") {
		t.Fatalf("expected '--agent is required' in error, got: %v", err)
	}
}

func TestResolveAgentFlagWinsOverEnv(t *testing.T) {
	t.Setenv("SOL_AGENT", "EnvAgent")
	agent, err := ResolveAgent("FlagAgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agent != "FlagAgent" {
		t.Fatalf("expected flag to win, got %q", agent)
	}
}

func TestClaudeDefaultsDir(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/test-sol")
	got := ClaudeDefaultsDir()
	if got != "/tmp/test-sol/.claude-defaults" {
		t.Fatalf("expected /tmp/test-sol/.claude-defaults, got %q", got)
	}
}

func TestEnsureClaudeDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	defaultsDir := filepath.Join(dir, ".claude-defaults")

	// Verify settings.json was created.
	settingsPath := filepath.Join(defaultsDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected settings.json to exist: %v", err)
	}

	// Verify the statusline path was resolved (no template placeholder).
	if strings.Contains(string(data), "{{STATUSLINE_PATH}}") {
		t.Error("settings.json still contains {{STATUSLINE_PATH}} placeholder")
	}

	// Verify no template placeholders remain.
	if strings.Contains(string(data), "{{STATUSLINE_PATH}}") {
		t.Error("settings.json still contains {{STATUSLINE_PATH}} placeholder")
	}
	if strings.Contains(string(data), "{{API_KEY_HELPER_PATH}}") {
		t.Error("settings.json still contains {{API_KEY_HELPER_PATH}} placeholder")
	}

	// Verify absolute path to statusline.sh is present.
	expectedStatuslinePath := filepath.Join(defaultsDir, "statusline.sh")
	if !strings.Contains(string(data), expectedStatuslinePath) {
		t.Errorf("settings.json should contain absolute path %q, got:\n%s", expectedStatuslinePath, data)
	}

	// Verify apikey-helper.sh is NOT present (removed).
	expectedApiKeyHelperPath := filepath.Join(defaultsDir, "apikey-helper.sh")
	if strings.Contains(string(data), expectedApiKeyHelperPath) {
		t.Errorf("settings.json should NOT contain apikey-helper.sh path, got:\n%s", data)
	}

	// Verify settings.json is valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}

	// Verify expected keys.
	if _, ok := parsed["statusLine"]; !ok {
		t.Error("settings.json missing statusLine key")
	}
	if _, ok := parsed["apiKeyHelper"]; ok {
		t.Error("settings.json should NOT have apiKeyHelper key (removed)")
	}
	if v, ok := parsed["gitAttribution"]; !ok || v != false {
		t.Error("settings.json missing or wrong gitAttribution")
	}

	// Verify statusline.sh was created and is executable.
	info, err := os.Stat(expectedStatuslinePath)
	if err != nil {
		t.Fatalf("expected statusline.sh to exist: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Error("statusline.sh should be executable")
	}

	// Verify apikey-helper.sh was NOT created.
	if _, err := os.Stat(filepath.Join(defaultsDir, "apikey-helper.sh")); err == nil {
		t.Error("apikey-helper.sh should NOT exist (removed)")
	}
}

func TestEnsureClaudeDefaultsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Call twice — should not error.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("second call error: %v", err)
	}
}

func TestEnsureClaudeDefaultsOverwritesStale(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	defaultsDir := filepath.Join(dir, ".claude-defaults")
	if err := os.MkdirAll(defaultsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a stale settings.json that is missing apiKeyHelper.
	stalePath := filepath.Join(defaultsDir, "settings.json")
	staleContent := `{"statusLine": {"enabled": true}, "gitAttribution": false}`
	if err := os.WriteFile(stalePath, []byte(staleContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// EnsureClaudeDefaults should overwrite the stale file.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	data, err := os.ReadFile(stalePath)
	if err != nil {
		t.Fatalf("expected settings.json to exist: %v", err)
	}

	// Verify statusLine is now present (was missing in the stale version).
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	if _, ok := parsed["statusLine"]; !ok {
		t.Error("settings.json should have been overwritten with current template containing statusLine")
	}
	// Verify apiKeyHelper is NOT present (removed from template).
	if _, ok := parsed["apiKeyHelper"]; ok {
		t.Error("settings.json should NOT have apiKeyHelper key (removed)")
	}
}

func TestSeedClaudeSettingsCopiesLocalSettings(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Seed defaults so settings.json exists.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	// Write a settings.local.json in .claude-defaults/.
	localContent := `{"theme": "dark"}`
	localSrc := filepath.Join(solHome, ".claude-defaults", "settings.local.json")
	if err := os.WriteFile(localSrc, []byte(localContent), 0o644); err != nil {
		t.Fatal(err)
	}

	agentConfigDir := t.TempDir()
	seedClaudeSettings(agentConfigDir)

	// Verify settings.local.json was copied to the agent config dir.
	localDst := filepath.Join(agentConfigDir, "settings.local.json")
	data, err := os.ReadFile(localDst)
	if err != nil {
		t.Fatalf("expected settings.local.json in agent config dir: %v", err)
	}
	if string(data) != localContent {
		t.Errorf("settings.local.json content = %q, want %q", string(data), localContent)
	}
}

func TestSeedClaudeSettingsMergesEnabledPlugins(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Seed defaults so settings.json exists.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	// Write installed_plugins.json with two plugins.
	pluginsDir := filepath.Join(solHome, ".claude-defaults", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installedJSON := `{
		"version": 2,
		"plugins": {
			"gopls-lsp@claude-plugins-official": [{"scope": "user", "version": "1.0.0"}],
			"pyright-lsp@claude-plugins-official": [{"scope": "user", "version": "1.0.0"}]
		}
	}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(installedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	agentConfigDir := t.TempDir()
	seedClaudeSettings(agentConfigDir)

	// Read the agent's settings.json and verify enabledPlugins was merged.
	data, err := os.ReadFile(filepath.Join(agentConfigDir, "settings.json"))
	if err != nil {
		t.Fatalf("expected settings.json in agent config dir: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}

	ep, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json missing enabledPlugins, got: %s", data)
	}
	if ep["gopls-lsp@claude-plugins-official"] != true {
		t.Error("gopls-lsp should be enabled")
	}
	if ep["pyright-lsp@claude-plugins-official"] != true {
		t.Error("pyright-lsp should be enabled")
	}

	// Verify original settings keys are preserved.
	if _, ok := settings["statusLine"]; !ok {
		t.Error("statusLine should still be present after plugin merge")
	}
}

func TestSeedClaudeSettingsNoPlugins(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Seed defaults — no installed_plugins.json exists.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	agentConfigDir := t.TempDir()
	seedClaudeSettings(agentConfigDir)

	// settings.json should still be valid, just without enabledPlugins.
	data, err := os.ReadFile(filepath.Join(agentConfigDir, "settings.json"))
	if err != nil {
		t.Fatalf("expected settings.json: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	if _, ok := settings["enabledPlugins"]; ok {
		t.Error("enabledPlugins should not be present when no plugins are installed")
	}
	if _, ok := settings["statusLine"]; !ok {
		t.Error("statusLine should still be present")
	}
}

func TestSeedClaudeSettingsSkipsLocalSettingsWhenAbsent(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Seed defaults so settings.json exists but no settings.local.json.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	agentConfigDir := t.TempDir()
	seedClaudeSettings(agentConfigDir)

	// Verify settings.local.json was NOT created in the agent config dir.
	localDst := filepath.Join(agentConfigDir, "settings.local.json")
	if _, err := os.Stat(localDst); !os.IsNotExist(err) {
		t.Error("settings.local.json should not be created when absent from defaults dir")
	}

	// settings.json should still have been copied.
	settingsDst := filepath.Join(agentConfigDir, "settings.json")
	if _, err := os.Stat(settingsDst); os.IsNotExist(err) {
		t.Error("settings.json should have been copied to agent config dir")
	}
}

func TestEnsureClaudeConfigDirCopiesSettings(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	// Seed defaults first.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	worldDir := filepath.Join(solHome, "testworld")
	dir, err := EnsureClaudeConfigDir(worldDir, "outpost", "Toast", "")
	if err != nil {
		t.Fatal(err)
	}

	// Verify settings.json was copied to agent config dir.
	agentSettings := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(agentSettings)
	if err != nil {
		t.Fatalf("expected settings.json in agent config dir: %v", err)
	}

	// Verify it contains the absolute statusline path.
	statuslinePath := filepath.Join(solHome, ".claude-defaults", "statusline.sh")
	if !strings.Contains(string(data), statuslinePath) {
		t.Errorf("agent settings.json should reference %q", statuslinePath)
	}
}

func TestEnsureClaudeConfigDirSelfHealsDefaults(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	// Do NOT pre-seed defaults — .claude-defaults/ doesn't exist.
	// EnsureClaudeConfigDir should self-heal by creating them.
	worldDir := filepath.Join(solHome, "testworld")
	dir, err := EnsureClaudeConfigDir(worldDir, "outpost", "Toast", "")
	if err != nil {
		t.Fatal(err)
	}

	// settings.json should exist — self-healed from embedded defaults.
	agentSettings := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(agentSettings)
	if err != nil {
		t.Fatal("settings.json should exist after self-healing defaults")
	}
	if !strings.Contains(string(data), "statusLine") {
		t.Error("settings.json should contain statusLine from embedded defaults")
	}

	// .claude-defaults/ should also now exist.
	defaultsSettings := filepath.Join(solHome, ".claude-defaults", "settings.json")
	if _, err := os.Stat(defaultsSettings); os.IsNotExist(err) {
		t.Error(".claude-defaults/settings.json should have been created")
	}
}

func TestEnsureClaudeConfigDirOverwritesSettings(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	// Seed defaults.
	if err := EnsureClaudeDefaults(); err != nil {
		t.Fatalf("EnsureClaudeDefaults() error: %v", err)
	}

	worldDir := filepath.Join(solHome, "testworld")

	// First call — seeds settings.json.
	dir, err := EnsureClaudeConfigDir(worldDir, "outpost", "Toast", "")
	if err != nil {
		t.Fatal(err)
	}

	// Overwrite agent settings.json with garbage.
	agentSettings := filepath.Join(dir, "settings.json")
	os.WriteFile(agentSettings, []byte(`{"old": true}`), 0o644)

	// Second call — should overwrite with defaults.
	_, err = EnsureClaudeConfigDir(worldDir, "outpost", "Toast", "")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(agentSettings)
	if strings.Contains(string(data), `"old"`) {
		t.Error("settings.json should have been overwritten with defaults")
	}
	if !strings.Contains(string(data), "statusLine") {
		t.Error("settings.json should contain statusLine from defaults")
	}
}

func TestEnsureClaudeConfigDirLegacyFallback(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	worldDir := filepath.Join(solHome, "testworld")

	// Empty account = legacy fallback (no .account file, symlink if source exists).
	dir, err := EnsureClaudeConfigDir(worldDir, "outpost", "Toast", "")
	if err != nil {
		t.Fatal(err)
	}

	// No .account file should exist.
	if _, err := os.Stat(filepath.Join(dir, ".account")); !os.IsNotExist(err) {
		t.Error("legacy mode should not create .account file")
	}
}

func TestSeedOnboardingStateFromSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create source ~/.claude/.claude.json with onboarding state.
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	source := map[string]any{
		"hasCompletedOnboarding": true,
		"lastOnboardingVersion":  "1.0.50",
		"firstStartTime":         "2025-01-15T10:30:00Z",
		"theme":                  "dark", // personal pref — should NOT be copied
		"numStartups":            float64(42),
	}
	data, _ := json.MarshalIndent(source, "", "  ")
	os.WriteFile(filepath.Join(claudeDir, ".claude.json"), data, 0o600)

	// Create empty agent config dir.
	configDir := filepath.Join(t.TempDir(), "agent-config")
	os.MkdirAll(configDir, 0o755)

	err := SeedOnboardingState(configDir)
	if err != nil {
		t.Fatalf("SeedOnboardingState() error: %v", err)
	}

	// Read back agent's .claude.json.
	agentData, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("agent .claude.json not created: %v", err)
	}

	var agentState map[string]any
	if err := json.Unmarshal(agentData, &agentState); err != nil {
		t.Fatalf("failed to parse agent .claude.json: %v", err)
	}

	// Verify seeded fields.
	if v, ok := agentState["hasCompletedOnboarding"].(bool); !ok || !v {
		t.Error("hasCompletedOnboarding should be true")
	}
	if v, ok := agentState["lastOnboardingVersion"].(string); !ok || v != "1.0.50" {
		t.Errorf("lastOnboardingVersion = %v, want 1.0.50", agentState["lastOnboardingVersion"])
	}
	if v, ok := agentState["firstStartTime"].(string); !ok || v != "2025-01-15T10:30:00Z" {
		t.Errorf("firstStartTime = %v, want 2025-01-15T10:30:00Z", agentState["firstStartTime"])
	}

	// Verify personal prefs NOT copied.
	if _, exists := agentState["theme"]; exists {
		t.Error("personal preference 'theme' should not be copied")
	}
	if _, exists := agentState["numStartups"]; exists {
		t.Error("personal field 'numStartups' should not be copied")
	}
}

func TestSeedOnboardingStateNoSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No ~/.claude/.claude.json exists.

	configDir := filepath.Join(t.TempDir(), "agent-config")
	os.MkdirAll(configDir, 0o755)

	err := SeedOnboardingState(configDir)
	if err != nil {
		t.Fatalf("SeedOnboardingState() error: %v", err)
	}

	// Should have minimal onboarding state.
	agentData, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("agent .claude.json not created: %v", err)
	}

	var agentState map[string]any
	if err := json.Unmarshal(agentData, &agentState); err != nil {
		t.Fatalf("failed to parse agent .claude.json: %v", err)
	}

	if v, ok := agentState["hasCompletedOnboarding"].(bool); !ok || !v {
		t.Error("hasCompletedOnboarding should be true even without source")
	}
}

func TestSeedOnboardingStatePreservesExisting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create source.
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	source := map[string]any{
		"hasCompletedOnboarding": true,
		"lastOnboardingVersion":  "2.0.0",
		"firstStartTime":         "2025-06-01T00:00:00Z",
	}
	data, _ := json.MarshalIndent(source, "", "  ")
	os.WriteFile(filepath.Join(claudeDir, ".claude.json"), data, 0o600)

	// Create agent config with pre-existing state.
	configDir := filepath.Join(t.TempDir(), "agent-config")
	os.MkdirAll(configDir, 0o755)
	existing := map[string]any{
		"hasCompletedOnboarding": true,
		"lastOnboardingVersion":  "1.0.0", // older version — should NOT be overwritten
		"customField":            "preserve-me",
	}
	existingData, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(configDir, ".claude.json"), existingData, 0o600)

	err := SeedOnboardingState(configDir)
	if err != nil {
		t.Fatalf("SeedOnboardingState() error: %v", err)
	}

	agentData, err := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read agent .claude.json: %v", err)
	}

	var agentState map[string]any
	if err := json.Unmarshal(agentData, &agentState); err != nil {
		t.Fatalf("failed to parse agent .claude.json: %v", err)
	}

	// Existing fields should NOT be overwritten.
	if v := agentState["lastOnboardingVersion"]; v != "1.0.0" {
		t.Errorf("lastOnboardingVersion = %v, want 1.0.0 (should not overwrite)", v)
	}
	if v := agentState["customField"]; v != "preserve-me" {
		t.Error("customField should be preserved")
	}

	// Missing field from source should be added.
	if _, exists := agentState["firstStartTime"]; !exists {
		t.Error("firstStartTime should be seeded from source since it was missing")
	}
}

// ----- detectWorldFromCwd -----

func TestDetectWorldFromCwdAtSOLHome(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.Chdir(solHome); err != nil {
		t.Fatal(err)
	}

	// cwd == SOL_HOME → no world component → should return "".
	got := detectWorldFromCwd()
	if got != "" {
		t.Fatalf("detectWorldFromCwd() at SOL_HOME = %q, want %q", got, "")
	}
}

func TestDetectWorldFromCwdInsideStoreDir(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	storeDir := filepath.Join(solHome, ".store", "subdir")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.Chdir(storeDir); err != nil {
		t.Fatal(err)
	}

	// .store/ subfolder → should be skipped → return "".
	got := detectWorldFromCwd()
	if got != "" {
		t.Fatalf("detectWorldFromCwd() inside .store/ = %q, want %q", got, "")
	}
}

func TestDetectWorldFromCwdOutsideSOLHome(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Use a completely separate directory.
	outside := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}

	got := detectWorldFromCwd()
	if got != "" {
		t.Fatalf("detectWorldFromCwd() outside SOL_HOME = %q, want %q", got, "")
	}
}

func TestDetectWorldFromCwdInsideWorldDir(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	worldSub := filepath.Join(solHome, "myworld", "outposts", "Toast", "worktree")
	if err := os.MkdirAll(worldSub, 0o755); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	if err := os.Chdir(worldSub); err != nil {
		t.Fatal(err)
	}

	got := detectWorldFromCwd()
	if got != "myworld" {
		t.Fatalf("detectWorldFromCwd() inside world subdir = %q, want %q", got, "myworld")
	}
}

func TestSeedOnboardingStateIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create source.
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0o755)
	source := map[string]any{
		"hasCompletedOnboarding": true,
		"lastOnboardingVersion":  "1.0.0",
	}
	data, _ := json.MarshalIndent(source, "", "  ")
	os.WriteFile(filepath.Join(claudeDir, ".claude.json"), data, 0o600)

	configDir := filepath.Join(t.TempDir(), "agent-config")
	os.MkdirAll(configDir, 0o755)

	// Run twice — should be idempotent.
	if err := SeedOnboardingState(configDir); err != nil {
		t.Fatalf("first SeedOnboardingState() error: %v", err)
	}
	if err := SeedOnboardingState(configDir); err != nil {
		t.Fatalf("second SeedOnboardingState() error: %v", err)
	}

	agentData, _ := os.ReadFile(filepath.Join(configDir, ".claude.json"))
	var agentState map[string]any
	json.Unmarshal(agentData, &agentState)

	if v, ok := agentState["hasCompletedOnboarding"].(bool); !ok || !v {
		t.Error("hasCompletedOnboarding should be true")
	}
}

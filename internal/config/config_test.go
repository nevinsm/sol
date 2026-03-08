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

	// Create account credentials.
	accountDir := filepath.Join(solHome, ".accounts", "alice")
	os.MkdirAll(accountDir, 0o755)
	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "sk-ant-oat01-test",
			"refreshToken":     "sk-ant-ort01-test",
			"expiresAt":        1900000000000,
			"scopes":           []string{"user:inference"},
			"subscriptionType": "max",
		},
	}
	data, _ := json.MarshalIndent(creds, "", "  ")
	os.WriteFile(filepath.Join(accountDir, ".credentials.json"), data, 0o600)

	worldDir := filepath.Join(solHome, "testworld")

	dir, err := EnsureClaudeConfigDir(worldDir, "agent", "Toast", "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Verify .account file.
	acctData, err := os.ReadFile(filepath.Join(dir, ".account"))
	if err != nil {
		t.Fatalf("expected .account file: %v", err)
	}
	if strings.TrimSpace(string(acctData)) != "alice" {
		t.Errorf("expected .account to contain 'alice', got %q", string(acctData))
	}

	// Verify .credentials.json is a regular file (not symlink).
	credsPath := filepath.Join(dir, ".credentials.json")
	info, err := os.Lstat(credsPath)
	if err != nil {
		t.Fatalf("expected .credentials.json: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("credentials should be a regular file, not a symlink")
	}

	// Verify no refreshToken in agent credentials.
	agentData, _ := os.ReadFile(credsPath)
	var agentCreds map[string]any
	json.Unmarshal(agentData, &agentCreds)
	oauth := agentCreds["claudeAiOauth"].(map[string]any)
	if _, hasRefresh := oauth["refreshToken"]; hasRefresh {
		t.Error("agent credentials should NOT contain refreshToken")
	}
	if oauth["accessToken"] != "sk-ant-oat01-test" {
		t.Errorf("expected accessToken preserved, got %v", oauth["accessToken"])
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

func TestEnsureClaudeConfigDirLegacyFallback(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	t.Setenv("HOME", t.TempDir())

	worldDir := filepath.Join(solHome, "testworld")

	// Empty account = legacy fallback (no .account file, symlink if source exists).
	dir, err := EnsureClaudeConfigDir(worldDir, "agent", "Toast", "")
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

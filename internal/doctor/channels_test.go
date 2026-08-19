package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorldConfigChannels creates a world.toml with agents.channels_enabled
// set explicitly, optionally overriding the runtime (defaults to claude).
func writeWorldConfigChannels(t *testing.T, solHome, world string, channelsEnabled bool) {
	t.Helper()
	worldDir := filepath.Join(solHome, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[agents]\ndefault_runtime = \"claude\"\n"
	if channelsEnabled {
		content += "channels_enabled = true\n"
	}
	cfg := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOL_HOME", solHome)
}

func withManagedSettingsPath(t *testing.T, path string) {
	t.Helper()
	orig := managedSettingsPath
	managedSettingsPath = func() string { return path }
	t.Cleanup(func() { managedSettingsPath = orig })
}

func TestCheckChannelsManagedSettingsSkipsWorldsWithFlagOff(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", false)
	withManagedSettingsPath(t, filepath.Join(dir, "managed-settings.json"))

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 0 {
		t.Fatalf("expected no results for a world with channels disabled, got %+v", results)
	}
}

func TestCheckChannelsManagedSettingsSkipsNonClaudeWorlds(t *testing.T) {
	dir := t.TempDir()
	worldDir := filepath.Join(dir, "myworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[agents]\ndefault_runtime = \"codex\"\nchannels_enabled = true\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOL_HOME", dir)
	withManagedSettingsPath(t, filepath.Join(dir, "managed-settings.json"))

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 0 {
		t.Fatalf("expected no results for a codex-only world, got %+v", results)
	}
}

func TestCheckChannelsManagedSettingsMissingFile(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	withManagedSettingsPath(t, filepath.Join(dir, "does-not-exist", "managed-settings.json"))

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected Passed=true, Warning=true (advisory), got Passed=%v Warning=%v", r.Passed, r.Warning)
	}
	if r.Fix == "" {
		t.Error("expected a non-empty Fix recipe")
	}
}

func TestCheckChannelsManagedSettingsMissingChannelsEnabledKey(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	// File exists but omits channelsEnabled — per the gate's asymmetry, this
	// is MORE restrictive than no file, not equivalent to it. Regression
	// guard for that asymmetry being surfaced as a distinct warning.
	if err := os.WriteFile(settingsPath, []byte(`{"allowedChannelPlugins":[{"plugin":"sol-channel","marketplace":"sol-official"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withManagedSettingsPath(t, settingsPath)

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if !results[0].Warning {
		t.Errorf("expected a warning when channelsEnabled key is missing, got %+v", results[0])
	}
}

func TestCheckChannelsManagedSettingsMissingAllowlistEntry(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"channelsEnabled":true,"allowedChannelPlugins":[{"plugin":"someone-elses-plugin","marketplace":"other"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withManagedSettingsPath(t, settingsPath)

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if !results[0].Warning {
		t.Errorf("expected a warning when sol's plugin is not on the allowlist, got %+v", results[0])
	}
}

func TestCheckChannelsManagedSettingsValidPasses(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"channelsEnabled":true,"allowedChannelPlugins":[{"plugin":"sol-channel","marketplace":"sol-official"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withManagedSettingsPath(t, settingsPath)

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected a clean pass (Passed=true, Warning=false), got Passed=%v Warning=%v: %s", r.Passed, r.Warning, r.Message)
	}
}

func TestCheckChannelsManagedSettingsDropinSatisfiesWhenMainFileAbsent(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	withManagedSettingsPath(t, settingsPath)

	dropinDir := filepath.Join(dir, "managed-settings.d")
	if err := os.MkdirAll(dropinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	frag := `{"channelsEnabled":true,"allowedChannelPlugins":[{"plugin":"sol-channel","marketplace":"sol-official"}]}`
	if err := os.WriteFile(filepath.Join(dropinDir, "10-sol-channels.json"), []byte(frag), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected a clean pass from the drop-in fragment alone (Passed=true, Warning=false), got Passed=%v Warning=%v: %s", r.Passed, r.Warning, r.Message)
	}
}

func TestCheckChannelsManagedSettingsDropinIgnoresUnrelatedAndMalformedFragments(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	withManagedSettingsPath(t, settingsPath)

	dropinDir := filepath.Join(dir, "managed-settings.d")
	if err := os.MkdirAll(dropinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A malformed fragment and a non-JSON file should both be skipped
	// rather than blocking the valid fragment that follows alphabetically.
	if err := os.WriteFile(filepath.Join(dropinDir, "01-broken.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dropinDir, "README.txt"), []byte("not a fragment"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := `{"channelsEnabled":true,"allowedChannelPlugins":[{"plugin":"sol-channel","marketplace":"sol-official"}]}`
	if err := os.WriteFile(filepath.Join(dropinDir, "20-good.json"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected a clean pass despite malformed/unrelated fragments, got Passed=%v Warning=%v: %s", r.Passed, r.Warning, r.Message)
	}
}

func TestCheckChannelsManagedSettingsDropinSplitAcrossFragmentsDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	withManagedSettingsPath(t, settingsPath)

	dropinDir := filepath.Join(dir, "managed-settings.d")
	if err := os.MkdirAll(dropinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// "Keep it simple" per the writ: a single fragment must provide both
	// keys on its own. Splitting channelsEnabled and allowedChannelPlugins
	// across two separate fragments does not count, since sol doesn't model
	// the drop-in's actual merge precedence.
	onlyEnabled := `{"channelsEnabled":true}`
	if err := os.WriteFile(filepath.Join(dropinDir, "10-enabled.json"), []byte(onlyEnabled), 0o644); err != nil {
		t.Fatal(err)
	}
	onlyAllowlist := `{"allowedChannelPlugins":[{"plugin":"sol-channel","marketplace":"sol-official"}]}`
	if err := os.WriteFile(filepath.Join(dropinDir, "20-allowlist.json"), []byte(onlyAllowlist), 0o644); err != nil {
		t.Fatal(err)
	}

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected a warning since no single fragment provides both keys, got Passed=%v Warning=%v", r.Passed, r.Warning)
	}
	if r.Fix == "" {
		t.Error("expected a non-empty Fix recipe")
	}
}

func TestCheckChannelsManagedSettingsDropinMissingBothCountsAsAbsent(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	withManagedSettingsPath(t, settingsPath)
	// Neither the main file nor a managed-settings.d/ dir exists at all.

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected an advisory warning, got Passed=%v Warning=%v", r.Passed, r.Warning)
	}
	if r.Fix == "" {
		t.Error("expected a non-empty Fix recipe")
	}
}

// TestChannelsFixRecipeMatchesDocs guards against the Fix field's install
// recipe drifting from docs/channels.md's "Installing managed-settings.json"
// section — both must stay byte-identical (writ sol-95a05c395b3ff1b1's
// acceptance criteria), anchored to managedSettingsInstallRecipe as the
// single source of truth rather than being hand-typed twice.
func TestChannelsFixRecipeMatchesDocs(t *testing.T) {
	docsPath := filepath.Join("..", "..", "docs", "channels.md")
	data, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", docsPath, err)
	}

	const marker = "Install it once per host:\n\n```sh\n"
	_, rest, ok := strings.Cut(string(data), marker)
	if !ok {
		t.Fatalf("could not find install recipe marker %q in %s", marker, docsPath)
	}
	docsRecipe, _, ok := strings.Cut(rest, "\n```")
	if !ok {
		t.Fatalf("could not find end of install recipe code fence in %s", docsPath)
	}

	want := managedSettingsInstallRecipe("/etc/claude-code/managed-settings.json")
	if docsRecipe != want {
		t.Errorf("docs/channels.md install recipe has drifted from the doctor Fix-field recipe.\n\ndocs:\n%s\n\ncode:\n%s", docsRecipe, want)
	}
}

func TestCheckChannelsManagedSettingsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeWorldConfigChannels(t, dir, "myworld", true)
	settingsPath := filepath.Join(dir, "managed-settings.json")
	if err := os.WriteFile(settingsPath, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	withManagedSettingsPath(t, settingsPath)

	results := CheckChannelsManagedSettings([]string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	if !results[0].Warning {
		t.Errorf("expected a warning for invalid JSON, got %+v", results[0])
	}
}

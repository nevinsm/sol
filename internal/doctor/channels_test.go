package doctor

import (
	"os"
	"path/filepath"
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

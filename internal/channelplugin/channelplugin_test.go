package channelplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func fixedClock(t *testing.T, at time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return at }
	t.Cleanup(func() { nowFunc = orig })
}

func fakeSolBinary(t *testing.T, path string) {
	t.Helper()
	orig := resolveSolBinary
	resolveSolBinary = func() (string, error) { return path, nil }
	t.Cleanup(func() { resolveSolBinary = orig })
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return v
}

func TestEnsureMarketplaceWritesExpectedFiles(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")

	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	mktDir := MarketplaceDir(solHome)
	plugDir := filepath.Join(mktDir, PluginName)

	marketplace := readJSON(t, filepath.Join(mktDir, ".claude-plugin", "marketplace.json"))
	if marketplace["name"] != MarketplaceName {
		t.Errorf("marketplace.json name = %v, want %q", marketplace["name"], MarketplaceName)
	}
	plugins, ok := marketplace["plugins"].([]any)
	if !ok || len(plugins) != 1 {
		t.Fatalf("marketplace.json plugins = %v, want one entry", marketplace["plugins"])
	}
	ref := plugins[0].(map[string]any)
	if ref["name"] != PluginName || ref["source"] != "./"+PluginName {
		t.Errorf("marketplace.json plugin ref = %+v", ref)
	}

	plugin := readJSON(t, filepath.Join(plugDir, ".claude-plugin", "plugin.json"))
	if plugin["name"] != PluginName || plugin["version"] != PluginVersion {
		t.Errorf("plugin.json = %+v", plugin)
	}

	mcp := readJSON(t, filepath.Join(plugDir, ".mcp.json"))
	servers, ok := mcp["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.json mcpServers missing: %+v", mcp)
	}
	entry, ok := servers[PluginName].(map[string]any)
	if !ok {
		t.Fatalf("mcp.json mcpServers[%q] missing: %+v", PluginName, servers)
	}
	if entry["type"] != "stdio" {
		t.Errorf("mcp.json server type = %v, want stdio", entry["type"])
	}
	if entry["command"] != "/opt/sol/bin/sol" {
		t.Errorf("mcp.json server command = %v, want resolved sol binary", entry["command"])
	}
	args, ok := entry["args"].([]any)
	if !ok || len(args) != 2 || args[0] != "channel" || args[1] != "serve" {
		t.Errorf("mcp.json server args = %v, want [channel serve]", entry["args"])
	}
}

func TestEnsureMarketplaceIdempotentAndOverwrites(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")

	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("first EnsureMarketplace: %v", err)
	}
	// Simulate a stale binary path (e.g. sol was reinstalled at a new
	// location) — a second call must overwrite, not preserve the old value,
	// same as EnsureClaudeDefaults's "sol owns this file" contract.
	fakeSolBinary(t, "/opt/sol/bin/sol-v2")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("second EnsureMarketplace: %v", err)
	}

	mcp := readJSON(t, filepath.Join(pluginDir(solHome), ".mcp.json"))
	servers := mcp["mcpServers"].(map[string]any)
	entry := servers[PluginName].(map[string]any)
	if entry["command"] != "/opt/sol/bin/sol-v2" {
		t.Errorf("mcp.json command not overwritten: got %v", entry["command"])
	}
}

func TestSeedAgentMergesWithoutClobberingExistingPlugins(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}

	agentConfigDir := t.TempDir()
	pluginsDir := filepath.Join(agentConfigDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Simulate an operator-installed plugin already seeded via
	// config.SeedClaudeConfig / `sol config claude` before SeedAgent runs.
	preexisting := `{"version":2,"plugins":{"some-other@marketplace":[{"scope":"user","installPath":"/x","version":"1.0.0","installedAt":"2026-01-01T00:00:00Z","lastUpdated":"2026-01-01T00:00:00Z"}]}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(preexisting), 0o644); err != nil {
		t.Fatal(err)
	}
	preexistingMkt := `{"marketplace-other":{"source":{"source":"directory","path":"/y"},"installLocation":"/y","lastUpdated":"2026-01-01T00:00:00Z"}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte(preexistingMkt), 0o644); err != nil {
		t.Fatal(err)
	}
	preexistingSettings := `{"enabledPlugins":{"some-other@marketplace":true},"skipDangerousModePermissionPrompt":true}`
	if err := os.WriteFile(filepath.Join(agentConfigDir, "settings.json"), []byte(preexistingSettings), 0o644); err != nil {
		t.Fatal(err)
	}

	fixed := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	fixedClock(t, fixed)

	if err := SeedAgent(solHome, agentConfigDir); err != nil {
		t.Fatalf("SeedAgent: %v", err)
	}

	installed := readJSON(t, filepath.Join(pluginsDir, "installed_plugins.json"))
	plugins := installed["plugins"].(map[string]any)
	if _, ok := plugins["some-other@marketplace"]; !ok {
		t.Errorf("SeedAgent clobbered pre-existing installed_plugins.json entry: %+v", plugins)
	}
	sol, ok := plugins[PluginKey()].([]any)
	if !ok || len(sol) != 1 {
		t.Fatalf("SeedAgent did not add sol channel plugin entry: %+v", plugins)
	}
	entry := sol[0].(map[string]any)
	if entry["installedAt"] != "2026-08-19T12:00:00Z" {
		t.Errorf("installedAt = %v, want fixed clock value", entry["installedAt"])
	}

	marketplaces := readJSON(t, filepath.Join(pluginsDir, "known_marketplaces.json"))
	if _, ok := marketplaces["marketplace-other"]; !ok {
		t.Errorf("SeedAgent clobbered pre-existing known_marketplaces.json entry: %+v", marketplaces)
	}
	if _, ok := marketplaces[MarketplaceName]; !ok {
		t.Errorf("SeedAgent did not add sol marketplace entry: %+v", marketplaces)
	}

	settings := readJSON(t, filepath.Join(agentConfigDir, "settings.json"))
	enabled := settings["enabledPlugins"].(map[string]any)
	if enabled["some-other@marketplace"] != true {
		t.Errorf("SeedAgent clobbered pre-existing enabledPlugins entry: %+v", enabled)
	}
	if enabled[PluginKey()] != true {
		t.Errorf("SeedAgent did not enable sol channel plugin: %+v", enabled)
	}
	if settings["skipDangerousModePermissionPrompt"] != true {
		t.Errorf("SeedAgent clobbered unrelated settings.json key: %+v", settings)
	}
}

func TestSeedAgentPreservesInstalledAtAcrossRepeatedCalls(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	agentConfigDir := t.TempDir()

	first := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	fixedClock(t, first)
	if err := SeedAgent(solHome, agentConfigDir); err != nil {
		t.Fatalf("first SeedAgent: %v", err)
	}

	second := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	fixedClock(t, second)
	if err := SeedAgent(solHome, agentConfigDir); err != nil {
		t.Fatalf("second SeedAgent: %v", err)
	}

	installed := readJSON(t, filepath.Join(agentConfigDir, "plugins", "installed_plugins.json"))
	plugins := installed["plugins"].(map[string]any)
	sol := plugins[PluginKey()].([]any)[0].(map[string]any)
	if sol["installedAt"] != "2026-08-19T12:00:00Z" {
		t.Errorf("installedAt changed across repeated SeedAgent calls: got %v", sol["installedAt"])
	}
	if sol["lastUpdated"] != "2026-08-20T09:00:00Z" {
		t.Errorf("lastUpdated did not advance: got %v", sol["lastUpdated"])
	}
}

// --- schema-drift guard ------------------------------------------------
//
// The plugin/marketplace file formats below are reverse-engineered from a
// real `claude` binary (writ sol-159ee545a38d78ac, sol-d792deb1a2e99eec) —
// there is no public schema to compile against. testdata/spike-fixtures/
// holds byte-for-byte copies of the JSON the spikes captured from the real
// `claude` CLI's own `/plugin install` output and marketplace packaging.
// This test asserts every key path present in each fixture is also present
// in sol's generated output (except the allowlisted per-server "env" key,
// which sol's plugin does not use — see EnsureMarketplace's doc comment).
// If Claude Code's on-disk format changes, update the fixture from a fresh
// spike and this test will fail loudly until the generator catches up.

// keyPaths recursively collects "dotted" key paths from a JSON value.
// Arrays are represented by their first element only (these files are lists
// of homogeneous records), keyed as "<path>[]".
func keyPaths(prefix string, v any, out map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			out[path] = true
			keyPaths(path, t[k], out)
		}
	case []any:
		if len(t) > 0 {
			keyPaths(prefix+"[]", t[0], out)
		}
	}
}

// normalizeInstanceKey rewrites a JSON object's single instance-named key
// (a plugin id, a marketplace name — a map key that is data, not schema) to
// a fixed placeholder before path comparison, so the spike's throwaway
// names ("sol-channel-bridge", "sol-spike-marketplace") compare equal to
// sol's production names ("sol-channel", "sol-official"). container is the
// dotted path whose child keys should each be normalized to "*" (e.g. "" for
// a root keyed by marketplace name, "mcpServers" for a map keyed by server
// name); atPath must be an object at every level for this to apply.
func normalizeInstanceKey(v any, container string) any {
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	return normalizeAt(obj, container, "")
}

func normalizeAt(v any, container, path string) any {
	obj, ok := v.(map[string]any)
	if !ok {
		if arr, ok := v.([]any); ok {
			out := make([]any, len(arr))
			for i, e := range arr {
				out[i] = normalizeAt(e, container, path)
			}
			return out
		}
		return v
	}
	out := map[string]any{}
	for k, val := range obj {
		childPath := k
		if path != "" {
			childPath = path + "." + k
		}
		key := k
		if path == container {
			key = "*"
		}
		out[key] = normalizeAt(val, container, childPath)
	}
	return out
}

// allowlistedFixtureOnlyKeys are fixture key paths sol's generator
// deliberately omits — a documented design choice, not drift. Sol's plugin
// resolves its session from inherited SOL_WORLD/SOL_AGENT env vars (see
// EnsureMarketplace) rather than templating per-server env into the shared
// .mcp.json, so it has no "env" key.
var allowlistedFixtureOnlyKeys = map[string]bool{
	"mcpServers.*.env": true,
}

// assertNoDrift asserts every key path in the fixture (after normalizing
// the object keyed at instanceKeyContainer to a fixed placeholder) is also
// present in generated. instanceKeyContainer is "" when the file's
// top-level keys are themselves instance names (known_marketplaces.json).
func assertNoDrift(t *testing.T, label, fixturePath, instanceKeyContainer string, generated map[string]any) {
	t.Helper()
	fixture := readJSON(t, fixturePath)

	fixtureKeys := map[string]bool{}
	keyPaths("", normalizeInstanceKey(fixture, instanceKeyContainer), fixtureKeys)
	generatedKeys := map[string]bool{}
	keyPaths("", normalizeInstanceKey(generated, instanceKeyContainer), generatedKeys)

	for k := range fixtureKeys {
		if allowlistedFixtureOnlyKeys[k] || strings.HasPrefix(k, "mcpServers.*.env.") {
			continue
		}
		if !generatedKeys[k] {
			t.Errorf("%s: schema drift — fixture key path %q not found in generated output (fixture keys: %v; generated keys: %v)",
				label, k, sortedKeys(fixtureKeys), sortedKeys(generatedKeys))
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// noInstanceKey marks a file whose keys are all fixed schema fields — no
// map is keyed by an arbitrary plugin/marketplace instance name, so no
// normalization pass is needed. Chosen so it can never equal a real dotted
// path (paths never contain NUL).
const noInstanceKey = "\x00"

func TestSchemaDriftGuardMarketplaceJSON(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	generated := readJSON(t, filepath.Join(MarketplaceDir(solHome), ".claude-plugin", "marketplace.json"))
	assertNoDrift(t, "marketplace.json", "testdata/spike-fixtures/marketplace.json", noInstanceKey, generated)
}

func TestSchemaDriftGuardPluginJSON(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	generated := readJSON(t, filepath.Join(pluginDir(solHome), ".claude-plugin", "plugin.json"))
	assertNoDrift(t, "plugin.json", "testdata/spike-fixtures/plugin.json", noInstanceKey, generated)
}

func TestSchemaDriftGuardMcpJSON(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	generated := readJSON(t, filepath.Join(pluginDir(solHome), ".mcp.json"))
	assertNoDrift(t, ".mcp.json", "testdata/spike-fixtures/mcp.json", "mcpServers", generated)
}

func TestSchemaDriftGuardInstalledPluginsJSON(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	agentConfigDir := t.TempDir()
	if err := SeedAgent(solHome, agentConfigDir); err != nil {
		t.Fatalf("SeedAgent: %v", err)
	}
	generated := readJSON(t, filepath.Join(agentConfigDir, "plugins", "installed_plugins.json"))
	assertNoDrift(t, "installed_plugins.json", "testdata/spike-fixtures/installed_plugins.json", "plugins", generated)
}

func TestSchemaDriftGuardKnownMarketplacesJSON(t *testing.T) {
	solHome := t.TempDir()
	fakeSolBinary(t, "/opt/sol/bin/sol")
	if err := EnsureMarketplace(solHome); err != nil {
		t.Fatalf("EnsureMarketplace: %v", err)
	}
	agentConfigDir := t.TempDir()
	if err := SeedAgent(solHome, agentConfigDir); err != nil {
		t.Fatalf("SeedAgent: %v", err)
	}
	generated := readJSON(t, filepath.Join(agentConfigDir, "plugins", "known_marketplaces.json"))
	assertNoDrift(t, "known_marketplaces.json", "testdata/spike-fixtures/known_marketplaces.json", "", generated)
}

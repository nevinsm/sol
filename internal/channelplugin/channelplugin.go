// Package channelplugin materializes sol's first-party Claude Code channel
// plugin: a marketplace + plugin package whose stdio MCP server is `sol
// channel serve` (see internal/channelserve). Sol is the vendor of this
// plugin — unlike a third-party plugin an operator installs by hand via
// `sol config claude`, this package generates the plugin/marketplace state
// directly, mirroring the exact file shapes recorded by the spike
// investigation (writ sol-d792deb1a2e99eec's findings.md and
// prototype/marketplace-src/).
//
// Two distinct write scopes, matching how Claude Code itself separates
// "plugin content" from "plugin installation record":
//
//   - EnsureMarketplace materializes the marketplace + plugin CONTENT
//     (marketplace.json, plugin.json, .mcp.json) sphere-wide, under
//     $SOL_HOME/.claude-defaults/plugins/marketplaces/<marketplace>/. This
//     mirrors EnsureClaudeDefaults: sol owns this content and always
//     overwrites it. By itself, materializing this content has no effect —
//     it is inert until an agent's own installed_plugins.json references it.
//   - SeedAgent merges the INSTALLATION RECORD (installed_plugins.json,
//     known_marketplaces.json, settings.json's enabledPlugins) into one
//     agent's own already-seeded config dir. This is the per-agent gate:
//     callers should only call SeedAgent for an agent whose world has opted
//     into channels (world.toml's agents.channels_enabled), so that turning
//     channels on for one world does not silently activate the plugin (and
//     its extra MCP subprocess) for every agent sphere-wide, even though the
//     underlying marketplace content is shared.
//
// See docs/decisions/0044-claude-channels-plugin.md for the design
// rationale and docs/channels.md for the operator-facing setup recipe.
package channelplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/fileutil"
)

// MarketplaceName and PluginName identify sol's first-party channel plugin.
// Together with PluginVersion they determine the on-disk layout under
// .claude-defaults/plugins/marketplaces/ and the installed_plugins.json key
// ("<PluginName>@<MarketplaceName>").
const (
	MarketplaceName = "sol-official"
	PluginName      = "sol-channel"
	PluginVersion   = "0.1.0"
)

// installedPluginsVersion is the schema version field seen in every
// installed_plugins.json observed during the spike investigation.
const installedPluginsVersion = 2

// resolveSolBinary returns the absolute path to the running sol binary.
// Overridden in tests. Mirrors internal/daemon/lifecycle.go's identical
// indirection for the same reason: os.Executable() cannot be driven
// deterministically in tests.
var resolveSolBinary = os.Executable

// nowFunc returns the current time. Overridden in tests for deterministic
// installedAt/lastUpdated assertions.
var nowFunc = func() time.Time { return time.Now().UTC() }

// ChannelsArg returns the --channels launch argument value that activates
// sol's channel plugin, e.g. "plugin:sol-channel@sol-official". Passed to
// ClaudeRuntime.BuildCommand only when the world's agents.channels_enabled
// config flag is on.
func ChannelsArg() string {
	return fmt.Sprintf("plugin:%s@%s", PluginName, MarketplaceName)
}

// PluginKey returns the installed_plugins.json / enabledPlugins map key for
// sol's channel plugin, e.g. "sol-channel@sol-official".
func PluginKey() string {
	return fmt.Sprintf("%s@%s", PluginName, MarketplaceName)
}

// MarketplaceDir returns the directory holding sol's channel marketplace
// content: $SOL_HOME/.claude-defaults/plugins/marketplaces/<marketplace>/.
func MarketplaceDir(solHome string) string {
	return filepath.Join(solHome, ".claude-defaults", "plugins", "marketplaces", MarketplaceName)
}

// pluginDir returns the directory holding sol's channel plugin content
// inside the marketplace: <marketplaceDir>/<plugin>/.
func pluginDir(solHome string) string {
	return filepath.Join(MarketplaceDir(solHome), PluginName)
}

// --- marketplace.json / plugin.json / .mcp.json shapes ---------------------
//
// Field names and nesting mirror the local directory marketplace format
// documented in writ sol-d792deb1a2e99eec's findings.md and captured
// verbatim in prototype/marketplace-src/. See channelplugin_test.go's
// schema-drift guard, which compares these shapes against fixtures copied
// from that spike output.

type marketplaceOwner struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type marketplacePluginRef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Source      string `json:"source"`
}

type marketplaceManifest struct {
	Schema      string                 `json:"$schema"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Owner       marketplaceOwner       `json:"owner"`
	Plugins     []marketplacePluginRef `json:"plugins"`
}

type pluginManifest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Keywords    []string `json:"keywords"`
}

type mcpServerStdio struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type mcpManifest struct {
	McpServers map[string]mcpServerStdio `json:"mcpServers"`
}

// EnsureMarketplace materializes sol's channel marketplace + plugin content
// under $SOL_HOME/.claude-defaults/plugins/marketplaces/<marketplace>/.
// Always overwrites (sol owns this content, same as EnsureClaudeDefaults) so
// upgraded field shapes propagate to existing installations. By itself this
// has no effect on any agent — see the package doc for the two-scope split.
func EnsureMarketplace(solHome string) error {
	mktDir := MarketplaceDir(solHome)
	plugDir := pluginDir(solHome)

	if err := os.MkdirAll(filepath.Join(mktDir, ".claude-plugin"), 0o755); err != nil {
		return fmt.Errorf("failed to create channel marketplace dir %q: %w", mktDir, err)
	}
	if err := os.MkdirAll(filepath.Join(plugDir, ".claude-plugin"), 0o755); err != nil {
		return fmt.Errorf("failed to create channel plugin dir %q: %w", plugDir, err)
	}

	marketplace := marketplaceManifest{
		Schema:      "https://anthropic.com/claude-code/marketplace.schema.json",
		Name:        MarketplaceName,
		Description: "Sol's first-party channel plugin marketplace — in-band sol-to-session message delivery for Claude Code (research preview). See docs/decisions/0044-claude-channels-plugin.md.",
		Owner:       marketplaceOwner{Name: "sol", Email: "sol@localhost.invalid"},
		Plugins: []marketplacePluginRef{
			{
				Name:        PluginName,
				Description: "Sol channel bridge — delivers pending sol nudge queue messages into the live session via Claude Code's channels feature. Thin stateless bridge over internal/nudge; see `sol channel serve`.",
				Category:    "development",
				Source:      "./" + PluginName,
			},
		},
	}
	if err := fileutil.AtomicWriteJSON(filepath.Join(mktDir, ".claude-plugin", "marketplace.json"), marketplace, 0o644); err != nil {
		return fmt.Errorf("failed to write channel marketplace manifest: %w", err)
	}

	plugin := pluginManifest{
		Name:        PluginName,
		Description: "Sol channel bridge — pushes pending sol nudge queue messages into the live Claude Code session via notifications/claude/channel.",
		Version:     PluginVersion,
		Keywords:    []string{"sol", "channel", "mcp"},
	}
	if err := fileutil.AtomicWriteJSON(filepath.Join(plugDir, ".claude-plugin", "plugin.json"), plugin, 0o644); err != nil {
		return fmt.Errorf("failed to write channel plugin manifest: %w", err)
	}

	solBin, err := resolveSolBinary()
	if err != nil {
		return fmt.Errorf("failed to resolve sol binary path for channel plugin .mcp.json: %w", err)
	}
	mcp := mcpManifest{
		McpServers: map[string]mcpServerStdio{
			PluginName: {
				Type:    "stdio",
				Command: solBin,
				// No --world/--agent args: the shared .mcp.json is identical
				// for every agent (Claude Code copies it verbatim per
				// installed_plugins.json), so `sol channel serve` resolves
				// its session from SOL_WORLD/SOL_AGENT in its inherited
				// process environment — the same env sol's dispatch already
				// sets on the session (internal/startup.Launch step 12) and
				// that stdio MCP children inherit from their parent `claude`
				// process.
				Args: []string{"channel", "serve"},
			},
		},
	}
	if err := fileutil.AtomicWriteJSON(filepath.Join(plugDir, ".mcp.json"), mcp, 0o644); err != nil {
		return fmt.Errorf("failed to write channel plugin .mcp.json: %w", err)
	}

	return nil
}

// --- per-agent installation record ------------------------------------------

type installedPluginEntry struct {
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
	Version     string `json:"version"`
	InstalledAt string `json:"installedAt"`
	LastUpdated string `json:"lastUpdated"`
}

type installedPluginsFile struct {
	Version int                              `json:"version"`
	Plugins map[string][]installedPluginEntry `json:"plugins"`
}

type marketplaceSourceRef struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type marketplaceRecord struct {
	Source          marketplaceSourceRef `json:"source"`
	InstallLocation string               `json:"installLocation"`
	LastUpdated     string               `json:"lastUpdated"`
}

type knownMarketplacesFile map[string]marketplaceRecord

// SeedAgent merges sol's channel plugin installation record into one
// agent's already-seeded config dir (agentConfigDir — the same directory
// config.SeedClaudeConfig just populated). Callers must call this only for
// agents whose world has channels enabled: unlike EnsureMarketplace, this
// writes per-agent state, and its presence is what actually causes Claude
// Code to spawn the plugin's MCP server for this specific agent.
//
// Merges rather than overwrites: config.SeedClaudeConfig may already have
// copied an operator's own plugins from .claude-defaults/plugins/ into
// agentConfigDir/plugins/ (via `sol config claude`); this only adds/updates
// sol's own entry, leaving any other installed plugin untouched. Idempotent:
// safe to call on every session start (matches Seed's documented contract).
func SeedAgent(solHome, agentConfigDir string) error {
	pluginsDir := filepath.Join(agentConfigDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create agent plugins dir %q: %w", pluginsDir, err)
	}

	if err := mergeInstalledPlugins(pluginsDir, pluginDir(solHome)); err != nil {
		return err
	}
	if err := mergeKnownMarketplaces(pluginsDir, MarketplaceDir(solHome)); err != nil {
		return err
	}
	if err := mergeEnabledPlugin(agentConfigDir); err != nil {
		return err
	}
	return nil
}

// mergeInstalledPlugins upserts sol's channel plugin entry into
// <pluginsDir>/installed_plugins.json, preserving every other plugin entry
// already present and preserving the original installedAt timestamp across
// repeated calls (only lastUpdated advances).
func mergeInstalledPlugins(pluginsDir, installPath string) error {
	path := filepath.Join(pluginsDir, "installed_plugins.json")
	file := readInstalledPlugins(path)

	now := nowFunc().Format(time.RFC3339)
	installedAt := now
	if existing, ok := file.Plugins[PluginKey()]; ok && len(existing) > 0 && existing[0].InstalledAt != "" {
		installedAt = existing[0].InstalledAt
	}

	if file.Plugins == nil {
		file.Plugins = make(map[string][]installedPluginEntry)
	}
	file.Version = installedPluginsVersion
	file.Plugins[PluginKey()] = []installedPluginEntry{{
		Scope:       "user",
		InstallPath: installPath,
		Version:     PluginVersion,
		InstalledAt: installedAt,
		LastUpdated: now,
	}}

	return fileutil.AtomicWriteJSON(path, file, 0o644)
}

// readInstalledPlugins reads and parses an existing installed_plugins.json.
// A missing or unparseable file is treated as empty (best-effort: a
// corrupt operator-authored file should not block sol's own seeding).
func readInstalledPlugins(path string) installedPluginsFile {
	file := installedPluginsFile{Version: installedPluginsVersion, Plugins: map[string][]installedPluginEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return file
	}
	var parsed installedPluginsFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return file
	}
	if parsed.Plugins == nil {
		parsed.Plugins = map[string][]installedPluginEntry{}
	}
	return parsed
}

// mergeKnownMarketplaces upserts sol's marketplace entry into
// <pluginsDir>/known_marketplaces.json, preserving every other marketplace
// entry already present.
func mergeKnownMarketplaces(pluginsDir, marketDir string) error {
	path := filepath.Join(pluginsDir, "known_marketplaces.json")
	file := readKnownMarketplaces(path)

	file[MarketplaceName] = marketplaceRecord{
		Source:          marketplaceSourceRef{Source: "directory", Path: marketDir},
		InstallLocation: marketDir,
		LastUpdated:     nowFunc().Format(time.RFC3339),
	}

	return fileutil.AtomicWriteJSON(path, file, 0o644)
}

func readKnownMarketplaces(path string) knownMarketplacesFile {
	file := knownMarketplacesFile{}
	data, err := os.ReadFile(path)
	if err != nil {
		return file
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return knownMarketplacesFile{}
	}
	if file == nil {
		file = knownMarketplacesFile{}
	}
	return file
}

// mergeEnabledPlugin sets enabledPlugins[<plugin>@<marketplace>] = true in
// the agent's own settings.json (already written by
// config.SeedClaudeConfig's seedClaudeSettings step), preserving every
// other key already present — including any enabledPlugins entries that
// step's own mergeEnabledPlugins call already added for operator-installed
// plugins.
func mergeEnabledPlugin(agentConfigDir string) error {
	path := filepath.Join(agentConfigDir, "settings.json")
	settings := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to read agent settings.json %q: %w", path, err)
		}
	} else if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse agent settings.json %q: %w", path, err)
	}

	enabled, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		enabled = map[string]any{}
	}
	enabled[PluginKey()] = true
	settings["enabledPlugins"] = enabled

	return fileutil.AtomicWriteJSON(path, settings, 0o644)
}

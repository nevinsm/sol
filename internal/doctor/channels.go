package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/nevinsm/sol/internal/channelplugin"
	"github.com/nevinsm/sol/internal/config"
)

// managedSettingsPath returns the platform-specific path to Claude Code's
// managed-settings.json (policySettings source — the only settings source
// the channels allowlist gate reads; see docs/decisions/0044-claude-channels-plugin.md
// and its cited spike findings). Windows is not returned (empty string) —
// CheckChannelsManagedSettings treats that as "unsupported platform, cannot
// validate" rather than "missing". A package var (not a plain function) so
// tests can point it at a scratch file instead of the real host path, which
// requires root to write and would be unsafe to touch from a test.
var managedSettingsPath = func() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "linux":
		return "/etc/claude-code/managed-settings.json"
	default:
		return ""
	}
}

// managedSettingsFile is the subset of managed-settings.json fields the
// channels gate reads (writ sol-d792deb1a2e99eec's findings.md, "How this
// fits the reverse-engineered gate logic").
type managedSettingsFile struct {
	ChannelsEnabled       bool                   `json:"channelsEnabled"`
	AllowedChannelPlugins []allowedChannelPlugin `json:"allowedChannelPlugins"`
}

type allowedChannelPlugin struct {
	Plugin      string `json:"plugin"`
	Marketplace string `json:"marketplace"`
}

// CheckChannelsManagedSettings verifies that any world with
// agents.channels_enabled = true using the claude runtime has a valid,
// operator-installed managed-settings.json allowlisting sol's channel
// plugin. Advisory (Passed=true, Warning=true): a missing or invalid file
// does not break sol — nudge.Deliver's doorbell fallback carries the whole
// delivery load in that case — it just means channels silently never
// deliver. Sol never writes this file itself (host-wide, requires root,
// operator-managed — same precedent as docs/credentials.md); this check
// only validates and points at the exact recipe.
//
// One result per world that has channels enabled and resolves at least one
// role to the claude runtime. Worlds with the flag off, or that never use
// claude, are skipped entirely — this check has nothing to say about them.
func CheckChannelsManagedSettings(worlds []string) []CheckResult {
	var results []CheckResult

	settingsPath := managedSettingsPath()

	for _, world := range worlds {
		cfg, err := config.LoadWorldConfig(world)
		if err != nil {
			// Already surfaced by CheckRuntimeBinaries/CheckRuntimeCredentials.
			continue
		}
		if !cfg.Agents.ChannelsEnabled {
			continue
		}
		usesClaude := false
		for _, role := range []string{"outpost", "envoy", "forge"} {
			if cfg.ResolveRuntime(role) == "claude" {
				usesClaude = true
				break
			}
		}
		if !usesClaude {
			continue
		}

		results = append(results, checkChannelsManagedSettingsForWorld(world, settingsPath))
	}

	return results
}

func checkChannelsManagedSettingsForWorld(world, settingsPath string) CheckResult {
	name := fmt.Sprintf("channels:%s", world)

	if settingsPath == "" {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"channels: world %q has agents.channels_enabled = true, but this platform's "+
					"managed-settings.json path is not yet known to sol doctor — validation skipped.",
				world),
			Fix: "See docs/channels.md for the managed-settings.json recipe for your platform.",
		}
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Name:    name,
				Passed:  true,
				Warning: true,
				Message: fmt.Sprintf(
					"channels: world %q has agents.channels_enabled = true, but %s does not exist.\n"+
						"      Without it, Claude Code silently drops every channel delivery attempt for this\n"+
						"      world's agents (the pane doorbell fallback still works, so nothing is lost —\n"+
						"      messages just never arrive in-band). See docs/channels.md.",
					world, settingsPath),
				Fix: channelsFixText(settingsPath),
			}
		}
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("channels: world %q: failed to read %s: %v", world, settingsPath, err),
			Fix:     channelsFixText(settingsPath),
		}
	}

	var parsed managedSettingsFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("channels: world %q: %s is not valid JSON: %v", world, settingsPath, err),
			Fix:     channelsFixText(settingsPath),
		}
	}

	if !parsed.ChannelsEnabled {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"channels: world %q: %s exists but is missing \"channelsEnabled\": true.\n"+
					"      A managed-settings.json file that exists WITHOUT this key is MORE restrictive\n"+
					"      than no file at all — it blocks channels outright rather than leaving the gate open.",
				world, settingsPath),
			Fix: channelsFixText(settingsPath),
		}
	}

	for _, allowed := range parsed.AllowedChannelPlugins {
		if allowed.Plugin == channelplugin.PluginName && allowed.Marketplace == channelplugin.MarketplaceName {
			return CheckResult{
				Name:    name,
				Passed:  true,
				Message: fmt.Sprintf("channels: world %q: managed-settings.json allowlists sol's channel plugin", world),
			}
		}
	}

	return CheckResult{
		Name:    name,
		Passed:  true,
		Warning: true,
		Message: fmt.Sprintf(
			"channels: world %q: %s exists with channelsEnabled: true, but allowedChannelPlugins\n"+
				"      does not include {\"plugin\": %q, \"marketplace\": %q}.",
			world, settingsPath, channelplugin.PluginName, channelplugin.MarketplaceName),
		Fix: channelsFixText(settingsPath),
	}
}

// channelsFixText renders the exact managed-settings.json content and
// install recipe, mirroring CheckRuntimeCredentials's Fix-field style.
func channelsFixText(settingsPath string) string {
	content, _ := json.MarshalIndent(managedSettingsFile{
		ChannelsEnabled: true,
		AllowedChannelPlugins: []allowedChannelPlugin{
			{Plugin: channelplugin.PluginName, Marketplace: channelplugin.MarketplaceName},
		},
	}, "", "  ")
	return fmt.Sprintf(
		"Install %s (requires root) with this content, then re-run sol doctor:\n\n%s\n\n"+
			"See docs/channels.md for the full recipe and its host-wide-not-per-agent scope caveat.",
		settingsPath, content)
}

package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

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

	dropinDir := managedSettingsDropinDir(settingsPath)

	// candidates holds every parsed source that could satisfy the gate: the
	// main file (if present and valid) plus every managed-settings.d/
	// fragment (if the drop-in dir exists). Kept simple per the spike's
	// note that the drop-in merge semantics weren't exercised: we don't
	// attempt to merge partial keys across sources — any single candidate
	// that alone provides both channelsEnabled: true and the allowlist
	// entry satisfies the check.
	var candidates []managedSettingsFile
	mainExists := false

	data, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		mainExists = true
		var parsed managedSettingsFile
		if jsonErr := json.Unmarshal(data, &parsed); jsonErr != nil {
			return CheckResult{
				Name:    name,
				Passed:  true,
				Warning: true,
				Message: fmt.Sprintf("channels: world %q: %s is not valid JSON: %v", world, settingsPath, jsonErr),
				Fix:     channelsFixText(settingsPath),
			}
		}
		candidates = append(candidates, parsed)
	case os.IsNotExist(err):
		// Fall through — a managed-settings.d/ fragment may still satisfy
		// the check on its own.
	default:
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("channels: world %q: failed to read %s: %v", world, settingsPath, err),
			Fix:     channelsFixText(settingsPath),
		}
	}

	fragments := readManagedSettingsDropin(dropinDir)
	candidates = append(candidates, fragments...)

	if !mainExists && len(fragments) == 0 {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"channels: world %q has agents.channels_enabled = true, but neither %s nor a\n"+
					"      %s fragment exists.\n"+
					"      Without one, Claude Code silently drops every channel delivery attempt for this\n"+
					"      world's agents (the pane doorbell fallback still works, so nothing is lost —\n"+
					"      messages just never arrive in-band). See docs/channels.md.",
				world, settingsPath, dropinDir),
			Fix: channelsFixText(settingsPath),
		}
	}

	anyEnabled := false
	for _, c := range candidates {
		if !c.ChannelsEnabled {
			continue
		}
		anyEnabled = true
		for _, allowed := range c.AllowedChannelPlugins {
			if allowed.Plugin == channelplugin.PluginName && allowed.Marketplace == channelplugin.MarketplaceName {
				return CheckResult{
					Name:    name,
					Passed:  true,
					Message: fmt.Sprintf("channels: world %q: managed-settings.json allowlists sol's channel plugin", world),
				}
			}
		}
	}

	if !anyEnabled {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"channels: world %q: %s (or a %s fragment) exists but none sets\n"+
					"      \"channelsEnabled\": true.\n"+
					"      A managed-settings.json file that exists WITHOUT this key is MORE restrictive\n"+
					"      than no file at all — it blocks channels outright rather than leaving the gate open.",
				world, settingsPath, dropinDir),
			Fix: channelsFixText(settingsPath),
		}
	}

	return CheckResult{
		Name:    name,
		Passed:  true,
		Warning: true,
		Message: fmt.Sprintf(
			"channels: world %q: %s (or a %s fragment) has channelsEnabled: true,\n"+
				"      but no single source's allowedChannelPlugins includes {\"plugin\": %q, \"marketplace\": %q}.",
			world, settingsPath, dropinDir, channelplugin.PluginName, channelplugin.MarketplaceName),
		Fix: channelsFixText(settingsPath),
	}
}

// managedSettingsDropinDir returns the managed-settings.d/ drop-in
// directory sibling to the given managed-settings.json path — spotted
// alongside the single-file form in writ sol-d792deb1a2e99eec's findings.md
// (binary-strings reverse engineering; the spike itself didn't exercise
// this variant). Any *.json fragment inside it that alone provides
// channelsEnabled: true and the allowlist entry satisfies the check exactly
// like the main file would, without sol needing to model the drop-in's
// actual merge precedence.
func managedSettingsDropinDir(settingsPath string) string {
	return filepath.Join(filepath.Dir(settingsPath), "managed-settings.d")
}

// readManagedSettingsDropin best-effort reads every *.json fragment in dir,
// skipping the directory entirely (missing dir, or one that can't be
// listed) and skipping any fragment that isn't valid JSON — a corrupt or
// unrelated fragment an operator dropped in for other reasons shouldn't
// block sol from recognizing a different, valid fragment. Sorted by name
// for deterministic ordering; ordering has no effect on the result since
// candidates are evaluated independently, not merged.
func readManagedSettingsDropin(dir string) []managedSettingsFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var fragments []managedSettingsFile
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var parsed managedSettingsFile
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue
		}
		fragments = append(fragments, parsed)
	}
	return fragments
}

// ManagedSettingsJSON renders the canonical managed-settings.json content
// that allowlists sol's channel plugin, built from sol's own plugin identity
// constants (channelplugin.PluginName / MarketplaceName) rather than
// hand-typed strings, so it can never drift from what the allowlist check
// above actually validates against. This is the single source of truth for
// that recipe: docs/channels.md's "Installing managed-settings.json" section
// embeds this exact text verbatim (both the "Content:" block and the sudo
// install heredoc use identical JSON, not independently authored prose),
// and channelsFixText below renders it into sol doctor's Fix field.
// TestManagedSettingsJSONMatchesDocs guards the docs/channels.md copies
// against drift — change this function, not the doc, when the shape
// changes, and let the test tell you what else to update.
func ManagedSettingsJSON() string {
	content, _ := json.MarshalIndent(managedSettingsFile{
		ChannelsEnabled: true,
		AllowedChannelPlugins: []allowedChannelPlugin{
			{Plugin: channelplugin.PluginName, Marketplace: channelplugin.MarketplaceName},
		},
	}, "", "  ")
	return string(content)
}

// ManagedSettingsInstallRecipe renders the exact `sudo install` one-liner
// that installs ManagedSettingsJSON at path. docs/channels.md ("Installing
// managed-settings.json") publishes this identical recipe verbatim for the
// Linux path — this function is the single source of truth for that text;
// TestManagedSettingsJSONMatchesDocs and TestChannelsFixRecipeMatchesDocs
// guard the two from silently drifting apart.
func ManagedSettingsInstallRecipe(path string) string {
	return fmt.Sprintf("sudo install -D -m 0644 /dev/stdin %s <<'EOF'\n%s\nEOF", path, ManagedSettingsJSON())
}

// channelsFixText renders the exact managed-settings.json install recipe,
// mirroring CheckRuntimeCredentials's Fix-field style.
func channelsFixText(settingsPath string) string {
	return fmt.Sprintf(
		"Install %s (requires root):\n\n%s\n\n"+
			"Then re-run sol doctor. See docs/channels.md for the full recipe, the\n"+
			"managed-settings.d/ drop-in alternative, and the host-wide-not-per-agent scope caveat.",
		settingsPath, ManagedSettingsInstallRecipe(settingsPath))
}

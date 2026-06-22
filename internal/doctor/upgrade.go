package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/config"
)

// knownClaudeConfigRoles are the active role directory names under
// <world>/.claude-config/. Any directory with a different name is considered
// defunct and should be flagged.
var knownClaudeConfigRoles = map[string]bool{
	"outposts":    true,
	"envoys":      true,
	"forge-merge": true,
}

// knownDefunctRoles maps defunct role directory names to a description of
// what replaced them, for use in fix messages.
var knownDefunctRoles = map[string]string{
	"forge": "forge-merge (renamed in the architectural simplification, ADR-0040)",
}

// CheckCredentialSymlinks checks that per-agent .credentials.json files under
// each world's .claude-config/ directory are symlinks rather than regular files.
//
// Background: the architectural simplification (ADR-0040) changed credential
// handling so that the claude adapter creates a single symlink at agent spawn
// time pointing at the operator-managed global credential file
// (~/.claude/.credentials.json). It is never swapped.
//
// Pre-simplification installations may have real files instead of symlinks;
// these expire silently (no refresh path) and cause 401 errors.
//
// With --fix: delete the stale regular file. The next session start recreates
// it as the correct symlink via EnsureConfigDir in the claude runtime.
func CheckCredentialSymlinks(solHome string, worlds []string) []CheckResult {
	var results []CheckResult
	for _, world := range worlds {
		worldDir := filepath.Join(solHome, world)
		configRoot := filepath.Join(worldDir, ".claude-config")
		if _, err := os.Stat(configRoot); os.IsNotExist(err) {
			continue
		}

		// Walk <world>/.claude-config/<role>/<agent>/.credentials.json
		_ = filepath.WalkDir(configRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.Name() != ".credentials.json" {
				return nil
			}
			// Use Lstat so we see the symlink itself, not its target.
			fi, err := os.Lstat(path)
			if err != nil {
				return nil
			}
			if fi.Mode()&os.ModeSymlink != 0 {
				// Correct post-simplification state — symlink.
				return nil
			}
			if !fi.Mode().IsRegular() {
				return nil
			}

			// Regular file: stale pre-simplification credential.
			agentName := filepath.Base(filepath.Dir(path))
			roleName := filepath.Base(filepath.Dir(filepath.Dir(path)))
			credPath := path // captured for closure

			results = append(results, CheckResult{
				Name:    fmt.Sprintf("credential_symlink:%s:%s:%s", world, roleName, agentName),
				Passed:  true,
				Warning: true,
				Message: fmt.Sprintf(
					"%s is a regular file (not a symlink) — stale pre-simplification credential that will cause 401 errors when it expires",
					path),
				Fix: fmt.Sprintf(
					"Delete the file (the next session start will recreate it as the correct symlink):\n  rm %s\nOr run: sol doctor --fix",
					path),
				Remediate: func() error {
					if err := os.Remove(credPath); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("failed to remove %s: %w", credPath, err)
					}
					fmt.Printf("  Removed %s\n", credPath)
					return nil
				},
			})
			return nil
		})
	}
	return results
}

// CheckObsoleteAccountsDir checks whether $SOL_HOME/.accounts/ exists.
//
// This directory was created by the pre-simplification account management
// machinery (internal/account/, ADR-0019) and is no longer used. Its presence
// is harmless but may confuse operators and its credentials may be stale.
//
// With --fix: move the directory to $SOL_HOME/.accounts.bak.<timestamp>/
// rather than deleting, so operators can recover any OAuth tokens stored there.
func CheckObsoleteAccountsDir(solHome string) CheckResult {
	const name = "obsolete_accounts_dir"
	accountsDir := filepath.Join(solHome, ".accounts")

	if _, err := os.Stat(accountsDir); os.IsNotExist(err) {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Message: "no stale .accounts/ directory",
		}
	}

	capturedDir := accountsDir
	return CheckResult{
		Name:    name,
		Passed:  true,
		Warning: true,
		Message: fmt.Sprintf(
			"%s exists — stale directory from pre-simplification account management (ADR-0019/ADR-0040); no longer used by sol",
			accountsDir),
		Fix: fmt.Sprintf(
			"Move or remove the directory (backup recommended in case you need to recover OAuth tokens):\n  mv %s %s.bak\nOr run: sol doctor --fix",
			accountsDir, accountsDir),
		Remediate: func() error {
			ts := time.Now().UTC().Format("20060102T150405Z")
			dest := fmt.Sprintf("%s.bak.%s", capturedDir, ts)
			if err := os.Rename(capturedDir, dest); err != nil {
				return fmt.Errorf("failed to move %s to %s: %w", capturedDir, dest, err)
			}
			fmt.Printf("  Moved %s → %s\n", capturedDir, dest)
			return nil
		},
	}
}

// CheckDeadWorldConfigKeys checks each world's world.toml for TOML keys
// that are not recognized by the current binary's WorldConfig schema, and for
// known no-op keys from the pre-simplification architecture.
//
// Specifically detected:
//   - Unknown top-level TOML sections (e.g. [budget], [accounts]) — these were
//     removed in ADR-0040 and are no longer parsed by the current binary.
//   - world.default_account — the [world.default_account] key is still parsed
//     (to avoid config parse errors for upgrading users) but does nothing;
//     the account routing system was removed in ADR-0040.
//
// No auto-fix is provided: config file mutation is too risky to automate.
// The operator should edit world.toml manually.
func CheckDeadWorldConfigKeys(solHome string, worlds []string) []CheckResult {
	var results []CheckResult
	for _, world := range worlds {
		path := config.WorldConfigPath(world)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}

		// Decode into WorldConfig to detect truly unknown keys via Undecoded().
		var cfg config.WorldConfig
		meta, err := toml.DecodeFile(path, &cfg)
		if err != nil {
			// Parse errors are already surfaced by CheckRuntimeBinaries; skip here.
			continue
		}

		// Collect unknown top-level TOML sections/keys that the struct
		// does not recognize. Deduplicate: only report each top-level section once.
		seen := make(map[string]bool)
		var deadKeys []string
		for _, key := range meta.Undecoded() {
			if len(key) == 0 {
				continue
			}
			topKey := key[0]
			if !seen[topKey] {
				seen[topKey] = true
				deadKeys = append(deadKeys, topKey)
			}
		}

		// Also check for world.default_account explicitly. It is still parsed
		// by WorldSection (to avoid breaking config on upgrade) but does nothing
		// since the account routing system was removed.
		if cfg.World.DefaultAccount != "" {
			deadKeys = append(deadKeys, "world.default_account")
		}

		if len(deadKeys) == 0 {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("dead_config_keys:%s", world),
				Passed:  true,
				Message: fmt.Sprintf("%s: no dead or unrecognized config keys found", path),
			})
			continue
		}

		sort.Strings(deadKeys)
		results = append(results, CheckResult{
			Name:    fmt.Sprintf("dead_config_keys:%s", world),
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"%s: %d dead/no-op config key(s) from pre-simplification architecture: %s",
				path, len(deadKeys), strings.Join(deadKeys, ", ")),
			Fix: fmt.Sprintf(
				"Edit %s and remove or comment out: %s\n(No auto-fix — config file mutation is not automated; edit manually.)",
				path, strings.Join(deadKeys, ", ")),
			// No Remediate: config file mutation is too risky to automate.
		})
	}
	return results
}

// CheckDefunctConfigDirs walks <world>/.claude-config/ for role directories
// whose names are no longer valid in the current architecture. This catches:
//
//   - "forge": the old daemon name before it was renamed to "forge-merge"
//     in the architectural simplification (ADR-0040)
//   - Any other unrecognized role directory name
//
// Active role directories (outposts, envoys, forge-merge) are silently skipped.
//
// With --fix: move the defunct role directory to <name>.bak.<timestamp>/
// rather than deleting it. Agent sessions that are currently active in tmux
// are skipped as a safety measure.
func CheckDefunctConfigDirs(solHome string, worlds []string) []CheckResult {
	var results []CheckResult
	for _, world := range worlds {
		worldDir := filepath.Join(solHome, world)
		configRoot := filepath.Join(worldDir, ".claude-config")

		entries, err := os.ReadDir(configRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("defunct_config_dirs:%s", world),
				Passed:  false,
				Message: fmt.Sprintf("failed to read %s: %v", configRoot, err),
				Fix:     "Check directory permissions",
			})
			continue
		}

		for _, roleEntry := range entries {
			if !roleEntry.IsDir() {
				continue
			}
			roleDir := roleEntry.Name()
			rolePath := filepath.Join(configRoot, roleDir)

			if knownClaudeConfigRoles[roleDir] {
				// Known active role — skip.
				continue
			}

			// Build the human-readable description.
			var desc string
			if replacement, isKnown := knownDefunctRoles[roleDir]; isKnown {
				desc = fmt.Sprintf("%s is a defunct role directory (replaced by %s)", rolePath, replacement)
			} else {
				desc = fmt.Sprintf("%s is an unrecognized role directory (known active roles: outposts, envoys, forge-merge)", rolePath)
			}

			// Count agent subdirectories for the message.
			agentEntries, _ := os.ReadDir(rolePath)

			// Build the remediation closure. Capture loop variables explicitly.
			capturedWorld := world
			capturedRolePath := rolePath
			capturedAgentEntries := agentEntries

			results = append(results, CheckResult{
				Name:    fmt.Sprintf("defunct_config_dir:%s:%s", world, roleDir),
				Passed:  true,
				Warning: true,
				Message: fmt.Sprintf("%s (%d agent config dir(s))", desc, len(agentEntries)),
				Fix: fmt.Sprintf(
					"Move the directory:\n  mv %s %s.bak\nOr run: sol doctor --fix",
					rolePath, rolePath),
				Remediate: func() error {
					// Safety: skip if any agent under this role has an active tmux session.
					for _, ae := range capturedAgentEntries {
						if !ae.IsDir() {
							continue
						}
						sessionName := config.SessionName(capturedWorld, ae.Name())
						if sessionActive(sessionName) {
							fmt.Printf("  Skipping %s/%s — tmux session %s is active\n",
								capturedRolePath, ae.Name(), sessionName)
							return nil
						}
					}
					ts := time.Now().UTC().Format("20060102T150405Z")
					dest := fmt.Sprintf("%s.bak.%s", capturedRolePath, ts)
					if err := os.Rename(capturedRolePath, dest); err != nil {
						return fmt.Errorf("failed to move %s to %s: %w", capturedRolePath, dest, err)
					}
					fmt.Printf("  Moved %s → %s\n", capturedRolePath, dest)
					return nil
				},
			})
		}
	}
	return results
}

// sessionActive checks if a tmux session with the given name is currently
// running. Returns false on any error (best-effort check).
func sessionActive(sessionName string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	return cmd.Run() == nil
}

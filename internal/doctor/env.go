package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envfile"
)

// CheckEnvFiles validates .env files for all initialized worlds in SOL_HOME.
// It checks for parse errors, empty values, and world-readable permissions.
// Missing .env files are not reported — absence is not an issue.
// Results are grouped per-world; returns a single passing result if no issues
// are found.
func CheckEnvFiles() []CheckResult {
	home := config.Home()

	entries, err := os.ReadDir(home)
	if err != nil {
		if os.IsNotExist(err) {
			// SOL_HOME not initialized yet — nothing to check.
			return []CheckResult{{
				Name:    "env_files",
				Passed:  true,
				Message: "no worlds found (SOL_HOME does not exist)",
			}}
		}
		return []CheckResult{{
			Name:    "env_files",
			Passed:  false,
			Message: fmt.Sprintf("cannot read SOL_HOME %q: %v", home, err),
		}}
	}

	var results []CheckResult

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		world := entry.Name()

		// Only process initialized worlds (those with world.toml).
		worldToml := filepath.Join(home, world, "world.toml")
		if _, err := os.Stat(worldToml); err != nil {
			continue
		}

		envPath := filepath.Join(home, world, ".env")
		info, statErr := os.Stat(envPath)
		if os.IsNotExist(statErr) {
			// Missing .env is fine — no check needed.
			continue
		}
		if statErr != nil {
			results = append(results, CheckResult{
				Name:    "env:" + world,
				Passed:  false,
				Message: fmt.Sprintf("world %q: cannot stat %s: %v", world, envPath, statErr),
			})
			continue
		}

		// Check file permissions: warn if world-readable (o+r bit set).
		mode := info.Mode().Perm()
		if mode&0o004 != 0 {
			results = append(results, CheckResult{
				Name:    "env:" + world,
				Passed:  false,
				Message: fmt.Sprintf("world %q: %s is world-readable (mode %04o)", world, envPath, mode),
				Fix:     fmt.Sprintf("Restrict permissions to protect secrets: chmod 0600 %s", envPath),
			})
		}

		// Parse the file; any error means malformed syntax.
		pairs, parseErr := envfile.ParseFile(envPath)
		if parseErr != nil {
			results = append(results, CheckResult{
				Name:    "env:" + world,
				Passed:  false,
				Message: fmt.Sprintf("world %q: parse error: %v", world, parseErr),
				Fix:     fmt.Sprintf("Fix the syntax error in %s", envPath),
			})
			continue
		}

		// Warn about keys with empty values — likely forgotten placeholders.
		// Sort keys for deterministic output.
		var emptyKeys []string
		for k, v := range pairs {
			if v == "" {
				emptyKeys = append(emptyKeys, k)
			}
		}
		sort.Strings(emptyKeys)
		for _, key := range emptyKeys {
			results = append(results, CheckResult{
				Name:    "env:" + world,
				Passed:  false,
				Message: fmt.Sprintf("world %q: %s: key %q has an empty value", world, envPath, key),
				Fix:     fmt.Sprintf("Set a value for %s in %s, or remove the key if it is unused", key, envPath),
			})
		}
	}

	if len(results) == 0 {
		return []CheckResult{{
			Name:    "env_files",
			Passed:  true,
			Message: "no .env issues found",
		}}
	}
	return results
}

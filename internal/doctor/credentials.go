package doctor

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envfile"
	"github.com/nevinsm/sol/internal/runtime/loader"
)

// sphereEnvTemplate is written to $SOL_HOME/.env by --fix when the file does
// not already exist. It is intentionally runtime-generic: no specific runtime
// CLI names or commands appear here; those are in docs/credentials.md.
const sphereEnvTemplate = `# sol sphere-level environment file.
# Configure long-lived credentials for each runtime your worlds use.
# See docs/credentials.md for per-runtime guidance.
#
# Examples (placeholders only — replace as needed for your runtimes):
# RUNTIME_X_TOKEN=...
# RUNTIME_Y_API_KEY=...
`

// CheckRuntimeCredentials inspects each world's configured runtime and verifies
// that at least one of the runtime's credential env var names is present in the
// merged env scope (sphere .env + world .env + process environment).
//
// The check is advisory (Passed=true, Warning=true) rather than blocking: a
// missing credential does not prevent sol from running, but it will cause
// agents to prompt for interactive auth mid-writ — breaking unattended operation.
//
// One result is emitted per unique (world, runtime) pair. A world that uses
// the same runtime for all roles produces one result, not one per role.
//
// With --fix: creates $SOL_HOME/.env with a runtime-generic template comment
// if the file does not already exist. Does nothing if the file already exists.
func CheckRuntimeCredentials(solHome string, worlds []string) []CheckResult {
	roles := []string{"outpost", "envoy", "forge"}
	var results []CheckResult

	for _, world := range worlds {
		cfg, err := config.LoadWorldConfig(world)
		if err != nil {
			// World config errors are already surfaced by CheckRuntimeBinaries;
			// skip here to avoid duplicate noise.
			continue
		}

		// Collect unique runtimes for this world.
		seen := make(map[string]bool)
		var runtimes []string
		for _, role := range roles {
			rt := cfg.ResolveRuntime(role)
			if !seen[rt] {
				seen[rt] = true
				runtimes = append(runtimes, rt)
			}
		}
		sort.Strings(runtimes)

		// Load the merged env scope for this world (sphere .env + world .env).
		fileEnv, err := envfile.LoadEnv(solHome, world)
		if err != nil {
			// Surface load errors but don't block the check — fall back to
			// process environment only.
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("credentials:%s", world),
				Passed:  false,
				Message: fmt.Sprintf("credentials: world %q: failed to load .env files: %v", world, err),
				Fix:     "Check .env file syntax — see 'sol doctor' for details",
			})
			continue
		}

		for _, rtName := range runtimes {
			rt, err := loader.Get(rtName)
			if err != nil {
				// Unknown runtime — skip; CheckRuntimeBinaries already handles it.
				continue
			}

			descriptor := rt.Descriptor()
			credKeys := descriptor.CredentialEnvKeys

			if len(credKeys) == 0 {
				// Runtime declares no credential env vars — nothing to check.
				continue
			}

			// Merge: file env (sphere+world) then process environment.
			// Process env wins over file env on collision.
			merged := make(map[string]string, len(fileEnv))
			maps.Copy(merged, fileEnv)
			for _, kv := range os.Environ() {
				if k, v, ok := strings.Cut(kv, "="); ok {
					merged[k] = v
				}
			}

			// Collect env var names (values of the map) and check for presence.
			var envVarNames []string
			for _, envVar := range credKeys {
				envVarNames = append(envVarNames, envVar)
			}
			sort.Strings(envVarNames)

			found := false
			for _, envVar := range envVarNames {
				if v := merged[envVar]; v != "" {
					found = true
					break
				}
			}

			checkName := fmt.Sprintf("credentials:%s:%s", world, rtName)

			if found {
				results = append(results, CheckResult{
					Name:    checkName,
					Passed:  true,
					Message: fmt.Sprintf("credentials: world %q (runtime %q): credential env var present", world, rtName),
				})
				continue
			}

			// No credential env var found — emit an advisory warning.
			sphereEnvPath := filepath.Join(solHome, ".env")
			capturedSolHome := solHome
			results = append(results, CheckResult{
				Name:    checkName,
				Passed:  true,
				Warning: true,
				Message: fmt.Sprintf(
					"credentials: world %q (runtime %q): none of {%s} is set\n"+
						"      Sol agents run unattended. For credentials that survive token expiration\n"+
						"      mid-writ, configure a long-lived credential for the configured runtime.\n"+
						"      See docs/credentials.md.",
					world, rtName, strings.Join(envVarNames, ", ")),
				Fix: fmt.Sprintf(
					"Set a credential env var for runtime %q in %s or %s.\n"+
						"See docs/credentials.md for per-runtime setup recipes.\n"+
						"Or run: sol doctor --fix  (creates %s with a template if absent)",
					rtName,
					filepath.Join(capturedSolHome, ".env"),
					filepath.Join(capturedSolHome, world, ".env"),
					sphereEnvPath),
				Remediate: func() error {
					return createSphereEnvTemplate(capturedSolHome)
				},
			})
		}
	}

	return results
}

// createSphereEnvTemplate creates $SOL_HOME/.env with a runtime-generic
// template comment block and mode 0600. Does nothing if the file already exists.
func createSphereEnvTemplate(solHome string) error {
	path := filepath.Join(solHome, ".env")

	// If the file already exists, --fix is a no-op for this check.
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			// Race: file appeared between Stat and OpenFile — that's fine.
			return nil
		}
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(sphereEnvTemplate); err != nil {
		return fmt.Errorf("failed to write template to %s: %w", path, err)
	}

	fmt.Printf("  Created %s with credential template (mode 0600)\n", path)
	return nil
}

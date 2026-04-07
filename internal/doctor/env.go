package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/envfile"
)

// CheckEnvFiles validates .env files at the sphere level and for each world.
// It checks $SOL_HOME/.env first, then $SOL_HOME/{world}/.env for each world
// in the provided list.
//
// Each .env file is checked for:
//   - Parseability (valid syntax)
//   - No keys with empty values
//   - File permissions (must not be group- or world-readable)
//
// Files that do not exist are skipped (not an error).
// Returns one CheckResult per file that exists.
func CheckEnvFiles(solHome string, worlds []string) []CheckResult {
	var results []CheckResult

	// Check sphere-level .env first.
	sphereEnv := filepath.Join(solHome, ".env")
	if r, ok := checkEnvFile("env:sphere", sphereEnv); ok {
		results = append(results, r)
	}

	// Check world-level .env files.
	for _, world := range worlds {
		worldEnv := filepath.Join(solHome, world, ".env")
		name := fmt.Sprintf("env:%s", world)
		if r, ok := checkEnvFile(name, worldEnv); ok {
			results = append(results, r)
		}
	}

	return results
}

// checkEnvFile validates a single .env file. Returns (result, true) if the
// file exists and was checked; returns ("", false) if the file does not exist.
func checkEnvFile(name, path string) (CheckResult, bool) {
	// Check if the file exists.
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return CheckResult{}, false
	}
	if err != nil {
		return CheckResult{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("%s: cannot stat: %v", path, err),
			Fix:     "Check file permissions",
		}, true
	}

	// Check file permissions: must not be group- or world-readable.
	mode := info.Mode().Perm()
	if mode&0o044 != 0 {
		return CheckResult{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("%s: permissions %04o — file is readable by group or others", path, mode),
			Fix:     fmt.Sprintf("Restrict permissions: chmod 600 %s", path),
		}, true
	}

	// Parse the file.
	env, err := envfile.ParseFile(path)
	if err != nil {
		return CheckResult{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("%s: parse error: %v", path, err),
			Fix:     "Fix syntax in the .env file",
		}, true
	}

	// Check for empty values.
	for k, v := range env {
		if v == "" {
			return CheckResult{
				Name:    name,
				Passed:  false,
				Message: fmt.Sprintf("%s: key %q has an empty value", path, k),
				Fix:     fmt.Sprintf("Set a value for %q or remove the line", k),
			}, true
		}
	}

	return CheckResult{
		Name:    name,
		Passed:  true,
		Message: fmt.Sprintf("%s (%d keys)", path, len(env)),
	}, true
}

// discoverWorlds returns a list of world names in solHome by scanning for
// subdirectories that contain a world.toml file. This allows the doctor to
// enumerate worlds without a database dependency.
//
// If the scan fails (permission denied, missing SOL_HOME, etc.), the error
// is returned alongside the (possibly nil) partial result so callers can
// surface a CheckResult — the doctor's job is to surface problems, not hide
// them.
func discoverWorlds(solHome string) ([]string, error) {
	entries, err := os.ReadDir(solHome)
	if err != nil {
		return nil, fmt.Errorf("read SOL_HOME %q: %w", solHome, err)
	}

	var worlds []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		toml := filepath.Join(solHome, e.Name(), "world.toml")
		if _, err := os.Stat(toml); err == nil {
			worlds = append(worlds, e.Name())
		}
	}
	return worlds, nil
}

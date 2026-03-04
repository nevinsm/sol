package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWorld infers the world from context when not explicitly provided.
//
// Precedence:
//  1. Explicit arg/flag — if non-empty, validate via RequireWorld and return
//  2. SOL_WORLD env var — validate and return
//  3. Path detection — if cwd is under $SOL_HOME/{world}/..., extract world name
//  4. Error — no world could be determined
func ResolveWorld(explicit string) (string, error) {
	// 1. Explicit argument takes highest priority.
	if explicit != "" {
		if err := RequireWorld(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}

	// 2. SOL_WORLD environment variable.
	if env := os.Getenv("SOL_WORLD"); env != "" {
		if err := RequireWorld(env); err != nil {
			return "", err
		}
		return env, nil
	}

	// 3. Path detection — check if cwd is under $SOL_HOME/{world}.
	world, err := detectWorldFromCwd()
	if err == nil {
		return world, nil
	}

	// 4. Nothing worked.
	return "", fmt.Errorf("world required: specify with --world, set SOL_WORLD, or cd into a world directory")
}

// detectWorldFromCwd checks if the current working directory is inside a
// $SOL_HOME/{world}/ tree. If so, it extracts the world name (the first
// path segment after SOL_HOME) and validates it via RequireWorld.
func detectWorldFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	home := Home()

	// Clean both paths for reliable prefix comparison.
	cwd = filepath.Clean(cwd)
	home = filepath.Clean(home)

	// cwd must be strictly under home (not equal to it).
	if !strings.HasPrefix(cwd, home+string(filepath.Separator)) {
		return "", fmt.Errorf("cwd is not under SOL_HOME")
	}

	// Strip the home prefix + separator to get the relative path.
	rel := cwd[len(home)+1:]

	// The first segment is the candidate world name.
	world := rel
	if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
		world = rel[:i]
	}

	// Validate that this is actually an initialized world.
	if err := RequireWorld(world); err != nil {
		return "", err
	}

	return world, nil
}

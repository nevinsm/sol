package config

import (
	"os"
	"path/filepath"
	"testing"
)

// setupResolveEnv creates a temporary SOL_HOME with an initialized world.
// Returns the SOL_HOME path and a cleanup function.
func setupResolveEnv(t *testing.T, worlds ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	for _, w := range worlds {
		worldDir := filepath.Join(home, w)
		if err := os.MkdirAll(worldDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Create world.toml so RequireWorld passes.
		if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// --- ResolveActorIdentity ---

func TestResolveActorIdentity(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		solAgent  string
		solWorld  string
		expected  string
	}{
		{
			name:      "explicit flag value takes precedence",
			flagValue: "explicit-identity",
			solAgent:  "Nova",
			solWorld:  "sol-dev",
			expected:  "explicit-identity",
		},
		{
			name:      "world/agent from env vars when flag empty",
			flagValue: "",
			solAgent:  "Nova",
			solWorld:  "sol-dev",
			expected:  "sol-dev/Nova",
		},
		{
			name:      "autarch when env vars unset",
			flagValue: "",
			solAgent:  "",
			solWorld:  "",
			expected:  Autarch,
		},
		{
			name:      "autarch when only SOL_AGENT set",
			flagValue: "",
			solAgent:  "Nova",
			solWorld:  "",
			expected:  Autarch,
		},
		{
			name:      "autarch when only SOL_WORLD set",
			flagValue: "",
			solAgent:  "",
			solWorld:  "sol-dev",
			expected:  Autarch,
		},
		{
			name:      "explicit autarch flag returned as-is",
			flagValue: Autarch,
			solAgent:  "Nova",
			solWorld:  "sol-dev",
			expected:  Autarch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SOL_AGENT", tt.solAgent)
			t.Setenv("SOL_WORLD", tt.solWorld)
			got := ResolveActorIdentity(tt.flagValue)
			if got != tt.expected {
				t.Errorf("ResolveActorIdentity(%q) with SOL_AGENT=%q SOL_WORLD=%q = %q, want %q",
					tt.flagValue, tt.solAgent, tt.solWorld, got, tt.expected)
			}
		})
	}
}

func TestResolveWorld_ExplicitValue(t *testing.T) {
	setupResolveEnv(t, "myworld")
	t.Setenv("SOL_WORLD", "")

	got, err := ResolveWorld("myworld")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "myworld" {
		t.Errorf("got %q, want %q", got, "myworld")
	}
}

func TestResolveWorld_ExplicitInvalid(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "")

	_, err := ResolveWorld("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent world, got nil")
	}
}

func TestResolveWorld_ExplicitBadName(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "")

	_, err := ResolveWorld("../evil")
	if err == nil {
		t.Fatal("expected validation error for bad name, got nil")
	}
}

func TestResolveWorld_EnvVar(t *testing.T) {
	setupResolveEnv(t, "envworld")
	t.Setenv("SOL_WORLD", "envworld")

	got, err := ResolveWorld("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "envworld" {
		t.Errorf("got %q, want %q", got, "envworld")
	}
}

func TestResolveWorld_EnvVarInvalid(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "nonexistent")

	_, err := ResolveWorld("")
	if err == nil {
		t.Fatal("expected error for invalid SOL_WORLD, got nil")
	}
}

func TestResolveWorld_ExplicitOverridesEnv(t *testing.T) {
	setupResolveEnv(t, "explicit", "fromenv")
	t.Setenv("SOL_WORLD", "fromenv")

	got, err := ResolveWorld("explicit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "explicit" {
		t.Errorf("got %q, want %q (explicit should override env)", got, "explicit")
	}
}

func TestResolveWorld_PathDetection(t *testing.T) {
	home := setupResolveEnv(t, "detected")
	t.Setenv("SOL_WORLD", "")

	// cd into a subdirectory of the world.
	subdir := filepath.Join(home, "detected", "outposts", "Nova", "worktree")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	origDir, _ := os.Getwd()
	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	got, err := ResolveWorld("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "detected" {
		t.Errorf("got %q, want %q", got, "detected")
	}
}

func TestResolveWorld_PathDetectionWorldRoot(t *testing.T) {
	home := setupResolveEnv(t, "atroot")
	t.Setenv("SOL_WORLD", "")

	// cd into the world directory itself (not a subdirectory).
	worldDir := filepath.Join(home, "atroot")
	origDir, _ := os.Getwd()
	if err := os.Chdir(worldDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	got, err := ResolveWorld("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "atroot" {
		t.Errorf("got %q, want %q", got, "atroot")
	}
}

func TestResolveWorld_PathDetectionNotAWorld(t *testing.T) {
	home := setupResolveEnv(t) // no worlds initialized
	t.Setenv("SOL_WORLD", "")

	// Create a directory under SOL_HOME that is NOT a valid world.
	notWorld := filepath.Join(home, "fakedir", "sub")
	if err := os.MkdirAll(notWorld, 0o755); err != nil {
		t.Fatal(err)
	}
	origDir, _ := os.Getwd()
	if err := os.Chdir(notWorld); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := ResolveWorld("")
	if err == nil {
		t.Fatal("expected error for non-world directory under SOL_HOME, got nil")
	}
}

func TestResolveWorld_CwdOutsideSolHome(t *testing.T) {
	setupResolveEnv(t, "myworld")
	t.Setenv("SOL_WORLD", "")

	// cd to a directory outside SOL_HOME.
	outside := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := ResolveWorld("")
	if err == nil {
		t.Fatal("expected error when cwd is outside SOL_HOME, got nil")
	}
	if got := err.Error(); got != "--world is required (or set SOL_WORLD, or run from inside a world directory)" {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestResolveWorld_NoneAvailable(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "")

	// cwd is not under SOL_HOME, no env, no explicit.
	outside := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := ResolveWorld("")
	if err == nil {
		t.Fatal("expected error when no world can be determined, got nil")
	}
}

func TestResolveWorld_CwdIsSolHome(t *testing.T) {
	home := setupResolveEnv(t, "myworld")
	t.Setenv("SOL_WORLD", "")

	// cd to SOL_HOME itself — should NOT detect a world.
	origDir, _ := os.Getwd()
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := ResolveWorld("")
	if err == nil {
		t.Fatal("expected error when cwd is SOL_HOME itself, got nil")
	}
}

func TestResolveWorld_ReservedName(t *testing.T) {
	home := setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "")

	// Create a directory named "store" (reserved) with a world.toml.
	storeDir := filepath.Join(home, "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "world.toml"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveWorld("store")
	if err == nil {
		t.Fatal("expected error for reserved world name, got nil")
	}
}

// --- ResolveWorldHint ---

func TestResolveWorldHint_ExplicitValue(t *testing.T) {
	setupResolveEnv(t) // no worlds initialized — hint must not validate existence
	t.Setenv("SOL_WORLD", "")

	if got := ResolveWorldHint("nonexistent-but-fine"); got != "nonexistent-but-fine" {
		t.Errorf("got %q, want %q (hint should not validate world existence)", got, "nonexistent-but-fine")
	}
}

func TestResolveWorldHint_EnvVar(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "envhint")

	if got := ResolveWorldHint(""); got != "envhint" {
		t.Errorf("got %q, want %q", got, "envhint")
	}
}

func TestResolveWorldHint_ExplicitOverridesEnv(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "fromenv")

	if got := ResolveWorldHint("explicit"); got != "explicit" {
		t.Errorf("got %q, want %q (explicit should override env)", got, "explicit")
	}
}

// TestResolveWorldHint_PathDetection is the regression test for confirmed
// fix #9 (sol-8d4afcfa0390dd73): mail send used to resolve world manually
// (flag -> SOL_WORLD only), skipping the cwd-detection fallback every other
// world-scoped command gets via ResolveWorld. ResolveWorldHint restores that
// fallback while intentionally NOT validating that the detected world
// exists (mail send only uses it to canonicalize a recipient, and an
// unresolvable hint is not itself an error there).
func TestResolveWorldHint_PathDetection(t *testing.T) {
	home := setupResolveEnv(t, "detected")
	t.Setenv("SOL_WORLD", "")

	subdir := filepath.Join(home, "detected", "outposts", "Nova", "worktree")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	origDir, _ := os.Getwd()
	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if got := ResolveWorldHint(""); got != "detected" {
		t.Errorf("got %q, want %q", got, "detected")
	}
}

func TestResolveWorldHint_NoneAvailable(t *testing.T) {
	setupResolveEnv(t)
	t.Setenv("SOL_WORLD", "")

	outside := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if got := ResolveWorldHint(""); got != "" {
		t.Errorf("got %q, want empty string when nothing resolves", got)
	}
}

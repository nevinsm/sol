package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
)

// run executes a command and fails the test on error, returning trimmed
// combined output. Mirrors the helper used by internal/forge's real-git tests.
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %s: %v", name, strings.Join(args, " "),
			strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out))
}

// --- classifyRemoteGitFailure (pure) ---

// TestClassifyRemoteGitFailure covers the git error strings this check needs
// to distinguish: auth-failure vs. unreachable. Auth-failure inputs mirror
// git's real wording across HTTPS credential helpers, SSH key auth, and the
// GIT_TERMINAL_PROMPT=0 "terminal prompts disabled" case (the ADR-0040
// scenario: an expired long-lived credential with no one watching for a
// prompt). Everything else is treated as a connectivity problem.
func TestClassifyRemoteGitFailure(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "https authentication failed",
			output: "remote: Invalid username or password.\nfatal: Authentication failed for 'https://example.invalid/repo.git/'",
			want:   "auth-failure",
		},
		{
			name:   "terminal prompts disabled (GIT_TERMINAL_PROMPT=0)",
			output: "fatal: could not read Username for 'https://example.invalid': terminal prompts disabled",
			want:   "auth-failure",
		},
		{
			name:   "ssh publickey denied",
			output: "git@example.invalid: Permission denied (publickey).\nfatal: Could not read from remote repository.",
			want:   "auth-failure",
		},
		{
			name:   "repository not found (private repo, no credentials)",
			output: "remote: Repository not found.\nfatal: repository 'https://example.invalid/private.git/' not found",
			want:   "auth-failure",
		},
		{
			name:   "dns resolution failure",
			output: "fatal: unable to access 'https://example.invalid/repo.git/': Could not resolve host: example.invalid",
			want:   "unreachable",
		},
		{
			name:   "connection refused",
			output: "ssh: connect to host example.invalid port 22: Connection refused",
			want:   "unreachable",
		},
		{
			name:   "connection timed out",
			output: "fatal: unable to access 'https://example.invalid/repo.git/': Failed to connect to example.invalid port 443: Connection timed out",
			want:   "unreachable",
		},
		{
			name:   "not a git repository (local path remote misconfigured)",
			output: "fatal: '/nonexistent/path/repo.git' does not appear to be a git repository",
			want:   "unreachable",
		},
		{
			name:   "empty output",
			output: "",
			want:   "unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyRemoteGitFailure(tt.output); got != tt.want {
				t.Errorf("classifyRemoteGitFailure(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

// --- CheckRemoteReachability (real git) ---

// setupDoctorRemoteRepo creates a bare "origin" repo and a managed-repo clone
// at $SOL_HOME/{world}/repo pointed at it, matching config.RepoPath layout.
// Returns the managed repo path.
func setupDoctorRemoteRepo(t *testing.T, world string) string {
	t.Helper()

	bareDir := filepath.Join(t.TempDir(), "origin.git")
	run(t, "git", "init", "--bare", "-b", "main", bareDir)

	// Seed the bare repo with one commit via a throwaway working clone.
	seed := t.TempDir()
	run(t, "git", "init", "-b", "main", seed)
	run(t, "git", "-C", seed, "config", "user.email", "test@example.com")
	run(t, "git", "-C", seed, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	run(t, "git", "-C", seed, "add", ".")
	run(t, "git", "-C", seed, "commit", "-m", "seed")
	run(t, "git", "-C", seed, "push", bareDir, "HEAD:refs/heads/main")

	repoPath := config.RepoPath(world)
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run(t, "git", "clone", bareDir, repoPath)
	return repoPath
}

// TestCheckRemoteReachability_OK verifies a reachable origin passes.
func TestCheckRemoteReachability_OK(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	setupDoctorRemoteRepo(t, "haven")

	results := CheckRemoteReachability([]string{"haven"})
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	r := results[0]
	if !r.Passed {
		t.Errorf("Passed = false, want true; message: %s", r.Message)
	}
	if !strings.Contains(r.Message, "reachable") {
		t.Errorf("Message = %q, want to contain %q", r.Message, "reachable")
	}
	if r.Fix != "" {
		t.Errorf("Fix = %q, want empty on a passing check", r.Fix)
	}
}

// TestCheckRemoteReachability_Unreachable verifies a remote pointed at a
// nonexistent local path is classified as unreachable, not auth-failure, and
// still points at docs/credentials.md in case it turns out to be a
// credential issue after all.
func TestCheckRemoteReachability_Unreachable(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	repoPath := setupDoctorRemoteRepo(t, "haven")

	run(t, "git", "-C", repoPath, "remote", "set-url", "origin", "/nonexistent/path/repo.git")

	results := CheckRemoteReachability([]string{"haven"})
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	r := results[0]
	if r.Passed {
		t.Fatal("Passed = true, want false (unreachable remote)")
	}
	if !strings.Contains(r.Message, "unreachable") {
		t.Errorf("Message = %q, want to contain %q", r.Message, "unreachable")
	}
	if !strings.Contains(r.Fix, "docs/credentials.md") {
		t.Errorf("Fix = %q, want to reference docs/credentials.md", r.Fix)
	}
	if r.Remediate != nil {
		t.Error("Remediate should be nil — credentials are operator-managed, no --fix action")
	}
}

// TestCheckRemoteReachability_SkipsWorldsWithoutManagedRepo verifies that a
// world with no repo directory yet (not synced) produces no result rather
// than a spurious failure.
func TestCheckRemoteReachability_SkipsWorldsWithoutManagedRepo(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	results := CheckRemoteReachability([]string{"never-synced"})
	if len(results) != 0 {
		t.Errorf("results = %d, want 0 for a world with no managed repo", len(results))
	}
}

// TestCheckRemoteReachability_MultipleWorlds verifies each world in the list
// gets its own independent result, keyed by name.
func TestCheckRemoteReachability_MultipleWorlds(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	setupDoctorRemoteRepo(t, "alpha")
	setupDoctorRemoteRepo(t, "beta")

	results := CheckRemoteReachability([]string{"alpha", "beta"})
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("%s: Passed = false, want true; message: %s", r.Name, r.Message)
		}
	}
	if results[0].Name == results[1].Name {
		t.Errorf("expected distinct check names, got %q twice", results[0].Name)
	}
}

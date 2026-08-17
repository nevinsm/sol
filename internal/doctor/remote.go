package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
)

// remoteReachabilityTimeout bounds each `git ls-remote` invocation so an
// unreachable or hanging remote cannot stall `sol doctor`.
const remoteReachabilityTimeout = 10 * time.Second

// CheckRemoteReachability verifies, for each world with a managed repo, that
// git can reach that repo's "origin" remote — the same operation the forge
// patrol depends on for branch sweeps and merge verification (Task A/B in
// sol-0ec6b898c083264f). Doctor gives an on-demand, synchronous answer to
// "can this world reach its remote right now" instead of waiting for forge's
// next patrol and heartbeat write.
//
// Each check runs `git ls-remote origin` against the world's managed repo
// ($SOL_HOME/{world}/repo) with GIT_TERMINAL_PROMPT=0 and a short timeout, so
// an unauthenticated remote fails fast with a message instead of hanging on
// an interactive credential prompt.
//
// GIT_TERMINAL_PROMPT=0 is set on the subprocess's own Env slice, not via
// os.Setenv. os.Setenv mutates the whole process's environment, which is
// unsafe here: a concurrent forge patrol (or another doctor check) could be
// reading or writing the same process-wide env at the same time. Setting it
// on cmd.Env only affects this one subprocess and requires no synchronization
// with the rest of the process.
//
// Worlds with no managed repo yet (not synced) are skipped — there is
// nothing to check.
//
// No --fix action: credentials are operator-managed by design (see
// docs/credentials.md) — sol does not store, rotate, or inject them.
func CheckRemoteReachability(worlds []string) []CheckResult {
	var results []CheckResult
	for _, world := range worlds {
		repoPath := config.RepoPath(world)
		info, err := os.Stat(repoPath)
		if err != nil || !info.IsDir() {
			continue
		}
		results = append(results, checkWorldRemoteReachability(world, repoPath))
	}
	return results
}

// checkWorldRemoteReachability runs the ls-remote probe for a single world's
// managed repo and classifies the outcome as ok / auth-failure / unreachable.
func checkWorldRemoteReachability(world, repoPath string) CheckResult {
	name := fmt.Sprintf("remote_reachability:%s", world)

	ctx, cancel := context.WithTimeout(context.Background(), remoteReachabilityTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-remote", "origin")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()

	if err == nil {
		return CheckResult{
			Name:    name,
			Passed:  true,
			Message: fmt.Sprintf("world %q: origin reachable", world),
		}
	}

	output := strings.TrimSpace(string(out))
	if classifyRemoteGitFailure(output) == "auth-failure" {
		return CheckResult{
			Name:   name,
			Passed: false,
			Message: fmt.Sprintf("world %q: git ls-remote origin failed (auth-failure): %s",
				world, firstLine(output)),
			Fix: "Credentials are operator-managed — see docs/credentials.md to configure a long-lived credential for this world's runtime.",
		}
	}
	return CheckResult{
		Name:   name,
		Passed: false,
		Message: fmt.Sprintf("world %q: git ls-remote origin failed (unreachable): %s",
			world, firstLine(output)),
		Fix: "Check network connectivity/DNS to the remote. If this turns out to be a credential problem, see docs/credentials.md.",
	}
}

// remoteAuthFailureMarkers are substrings (matched case-insensitively) that
// git prints on auth-related remote failures across the common transports
// (HTTPS credential helpers, SSH key auth, and the terminal-prompt-disabled
// case GIT_TERMINAL_PROMPT=0 produces when a prompt would otherwise appear).
//
// "repository not found" is included because for private repos without
// credentials, git (particularly GitHub) reports "not found" rather than a
// permission error — from the operator's perspective this is a credential
// problem, not a dead remote.
var remoteAuthFailureMarkers = []string{
	"authentication failed",
	"could not read username",
	"could not read password",
	"terminal prompts disabled",
	"permission denied (publickey)",
	"permission denied, please try again",
	"invalid username or password",
	"repository not found",
	"access denied",
}

// classifyRemoteGitFailure inspects the combined output of a failed
// `git ls-remote` invocation and classifies it as "auth-failure" or
// "unreachable". Anything not recognised as an auth failure is classified as
// "unreachable" — the common remaining causes (DNS resolution, connection
// refused/timed out, TLS failures, a deleted/moved remote, or the context
// deadline expiring) are all connectivity issues from the operator's point
// of view.
func classifyRemoteGitFailure(output string) string {
	lower := strings.ToLower(output)
	for _, marker := range remoteAuthFailureMarkers {
		if strings.Contains(lower, marker) {
			return "auth-failure"
		}
	}
	return "unreachable"
}

// firstLine returns the first non-empty line of s, or s itself if it has no
// newlines. Keeps doctor's one-line-per-check output readable when git's
// error output spans multiple lines.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return s
}

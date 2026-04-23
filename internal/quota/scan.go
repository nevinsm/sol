package quota

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/session"
)

// ScanResult describes the outcome of scanning a single session.
type ScanResult struct {
	Session string `json:"session"`
	Account string `json:"account"`
	Limited bool   `json:"limited"`
}

// ScanWorld scans all running sessions in a world for rate limit patterns.
// It captures pane output, matches against known error patterns, and updates
// the quota state file. Returns the scan results for each session checked.
func ScanWorld(world string) ([]ScanResult, error) {
	mgr := session.New()

	// List sessions before acquiring the lock to minimise lock hold time.
	sessions, err := mgr.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	// Load world config once so each session can resolve its runtime by role.
	// A failure here is non-fatal: fall back to a zero config (which yields
	// the default "claude" runtime) so scanning still proceeds.
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		fmt.Fprintf(os.Stderr, "quota: failed to load world config for %s: %v\n", world, err)
	}

	lock, state, err := AcquireLock()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire quota lock: %w", err)
	}
	defer lock.Release()

	// Expire any limits that have passed.
	state.ExpireLimits()

	var results []ScanResult

	for _, sess := range sessions {
		if sess.World != world || !sess.Alive {
			continue
		}

		// Capture bottom 20 lines of pane output.
		output, err := mgr.Capture(sess.Name, 20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "quota: failed to capture %s: %v\n", sess.Name, err)
			continue
		}

		// Resolve which account this session is using.
		accountHandle := resolveSessionAccount(world, sess)

		// Resolve the runtime for this session's role so the correct
		// rate-limit patterns are matched. Falls back to "claude" inside
		// ResolveRuntime when nothing is configured.
		runtime := worldCfg.ResolveRuntime(sess.Role)

		limited, resetsAt := DetectRateLimitForRuntime(output, runtime)

		result := ScanResult{
			Session: sess.Name,
			Account: accountHandle,
			Limited: limited,
		}
		results = append(results, result)

		applyScanResult(state, accountHandle, limited, resetsAt)
	}

	if err := Save(state); err != nil {
		return results, fmt.Errorf("failed to save quota state: %w", err)
	}

	return results, nil
}

// applyScanResult updates account state for a single scanned session.
//
// The caller must hold the quota lock. The account is left untouched when
// handle is empty (e.g. could not be resolved).
//
// When limited is true, the account is unconditionally marked Limited.
// When limited is false, the account is only transitioned to Available if it
// is not currently in a status that another caller owns: Limited (set by a
// concurrent scan) or Assigned (set by Rotate). Without this guard, a scan
// of an in-flight session would clobber an Assigned account, allowing the
// next Rotate to hand the same credentials to a second agent.
func applyScanResult(state *State, handle string, limited bool, resetsAt *time.Time) {
	if handle == "" {
		return
	}
	if limited {
		state.MarkLimited(handle, resetsAt)
		return
	}
	existing := state.Accounts[handle]
	if existing == nil || (existing.Status != Limited && existing.Status != Assigned) {
		state.MarkAvailable(handle)
		state.MarkLastUsed(handle)
	}
}

// resolveSessionAccount determines which account a session is using.
// Delegates to ResolveCurrentAccount after extracting agent name and role
// from the session info.
func resolveSessionAccount(world string, sess session.SessionInfo) string {
	// Extract agent name from session name (format: sol-{world}-{agentName}).
	agentName := extractAgentName(sess.Name, world)
	if agentName == "" {
		return ""
	}

	role := sess.Role
	if role == "" {
		role = "outpost"
	}

	return ResolveCurrentAccount(world, agentName, role)
}


// extractAgentName extracts the agent name from a session name.
// Session names follow the format: sol-{world}-{agentName}.
func extractAgentName(sessionName, world string) string {
	prefix := "sol-" + world + "-"
	if !strings.HasPrefix(sessionName, prefix) {
		return ""
	}
	return sessionName[len(prefix):]
}

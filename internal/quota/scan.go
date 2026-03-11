package quota

import (
	"fmt"
	"os"
	"path/filepath"
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

	sessions, err := mgr.List()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	state, err := Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load quota state: %w", err)
	}

	// Expire any cooldowns that have passed.
	state.ExpireCooldowns()

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

		limited, resetsAt := DetectRateLimit(output)

		result := ScanResult{
			Session: sess.Name,
			Account: accountHandle,
			Limited: limited,
		}
		results = append(results, result)

		if accountHandle == "" {
			continue
		}

		if limited {
			state.MarkLimited(accountHandle, resetsAt)
		} else {
			// Only mark available if not already limited by another session.
			existing := state.Accounts[accountHandle]
			if existing == nil || existing.Status != Limited {
				state.MarkAvailable(accountHandle)
				state.TouchLastUsed(accountHandle)
			}
		}
	}

	if err := Save(state); err != nil {
		return results, fmt.Errorf("failed to save quota state: %w", err)
	}

	return results, nil
}

// resolveSessionAccount determines which account a session is using.
// First checks the .account metadata file (broker-managed), then falls
// back to reading the .credentials.json symlink (legacy).
func resolveSessionAccount(world string, sess session.SessionInfo) string {
	worldDir := config.WorldDir(world)

	// Extract agent name from session name (format: sol-{world}-{agentName}).
	agentName := extractAgentName(sess.Name, world)
	if agentName == "" {
		return ""
	}

	role := sess.Role
	if role == "" {
		role = "outpost"
	}

	configDir := config.ClaudeConfigDir(worldDir, role, agentName)

	// Prefer .account file (broker-managed).
	if handle := readAccountFile(configDir); handle != "" {
		return handle
	}

	// Fallback: read symlink target (legacy).
	credsPath := filepath.Join(configDir, ".credentials.json")
	target, err := os.Readlink(credsPath)
	if err != nil {
		return ""
	}

	// Match against account directories: $SOL_HOME/.accounts/{handle}/.credentials.json
	accountsDir := config.AccountsDir()
	rel, err := filepath.Rel(accountsDir, target)
	if err != nil {
		return ""
	}

	// rel should be "{handle}/.credentials.json"
	parts := strings.SplitN(rel, string(filepath.Separator), 2)
	if len(parts) != 2 || parts[1] != ".credentials.json" {
		return ""
	}

	// Validate the handle doesn't look like a path traversal.
	handle := parts[0]
	if handle == "" || handle == "." || handle == ".." || strings.Contains(handle, "/") {
		return ""
	}

	return handle
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

// AvailableAccounts returns accounts that are currently available (not rate-limited).
func AvailableAccounts(state *State) []string {
	now := time.Now().UTC()
	var available []string
	for handle, acct := range state.Accounts {
		switch {
		case acct.Status == Available:
			available = append(available, handle)
		case acct.Status == Limited && acct.ResetsAt != nil && now.After(*acct.ResetsAt):
			available = append(available, handle)
		}
	}
	return available
}

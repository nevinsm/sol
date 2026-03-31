package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/protocol"
)

// DefaultPromptPrefix is the Claude Code prompt character used for idle detection.
// Claude Code renders "❯ " (U+276F + space) when waiting for input.
const DefaultPromptPrefix = "❯ "

// ErrIdleTimeout is returned by WaitForIdle when the timeout expires
// without detecting an idle prompt.
var ErrIdleTimeout = errors.New("session not idle before timeout")

// sessionNudgeLocks serializes nudges to the same session.
// Uses channel-based semaphores instead of sync.Mutex to support
// timed lock acquisition — preventing permanent lockout if a nudge hangs.
var sessionNudgeLocks sync.Map // map[string]chan struct{}

// nudgeLockTimeout is how long to wait to acquire the per-session nudge lock.
const nudgeLockTimeout = 30 * time.Second

// startupVerifyDelay is the time to wait after session creation before checking
// if the process survived startup. Short enough to not slow down normal startup,
// long enough to catch missing binaries, bad flags, and permission errors.
const startupVerifyDelay = 1500 * time.Millisecond

// sendKeysChunkSize is the maximum bytes per tmux send-keys call.
// Messages larger than this are split into chunks with inter-chunk delays.
const sendKeysChunkSize = 512

// Manager wraps tmux to provide process containers for AI agents.
// No fields needed — all state lives in tmux server and .runtime/sessions/.
type Manager struct{}

// New returns a new session Manager.
func New() *Manager {
	return &Manager{}
}

// SessionInfo describes a registered session with live status.
type SessionInfo struct {
	Name      string    `json:"name"`
	PID       int       `json:"pid"`
	Role      string    `json:"role"`
	World     string    `json:"world"`
	WorkDir   string    `json:"workdir"`
	StartedAt time.Time `json:"started_at"`
	CreatedAt time.Time `json:"created_at"` // tmux session creation time (#{session_created}); zero if unavailable
	Alive     bool      `json:"alive"`
}

// HealthStatus represents the health of a session.
type HealthStatus int

const (
	Healthy   HealthStatus = iota // exit 0: session alive, recent activity
	Dead                          // exit 1: tmux session doesn't exist
	AgentDead                     // exit 2: session exists but process exited
	Hung                          // exit 2: session exists but no output change
)

func (h HealthStatus) String() string {
	switch h {
	case Healthy:
		return "healthy"
	case Dead:
		return "dead"
	case AgentDead:
		return "agent-dead"
	case Hung:
		return "hung"
	default:
		return fmt.Sprintf("unknown(%d)", int(h))
	}
}

// ExitCode returns the process exit code for this health status.
// Hung maps to exit code 2 (same as AgentDead) per the 0/1/2 convention.
func (h HealthStatus) ExitCode() int {
	if h == Hung {
		return 2
	}
	return int(h)
}

// sessionsDir returns the path to $SOL_HOME/.runtime/sessions/.
func sessionsDir() string {
	return filepath.Join(config.RuntimeDir(), "sessions")
}

// metadataPath returns the path to the metadata file for a session.
func metadataPath(name string) string {
	return filepath.Join(sessionsDir(), name+".json")
}

// captureHashPath returns the path to the last-capture-hash file for a session.
func captureHashPath(name string) string {
	return filepath.Join(sessionsDir(), name+".last-capture-hash")
}

// tmuxExactTarget returns a tmux target string that forces exact session matching.
// Without the "=" prefix, tmux uses prefix matching which can target the wrong session.
// The trailing ":" selects the session's current window/pane, which is required for
// pane-targeting commands (send-keys, capture-pane) to resolve the "=" prefix correctly.
func tmuxExactTarget(name string) string {
	return "=" + name + ":"
}

// tmuxCmd creates a tmux command with a 10-second timeout.
// The caller MUST call the returned cancel function after the command
// completes to release the context resources.
func tmuxCmd(args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	return cmd, cancel
}

// prependEnv wraps a command string with export statements for the given
// environment variables. This ensures the spawned process receives the env
// vars immediately, regardless of tmux version or respawn behavior.
// The set-environment calls still happen separately for future pane inheritance.
func prependEnv(cmd string, env map[string]string) string {
	if len(env) == 0 {
		return cmd
	}
	var exports []string
	for k, v := range env {
		exports = append(exports, k+"="+config.ShellQuote(v))
	}
	sort.Strings(exports) // deterministic order for testing
	return "export " + strings.Join(exports, " ") + " && " + cmd
}

// sessionMeta is the JSON structure written to the metadata file.
type sessionMeta struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	World     string    `json:"world"`
	WorkDir   string    `json:"workdir"`
	StartedAt time.Time `json:"started_at"`
}

// captureHash is the JSON structure for the last-capture-hash file.
type captureHash struct {
	Hash      string    `json:"hash"`
	Timestamp time.Time `json:"timestamp"`
}

// writeHashFile writes the capture hash to disk. Errors are logged to
// stderr but not returned — a hash write failure degrades future health
// checks but does not affect the current check's result.
func writeHashFile(path string, ch captureHash) {
	j, err := json.Marshal(ch)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to marshal capture hash: %v\n", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to create hash directory: %v\n", err)
		return
	}
	if err := os.WriteFile(path, j, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to write hash file %s: %v\n", path, err)
	}
}

// Start creates a tmux session with the given name and runs cmd inside it.
// Writes session metadata to $SOL_HOME/.runtime/sessions/{name}.json.
// Env vars are set in the tmux session environment.
// Returns error if session already exists.
func (m *Manager) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	if m.Exists(name) {
		return fmt.Errorf("session %q already exists", name)
	}

	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}

	// Pre-trust the working directory so Claude Code doesn't block on
	// an interactive trust prompt in automated sessions.
	if err := protocol.TrustDirectory(workdir); err != nil {
		fmt.Fprintf(os.Stderr, "session: failed to pre-trust directory %s: %v\n", workdir, err)
	}

	// Create the tmux session. Prepend env vars to the command so the
	// initial process receives them immediately (e.g., CLAUDE_CONFIG_DIR).
	// The set-environment calls below still happen for future pane inheritance.
	wrappedCmd := prependEnv(cmd, env)
	newSess, newSessCancel := tmuxCmd("new-session", "-d", "-s", name, "-c", workdir, wrappedCmd)
	defer newSessCancel()
	if out, err := newSess.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	// For human-supervised roles (envoy), keep the pane alive after
	// the process exits so we can inspect the exit code and any error output.
	// Without this, tmux destroys the session immediately and all crash
	// evidence is lost. Not applied to regular agents because the prefect
	// uses Exists() to detect dead sessions for auto-respawn.
	if role == "envoy" {
		remain, remainCancel := tmuxCmd("set-option", "-t", tmuxExactTarget(name), "remain-on-exit", "on")
		if out, err := remain.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "session: failed to set remain-on-exit for %s: %s\n", name, strings.TrimSpace(string(out)))
		}
		remainCancel()
	}

	// Set environment variables
	for k, v := range env {
		setEnv, setEnvCancel := tmuxCmd("set-environment", "-t", tmuxExactTarget(name), k, v)
		if out, err := setEnv.CombinedOutput(); err != nil {
			setEnvCancel()
			// Best-effort cleanup on env failure
			_ = m.Stop(name, true)
			return fmt.Errorf("failed to set env %q in session %q: %s: %w", k, name, strings.TrimSpace(string(out)), err)
		}
		setEnvCancel()
	}

	// Write metadata file
	meta := sessionMeta{
		Name:      name,
		Role:      role,
		World:     world,
		WorkDir:   workdir,
		StartedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = m.Stop(name, true)
		return fmt.Errorf("failed to marshal session metadata: %w", err)
	}
	if err := fileutil.AtomicWrite(metadataPath(name), data, 0o644); err != nil {
		_ = m.Stop(name, true)
		return fmt.Errorf("failed to write session metadata for %q: %w", name, err)
	}

	// Verify the process survived startup. Catches missing binaries,
	// bad flags, permission errors, and immediate crashes.
	time.Sleep(startupVerifyDelay)
	// Check if the session still exists — tmux destroys it when the process
	// exits (unless remain-on-exit is set for envoy roles).
	if !m.Exists(name) {
		_ = os.Remove(metadataPath(name))
		_ = os.Remove(captureHashPath(name))
		return fmt.Errorf("session %q: process died during startup", name)
	}
	// Session exists — check if the pane process is dead (remain-on-exit case).
	target := tmuxExactTarget(name)
	paneCmd, paneCmdCancel := tmuxCmd("list-panes", "-t", target, "-F", "#{pane_dead}")
	defer paneCmdCancel()
	if out, err := paneCmd.Output(); err == nil {
		if strings.TrimSpace(string(out)) == "1" {
			_ = m.Stop(name, true)
			return fmt.Errorf("session %q: process died during startup", name)
		}
	}

	return nil
}

// Stop kills a tmux session. If force=false, sends C-c first and waits 5s
// before killing. If force=true, kills immediately. Removes session metadata file.
func (m *Manager) Stop(name string, force bool) error {
	if !m.Exists(name) {
		// Session doesn't exist in tmux, but clean up any stale metadata.
		_ = os.Remove(metadataPath(name))
		_ = os.Remove(captureHashPath(name))
		return fmt.Errorf("session %q not found", name)
	}

	if !force {
		// Send C-c for graceful shutdown.
		interrupt, interruptCancel := tmuxCmd("send-keys", "-t", tmuxExactTarget(name), "C-c")
		_ = interrupt.Run()
		interruptCancel()
		// Wait up to 5 seconds for the session to exit.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !m.Exists(name) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Kill the session if it still exists.
	var killErr error
	if m.Exists(name) {
		kill, killCancel := tmuxCmd("kill-session", "-t", tmuxExactTarget(name))
		defer killCancel()
		if out, err := kill.CombinedOutput(); err != nil {
			killErr = fmt.Errorf("failed to kill session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
	}

	// Always remove metadata — even if kill failed (session may already be dead).
	_ = os.Remove(metadataPath(name))
	_ = os.Remove(captureHashPath(name))

	return killErr
}

// List returns all sessions with metadata from .runtime/sessions/*.json,
// enriched with live status from tmux.
func (m *Manager) List() ([]SessionInfo, error) {
	dir := sessionsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	var sessions []SessionInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "session: skipping unreadable metadata %s: %v\n", entry.Name(), err)
			continue
		}

		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			fmt.Fprintf(os.Stderr, "session: skipping corrupt metadata %s: %v\n", entry.Name(), err)
			continue
		}

		info := SessionInfo{
			Name:      meta.Name,
			Role:      meta.Role,
			World:     meta.World,
			WorkDir:   meta.WorkDir,
			StartedAt: meta.StartedAt,
			Alive:     m.Exists(meta.Name),
		}

		// Get tmux server PID and session creation time if session is alive
		if info.Alive {
			pid, pidCancel := tmuxCmd("display-message", "-t", tmuxExactTarget(meta.Name), "-p", "#{pid}")
			if out, err := pid.Output(); err == nil {
				if p, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
					info.PID = p
				}
			}
			pidCancel()

			created, createdCancel := tmuxCmd("display-message", "-t", tmuxExactTarget(meta.Name), "-p", "#{session_created}")
			if out, err := created.Output(); err == nil {
				if ts, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
					info.CreatedAt = time.Unix(ts, 0)
				}
			}
			createdCancel()
		}

		sessions = append(sessions, info)
	}

	return sessions, nil
}

// Health checks session health using three signals:
// 1. Does the tmux session exist?
// 2. Is there a running process in the session?
// 3. Has the pane content changed since last check?
func (m *Manager) Health(name string, maxInactivity time.Duration) (HealthStatus, error) {
	// Check if session exists
	if !m.Exists(name) {
		return Dead, nil
	}

	// Check if the pane process is dead
	paneCmd, paneCmdCancel := tmuxCmd("list-panes", "-t", tmuxExactTarget(name), "-F", "#{pane_dead}")
	defer paneCmdCancel()
	if out, err := paneCmd.Output(); err == nil {
		if strings.TrimSpace(string(out)) == "1" {
			return AgentDead, nil
		}
	}

	// Capture and hash pane content
	content, err := m.Capture(name, 50)
	if err != nil {
		return Healthy, nil // Can't capture, but session exists — assume healthy
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))

	hashFile := captureHashPath(name)
	data, err := os.ReadFile(hashFile)
	if err != nil {
		// No previous hash — write current and return healthy
		writeHashFile(hashFile, captureHash{Hash: hash, Timestamp: time.Now().UTC()})
		return Healthy, nil
	}

	var prev captureHash
	if err := json.Unmarshal(data, &prev); err != nil {
		// Corrupted file — overwrite and return healthy
		writeHashFile(hashFile, captureHash{Hash: hash, Timestamp: time.Now().UTC()})
		return Healthy, nil
	}

	if prev.Hash != hash {
		// Content changed — update and return healthy
		writeHashFile(hashFile, captureHash{Hash: hash, Timestamp: time.Now().UTC()})
		return Healthy, nil
	}

	// Content unchanged — check if past maxInactivity
	if time.Since(prev.Timestamp) > maxInactivity {
		return Hung, nil
	}

	return Healthy, nil
}

// Capture returns the last N lines of visible output from the session's pane.
func (m *Manager) Capture(name string, lines int) (string, error) {
	if !m.Exists(name) {
		return "", fmt.Errorf("session %q not found", name)
	}

	capCmd, capCancel := tmuxCmd("capture-pane", "-t", tmuxExactTarget(name), "-p", "-S", fmt.Sprintf("-%d", lines))
	defer capCancel()
	out, err := capCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to capture pane for session %q: %w", name, err)
	}

	return string(out), nil
}

// Attach attaches the current terminal to the tmux session (replaces process).
// This calls syscall.Exec — it does not return on success.
func (m *Manager) Attach(name string) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found in PATH: %w", err)
	}

	return syscall.Exec(tmuxPath, []string{"tmux", "attach-session", "-t", tmuxExactTarget(name)}, os.Environ())
}

// Inject sends text to the session's active pane using tmux send-keys in
// literal mode, then presses Enter to submit it.
// If submit is false, the text is staged without pressing Enter.
//
// Deprecated: Use NudgeSession for reliable delivery to Claude Code sessions.
// Inject does not handle copy mode, vim mode, control character sanitization,
// detached session wakeup, or per-session serialization. Retained for callers
// (e.g., sentinel) that intentionally bypass those safeguards.
func (m *Manager) Inject(name string, text string, submit bool) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	sendCmd, sendCancel := tmuxCmd("send-keys", "-t", tmuxExactTarget(name), "-l", "--", text)
	defer sendCancel()
	if out, err := sendCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to inject into session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	if submit {
		if err := m.SendKeys(name, "Enter"); err != nil {
			return fmt.Errorf("failed to submit injected text in session %q: %w", name, err)
		}
	}

	return nil
}

// SendKeys sends keys to the session's active pane using tmux send-keys in
// non-literal mode. This interprets special key names like "Enter", "C-c", etc.
// Distinct from Inject which uses literal mode (-l).
func (m *Manager) SendKeys(name string, keys string) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	sendCmd, sendCancel := tmuxCmd("send-keys", "-t", tmuxExactTarget(name), keys)
	defer sendCancel()
	if out, err := sendCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to send keys to session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	return nil
}

// Cycle atomically replaces the running process in a tmux session using
// respawn-pane -k. The old process is killed and the new command starts in
// its place without destroying the session. This is safe for self-handoff —
// the calling process may be killed by respawn-pane, but the new session
// starts reliably because tmux handles the transition server-side.
//
// All durable side-effects (env refresh, metadata update, hash clear) are
// performed BEFORE the respawn call, since respawn-pane -k may kill the
// calling process in self-handoff scenarios.
func (m *Manager) Cycle(name, workdir, cmd string, env map[string]string, role, world string) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	// Ensure pane survives process death during the kill+respawn transition.
	// Without this, tmux may destroy the pane before respawn-pane can start
	// the new command.
	remain, remainCancel := tmuxCmd("set-option", "-t", tmuxExactTarget(name), "remain-on-exit", "on")
	if out, err := remain.CombinedOutput(); err != nil {
		remainCancel()
		return fmt.Errorf("failed to set remain-on-exit for %s: %s: %w", name, strings.TrimSpace(string(out)), err)
	}
	remainCancel()

	// Refresh environment variables in the tmux session.
	for k, v := range env {
		setEnv, setEnvCancel := tmuxCmd("set-environment", "-t", tmuxExactTarget(name), k, v)
		if out, err := setEnv.CombinedOutput(); err != nil {
			setEnvCancel()
			return fmt.Errorf("failed to set env %q in session %q: %s: %w", k, name, strings.TrimSpace(string(out)), err)
		}
		setEnvCancel()
	}

	// Clear capture hash — fresh process gets a fresh health baseline.
	_ = os.Remove(captureHashPath(name))

	// Update metadata before respawn (respawn may kill calling process).
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create sessions directory: %w", err)
	}
	meta := sessionMeta{
		Name:      name,
		Role:      role,
		World:     world,
		WorkDir:   workdir,
		StartedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session metadata: %w", err)
	}
	if err := fileutil.AtomicWrite(metadataPath(name), data, 0o644); err != nil {
		return fmt.Errorf("failed to write session metadata for %q: %w", name, err)
	}

	// Atomically kill current process and start new command.
	// Prepend env vars to the command so the new process receives them
	// immediately, in addition to the set-environment calls above.
	// NOTE: In self-handoff scenarios, this call kills the calling process.
	// Everything below this line may not execute. All durable writes are above.
	wrappedCmd := prependEnv(cmd, env)
	respawn, respawnCancel := tmuxCmd("respawn-pane", "-k", "-t", tmuxExactTarget(name), "-c", workdir, wrappedCmd)
	defer respawnCancel()
	if out, err := respawn.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to respawn pane in session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	// Restore remain-on-exit to off for non-supervised roles (outpost, forge, etc.).
	// envoy needs it on for crash inspection; all other roles should
	// have it off so a crashed process results in session.Dead (not AgentDead),
	// which prefect/sentinel use to trigger respawn.
	if role != "envoy" {
		clearRemain, clearRemainCancel := tmuxCmd("set-option", "-t", tmuxExactTarget(name), "remain-on-exit", "off")
		if out, err := clearRemain.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "session: failed to restore remain-on-exit for %s: %s\n", name, strings.TrimSpace(string(out)))
		}
		clearRemainCancel()
	}

	// Best-effort verification: if we reached here, this is NOT a self-handoff
	// (self-handoff kills the calling process at respawn-pane). Check if the
	// new process survived startup.
	time.Sleep(startupVerifyDelay)
	if !m.Exists(name) {
		return fmt.Errorf("session %q: process died during startup (cycle)", name)
	}
	target := tmuxExactTarget(name)
	paneCmd, paneCmdCancel := tmuxCmd("list-panes", "-t", target, "-F", "#{pane_dead}")
	defer paneCmdCancel()
	if out, err := paneCmd.Output(); err == nil {
		if strings.TrimSpace(string(out)) == "1" {
			return fmt.Errorf("session %q: process died during startup (cycle)", name)
		}
	}

	return nil
}

// GetMeta reads session metadata from the JSON file without checking tmux.
// Returns the metadata if the file exists, even for dead sessions.
// Returns nil, nil if no metadata file exists.
func (m *Manager) GetMeta(name string) (*SessionInfo, error) {
	data, err := os.ReadFile(metadataPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read session metadata for %q: %w", name, err)
	}

	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse session metadata for %q: %w", name, err)
	}

	return &SessionInfo{
		Name:      meta.Name,
		Role:      meta.Role,
		World:     meta.World,
		WorkDir:   meta.WorkDir,
		StartedAt: meta.StartedAt,
		Alive:     m.Exists(meta.Name),
	}, nil
}

// NudgeSession sends a message to a Claude Code session reliably.
// This is the canonical way to send messages to Claude sessions.
// Uses: per-session mutex + copy mode exit + sanitization + chunking +
// 500ms debounce + ESC (for vim mode) + 600ms readline gap + Enter with retry.
// After sending, triggers SIGWINCH to wake Claude in detached sessions.
//
// Nudges to the same session are serialized to prevent interleaving.
// If multiple goroutines try to nudge the same session concurrently, they will
// queue up and execute one at a time.
func (m *Manager) NudgeSession(name string, message string) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	// Serialize nudges to this session to prevent interleaving.
	// Use a timed lock to avoid permanent blocking if a previous nudge hung.
	if !acquireNudgeLock(name, nudgeLockTimeout) {
		return fmt.Errorf("nudge lock timeout for session %q: previous nudge may be hung", name)
	}
	defer releaseNudgeLock(name)

	target := tmuxExactTarget(name)

	// 1. Exit copy/scroll mode if active — copy mode intercepts input,
	//    preventing delivery to the underlying process.
	modeCmd, modeCancel := tmuxCmd("display-message", "-p", "-t", target, "#{pane_in_mode}")
	modeOut, err := modeCmd.Output()
	modeCancel()
	if err == nil && strings.TrimSpace(string(modeOut)) == "1" {
		cancelCmd, cancelCancel := tmuxCmd("send-keys", "-t", target, "-X", "cancel")
		_ = cancelCmd.Run()
		cancelCancel()
		time.Sleep(50 * time.Millisecond)
	}

	// 2. Sanitize control characters that corrupt delivery
	sanitized := sanitizeNudgeMessage(message)

	// 3. Send text via send-keys -l. Messages > 512 bytes are chunked
	//    with 10ms inter-chunk delays to avoid argument length limits.
	if err := m.sendMessageChunked(name, sanitized); err != nil {
		return fmt.Errorf("failed to send nudge message to session %q: %w", name, err)
	}

	// 4. Wait 500ms for text delivery to complete
	time.Sleep(500 * time.Millisecond)

	// 5. Send Escape to exit vim INSERT mode if enabled (harmless in normal mode)
	_ = m.SendKeys(name, "Escape")

	// 6. Wait 600ms — must exceed bash readline's keyseq-timeout (500ms default)
	// so ESC is processed alone, not as a meta prefix for the subsequent Enter.
	// Without this, ESC+Enter within 500ms becomes M-Enter (meta-return) which
	// does NOT submit the line.
	time.Sleep(600 * time.Millisecond)

	// 7. Send Enter with retry (critical for message submission)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(200 * time.Millisecond)
		}
		if err := m.SendKeys(name, "Enter"); err != nil {
			lastErr = err
			continue
		}
		// 8. Wake the pane to trigger SIGWINCH for detached sessions
		m.wakePaneIfDetached(name)
		return nil
	}
	return fmt.Errorf("failed to send Enter after 3 attempts: %w", lastErr)
}

// sendMessageChunked sends a sanitized message to a session's pane.
// For small messages (≤ sendKeysChunkSize), uses a single send-keys -l.
// For larger messages, sends in chunks with 10ms inter-chunk delays.
func (m *Manager) sendMessageChunked(name, text string) error {
	target := tmuxExactTarget(name)
	if len(text) <= sendKeysChunkSize {
		cmd, cancel := tmuxCmd("send-keys", "-t", target, "-l", "--", text)
		defer cancel()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to send text to session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	// Send in chunks to avoid tmux send-keys argument length limits.
	for i := 0; i < len(text); i += sendKeysChunkSize {
		end := i + sendKeysChunkSize
		if end > len(text) {
			end = len(text)
		}
		chunk := text[i:end]
		cmd, cancel := tmuxCmd("send-keys", "-t", target, "-l", "--", chunk)
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("failed to send chunk to session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
		if i+sendKeysChunkSize < len(text) {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
}

// sanitizeNudgeMessage removes control characters that corrupt tmux send-keys
// delivery. ESC (0x1b) triggers terminal escape sequences, CR (0x0d) acts as
// premature Enter, BS (0x08) deletes characters. TAB is replaced with a space
// to avoid triggering shell completion. Printable characters (including quotes,
// backticks, and Unicode) are preserved.
func sanitizeNudgeMessage(msg string) string {
	var b strings.Builder
	b.Grow(len(msg))
	for _, r := range msg {
		switch {
		case r == '\t': // TAB → space (avoid triggering completion)
			b.WriteRune(' ')
		case r == '\n': // preserve newlines
			b.WriteRune(r)
		case r < 0x20: // strip all other control chars (ESC, CR, BS, etc.)
			continue
		case r == 0x7f: // DEL
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isSessionAttached returns true if the session has any clients attached.
func (m *Manager) isSessionAttached(name string) bool {
	cmd, cancel := tmuxCmd("display-message", "-t", tmuxExactTarget(name), "-p", "#{session_attached}")
	defer cancel()
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != "0"
}

// wakePaneIfDetached triggers a SIGWINCH only if the session is detached.
// This avoids unnecessary latency on attached sessions where Claude is
// already processing terminal events.
func (m *Manager) wakePaneIfDetached(name string) {
	if m.isSessionAttached(name) {
		return
	}
	m.wakePane(name)
}

// wakePane triggers a SIGWINCH in a session by resizing the window slightly
// then restoring. This wakes up Claude Code's event loop in detached sessions.
func (m *Manager) wakePane(name string) {
	target := tmuxExactTarget(name)

	// Get current window width.
	widthCmd, widthCancel := tmuxCmd("display-message", "-p", "-t", target, "#{window_width}")
	widthOut, err := widthCmd.Output()
	widthCancel()
	if err != nil {
		return // session may be dead
	}
	widthStr := strings.TrimSpace(string(widthOut))
	w, err := strconv.Atoi(widthStr)
	if err != nil || w < 1 {
		return
	}

	// Bump width +1, sleep, then restore.
	resizeUp, resizeUpCancel := tmuxCmd("resize-window", "-t", target, "-x", fmt.Sprintf("%d", w+1))
	_ = resizeUp.Run()
	resizeUpCancel()

	time.Sleep(50 * time.Millisecond)

	resizeDown, resizeDownCancel := tmuxCmd("resize-window", "-t", target, "-x", widthStr)
	_ = resizeDown.Run()
	resizeDownCancel()

	// Reset window-size to "latest" after the resize dance. tmux automatically
	// sets window-size to "manual" whenever resize-window is called, which
	// permanently locks the window at the current dimensions.
	resetCmd, resetCancel := tmuxCmd("set-option", "-w", "-t", target, "window-size", "latest")
	_ = resetCmd.Run()
	resetCancel()
}

// getSessionNudgeSem returns the channel semaphore for serializing nudges to a session.
func getSessionNudgeSem(session string) chan struct{} {
	sem := make(chan struct{}, 1)
	actual, _ := sessionNudgeLocks.LoadOrStore(session, sem)
	return actual.(chan struct{})
}

// acquireNudgeLock attempts to acquire the per-session nudge lock with a timeout.
// Returns true if the lock was acquired, false if the timeout expired.
func acquireNudgeLock(session string, timeout time.Duration) bool {
	sem := getSessionNudgeSem(session)
	select {
	case sem <- struct{}{}:
		return true
	case <-time.After(timeout):
		return false
	}
}

// releaseNudgeLock releases the per-session nudge lock.
func releaseNudgeLock(session string) {
	sem := getSessionNudgeSem(session)
	select {
	case <-sem:
	default:
		// Lock wasn't held — shouldn't happen, but don't block
	}
}

// matchesPromptPrefix reports whether a captured pane line matches the
// prompt prefix. It normalizes non-breaking spaces (U+00A0) to regular
// spaces before matching, because Claude Code uses NBSP after its ❯
// prompt character.
func matchesPromptPrefix(line, promptPrefix string) bool {
	if promptPrefix == "" {
		return false
	}
	trimmed := strings.TrimSpace(line)
	// Normalize NBSP (U+00A0) → regular space.
	trimmed = strings.ReplaceAll(trimmed, "\u00a0", " ")
	normalizedPrefix := strings.ReplaceAll(promptPrefix, "\u00a0", " ")
	prefix := strings.TrimSpace(normalizedPrefix)
	return strings.HasPrefix(trimmed, normalizedPrefix) || (prefix != "" && trimmed == prefix)
}

// linesContainPrompt checks whether any of the captured pane lines
// contain the Claude Code prompt prefix, indicating the agent is at
// an idle input prompt.
func linesContainPrompt(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if matchesPromptPrefix(trimmed, DefaultPromptPrefix) {
			return true
		}
	}
	return false
}

// linesAreBusy checks whether the captured pane lines indicate Claude Code
// is actively running a tool call. The status bar shows "esc to interrupt"
// while a tool is executing.
func linesAreBusy(lines []string) bool {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "esc to interrupt") {
			return true
		}
	}
	return false
}

// WaitForIdle polls capture-pane output until the session appears to be at
// an idle prompt. It requires 2 consecutive idle detections 200ms apart to
// filter out transient prompt appearances during inter-tool-call gaps where
// the prompt briefly flashes between tool invocations.
//
// Returns nil if idle within timeout, ErrIdleTimeout on expiry.
// Returns immediately with error if session not found.
func (m *Manager) WaitForIdle(name string, timeout time.Duration) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	const requiredConsecutive = 2
	consecutiveIdle := 0

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		content, err := m.Capture(name, 5)
		if err != nil {
			// Session disappeared during polling — return immediately.
			if !m.Exists(name) {
				return fmt.Errorf("session %q not found", name)
			}
			consecutiveIdle = 0
			time.Sleep(200 * time.Millisecond)
			continue
		}

		lines := strings.Split(content, "\n")

		// Check status bar first: if "esc to interrupt" is visible,
		// Claude Code is actively running a tool call — NOT idle.
		if linesAreBusy(lines) {
			consecutiveIdle = 0
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if linesContainPrompt(lines) {
			consecutiveIdle++
			if consecutiveIdle >= requiredConsecutive {
				return nil
			}
		} else {
			consecutiveIdle = 0
		}
		time.Sleep(200 * time.Millisecond)
	}
	return ErrIdleTimeout
}

// Exists returns true if a tmux session with this name exists.
func (m *Manager) Exists(name string) bool {
	cmd, cancel := tmuxCmd("has-session", "-t", tmuxExactTarget(name))
	defer cancel()
	return cmd.Run() == nil
}

// CountSessions returns the number of active tmux sessions whose names
// start with the given prefix. Returns 0 (not an error) when the tmux
// server is not running.
func (m *Manager) CountSessions(prefix string) (int, error) {
	cmd, cancel := tmuxCmd("list-sessions", "-F", "#{session_name}")
	defer cancel()

	out, err := cmd.Output()
	if err != nil {
		// tmux exits non-zero when no server is running — that means zero sessions.
		return 0, nil
	}

	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" && strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count, nil
}

// IsAtPrompt returns true if the session's pane currently shows the Claude
// Code prompt prefix, indicating the agent is idle. This is a non-blocking
// point-in-time snapshot — it does not poll or require consecutive checks.
func (m *Manager) IsAtPrompt(name string) bool {
	content, err := m.Capture(name, 5)
	if err != nil {
		return false
	}
	lines := strings.Split(content, "\n")
	return linesContainPrompt(lines)
}

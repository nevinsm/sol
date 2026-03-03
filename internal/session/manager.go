package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/protocol"
)

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
	Alive     bool      `json:"alive"`
}

// HealthStatus represents the health of a session.
type HealthStatus int

const (
	Healthy   HealthStatus = iota // exit 0: session alive, recent activity
	Dead                          // exit 1: tmux session doesn't exist
	AgentDead                     // exit 2: session exists but process exited
	Hung                          // exit 3: session exists but no output change
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
func (h HealthStatus) ExitCode() int {
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

	// Create the tmux session
	newSess, newSessCancel := tmuxCmd("new-session", "-d", "-s", name, "-c", workdir, cmd)
	defer newSessCancel()
	if out, err := newSess.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	// For human-supervised roles (envoy, governor), keep the pane alive after
	// the process exits so we can inspect the exit code and any error output.
	// Without this, tmux destroys the session immediately and all crash
	// evidence is lost. Not applied to regular agents because the prefect
	// uses Exists() to detect dead sessions for auto-respawn.
	if role == "envoy" || role == "governor" {
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
	if err := os.WriteFile(metadataPath(name), data, 0o644); err != nil {
		_ = m.Stop(name, true)
		return fmt.Errorf("failed to write session metadata for %q: %w", name, err)
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

		// Get tmux server PID if session is alive
		if info.Alive {
			pid, pidCancel := tmuxCmd("display-message", "-t", tmuxExactTarget(meta.Name), "-p", "#{pid}")
			if out, err := pid.Output(); err == nil {
				if p, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
					info.PID = p
				}
			}
			pidCancel()
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
// literal mode, then presses Enter to submit it. Used for nudge delivery.
// If submit is false, the text is staged without pressing Enter.
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

// Exists returns true if a tmux session with this name exists.
func (m *Manager) Exists(name string) bool {
	cmd, cancel := tmuxCmd("has-session", "-t", tmuxExactTarget(name))
	defer cancel()
	return cmd.Run() == nil
}

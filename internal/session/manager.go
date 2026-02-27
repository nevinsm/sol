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

// tmuxCmd creates a tmux command with a 10-second timeout.
func tmuxCmd(args ...string) *exec.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, "tmux", args...)
	// Store cancel in a goroutine that waits for the context to finish
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return cmd
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

	// Create the tmux session
	tmux := tmuxCmd("new-session", "-d", "-s", name, "-c", workdir, cmd)
	if out, err := tmux.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	// Set environment variables
	for k, v := range env {
		tmux := tmuxCmd("set-environment", "-t", name, k, v)
		if out, err := tmux.CombinedOutput(); err != nil {
			// Best-effort cleanup on env failure
			_ = m.Stop(name, true)
			return fmt.Errorf("failed to set env %q in session %q: %s: %w", k, name, strings.TrimSpace(string(out)), err)
		}
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
		return fmt.Errorf("session %q not found", name)
	}

	if !force {
		// Send C-c for graceful shutdown
		interrupt := tmuxCmd("send-keys", "-t", name, "C-c", "")
		_ = interrupt.Run()
		// Wait up to 5 seconds for the session to exit
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !m.Exists(name) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Kill the session if it still exists
	if m.Exists(name) {
		kill := tmuxCmd("kill-session", "-t", name)
		if out, err := kill.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to kill session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
		}
	}

	// Remove metadata file
	_ = os.Remove(metadataPath(name))
	// Remove capture hash file
	_ = os.Remove(captureHashPath(name))

	return nil
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
			continue
		}

		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
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
			pid := tmuxCmd("display-message", "-t", meta.Name, "-p", "#{pid}")
			if out, err := pid.Output(); err == nil {
				if p, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
					info.PID = p
				}
			}
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
	paneCmd := tmuxCmd("list-panes", "-t", name, "-F", "#{pane_dead}")
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
		ch := captureHash{Hash: hash, Timestamp: time.Now().UTC()}
		if j, err := json.Marshal(ch); err == nil {
			_ = os.MkdirAll(filepath.Dir(hashFile), 0o755)
			_ = os.WriteFile(hashFile, j, 0o644)
		}
		return Healthy, nil
	}

	var prev captureHash
	if err := json.Unmarshal(data, &prev); err != nil {
		// Corrupted file — overwrite and return healthy
		ch := captureHash{Hash: hash, Timestamp: time.Now().UTC()}
		if j, err := json.Marshal(ch); err == nil {
			_ = os.WriteFile(hashFile, j, 0o644)
		}
		return Healthy, nil
	}

	if prev.Hash != hash {
		// Content changed — update and return healthy
		ch := captureHash{Hash: hash, Timestamp: time.Now().UTC()}
		if j, err := json.Marshal(ch); err == nil {
			_ = os.WriteFile(hashFile, j, 0o644)
		}
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

	cmd := tmuxCmd("capture-pane", "-t", name, "-p", "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.Output()
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

	return syscall.Exec(tmuxPath, []string{"tmux", "attach-session", "-t", name}, os.Environ())
}

// Inject sends text to the session's active pane using tmux send-keys in
// literal mode. Used for nudge delivery.
func (m *Manager) Inject(name string, text string) error {
	if !m.Exists(name) {
		return fmt.Errorf("session %q not found", name)
	}

	cmd := tmuxCmd("send-keys", "-t", name, "-l", "--", text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to inject into session %q: %s: %w", name, strings.TrimSpace(string(out)), err)
	}

	return nil
}

// Exists returns true if a tmux session with this name exists.
func (m *Manager) Exists(name string) bool {
	cmd := tmuxCmd("has-session", "-t", name)
	return cmd.Run() == nil
}

# Prompt 02: Loop 0 — Session Manager (tmux)

You are adding the session manager to the `sol` orchestration system. This
package wraps tmux to provide process containers for AI agents — creating
sessions, injecting text, capturing output, checking health, and enabling
interactive attachment.

**Working directory:** `~/sol-src/`
**Prerequisite:** Prompt 01 (scaffold + store) has been completed.

Read the existing code first to understand the project structure, config
package, and cobra setup before adding new code.

---

## Task 1: Session Manager Package

Create `internal/session/manager.go`:

```go
package session

type Manager struct {
    // No fields needed — all state lives in tmux server and .runtime/sessions/
}

func New() *Manager

type SessionInfo struct {
    Name      string    `json:"name"`
    PID       int       `json:"pid"`       // tmux server PID for the session
    Role      string    `json:"role"`      // outpost|sentinel|forge|crew
    World       string    `json:"world"`
    WorkDir   string    `json:"workdir"`
    StartedAt time.Time `json:"started_at"`
    Alive     bool      `json:"alive"`     // tmux session exists
}

type HealthStatus int

const (
    Healthy    HealthStatus = iota // exit 0: session alive, recent activity
    Dead                           // exit 1: tmux session doesn't exist
    AgentDead                      // exit 2: session exists but process exited
    Hung                           // exit 3: session exists but no output change
)

func (h HealthStatus) String() string
func (h HealthStatus) ExitCode() int

// Start creates a tmux session with the given name and runs cmd inside it.
// Writes session metadata to $SOL_HOME/.runtime/sessions/{name}.json.
// Env vars are set in the tmux session environment.
// Returns error if session already exists.
func (m *Manager) Start(name, workdir, cmd string, env map[string]string, role, world string) error

// Stop kills a tmux session. If force=false, sends C-c first and waits 5s
// before killing. If force=true, kills immediately. Removes session metadata file.
func (m *Manager) Stop(name string, force bool) error

// List returns all sessions with metadata from .runtime/sessions/*.json,
// enriched with live status from tmux.
func (m *Manager) List() ([]SessionInfo, error)

// Health checks session health using three signals:
// 1. Does the tmux session exist?
// 2. Is there a running process in the session?
// 3. Has the pane content changed since last check? (compare capture output hash)
//
// maxInactivity: if pane content unchanged for this duration, report Hung.
// The health check writes a .last-capture-hash file per session to track changes.
func (m *Manager) Health(name string, maxInactivity time.Duration) (HealthStatus, error)

// Capture returns the last N lines of visible output from the session's pane.
func (m *Manager) Capture(name string, lines int) (string, error)

// Attach attaches the current terminal to the tmux session (replaces process).
// This calls os.Exec — it does not return on success.
func (m *Manager) Attach(name string) error

// Inject sends text to the session's active pane using tmux send-keys in
// literal mode. Used for nudge delivery.
func (m *Manager) Inject(name string, text string) error

// Exists returns true if a tmux session with this name exists.
func (m *Manager) Exists(name string) bool
```

### tmux Commands

The package shells out to tmux. Key commands:

```bash
# Start a session
tmux new-session -d -s <name> -c <workdir> <cmd>

# Set environment variable in session
tmux set-environment -t <name> <key> <value>

# Check if session exists
tmux has-session -t <name>

# List sessions
tmux list-sessions -F "#{session_name}"

# Capture pane content
tmux capture-pane -t <name> -p -S -<lines>

# Send keys (literal mode for safe text injection)
tmux send-keys -t <name> -l -- <text>

# Kill session
tmux kill-session -t <name>

# Attach (replaces current process)
exec tmux attach-session -t <name>
```

### Session Metadata

On `Start`, write `$SOL_HOME/.runtime/sessions/{name}.json`:

```json
{
  "name": "Toast",
  "role": "outpost",
  "world": "myworld",
  "workdir": "/home/user/sol/myworld/outposts/Toast/world",
  "started_at": "2026-02-25T10:30:00Z"
}
```

On `Stop`, remove this file.

On `List`, read all `.json` files from the directory and enrich each with
live tmux status (does the session still exist?).

### Health Check Detail

The `Health` function uses a `.last-capture-hash` file to detect inactivity:

1. Capture last 50 lines of pane content
2. Hash the content (SHA-256)
3. Read `$SOL_HOME/.runtime/sessions/{name}.last-capture-hash`
4. If file doesn't exist: write hash + timestamp, return Healthy
5. If hash differs: update file, return Healthy
6. If hash matches: check timestamp in file. If age > maxInactivity, return Hung
7. If tmux session doesn't exist: return Dead
8. If session exists but no running process (`tmux list-panes -t <name> -F "#{pane_dead}"` returns 1): return AgentDead

---

## Task 2: CLI Commands

Add `cmd/session.go` with these subcommands:

### `sol session start <name>`

```
sol session start <name> --workdir=<dir> --cmd=<command> [--env=KEY=VAL]... [--role=outpost] [--world=<world>]
```

Creates a tmux session. `--env` can be repeated. Default role: `outpost`.
Prints `"Session <name> started"` on success. Exit 1 if session already exists.

### `sol session stop <name>`

```
sol session stop <name> [--force]
```

Gracefully stops (or force-kills) a session. Prints confirmation. Exit 1 if
session doesn't exist.

### `sol session list`

```
sol session list [--json]
```

Lists all registered sessions with status. Default: human-readable table.

### `sol session health <name>`

```
sol session health <name> [--max-inactivity=30m]
```

Prints health status. Exit code matches HealthStatus enum (0-3).

### `sol session capture <name>`

```
sol session capture <name> [--lines=50]
```

Prints captured pane content to stdout.

### `sol session attach <name>`

```
sol session attach <name>
```

Attaches to the tmux session. Replaces the current process (exec).

### `sol session inject <name>`

```
sol session inject <name> --message=<text>
```

Sends text to the session's pane. Used for testing nudge delivery.

---

## Task 3: Tests

Create `internal/session/manager_test.go`. Tests must use an **isolated
tmux server** to avoid interfering with any running tmux sessions.

Set `TMUX_TMPDIR` to a test-specific temp directory so the tmux socket is
isolated. Each test should create its own tmux server.

**Test cases:**

1. **Start/Stop**: Start a session running `sleep 300`, verify it exists,
   stop it, verify it's gone
2. **List**: Start 3 sessions, list, verify all 3 appear with correct names
3. **Capture**: Start a session running `echo "hello world" && sleep 300`,
   wait briefly, capture, verify output contains "hello world"
4. **Inject**: Start a session running `cat` (reads stdin), inject "test
   message", capture, verify "test message" appears
5. **Health — healthy**: Start a session that outputs text periodically,
   check health, verify Healthy
6. **Health — dead**: Start and immediately stop a session, check health,
   verify Dead
7. **Exists**: Verify true for existing session, false for nonexistent
8. **Metadata**: Start a session, verify .runtime/sessions/{name}.json
   exists with correct content. Stop, verify file removed.
9. **Double start**: Start a session, try to start another with same name,
   verify error

Use `t.Cleanup` to kill any tmux sessions/servers created during tests.

**Important:** Some tests need brief `time.Sleep` calls to let tmux
stabilize (session creation, output capture). Use 200-500ms sleeps where
needed. Document why with a comment.

Run tests and fix any issues before completing.

---

## Task 4: Verify

After implementation:

1. `make test` passes all tests (store + session)
2. `SOL_HOME=/tmp/sol-test bin/sol session start test1 --workdir=/tmp --cmd="sleep 300" --world=myworld`
3. `SOL_HOME=/tmp/sol-test bin/sol session list` shows test1
4. `SOL_HOME=/tmp/sol-test bin/sol session health test1` exits 0
5. `SOL_HOME=/tmp/sol-test bin/sol session capture test1` shows output
6. `SOL_HOME=/tmp/sol-test bin/sol session inject test1 --message="hello"`
7. `SOL_HOME=/tmp/sol-test bin/sol session stop test1`
8. `SOL_HOME=/tmp/sol-test bin/sol session list` shows no sessions

Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- Shell out to tmux via `os/exec`. Don't use a tmux library.
- All tmux commands should have a 10-second timeout.
- Error messages should include the session name: `"session %q not found"`.
- The `Attach` function uses `syscall.Exec` to replace the process. It
  should resolve the tmux binary path first with `exec.LookPath`.
- Commit after tests pass.

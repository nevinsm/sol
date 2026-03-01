# Prompt 06: Arc 2 — Init Guided Mode

You are adding Claude-powered guided setup to `sol init --guided`. This
creates an ephemeral Claude session that walks the operator through
first-time setup conversationally, then calls `setup.Run()` with the
collected parameters.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 05 is complete (interactive init with huh).

Read the existing code first. Understand:
- `cmd/init.go` — `runInit()` dispatch, `runFlagInit()`,
  `runInteractiveInit()`, `printInitSuccess()`
- `internal/setup/setup.go` — `Params`, `Run()`, `Result`
- `internal/session/session.go` — how tmux sessions are started
  (reference for ephemeral session)
- `internal/protocol/claudemd.go` — how CLAUDE.md files are generated
  for agent sessions

---

## Task 1: Guided Mode Flag

**Modify** `cmd/init.go`.

Add the `--guided` flag:

```go
var initGuided bool

// In init():
initCmd.Flags().BoolVar(&initGuided, "guided", false,
    "Claude-powered guided setup")
```

Update `runInit()` dispatch:

```go
func runInit(cmd *cobra.Command, args []string) error {
    // Flag mode: --name provided → run directly.
    if initName != "" {
        return runFlagInit()
    }

    // Guided mode: --guided flag → Claude session.
    if initGuided {
        return runGuidedInit()
    }

    // Interactive mode: stdin is a TTY → prompt for input.
    if isTerminal() {
        return runInteractiveInit()
    }

    // Non-interactive, no flags → error.
    return fmt.Errorf("--name flag is required when stdin is not a terminal\n" +
        "Usage: sol init --name=<world> [--source-repo=<path>]")
}
```

---

## Task 2: Guided Init CLAUDE.md

**Modify** `internal/protocol/claudemd.go`.

Add a CLAUDE.md generator for the guided init session:

```go
// GuidedInitClaudeMDContext holds context for the guided init CLAUDE.md.
type GuidedInitClaudeMDContext struct {
    SOLHome    string
    SolBinary  string // path to sol binary
}

// GenerateGuidedInitClaudeMD returns the CLAUDE.md for a guided init session.
func GenerateGuidedInitClaudeMD(ctx GuidedInitClaudeMDContext) string {
    return fmt.Sprintf(`# Sol Guided Setup

You are helping an operator set up sol for the first time.

## Your Role
You are a setup assistant. Your job is to help the operator configure sol
by asking questions conversationally and then running the setup command.

## What You Need to Collect
1. **World name** — a short identifier for their first project/world
   (e.g., "myapp", "backend", "frontend"). Must match: [a-zA-Z0-9][a-zA-Z0-9_-]*
   Cannot be: "store", "runtime", "sol"

2. **Source repository** (optional) — the path to the git repository
   they want agents to work on. Must be a directory that exists.

## Setup Command
Once you have the world name (and optionally source repo), run:

` + "```bash\n%s init --name=<world> --skip-checks" + `
# or with source repo:
%s init --name=<world> --source-repo=<path> --skip-checks
` + "```" + `

## Conversation Guidelines
- Be concise and friendly. This is a setup wizard, not a lecture.
- Ask one question at a time.
- Provide examples and suggestions when relevant.
- If the operator seems unsure about world names, suggest naming it
  after their project.
- Explain what sol does briefly if asked, but stay focused on setup.
- After successful setup, summarize what was created and suggest next steps.

## Important
- SOL_HOME will be: %s
- Do NOT modify any files directly. Use the sol CLI commands above.
- If setup fails, help the operator diagnose the issue.
- If they want to exit, let them — don't be pushy.
`, ctx.SolBinary, ctx.SolBinary, ctx.SOLHome)
}
```

---

## Task 3: Guided Init Implementation

**Modify** `cmd/init.go`.

```go
func runGuidedInit() error {
    // DEGRADE: check prerequisites for guided mode.
    // Guided mode needs tmux (for the session) and claude (for the AI).
    tmuxCheck := doctor.CheckTmux()
    claudeCheck := doctor.CheckClaude()

    if !tmuxCheck.Passed || !claudeCheck.Passed {
        fmt.Println("Guided mode requires tmux and claude CLI.")
        if !tmuxCheck.Passed {
            fmt.Printf("  ✗ %s\n    → %s\n", tmuxCheck.Message, tmuxCheck.Fix)
        }
        if !claudeCheck.Passed {
            fmt.Printf("  ✗ %s\n    → %s\n", claudeCheck.Message, claudeCheck.Fix)
        }
        fmt.Println("\nFalling back to interactive mode...")
        fmt.Println()
        return runInteractiveInit()
    }

    // Determine sol binary path.
    solBin, err := os.Executable()
    if err != nil {
        solBin = "sol" // fallback
    }

    // Create a temporary directory for the guided session.
    tmpDir, err := os.MkdirTemp("", "sol-guided-*")
    if err != nil {
        return fmt.Errorf("failed to create temp directory: %w", err)
    }
    defer os.RemoveAll(tmpDir)

    // Write CLAUDE.md into the temp directory.
    claudeDir := filepath.Join(tmpDir, ".claude")
    if err := os.MkdirAll(claudeDir, 0o755); err != nil {
        return fmt.Errorf("failed to create .claude directory: %w", err)
    }

    ctx := protocol.GuidedInitClaudeMDContext{
        SOLHome:   config.Home(),
        SolBinary: solBin,
    }
    content := protocol.GenerateGuidedInitClaudeMD(ctx)
    claudeMDPath := filepath.Join(claudeDir, "CLAUDE.md")
    if err := os.WriteFile(claudeMDPath, []byte(content), 0o644); err != nil {
        return fmt.Errorf("failed to write CLAUDE.md: %w", err)
    }

    // Start an ephemeral tmux session with Claude.
    sessionName := "sol-guided-init"

    // Kill any existing guided init session.
    mgr := session.New()
    if mgr.Exists(sessionName) {
        mgr.Stop(sessionName)
    }

    // Start Claude in the temp directory.
    claudeCmd := fmt.Sprintf("cd %s && claude", tmpDir)
    if err := mgr.Start(sessionName, claudeCmd); err != nil {
        return fmt.Errorf("failed to start guided session: %w", err)
    }

    fmt.Println("Starting guided setup with Claude...")
    fmt.Printf("Session: %s\n", sessionName)
    fmt.Println()
    fmt.Println("The Claude session will guide you through setup.")
    fmt.Println("When finished, the session will end automatically.")
    fmt.Println()

    // Attach to the session (blocks until detach or exit).
    if err := mgr.Attach(sessionName); err != nil {
        return fmt.Errorf("failed to attach to guided session: %w", err)
    }

    // Clean up the session if it's still running.
    if mgr.Exists(sessionName) {
        mgr.Stop(sessionName)
    }

    return nil
}
```

### Import updates

Add to imports in `cmd/init.go`:

```go
"path/filepath"

"github.com/nevinsm/sol/internal/doctor"
"github.com/nevinsm/sol/internal/protocol"
"github.com/nevinsm/sol/internal/session"
```

---

## Task 4: Session Manager Check

Read `internal/session/session.go` to verify:
- `mgr.Start(name, command)` exists and starts a tmux session
- `mgr.Attach(name)` exists and attaches to a session (blocking)
- `mgr.Exists(name)` checks if a session is alive
- `mgr.Stop(name)` kills a session

If the session manager's `Start` method signature differs (e.g., takes
different parameters), adapt the implementation accordingly. The key
requirement is:

1. Start a tmux session named `sol-guided-init`
2. The session runs `claude` in the temp directory (which has the
   CLAUDE.md)
3. Attach the operator to the session (blocking)
4. Clean up when done

If `Start` doesn't accept a command string directly, use whatever
mechanism the session manager provides. You may need to use
`session.StartOpts` or similar.

---

## Task 5: Tests

### Unit tests

Guided mode is hard to test (requires tmux + claude). Focus on:

```go
func TestGuidedInitClaudeMD(t *testing.T)
    // Generate the CLAUDE.md content.
    // Verify it contains "World name", "Source repository", setup command.
    // Verify it includes the SOL_HOME path.
    // Verify it includes the sol binary path.

func TestGuidedInitDEGRADE(t *testing.T)
    // This test verifies the DEGRADE behavior conceptually.
    // If tmux is not available, guided mode should fall back to interactive.
    // Hard to test without mocking exec.LookPath.
    // Instead, just verify the guided mode code path exists and compiles.
```

### Integration tests

```go
func TestGuidedFlagExists(t *testing.T)
    // Run: sol init --help
    // Verify output contains "--guided"
```

---

## Task 6: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Manual smoke test (requires tmux + claude):
   ```bash
   export SOL_HOME=/tmp/sol-guided-test
   rm -rf /tmp/sol-guided-test

   # Guided mode
   bin/sol init --guided
   # Should open a Claude session that guides through setup.
   # After setup completes, detach (Ctrl+B D) or let it finish.

   # Verify
   ls /tmp/sol-guided-test/
   cat /tmp/sol-guided-test/*/world.toml

   rm -rf /tmp/sol-guided-test
   ```

---

## Guidelines

- **DEGRADE is critical.** If tmux or claude is not available, guided
  mode falls back to interactive mode with a clear message. The system
  never fails hard because of an optional feature.
- The guided session is ephemeral — the temp directory is cleaned up
  after the session ends. No persistent state from the guided
  conversation.
- Claude runs in the temp directory so it picks up the `.claude/CLAUDE.md`
  file automatically. The CLAUDE.md instructs Claude to use `sol init`
  commands — it doesn't modify the filesystem directly.
- The session name `sol-guided-init` is fixed (not per-world). Only one
  guided init can run at a time, which is correct — you only set up
  sol once.
- `--guided` and `--name` are mutually exclusive in practice: if
  `--name` is provided, flag mode runs; if `--guided` is provided,
  guided mode runs. No explicit conflict check needed because the
  dispatch order handles it.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(init): add guided mode with ephemeral Claude session`

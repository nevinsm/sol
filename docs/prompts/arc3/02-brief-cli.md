# Prompt 02: Arc 3 — Brief CLI Commands

**Working directory:** ~/gt-src/
**Prerequisite:** Prompt 01 complete (brief package exists at `internal/brief/`)

## Context

Read these files before making changes:

- `internal/brief/brief.go` — the brief package (Inject, WriteSessionStart, CheckSave)
- `cmd/doctor.go` — example of a simple command with `SilenceErrors`/`SilenceUsage`
- `cmd/root.go` — PersistentPreRunE and command registration pattern
- `docs/decisions/0013-brief-system.md` — brief system design
- `docs/arc-roadmap.md` — Arc 3 brief system section

## Task 1: `sol brief inject`

Create `cmd/brief.go` with a `briefCmd` parent and `briefInjectCmd` subcommand.

```go
var briefCmd = &cobra.Command{
    Use:   "brief",
    Short: "Manage agent brief files",
}
```

### `sol brief inject --path=<path> [--max-lines=200]`

```go
var briefInjectCmd = &cobra.Command{
    Use:   "inject",
    Short: "Inject brief into session context",
    Long: `Read a brief file and output framed content for session injection.

Used by Claude Code hooks to inject agent context on session start
and after context compaction. Also records session start timestamp
for the stop hook save check.`,
    SilenceErrors: true,
    SilenceUsage:  true,
    RunE: func(cmd *cobra.Command, args []string) error { ... },
}
```

Flags:
- `--path` (required) — path to the brief file (e.g., `.brief/memory.md`)
- `--max-lines` (default 200) — truncation limit

Behavior:
1. Call `brief.Inject(path, maxLines)`
2. If result is non-empty, print to stdout (this becomes the hook output
   injected into the Claude session)
3. Call `brief.WriteSessionStart(filepath.Dir(path))` to record the session
   start timestamp
4. If brief file doesn't exist or is empty, still write session start
   (the stop hook needs the timestamp regardless)

Exit 0 on success, even if brief is missing (missing = clean start, not error).

### `sol brief check-save <path>`

```go
var briefCheckSaveCmd = &cobra.Command{
    Use:   "check-save <path>",
    Short: "Check if brief was updated since session start",
    Long: `Stop hook command. Checks whether the brief file was modified
since the session started. If not, outputs a nudge message and exits
with code 1 to block the stop.

Set SOL_STOP_HOOK_ACTIVE=true on second invocation to allow stop
without brief update (prevents infinite loops).`,
    SilenceErrors: true,
    SilenceUsage:  true,
    Args:          cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error { ... },
}
```

No flags — path is a positional argument.

Behavior:
1. Check env var `SOL_STOP_HOOK_ACTIVE`. If `"true"`, exit 0 immediately
   (allow stop — prevents infinite loop if agent ignores first nudge)
2. Call `brief.CheckSave(path)`
3. If updated (true), exit 0
4. If not updated (false), print nudge message to stdout and return
   `&exitError{code: 1}`:

```
Your brief has not been updated since this session started.
Please update .brief/memory.md with key context before exiting:
- Decisions made and rationale
- Current state of work
- What to do next

Then try exiting again.
```

## Task 2: Register Commands

In the `init()` function of `cmd/brief.go`:

```go
func init() {
    rootCmd.AddCommand(briefCmd)
    briefCmd.AddCommand(briefInjectCmd)
    briefCmd.AddCommand(briefCheckSaveCmd)
    briefInjectCmd.Flags().StringVar(&briefInjectPath, "path", "", "path to brief file")
    briefInjectCmd.MarkFlagRequired("path")
    briefInjectCmd.Flags().IntVar(&briefInjectMaxLines, "max-lines", 200, "maximum lines before truncation")
}
```

## Task 3: Bypass PersistentPreRunE

Brief commands may run inside agent sessions before SOL_HOME is fully set up
(or in contexts where EnsureDirs isn't needed). Add `"brief"` to the bypass
list in `cmd/root.go`:

```go
switch cmd.Name() {
case "doctor", "init", "brief":
    return nil
}
```

Wait — actually brief commands run inside agent directories, not before SOL_HOME
exists. SOL_HOME will exist by the time agents run. Read the PersistentPreRunE
logic and only add the bypass if `EnsureDirs` would fail in the hook context.
If `EnsureDirs` just verifies existing directories, no bypass is needed. Use your
judgment after reading the code.

## Task 4: Tests

### `cmd/brief_test.go` or add to existing cmd test file

- `TestBriefInjectWithContent` — create a temp brief file with content, run
  inject command, verify stdout contains framed content and `.session_start`
  file was created
- `TestBriefInjectMissingFile` — run inject on nonexistent path, verify no
  error and `.session_start` is still written
- `TestBriefInjectTruncation` — create a brief file with 300 lines, run inject
  with `--max-lines=200`, verify truncation notice in output
- `TestBriefCheckSaveUpdated` — create brief file and `.session_start` (with
  session_start in the past), verify exit 0
- `TestBriefCheckSaveNotUpdated` — create `.session_start` after brief file
  mtime, verify exit 1 with nudge message
- `TestBriefCheckSaveStopHookActive` — set `SOL_STOP_HOOK_ACTIVE=true`, verify
  exit 0 regardless of brief state

## Verification

- `make build && make test` passes
- Smoke test:
  ```
  mkdir -p /tmp/sol-brief-test/.brief
  echo "some context" > /tmp/sol-brief-test/.brief/memory.md
  bin/sol brief inject --path=/tmp/sol-brief-test/.brief/memory.md
  # Should print framed content and create .session_start
  cat /tmp/sol-brief-test/.brief/.session_start
  # Should show RFC3339 timestamp
  bin/sol brief check-save /tmp/sol-brief-test/.brief/memory.md
  # Should exit 1 (brief not modified since inject wrote session_start)
  echo "updated" >> /tmp/sol-brief-test/.brief/memory.md
  bin/sol brief check-save /tmp/sol-brief-test/.brief/memory.md
  # Should exit 0
  rm -rf /tmp/sol-brief-test
  ```

## Commit

```
feat(brief): add brief inject and check-save CLI commands
```

# Prompt 05: Arc 3 — Governor Package + Mirror

**Working directory:** ~/gt-src/
**Prerequisite:** Prompt 02 complete (brief CLI exists)

## Context

Read these files before making changes:

- `internal/envoy/envoy.go` — envoy lifecycle pattern (Create, Start, Stop)
- `internal/forge/forge.go` — forge lifecycle (singleton per world, Start, Stop)
- `internal/session/session.go` — session manager interface
- `internal/store/agents.go` — agent operations
- `internal/config/config.go` — Home(), WorldDir(), LoadWorldConfig()
- `internal/protocol/hooks.go` — hook installation pattern
- `docs/decisions/0010-governor-design.md` — governor design
- `docs/decisions/0013-brief-system.md` — brief system hooks

## Task 1: Directory Layout

Create `internal/governor/governor.go`:

```go
package governor

// GovernorDir returns the root directory for a world's governor.
// $SOL_HOME/{world}/governor/
func GovernorDir(world string) string

// MirrorPath returns the read-only mirror path.
// $SOL_HOME/{world}/governor/mirror/
func MirrorPath(world string) string

// BriefDir returns the brief directory for the governor.
// $SOL_HOME/{world}/governor/.brief/
func BriefDir(world string) string

// BriefPath returns the governor's memory file path.
// $SOL_HOME/{world}/governor/.brief/memory.md
func BriefPath(world string) string

// WorldSummaryPath returns the governor's world summary file path.
// $SOL_HOME/{world}/governor/.brief/world-summary.md
func WorldSummaryPath(world string) string
```

## Task 2: Mirror Setup

```go
// SetupMirror clones or updates the read-only mirror of the source repo.
// If mirror doesn't exist, clones. If it exists, pulls latest.
func SetupMirror(world, sourceRepo string) error
```

Implementation:
1. If `MirrorPath(world)` doesn't exist:
   - `git clone <sourceRepo> <mirrorPath>`
2. If it exists:
   - `git -C <mirrorPath> pull --ff-only`
   - If pull fails (e.g., diverged), log warning but don't error — mirror
     is best-effort

Error format: `fmt.Errorf("failed to setup governor mirror for world %q: %w", world, err)`

```go
// RefreshMirror pulls latest changes in the mirror.
func RefreshMirror(world string) error
```

Implementation:
1. Verify mirror exists. If not, return error "mirror not found — run governor start first"
2. `git -C <mirrorPath> checkout main` (or default branch)
3. `git -C <mirrorPath> pull --ff-only`

## Task 3: Start

```go
type StartOpts struct {
    World      string
    SourceRepo string // from world config or flag
}

func Start(opts StartOpts, sphereStore SphereStore, mgr SessionManager) error
```

Interfaces (minimal):

```go
type SphereStore interface {
    EnsureAgent(name, world, role string) error
    UpdateAgentState(name, world, state, tetherItem string) error
}

type SessionManager interface {
    Exists(name string) bool
    Start(name, workdir, command string, env []string, role, world string) error
}
```

Steps:
1. Create governor directory and brief directory
2. Register agent: `sphereStore.EnsureAgent("governor", world, "governor")`
   — use EnsureAgent (not CreateAgent) since governor is a singleton and
   start may be called repeatedly
3. Check if session already exists. If so, return error
   "governor session for world %q already running"
4. Call `SetupMirror(world, opts.SourceRepo)` — clone or refresh mirror
5. Install CLAUDE.md — write placeholder for now (prompt 06 replaces with
   protocol generator). Put it in GovernorDir, not MirrorPath.
6. Install hooks in GovernorDir — write `.claude/settings.local.json`:

```json
{
    "hooks": {
        "SessionStart": [
            {
                "matcher": "startup|resume",
                "command": "sol brief inject --path=.brief/memory.md --max-lines=200 && sol governor refresh-mirror --world={world}"
            },
            {
                "matcher": "compact",
                "command": "sol brief inject --path=.brief/memory.md --max-lines=200"
            }
        ],
        "Stop": [
            {
                "command": "sol brief check-save .brief/memory.md"
            }
        ]
    }
}
```

Read existing `protocol/hooks.go` to match the exact JSON format. The
`{world}` placeholder in the command should be replaced with the actual
world name at install time.

7. Start tmux session: `mgr.Start(SessionName(world), GovernorDir(world), "claude --dangerously-skip-permissions", nil, "governor", world)`
8. Update agent state to "idle"

Session name: `config.SessionName(world, "governor")` → `sol-{world}-governor`

## Task 4: Stop

```go
func Stop(world string, sphereStore SphereStore, mgr StopManager) error
```

Steps:
1. Check session exists. If not, just update state.
2. Stop session
3. Update agent state to "idle"

Do NOT remove the governor directory, mirror, or brief.

## Task 5: Tests

Create `internal/governor/governor_test.go`:

- `TestGovernorDir` — verify path construction for all helpers
- `TestSetupMirrorClone` — create temp git repo, call SetupMirror, verify clone
- `TestSetupMirrorRefresh` — setup mirror, add commit to source, call SetupMirror
  again, verify new commit visible
- `TestRefreshMirror` — verify pull works on existing mirror
- `TestRefreshMirrorNoMirror` — verify error when mirror doesn't exist
- `TestStart` — mock store and session manager, verify agent ensured with
  role "governor", session started with correct workdir, hooks file written
- `TestStartAlreadyRunning` — session exists, verify error
- `TestStop` — verify session stopped, state updated
- `TestStopNoSession` — session doesn't exist, no error

## Verification

- `make build && make test` passes

## Guidelines

- Governor is a singleton per world — no "name" parameter (it's always "governor")
- Mirror is read-only — governor never modifies it
- Mirror setup failures are not fatal for start (warn but continue — governor
  can still dispatch work without codebase access)
  Actually, reconsider: the ADR says mirror is for making better work items.
  If mirror fails, governor still works but produces worse items. Warn, don't fail.
- Follow forge patterns for singleton lifecycle

## Commit

```
feat(governor): add governor package with mirror and lifecycle management
```

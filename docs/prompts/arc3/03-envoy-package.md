# Prompt 03: Arc 3 — Envoy Package

**Working directory:** ~/gt-src/
**Prerequisite:** Prompt 02 complete (brief CLI exists)

## Context

Read these files to understand existing patterns:

- `internal/dispatch/dispatch.go` — Cast/Resolve flow, `WorktreePath`, `autoProvision`
- `internal/forge/forge.go` — forge lifecycle (singleton per world, `WorktreePath`, `Start`, `Stop`)
- `internal/session/session.go` — tmux session manager interface
- `internal/store/agents.go` — `CreateAgent`, `EnsureAgent`, `UpdateAgentState`, agent struct
- `internal/protocol/hooks.go` — `InstallHooks` (how hooks are written for outpost agents)
- `internal/protocol/claudemd.go` — `InstallClaudeMD` pattern
- `internal/config/config.go` — `Home()`, `WorldDir()`, directory conventions
- `docs/decisions/0009-envoy-design.md` — envoy design
- `docs/decisions/0013-brief-system.md` — brief system hooks

## Task 1: Directory Layout

Define directory helpers in `internal/envoy/envoy.go`:

```go
package envoy

// EnvoyDir returns the root directory for an envoy.
// $SOL_HOME/{world}/envoys/{name}/
func EnvoyDir(world, name string) string

// WorktreePath returns the persistent worktree path for an envoy.
// $SOL_HOME/{world}/envoys/{name}/worktree/
func WorktreePath(world, name string) string

// BriefDir returns the brief directory for an envoy.
// $SOL_HOME/{world}/envoys/{name}/.brief/
func BriefDir(world, name string) string

// BriefPath returns the path to the envoy's memory file.
// $SOL_HOME/{world}/envoys/{name}/.brief/memory.md
func BriefPath(world, name string) string
```

## Task 2: Create

```go
type CreateOpts struct {
    World      string
    Name       string
    SourceRepo string // path to git repo for worktree
}

func Create(opts CreateOpts, sphereStore SphereStore) error
```

Interface (keep minimal — only what Create needs):

```go
type SphereStore interface {
    CreateAgent(name, world, role string) error
}
```

Steps:
1. Create envoy directory: `$SOL_HOME/{world}/envoys/{name}/`
2. Create brief directory: `$SOL_HOME/{world}/envoys/{name}/.brief/`
3. Create persistent worktree using `git worktree add`:
   - Source repo comes from `opts.SourceRepo`
   - Branch name: `envoy/{world}/{name}` (unique per envoy)
   - Target: `WorktreePath(world, name)`
   - If worktree already exists (idempotent re-create), skip
4. Register agent: `sphereStore.CreateAgent(name, world, "envoy")`

Error format: `fmt.Errorf("failed to create envoy %q in world %q: %w", name, world, err)`

## Task 3: Start

```go
type StartOpts struct {
    World string
    Name  string
}

func Start(opts StartOpts, sphereStore StartStore, mgr SessionManager) error
```

Interfaces:

```go
type StartStore interface {
    GetAgent(name, world string) (store.Agent, error)
    UpdateAgentState(name, world, state, tetherItem string) error
}

type SessionManager interface {
    Exists(name string) bool
    Start(name, workdir, command string, env []string, role, world string) error
}
```

Steps:
1. Get agent record, verify role is "envoy"
2. Check if session already exists (`mgr.Exists`). If so, return error
   "envoy session %q already running"
3. Install CLAUDE.md (Task 4 handles the content — for now write a placeholder
   that says "Envoy CLAUDE.md — to be replaced by protocol generator")
4. Install hooks — write `.claude/settings.local.json` in the worktree with
   brief hooks:

```json
{
    "hooks": {
        "SessionStart": [
            {
                "matcher": "startup|resume",
                "command": "sol brief inject --path=.brief/memory.md --max-lines=200"
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

Read the existing `protocol/hooks.go` to understand the JSON structure used for
outpost hooks and match that format exactly. The hook structure above is the
logical intent — adapt the field names to match whatever Claude Code hook format
the existing code uses.

5. Start tmux session: `mgr.Start(SessionName(world, name), WorktreePath(world, name), "claude --dangerously-skip-permissions", nil, "envoy", world)`
6. Update agent state to "idle" (envoy starts idle, not working — it tethers voluntarily)

Session name convention: `sol-{world}-{name}` (same as outpost agents, using
`dispatch.SessionName` or `config.SessionName`).

## Task 4: Stop

```go
func Stop(world, name string, sphereStore StartStore, mgr StopManager) error
```

```go
type StopManager interface {
    Exists(name string) bool
    Stop(name string, force bool) error
}
```

Steps:
1. Check session exists. If not, just update state (session may have crashed).
2. Stop session: `mgr.Stop(SessionName(world, name), true)`
3. Update agent state to "idle"

Do NOT remove the worktree or directory. Envoy worktrees are persistent.

## Task 5: List

```go
func List(world string, sphereStore ListStore) ([]store.Agent, error)
```

```go
type ListStore interface {
    ListAgents() ([]store.Agent, error)
}
```

Filter the agent list to `role == "envoy"` and `world == world`. Return the
filtered list. If world is empty, return all envoys across all worlds.

## Task 6: Tests

Create `internal/envoy/envoy_test.go`:

- `TestEnvoyDir` — verify path construction
- `TestCreate` — mock store, verify agent created with role "envoy", verify
  directory structure created. Use a temp dir as SOL_HOME. For the git worktree,
  create a real temp git repo as the source.
- `TestCreateIdempotentWorktree` — calling create when worktree exists should
  not error (or handle gracefully)
- `TestStart` — mock store and session manager, verify session started with
  correct name and workdir, verify hooks file written
- `TestStartAlreadyRunning` — session exists, verify error
- `TestStop` — mock session manager, verify session stopped
- `TestStopNoSession` — session doesn't exist, verify no error (just updates state)
- `TestList` — create multiple agents with different roles, verify only envoys
  returned

## Verification

- `make build && make test` passes
- No dependencies on protocol package yet (placeholder CLAUDE.md is fine)

## Guidelines

- Follow forge package patterns for lifecycle management
- Envoy worktrees are persistent — never removed by sol
- Session naming follows existing convention (`config.SessionName` or equivalent)
- Keep interfaces minimal — only methods the function actually calls
- The hooks JSON structure must match what Claude Code expects. Read the existing
  hooks code carefully.

## Commit

```
feat(envoy): add envoy package with create, start, stop, list
```

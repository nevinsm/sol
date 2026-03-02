# Prompt 06: Arc 3 — Governor Protocol + CLI

**Working directory:** ~/gt-src/
**Prerequisite:** Prompt 05 complete (governor package exists at `internal/governor/`)

## Context

Read these files before making changes:

- `internal/governor/governor.go` — governor lifecycle (Start, Stop, mirror)
- `internal/protocol/claudemd.go` — existing CLAUDE.md generators
- `cmd/envoy.go` — envoy CLI pattern (for consistency)
- `cmd/forge.go` — forge CLI pattern (singleton: start/stop/attach)
- `docs/decisions/0010-governor-design.md` — governor design
- `docs/arc-roadmap.md` — Arc 3 governor section

## Task 1: Governor CLAUDE.md Generator

Add to `internal/protocol/claudemd.go`:

```go
type GovernorClaudeMDContext struct {
    World     string
    SolBinary string
    MirrorDir string // relative path to mirror for codebase research
}

func GenerateGovernorClaudeMD(ctx GovernorClaudeMDContext) string
```

The generated CLAUDE.md should include:

### Identity
- You are the governor of world "{world}" — a work coordinator
- You parse natural language requests into work items and dispatch them to agents
- You maintain accumulated world knowledge in your brief

### Brief Maintenance
- Your brief (`.brief/memory.md`) persists across sessions — keep it under 200 lines
- Also maintain `.brief/world-summary.md` — a structured summary for external consumers
- Update both before exiting
- World summary format:

```markdown
# World Summary: {world}
## Project       — what this codebase is
## Architecture  — key modules, patterns, tech stack
## Priorities    — active work themes, what's in flight
## Constraints   — known problem areas, things to avoid
```

### Codebase Research
- Read-only mirror at `{mirrorDir}/` — use for understanding code, never edit
- Pull latest before major research: `git -C {mirrorDir} pull --ff-only`
- Use the mirror to write better work item descriptions

### Work Dispatch Flow
When the operator gives you a work request:
1. Research the codebase (mirror) to understand scope
2. Break the request into focused work items
3. Create items: `{sol} store create-item --world={world} --title="..." --description="..."`
4. Optionally group into a caravan:
   `{sol} caravan create --name="..." --item=<id1> --item=<id2> --world={world}`
5. Dispatch to available agents:
   `{sol} cast --world={world} --work-item=<id>`
6. Track progress: `{sol} status {world}`

### Available Commands
Full sol CLI reference for governor operations:

```
# Work Items
{sol} store create-item --world={world} --title="..." --description="..."
{sol} store list-items --world={world} [--state=open]

# Dispatch
{sol} cast --world={world} --work-item=<id> [--agent=<name>]

# Caravans
{sol} caravan create --name="..." --item=<id> [--item=<id>] --world={world}
{sol} caravan add-items <caravan-id> --item=<id> --world={world}
{sol} caravan check <caravan-id>
{sol} caravan status [--world={world}]
{sol} caravan launch <caravan-id> --world={world}

# Monitoring
{sol} status {world}
{sol} agent list --world={world}

# Communication
{sol} escalate --world={world} --agent=governor --message="..."
```

### Guidelines
- You coordinate — you don't write code
- Create focused, well-scoped work items (one concern per item)
- Include enough context in descriptions for an agent to work autonomously
- Check agent availability before dispatching (`sol agent list`)
- Use the mirror to verify your understanding of the codebase

Add `InstallGovernorClaudeMD` following the existing `Install*` pattern.

## Task 2: Update Governor Start to Use Protocol Generator

In `internal/governor/governor.go`, update `Start` to replace the placeholder
CLAUDE.md with the real protocol generator. Follow the same approach envoy uses
(from prompt 03/04).

## Task 3: Governor CLI — `cmd/governor.go`

Create `cmd/governor.go` with subcommands. Governor is a singleton per world
(like forge), so no "create" command — start handles registration.

### `sol governor start --world=<world> [--source-repo=<path>]`

- `--world` required
- `--source-repo` optional (defaults to world config)
- Calls `governor.Start()`
- Prints: `Started governor for world %q`

If `--source-repo` not provided, read from `config.LoadWorldConfig(world)`.
If neither set, error "source repo required".

### `sol governor stop --world=<world>`

- Calls `governor.Stop()`
- Prints: `Stopped governor for world %q`

### `sol governor attach --world=<world>`

- Attach to governor's tmux session
- Follow existing session attach pattern

### `sol governor brief --world=<world>`

- Display `.brief/memory.md` contents
- If file doesn't exist: "No brief found for governor in world %q"

### `sol governor debrief --world=<world>`

- Archive brief to `.brief/archive/{timestamp}.md` and reset
- Same archiving logic as envoy debrief (prompt 04)

### `sol governor refresh-mirror --world=<world>`

- Calls `governor.RefreshMirror(world)`
- Prints: `Refreshed mirror for world %q`
- This command is called by the SessionStart hook and can be run manually

### `sol governor summary --world=<world>`

- Display `.brief/world-summary.md` contents
- If file doesn't exist: "No world summary found for world %q"
- This is what the Senate (Arc 4) will use to read world context

### Command Registration

```go
func init() {
    rootCmd.AddCommand(governorCmd)
    governorCmd.AddCommand(governorStartCmd, governorStopCmd, governorAttachCmd,
        governorBriefCmd, governorDebriefCmd, governorRefreshMirrorCmd,
        governorSummaryCmd)
    // flags...
}
```

All subcommands: `SilenceUsage: true`.

## Task 4: Tests

### Protocol tests

- `TestGenerateGovernorClaudeMD` — verify contains world name, mirror reference,
  sol CLI commands, brief instructions, world summary format
- `TestInstallGovernorClaudeMD` — verify file written

### CLI tests

- `TestGovernorStartCommand` — verify governor directory created, session started
- `TestGovernorStopCommand` — verify session stopped
- `TestGovernorBriefCommand` — create brief file, verify output
- `TestGovernorDebriefCommand` — verify archive and reset
- `TestGovernorSummaryCommand` — create world-summary.md, verify output
- `TestGovernorRefreshMirrorCommand` — verify mirror updated (needs real git repo)

## Verification

- `make build && make test` passes

## Guidelines

- Governor is always named "governor" in the agents table — no custom names
- Mirror is read-only in the CLAUDE.md instructions — governor uses it for research
- The CLAUDE.md sol command reference should be comprehensive — governor needs
  to operate autonomously via CLI
- Follow forge CLI patterns for singleton commands

## Commit

```
feat(governor): add governor protocol generator and CLI commands
```

# Prompt 04: Arc 3 — Envoy Protocol + CLI

**Working directory:** ~/gt-src/
**Prerequisite:** Prompt 03 complete (envoy package exists at `internal/envoy/`)

## Context

Read these files before making changes:

- `internal/envoy/envoy.go` — envoy lifecycle (Create, Start, Stop, List)
- `internal/protocol/claudemd.go` — existing CLAUDE.md generators (outpost, forge, guided init)
- `internal/protocol/hooks.go` — existing hook installation
- `cmd/forge.go` — forge CLI pattern (singleton per world: start/stop/attach)
- `cmd/agent.go` — agent CLI pattern (create/list with flags)
- `cmd/session.go` — session attach pattern
- `internal/brief/brief.go` — brief package
- `docs/decisions/0009-envoy-design.md` — envoy design

## Task 1: Envoy CLAUDE.md Generator

Add to `internal/protocol/claudemd.go`:

```go
type EnvoyClaudeMDContext struct {
    AgentName string
    World     string
    SolBinary string // path to sol binary (for CLI references)
}

func GenerateEnvoyClaudeMD(ctx EnvoyClaudeMDContext) string
```

The generated CLAUDE.md should include:

### Identity
- You are an envoy — a persistent, context-aware agent in world "{world}"
- Your name is "{name}"
- You maintain accumulated context in `.brief/memory.md`

### Brief Maintenance
- Your brief (`.brief/memory.md`) is your persistent memory across sessions
- Keep it under 200 lines — consolidate older entries, focus on current state
- Update your brief before exiting with key decisions, current state, and next steps
- On startup, review your brief — it may be stale if your last session crashed
- Organize naturally: what matters now at the top, historical context below

### Work Flow — Three Modes
1. **Tethered work**: You may be assigned a work item. Check your tether:
   `cat $SOL_HOME/{world}/outposts/{name}/.tether` (if exists)
   When tethered, focus on that work item. Resolve when done.
2. **Self-service**: Create your own work item with
   `{sol} store create-item --world={world} --title="..." --description="..."`
   Then tether yourself (the operator or governor will handle this).
3. **Freeform**: No tether — exploration, research, design. No resolve needed.

### Resolving Work
When your tethered work is complete:
1. Ensure all changes are committed and pushed to your branch
2. Run `{sol} resolve --world={world} --agent={name}`
3. This creates a merge request through forge — your session stays alive
4. After resolve, reset your worktree for the next task:
   ```
   git checkout main && git pull
   ```
5. Update your brief with what you accomplished

### Available Commands
- `{sol} resolve --world={world} --agent={name}` — submit work for merge
- `{sol} store create-item --world={world} --title="..." --description="..."` — create work item
- `{sol} escalate --world={world} --agent={name} --message="..."` — escalate to operator
- `{sol} status {world}` — check world status
- `{sol} handoff --world={world} --from={name} --to=<agent> --message="..."` — hand off work

### Guidelines
- You are human-supervised — ask when uncertain
- All code goes through forge (merge pipeline) — never push to main directly
- Your worktree persists across sessions — keep it clean

Add an `InstallEnvoyClaudeMD` function that writes the generated content to
`{worktreeDir}/.claude/CLAUDE.md`, following the pattern of `InstallClaudeMD`.

## Task 2: Update Envoy Start to Use Protocol Generator

In `internal/envoy/envoy.go`, update the `Start` function to replace the
placeholder CLAUDE.md with the real protocol generator. Import and call
`protocol.InstallEnvoyClaudeMD`.

This means the envoy package needs to depend on the protocol package (or accept
the CLAUDE.md content as a parameter). Follow whichever pattern forge uses —
read `internal/forge/forge.go` to see how it gets its CLAUDE.md installed.

## Task 3: Envoy CLI — `cmd/envoy.go`

Create `cmd/envoy.go` with the following subcommands:

### `sol envoy create <name> --world=<world> [--source-repo=<path>]`

- `name` is a positional argument
- `--world` is required
- `--source-repo` is optional (defaults to world config's source_repo)
- Calls `envoy.Create()`
- Prints success: `Created envoy %q in world %q`

If `--source-repo` is not provided, read it from the world config
(`config.LoadWorldConfig(world)`). If neither is set, error with
"source repo required: set in world.toml or pass --source-repo".

### `sol envoy start <name> --world=<world>`

- Calls `envoy.Start()`
- Prints: `Started envoy %q in world %q`

### `sol envoy stop <name> --world=<world>`

- Calls `envoy.Stop()`
- Prints: `Stopped envoy %q in world %q`

### `sol envoy attach <name> --world=<world>`

- Looks up session name using the naming convention
- Attaches to tmux session using `mgr.Attach(sessionName)` or
  `syscall.Exec("tmux", ["tmux", "attach-session", "-t", sessionName], env)`
- Follow the pattern used in `cmd/session.go` for session attach

### `sol envoy list [--world=<world>]`

- If `--world` given, list envoys in that world
- If no world, list all envoys
- Output: table with NAME, WORLD, STATE, SESSION columns
- `--json` flag for JSON output

### `sol envoy brief <name> --world=<world>`

- Reads and displays the envoy's brief file
- Path: `envoy.BriefPath(world, name)`
- If file doesn't exist, print "No brief found for envoy %q"
- Simple cat-style output

### `sol envoy debrief <name> --world=<world>`

- Archives the current brief and resets it for a fresh start
- Archive path: `.brief/archive/{timestamp}.md` (RFC3339 timestamp in filename,
  replacing colons with dashes for filesystem safety)
- Move (rename) current `memory.md` to archive
- Print: `Archived brief to .brief/archive/{filename}`
- Print: `Envoy %q ready for fresh engagement`

### Command Registration

```go
func init() {
    rootCmd.AddCommand(envoyCmd)
    envoyCmd.AddCommand(envoyCreateCmd, envoyStartCmd, envoyStopCmd,
        envoyAttachCmd, envoyListCmd, envoyBriefCmd, envoyDebriefCmd)
    // flags...
}
```

All subcommands should have `SilenceUsage: true`.

## Task 4: Tests

### Protocol tests (add to `internal/protocol/protocol_test.go` or new file)

- `TestGenerateEnvoyClaudeMD` — verify output contains agent name, world name,
  resolve command, brief instructions
- `TestInstallEnvoyClaudeMD` — verify file written to correct path

### CLI tests (`cmd/envoy_test.go` or integration)

- `TestEnvoyCreateCommand` — verify envoy directory and agent record created
- `TestEnvoyListCommand` — create envoys, verify list output
- `TestEnvoyBriefCommand` — create brief file, verify output
- `TestEnvoyDebriefCommand` — create brief file, verify archived and original
  removed

For tests that need a git repo (create with worktree), use a real temp git repo
with at least one commit. Follow the pattern in existing integration test helpers.

## Verification

- `make build && make test` passes
- Smoke test (requires SOL_HOME with an initialized world and source repo):
  ```
  bin/sol envoy create scout --world=myworld
  bin/sol envoy list --world=myworld
  bin/sol envoy start scout --world=myworld
  # In another terminal: bin/sol envoy attach scout --world=myworld
  bin/sol envoy stop scout --world=myworld
  ```

## Guidelines

- Follow existing CLI patterns (forge for singleton, agent for create/list)
- `--world` flag is always required for envoy commands (envoys are per-world)
- Session naming: `config.SessionName(world, name)` → `sol-{world}-{name}`
- The CLAUDE.md should reference `sol` commands, not `bin/sol`
  (use the SolBinary from context or just hardcode "sol")
- Debrief archive uses RFC3339 timestamp with colons replaced by dashes

## Commit

```
feat(envoy): add envoy protocol generator and CLI commands
```

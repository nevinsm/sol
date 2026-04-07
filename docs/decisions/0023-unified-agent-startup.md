# ADR-0023: Unified Agent Startup and Role-Specific System Prompts

Status: Accepted

## Context

Agent session startup logic was duplicated across five locations: each
role's CLI start command (`cmd/forge.go`, `cmd/cast.go`, `cmd/governor.go`,
`cmd/envoy.go`) and the prefect's `respawnCommand()` switch. Each location
independently handled worktree setup, persona installation, hooks, config
directory isolation, workflow instantiation, prime context, and tmux session
creation.

This duplication caused real bugs:

- **Prefect respawn skipped `CLAUDE_CONFIG_DIR`.** A respawned agent used
  the autarch's personal Claude Code config directory, loading wrong
  auto-memory, settings, and project associations. The agent's behavior
  changed dramatically depending on whether it was started via CLI or
  respawned by the prefect.
- **Persona and hooks were not reinstalled on respawn.** If the worktree
  was reset between crash and respawn, the agent arrived without its
  CLAUDE.local.md or settings.local.json hooks.
- **No system prompt control.** All agents ran with Claude Code's default
  system prompt, which frames the agent as an interactive assistant.
  Autonomous roles (forge, outpost) received instructions to use plan mode,
  explore codebases, and ask user questions — all counterproductive. The
  persona in CLAUDE.local.md tried to override this, but degraded during
  context compaction while the system prompt persisted (re-injected every
  turn).
- **No workflow state preservation across handoffs.** When `PreCompact`
  triggered a handoff, the fresh session restarted from step 1 with no
  knowledge of in-flight work. Claimed MRs were orphaned.

## Decision

### Centralized startup with distributed role configuration

Introduce `internal/startup` — a package that owns the universal agent
session launch sequence. Role-specific behavior is provided by each role's
package via a `RoleConfig` struct with function fields:

```go
type RoleConfig struct {
    Role             string
    WorktreeDir      func(world, agent string) string
    Persona          func(world, agent string) ([]byte, error)
    Hooks            func(world, agent string) HookSet
    SystemPromptFile string
    SystemPromptContent string
    ReplacePrompt    bool
    Workflow         string
    NeedsItem        bool
    PrimeBuilder     func(world, agent string) string
}
```

The startup package owns the **sequence** (9 steps, always in order):

1. Verify worktree exists
2. Install persona (CLAUDE.local.md)
3. Install hooks (settings.local.json)
4. Ensure CLAUDE_CONFIG_DIR
5. Ensure agent record in sphere store
6. Instantiate workflow (if workflow set)
7. Build prime context
8. Build claude command (with system prompt flags)
9. Start tmux session with env vars

Role packages own the **content** — what persona, what prompt, what
workflow, what prime. The startup package never imports role packages;
roles register themselves via `startup.Register()` at init time.

This follows the `encoding/` pattern from the standard library: shared
contract, per-format implementations. We chose function fields over an
interface to avoid forcing every role to implement 10 methods — roles
only specify what differs.

### System prompt strategy: replace vs append

Claude Code supports `--system-prompt-file` (full replacement) and
`--append-system-prompt-file` (additive). We use both, split by role type:

**Full replacement** (`ReplacePrompt: true`) for autonomous roles:
- **Forge** — merge queue processor. The default system prompt's plan mode,
  codebase exploration, and interactive assistance instructions actively
  conflict with workflow-driven patrol behavior. Replacing eliminates the
  conflict at the source.
- **Outpost** — dispatched workers. Same conflict — outposts execute work
  items via workflow steps, not interactive assistance. No user to ask
  questions of.

Replacement prompts preserve useful parts of the default (tool usage
conventions, safety guidelines, output formatting) while removing
interactive framing and plan mode.

**Append mode** (`ReplacePrompt: false`) for interactive roles:
- **Governor** — human-directed coordinator. The default system prompt's
  interactive behavior is appropriate. Appended instructions add dispatch
  protocol, agent oversight, and governor constraints.
- **Envoy** — persistent human-directed agent. Same rationale — the
  interactive framing is desired. Appended instructions add memory protocol
  and resolve protocol.

This split is a deliberate design choice: autonomous agents need a
different operating frame than interactive ones. The system prompt is the
most durable instruction surface in Claude Code (re-injected every turn,
survives compaction), making it the right place to establish role identity.

### Resume state via file

When `PreCompact` triggers a handoff, the handoff command captures
workflow state to `.resume_state.json` in the agent's directory:

```go
type ResumeState struct {
    CurrentStep     string
    StepDescription string
    ClaimedResource string
    Reason          string
}
```

On respawn, the prefect checks for this file. If present, it calls
`startup.Resume()` instead of `startup.Launch()`. Resume adds `--continue`
(for conversation history) and prepends state context to the prime:
"You were on step N. Resume from there."

We chose file-based over DB-based storage because:
- State is per-agent, ephemeral, and consumed once
- Must survive process crashes (agent dies, prefect restarts)
- No DB transaction needed — write-read-delete lifecycle
- Cleanup is idempotent (delete file after use)

### Role registry

Roles register at init time in their cmd package:

```go
func init() {
    startup.Register("forge", forge.ForgeRoleConfig())
    startup.Register("agent", dispatch.OutpostRoleConfig())
    startup.Register("governor", governor.RoleConfig())
    startup.Register("envoy", envoy.RoleConfig())
}
```

The prefect resolves roles via `startup.ConfigFor(agent.Role)`. If found,
it uses `startup.Launch()` (or `Resume()` if state exists). If not found,
it falls back to a legacy command-based respawn. This allows non-Claude-Code
roles (sentinel) to remain outside the startup system.

## Consequences

- **Parity by construction.** CLI starts and prefect respawns go through
  the same `Launch()` function. There is no second code path to drift out
  of sync.
- **Adding a new role** requires implementing a `RoleConfig()` function
  and calling `startup.Register()`. The 9-step sequence is inherited.
- **System prompt changes** are version-controlled in
  `internal/protocol/prompts/`. Changes to forge behavior go in forge.md,
  not scattered across persona docs and prime context.
- **Context compaction resilience.** The system prompt survives compaction
  (re-injected every turn). Role identity is now durable, not dependent
  on CLAUDE.local.md staying in context.
- **Handoff continuity.** Forge can be mid-patrol with a claimed MR, hit
  context limits, and the fresh session knows exactly where it was.
- **The workflow loop problem remains.** The workflow system is linear —
  completed workflows stay "done." The forge patrol is a loop but the
  system doesn't re-instantiate done workflows. This is tracked separately.

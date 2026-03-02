# Prompt 07: Arc 3 — Resolve, Prefect, and Sentinel Adaptation

**Working directory:** ~/gt-src/
**Prerequisite:** Prompts 04 and 06 complete (envoy and governor exist)

## Context

Read these files carefully before making changes:

- `internal/dispatch/dispatch.go` — `Resolve` function (the full resolve flow),
  `autoProvision`, `SessionName`
- `internal/prefect/prefect.go` — `heartbeat()`, `respawnCommand()`,
  `worktreeForAgent()`, `getSentineledWorlds()`
- `internal/sentinel/sentinel.go` — `patrol()`, agent filtering
- `internal/envoy/envoy.go` — envoy directory helpers
- `internal/governor/governor.go` — governor directory helpers
- `internal/store/agents.go` — agent struct and role field
- `docs/decisions/0009-envoy-design.md` — envoy resolve behavior
- `docs/decisions/0010-governor-design.md` — governor supervision model

## Task 1: Modify Resolve for Envoy Role

In `internal/dispatch/dispatch.go`, modify the `Resolve` function to handle
`role=envoy` differently:

**What stays the same for envoy:**
- Git add, commit, push
- Create merge request
- Update work item status to "done"
- Update agent state to "idle"
- Clear tether

**What changes for envoy:**
- Do NOT kill the tmux session (skip the goroutine that sleeps + stops session)
- Do NOT call `workflow.Remove` (envoys don't use workflow system)

To implement this, you need to know the agent's role during resolve. Currently
`Resolve` takes `ResolveOpts` with World and AgentName. You'll need to look up
the agent record to check the role.

Read the resolve flow carefully. Find the point where the session is killed
(likely a goroutine with `time.Sleep` then `mgr.Stop`). Wrap that in a role
check:

```go
agent, err := sphereStore.GetAgent(opts.AgentName, opts.World)
// ... (early in the function, before the session kill decision)

// After work item update, MR creation, state update, tether clear:
if agent.Role != "envoy" {
    // Kill session (existing behavior)
    go func() {
        time.Sleep(1 * time.Second)
        mgr.Stop(sessName, true)
    }()
    workflow.Remove(world, agentName) // best-effort
}
```

The resolve result should indicate whether the session was kept alive. Add a
field if helpful:

```go
type ResolveResult struct {
    // existing fields...
    SessionKept bool // true if session was not killed (envoy resolve)
}
```

**Important:** Resolve must still work correctly for regular agents (role=agent).
Do not change the default behavior — only add the envoy exception.

## Task 2: Prefect Heartbeat — Skip Envoy and Governor

In `internal/prefect/prefect.go`, update the `heartbeat()` function to skip
agents with `role=envoy` and `role=governor`. These are human-supervised and
should not be auto-respawned.

Find the section where the prefect iterates over agents and checks session
health. Add a skip condition:

```go
// Skip human-supervised roles — envoys and governors are not auto-respawned
if agent.Role == "envoy" || agent.Role == "governor" {
    continue
}
```

This should go early in the loop, before the session health check and respawn
logic.

## Task 3: Prefect `respawnCommand` — Handle New Roles

Update `respawnCommand` in `internal/prefect/prefect.go` to handle the new
roles. Since envoy and governor are skipped in heartbeat, this is a safety net:

```go
func respawnCommand(agent store.Agent) string {
    switch agent.Role {
    case "sentinel":
        return fmt.Sprintf("sol sentinel run %s", agent.World)
    case "envoy", "governor":
        // Should never reach here — skipped in heartbeat.
        // But if it does, start a Claude session.
        return "claude --dangerously-skip-permissions"
    default:
        return "claude --dangerously-skip-permissions"
    }
}
```

## Task 4: Prefect `worktreeForAgent` — Handle New Roles

Update `worktreeForAgent` to return correct directories:

```go
func worktreeForAgent(agent store.Agent) string {
    switch agent.Role {
    case "forge":
        return forge.WorktreePath(agent.World)
    case "sentinel":
        return config.Home()
    case "envoy":
        return envoy.WorktreePath(agent.World, agent.Name)
    case "governor":
        return governor.GovernorDir(agent.World)
    default:
        return dispatch.WorktreePath(agent.World, agent.Name)
    }
}
```

This requires importing the envoy and governor packages. These imports should
be clean (no circular dependencies) since envoy/governor only depend on config
and store, not prefect.

## Task 5: Sentinel — Verify No Changes Needed

Read `internal/sentinel/sentinel.go` and confirm that the sentinel already
filters to `role == "agent"` only. Envoys and governors should be invisible
to sentinel health monitoring.

If sentinel does NOT filter by role, add the filter. But based on the existing
code survey, it already does:

```go
if a.Role == "agent" {
    activeAgents = append(activeAgents, a)
}
```

Verify this is correct and add a comment if not already present:

```go
// Only monitor outpost agents — envoys and governors are human-supervised.
```

## Task 6: Tests

### Resolve tests (in `internal/dispatch/` test file)

- `TestResolveEnvoyKeepsSession` — create an envoy agent (role=envoy), resolve
  its work item, verify session was NOT stopped and `SessionKept` is true
- `TestResolveAgentKillsSession` — resolve a regular agent (role=agent), verify
  session was stopped (existing behavior preserved)

### Prefect tests (in `internal/prefect/` test file)

- `TestHeartbeatSkipsEnvoy` — create envoy agent with dead session, verify
  prefect does NOT respawn it
- `TestHeartbeatSkipsGovernor` — create governor agent with dead session, verify
  prefect does NOT respawn it
- `TestWorktreeForEnvoy` — verify returns envoy worktree path
- `TestWorktreeForGovernor` — verify returns governor directory

### Sentinel tests

- `TestSentinelIgnoresEnvoy` — create envoy agent, verify sentinel patrol
  does not include it in active agents
- `TestSentinelIgnoresGovernor` — same for governor

## Verification

- `make build && make test` passes
- Existing dispatch tests still pass (resolve behavior for role=agent unchanged)
- Existing prefect tests still pass
- Existing sentinel tests still pass

## Guidelines

- Minimal changes to existing code — add role checks, don't restructure
- The envoy resolve change is the most critical — test it thoroughly
- Prefect and sentinel changes are defensive (skip new roles)
- Do not change forge or consul behavior

## Commit

```
feat(arc3): adapt resolve, prefect, and sentinel for envoy and governor roles
```

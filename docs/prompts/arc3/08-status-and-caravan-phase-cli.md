# Prompt 08: Arc 3 — Status Role-Aware Sections + Caravan Phase CLI

**Working directory:** ~/gt-src/
**Prerequisite:** Prompts 04 and 06 complete (envoy and governor roles exist)

## Context

Read these files before making changes:

- `internal/status/status.go` — `Gather`, `WorldStatus`, `AgentStatus`, interfaces
- `internal/status/sphere.go` — `GatherSphere`, `SphereStatus`, `WorldSummary`
- `internal/status/render.go` — `RenderWorld`, `RenderSphere`, lipgloss styles
- `internal/status/status_test.go` — existing test patterns and mocks
- `internal/status/sphere_test.go` — sphere test patterns
- `internal/status/render_test.go` — render test patterns
- `internal/envoy/envoy.go` — `BriefPath` for brief mtime
- `internal/governor/governor.go` — `GovernorDir`, `BriefPath`
- `cmd/caravan.go` — caravan create and add-items commands
- `internal/store/caravans.go` — `CreateCaravanItem` (now accepts phase)
- `docs/arc-roadmap.md` — Arc 3 status display section

## Task 1: Update WorldStatus for Role-Aware Agent Grouping

In `internal/status/status.go`, update `WorldStatus` to separate agents by role:

```go
type WorldStatus struct {
    World        string          `json:"world"`
    Prefect      PrefectInfo     `json:"prefect"`
    Forge        ForgeInfo       `json:"forge"`
    Sentinel     SentinelInfo    `json:"sentinel"`
    Governor     GovernorInfo    `json:"governor"`      // NEW
    Agents       []AgentStatus   `json:"agents"`        // role=agent only
    Envoys       []EnvoyStatus   `json:"envoys"`        // NEW
    MergeQueue   []MergeStatus   `json:"merge_queue"`
    Caravans     []CaravanInfo   `json:"caravans,omitempty"`
    Summary      Summary         `json:"summary"`
}
```

New types:

```go
type GovernorInfo struct {
    Running      bool   `json:"running"`
    SessionAlive bool   `json:"session_alive"`
    BriefAge     string `json:"brief_age,omitempty"` // human-readable age of brief
}

type EnvoyStatus struct {
    Name         string `json:"name"`
    State        string `json:"state"`
    SessionAlive bool   `json:"session_alive"`
    TetherItem   string `json:"tether_item,omitempty"`
    WorkTitle    string `json:"work_title,omitempty"`
    BriefAge     string `json:"brief_age,omitempty"` // age of .brief/memory.md
}
```

## Task 2: Update Gather for Role Separation

In the `Gather` function, after listing agents, separate them by role:

```go
for _, agent := range agents {
    switch agent.Role {
    case "governor":
        // Populate GovernorInfo
    case "envoy":
        // Build EnvoyStatus (similar to AgentStatus but with brief age)
    case "agent":
        // Existing AgentStatus logic
    // forge, sentinel, consul — already handled separately
    }
}
```

For brief age calculation, stat the brief file and compute the age from mtime:

```go
func briefAge(path string) string {
    info, err := os.Stat(path)
    if err != nil {
        return "" // no brief
    }
    return formatDuration(time.Since(info.ModTime()))
}
```

Use `envoy.BriefPath(world, name)` for envoys and `governor.BriefPath(world)`
for governor.

Governor health: check if governor agent exists AND session alive. Populate
`GovernorInfo` similar to how `ForgeInfo` and `SentinelInfo` are populated.

**Important:** Governor and envoy status do NOT affect `Health()`. The health
computation should only consider outpost agents (role=agent), prefect, forge,
and sentinel. This matches the ADR: envoys and governors are human-supervised.

## Task 3: Update GatherSphere for New Roles

In `internal/status/sphere.go`, update `WorldSummary` in `SphereStatus`:

```go
type WorldSummary struct {
    Name       string `json:"name"`
    SourceRepo string `json:"source_repo,omitempty"`
    Health     string `json:"health"`
    Agents     int    `json:"agents"`       // count of role=agent
    Envoys     int    `json:"envoys"`       // NEW: count of role=envoy
    Governor   bool   `json:"governor"`     // NEW: governor running?
    Working    int    `json:"working"`
    Idle       int    `json:"idle"`
    Dead       int    `json:"dead"`
}
```

Update `gatherWorldSummary` to count envoys and check governor status.

## Task 4: Update RenderWorld for Role Sections

In `internal/status/render.go`, update `RenderWorld` to render role-separated
sections. The new layout:

```
World: myworld [HEALTHY]

  Processes
    prefect    ● running
    forge      ● running (session: sol-myworld-forge)
    sentinel   ● running (session: sol-myworld-sentinel)
    governor   ● running (session: sol-myworld-governor)  brief: 2h ago

  Outposts (3)
    NAME     STATE    SESSION  WORK
    Toast    working  alive    Implement auth middleware
    Crisp    idle     alive    —
    Flint    working  dead     Fix CSS regression

  Envoys (1)
    NAME     STATE    SESSION  WORK           BRIEF
    Scout    working  alive    Design review  45m ago

  Merge Queue (2 ready, 1 claimed)
    ...

  Caravans (1 open)
    ...

  Summary: 3 agents, 1 envoy | 2 working, 1 idle, 1 dead
```

Rules:
- **Processes** section: add governor line (only if governor agent exists)
- **Outposts** section: only `role=agent` agents. Header shows count.
  Omit section entirely if no outpost agents.
- **Envoys** section: only `role=envoy` agents. Has BRIEF column showing
  brief age. Omit section entirely if no envoys.
- **Governor** brief age shown inline in Processes section
- **Summary** line: separate counts for agents and envoys

## Task 5: Update RenderSphere for New Columns

In the sphere overview worlds table, add envoy count and governor indicator:

```
Sol Sphere [HEALTHY]

  Processes
    prefect     ● running
    consul      ● running  patrol: 42, last: 5m ago
    chronicle   ● running

  Worlds (2)
    WORLD     AGENTS  ENVOYS  GOV  WORKING  IDLE  DEAD  HEALTH
    alpha     3       1       ●    2        1     0     healthy
    beta      2       0       —    1        0     1     degraded

  Caravans (1 open)
    ...
```

New columns: ENVOYS (count), GOV (● if running, — if not).

## Task 6: Caravan Phase in Status Display

Update caravan display in both `RenderWorld` and `RenderSphere` to show phase
info when phases > 0 exist:

```
  Caravans (1 open)
    auth-overhaul  3 items  phase 0: 2/2 done, phase 1: 0/1 ready
```

This requires `CaravanInfo` to include phase breakdown. Add:

```go
type PhaseProgress struct {
    Phase    int `json:"phase"`
    Total    int `json:"total"`
    Done     int `json:"done"`
    Ready    int `json:"ready"`
}

type CaravanInfo struct {
    ID     string          `json:"id"`
    Name   string          `json:"name"`
    Status string          `json:"status"`
    Items  int             `json:"items"`
    Phases []PhaseProgress `json:"phases,omitempty"` // NEW
}
```

## Task 7: Caravan Phase CLI Flags

In `cmd/caravan.go`, update caravan commands to support phases:

### `sol caravan create` — add `--phase` flag

Currently items are added with `--item=<id>`. Add `--phase=<n>` that applies
to all items in this creation (default 0):

```
sol caravan create --name="auth" --item=abc --item=def --phase=0 --world=myworld
```

For more granular phase assignment, use `sol caravan add-items`:

### `sol caravan add-items` — add `--phase` flag

```
sol caravan add-items <caravan-id> --item=<id> --phase=1 --world=myworld
```

Update the calls to `store.CreateCaravanItem` to pass the phase value.

### `sol caravan status` / `sol caravan check` — show phases

Update the display to include phase info when phases exist. In JSON output,
include the phase field in each item.

## Task 8: Tests

### Status tests

- `TestGatherWithGovernor` — create governor agent, verify GovernorInfo populated
- `TestGatherWithEnvoys` — create envoy agents, verify separated from outpost agents
- `TestGatherMixedRoles` — create agents of all roles, verify proper grouping
- `TestGatherEnvoyBriefAge` — create envoy with brief file, verify age populated
- `TestHealthIgnoresEnvoyGovernor` — envoy/governor dead sessions don't affect health
- `TestGatherSphereWithEnvoysAndGovernor` — verify sphere summary counts

### Render tests

- `TestRenderWorldWithEnvoys` — verify Envoys section with BRIEF column
- `TestRenderWorldWithGovernor` — verify governor in Processes section
- `TestRenderWorldNoEnvoys` — verify Envoys section omitted when empty
- `TestRenderSphereNewColumns` — verify ENVOYS and GOV columns in worlds table
- `TestRenderCaravanPhases` — verify phase progress display

### Caravan CLI tests

- `TestCaravanCreateWithPhase` — verify items created with correct phase
- `TestCaravanAddItemsWithPhase` — verify phase passed through
- `TestCaravanStatusShowsPhases` — verify phase info in output

## Verification

- `make build && make test` passes
- JSON output backward-compatible: new fields are additive (`envoys`, `governor`
  added to existing structure; old fields unchanged)
- Empty envoys/governor: verify no rendering artifacts when roles don't exist

## Guidelines

- Sections are omitted when empty — don't show "Envoys (0)" with empty table
- Governor and envoy health do NOT affect world/sphere health calculations
- Keep JSON output backward-compatible — only add new fields
- Brief age format: use the existing `formatDuration` helper
- The BRIEF column in the Envoys table shows time since last brief update

## Commit

```
feat(status): add role-aware sections and caravan phase display
```

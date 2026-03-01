# Prompt 07: Arc 2 — Status Sphere-Level Gathering

You are building the data-gathering layer for the sphere-level status
overview (`sol status` with no args). This prompt adds new types and a
`GatherSphere()` function. Rendering comes in prompt 08.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 06 is complete.

Read the existing code first. Understand:
- `internal/status/status.go` — `WorldStatus`, `Gather()`,
  `GatherCaravans()`, all existing types
- `internal/prefect/pidfile.go` — `ReadPID()`, `IsRunning()`
- `internal/consul/consul.go` — `ReadHeartbeat()`, `Heartbeat`,
  `IsStale()`
- `internal/dispatch/dispatch.go` — `SessionName()`
- `internal/store/worlds.go` — `ListWorlds()`, `World` type
- `internal/store/store.go` — `OpenWorld()`, `OpenSphere()`
- `cmd/status.go` — current `sol status <world>` implementation
- `cmd/world.go` — `worldStatusCmd` implementation

---

## Task 1: Sphere-Level Types

**Modify** `internal/status/status.go`.

Add new types for sphere-level status. Place them alongside the existing
world-level types.

```go
// SphereStatus holds the complete runtime state for the sphere.
type SphereStatus struct {
    SOLHome    string          `json:"sol_home"`
    Prefect    PrefectInfo     `json:"prefect"`
    Consul     ConsulInfo      `json:"consul"`
    Chronicle  ChronicleInfo   `json:"chronicle"`
    Worlds     []WorldSummary  `json:"worlds"`
    Caravans   []CaravanInfo   `json:"caravans,omitempty"`
    Health     string          `json:"health"`
}

// ConsulInfo holds consul process state.
type ConsulInfo struct {
    Running       bool   `json:"running"`
    HeartbeatAge  string `json:"heartbeat_age,omitempty"` // human-readable duration
    PatrolCount   int    `json:"patrol_count,omitempty"`
    Stale         bool   `json:"stale"`
}

// WorldSummary holds a condensed view of one world for the sphere overview.
type WorldSummary struct {
    Name       string `json:"name"`
    SourceRepo string `json:"source_repo,omitempty"`
    Agents     int    `json:"agents"`      // total registered agents
    Working    int    `json:"working"`     // agents in working state
    Idle       int    `json:"idle"`        // agents in idle state
    Stalled    int    `json:"stalled"`     // agents in stalled state
    Dead       int    `json:"dead"`        // working agents with dead sessions
    Forge      bool   `json:"forge"`       // forge session alive
    Sentinel   bool   `json:"sentinel"`    // sentinel session alive
    MRReady    int    `json:"mr_ready"`    // merge requests in ready phase
    MRFailed   int    `json:"mr_failed"`   // merge requests in failed phase
    Health     string `json:"health"`      // "healthy", "unhealthy", "degraded"
}
```

---

## Task 2: GatherSphere Function

**Add** to `internal/status/status.go` (or create a new file
`internal/status/sphere.go` if the file is getting long — your
judgment):

```go
// SphereStore abstractions needed for GatherSphere.
// Extend the existing SphereStore interface or create a new one:

// WorldLister abstracts world listing for sphere status.
type WorldLister interface {
    ListWorlds() ([]store.World, error)
}

// GatherSphere collects runtime state for the entire sphere.
//
// Parameters:
//   - worldLister: for listing registered worlds
//   - checker: for checking session liveness
//   - worldOpener: function to open a world store (for per-world stats)
//
// The function degrades gracefully: if any per-world query fails,
// that world gets partial data rather than causing the whole gather
// to fail.
func GatherSphere(worldLister WorldLister, checker SessionChecker,
    worldOpener func(string) (*store.Store, error),
    caravanStore CaravanStore) *SphereStatus {

    result := &SphereStatus{
        SOLHome: config.Home(),
    }

    // 1. Check prefect.
    pid, err := prefect.ReadPID()
    if err == nil && pid != 0 && prefect.IsRunning(pid) {
        result.Prefect = PrefectInfo{Running: true, PID: pid}
    }

    // 2. Check consul.
    result.Consul = gatherConsulInfo(checker)

    // 3. Check chronicle.
    const chronicleSessionName = "sol-chronicle"
    if checker.Exists(chronicleSessionName) {
        result.Chronicle = ChronicleInfo{Running: true, SessionName: chronicleSessionName}
    }

    // 4. Gather per-world summaries.
    worlds, err := worldLister.ListWorlds()
    if err == nil {
        for _, w := range worlds {
            summary := gatherWorldSummary(w, checker, worldOpener)
            result.Worlds = append(result.Worlds, summary)
        }
    }

    // 5. Gather open caravans (sphere-wide).
    if caravanStore != nil {
        caravans, err := caravanStore.ListCaravans("open")
        if err == nil {
            for _, c := range caravans {
                items, err := caravanStore.ListCaravanItems(c.ID)
                if err != nil {
                    continue
                }
                info := CaravanInfo{
                    ID:         c.ID,
                    Name:       c.Name,
                    Status:     c.Status,
                    TotalItems: len(items),
                }
                statuses, err := caravanStore.CheckCaravanReadiness(c.ID, worldOpener)
                if err == nil {
                    for _, st := range statuses {
                        switch {
                        case st.WorkItemStatus == "done" || st.WorkItemStatus == "closed":
                            info.DoneItems++
                        case st.WorkItemStatus == "open" && st.Ready:
                            info.ReadyItems++
                        }
                    }
                }
                result.Caravans = append(result.Caravans, info)
            }
        }
    }

    // 6. Compute sphere health.
    result.Health = computeSphereHealth(result)

    return result
}
```

### Helper functions

```go
func gatherConsulInfo(checker SessionChecker) ConsulInfo {
    info := ConsulInfo{}

    // Check consul session.
    // Consul session name pattern: "sol-sphere-consul" or similar.
    // Read the actual consul session name from the codebase.
    // Check consul heartbeat file.
    hb, err := consul.ReadHeartbeat()
    if err == nil {
        info.Running = true
        info.PatrolCount = hb.PatrolCount

        age := time.Since(hb.Timestamp)
        info.HeartbeatAge = formatDuration(age)
        info.Stale = hb.IsStale(10 * time.Minute) // 10 min default
    }

    return info
}

func gatherWorldSummary(w store.World, checker SessionChecker,
    worldOpener func(string) (*store.Store, error)) WorldSummary {

    summary := WorldSummary{
        Name:       w.Name,
        SourceRepo: w.SourceRepo,
    }

    // Check forge and sentinel sessions.
    forgeSess := dispatch.SessionName(w.Name, "forge")
    summary.Forge = checker.Exists(forgeSess)

    sentinelSess := dispatch.SessionName(w.Name, "sentinel")
    summary.Sentinel = checker.Exists(sentinelSess)

    // Open world store for agent and MR counts (non-fatal if fails).
    ws, err := worldOpener(w.Name)
    if err != nil {
        summary.Health = "unknown"
        return summary
    }
    defer ws.Close()

    // Get merge request counts.
    mrs, err := ws.ListMergeRequests("")
    if err == nil {
        for _, mr := range mrs {
            switch mr.Phase {
            case "ready":
                summary.MRReady++
            case "failed":
                summary.MRFailed++
            }
        }
    }

    // Open sphere store for agent counts.
    // Note: we need a SphereStore interface here. The caller should
    // pass in the sphere store or the agent data.
    // Alternative: accept a SphereStore parameter too.
    //
    // Actually, rethink the signature. GatherSphere already has
    // access to the sphere store through worldLister. Add a
    // SphereStore parameter for agent queries.

    summary.Health = "healthy"
    if summary.MRFailed > 0 {
        summary.Health = "unhealthy"
    }

    return summary
}

// formatDuration formats a duration as a human-readable string.
func formatDuration(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    if d < time.Hour {
        return fmt.Sprintf("%dm", int(d.Minutes()))
    }
    if d < 24*time.Hour {
        return fmt.Sprintf("%dh", int(d.Hours()))
    }
    return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func computeSphereHealth(s *SphereStatus) string {
    if !s.Prefect.Running {
        return "degraded"
    }
    for _, w := range s.Worlds {
        if w.Health == "unhealthy" || w.Dead > 0 {
            return "unhealthy"
        }
    }
    if s.Consul.Stale {
        return "degraded"
    }
    return "healthy"
}
```

**Important:** The implementation above is a starting point. Read the
actual consul and prefect code to get the correct session names,
heartbeat file locations, and staleness thresholds. Adapt the
`gatherConsulInfo` and `gatherWorldSummary` functions to match the
actual APIs.

The key design constraint: **GatherSphere must degrade gracefully.**
If a world's database can't be opened, if consul has no heartbeat, if
a session check fails — the function fills in what it can and moves on.
Never return an error from GatherSphere.

---

## Task 3: Agent Counts in World Summary

`GatherSphere` needs agent counts per world. The cleanest approach is
to accept the sphere store (which has `ListAgents`) as a parameter:

Update the `GatherSphere` signature to include a `SphereStore`
parameter (using the existing `SphereStore` interface):

```go
func GatherSphere(sphereStore SphereStore, worldLister WorldLister,
    checker SessionChecker,
    worldOpener func(string) (*store.Store, error),
    caravanStore CaravanStore) *SphereStatus {
```

Then in `gatherWorldSummary`, pass the sphere store and query agents:

```go
func gatherWorldSummary(w store.World, sphereStore SphereStore,
    checker SessionChecker,
    worldOpener func(string) (*store.Store, error)) WorldSummary {

    // ... existing code ...

    // Agent counts.
    agents, err := sphereStore.ListAgents(w.Name, "")
    if err == nil {
        summary.Agents = len(agents)
        for _, a := range agents {
            switch a.State {
            case "working":
                summary.Working++
                sessName := dispatch.SessionName(w.Name, a.Name)
                if !checker.Exists(sessName) {
                    summary.Dead++
                }
            case "idle":
                summary.Idle++
            case "stalled":
                summary.Stalled++
            }
        }
    }

    // ... rest of code ...
}
```

---

## Task 4: Tests

**Create** `internal/status/sphere_test.go` (or add to existing test
file).

Use mock implementations of the interfaces:

```go
// mockWorldLister returns a fixed list of worlds.
type mockWorldLister struct {
    worlds []store.World
}
func (m *mockWorldLister) ListWorlds() ([]store.World, error) {
    return m.worlds, nil
}

// mockChecker returns pre-configured session existence.
type mockChecker struct {
    alive map[string]bool
}
func (m *mockChecker) Exists(name string) bool {
    return m.alive[name]
}
```

### Test cases

```go
func TestGatherSphereEmpty(t *testing.T)
    // No worlds registered.
    // Verify: result has empty Worlds slice.
    // Verify: Health is computed (may be "degraded" if prefect isn't running).

func TestGatherSphereWithWorlds(t *testing.T)
    // Register 2 worlds in mock.
    // Verify: result.Worlds has 2 entries.
    // Verify: world names match.

func TestGatherSphereProcessChecks(t *testing.T)
    // Mock: chronicle session alive, forge sessions alive for one world.
    // Verify: Chronicle.Running = true.
    // Verify: World[0].Forge = true, World[1].Forge = false.

func TestSphereHealthComputation(t *testing.T)
    // Healthy: prefect running, no dead sessions, consul fresh.
    // Degraded: prefect not running.
    // Degraded: consul stale.
    // Unhealthy: world has dead sessions.

func TestWorldSummaryDegrades(t *testing.T)
    // Provide a worldOpener that returns an error.
    // Verify: summary has Health="unknown", not a panic or error.

func TestFormatDuration(t *testing.T)
    // Test various durations:
    // 30s → "30s"
    // 5m → "5m"
    // 2h → "2h"
    // 3d → "3d"
```

---

## Task 5: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Verify with:
   ```bash
   go test ./internal/status/ -v -run Sphere
   ```

---

## Guidelines

- `GatherSphere` never returns an error. It always returns a
  `*SphereStatus` with whatever data it could collect. This follows
  the DEGRADE principle — partial information is better than no
  information.
- Per-world data gathering opens and closes world stores individually.
  If one world's database is corrupted, only that world shows
  "unknown" health — others are unaffected.
- The consul info uses the heartbeat file, not a session check. Consul
  is a Go process (not a tmux session), so session-based liveness
  doesn't apply. The heartbeat age is the canonical signal.
- The `formatDuration` helper produces compact strings suitable for
  table columns ("5m", "2h", "3d"). Don't use `time.Duration.String()`
  which produces verbose output like "5m30.123456s".
- Agent state counting in `WorldSummary` mirrors the logic in
  `Gather()` for consistency. Dead = working agent with dead session.
- Role-aware sections (outposts vs. envoys vs. governor) are
  explicitly NOT in Arc 2. All agents are counted uniformly. Arc 3
  will add role filtering when envoy and governor roles exist.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(status): add sphere-level status gathering`

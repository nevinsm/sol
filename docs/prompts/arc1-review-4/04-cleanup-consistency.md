# Prompt 04: Arc 1 Review-4 — Cleanup and Consistency

You are fixing cleanup items, consistency issues, and removing dead code found during the
fourth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 03 of arc1-review-4 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/dispatch/dispatch.go` — fire-and-forget goroutine, assignee sentinel
- `internal/consul/consul.go` — `log.Printf` usage, event metadata types
- `internal/forge/toolbox.go` — `RunGates` (no timeout)
- `internal/forge/toolbox_test.go` — empty `TestPushGitOps`
- `internal/store/schema.go` — `var` vs `const` for schema strings
- `test/integration/loop5_test.go` — unused `fmt.Sprintf` import hack
- `test/integration/loop1_test.go` — redundant binary build in `TestFlockSerialization`
- `test/integration/cli_test.go` — `gtBin` function

---

## Task 1: Replace consul stdlib log with events logger

**File:** `internal/consul/consul.go`

The consul uses `log.Printf` for operational logging while the rest of the codebase uses
`events.Logger`. This makes consul output invisible in the event feed.

**Fix:** The consul already has a `logger *events.Logger` field. Replace all `log.Printf`
calls with `d.logger.Log()` calls, guarded by a nil check.

Create a helper method to keep it clean:

```go
func (d *Consul) logInfo(eventType string, meta map[string]any) {
    if d.logger != nil {
        d.logger.Log(eventType, meta)
    }
}
```

Then replace each `log.Printf` with the appropriate event log. Examples:

```go
// Line 180 — error recovery:
// Before: log.Printf("consul: stale tether recovery error: %v", err)
// After:
d.logInfo("consul_error", map[string]any{"action": "stale_tether_recovery", "error": err.Error()})

// Line 229 — patrol summary:
// Before: log.Printf("[%s] Patrol #%d: ...")
// After:
d.logInfo("consul_patrol", map[string]any{
    "patrol_count":  d.patrolCount,
    "stale_tethers": staleTethers,
    // ... other fields as ints, not formatted strings
})
```

Important: pass integer values as `int`, not as `fmt.Sprintf("%d", ...)`. The sentinel
passes ints directly — match that pattern. Fix the existing event metadata on lines 222-225
that format ints as strings.

Remove the `"log"` import when done. Replace all ~11 `log.Printf` calls.

If there are log calls that are purely for stderr debugging during development (not
operational events), convert them to `fmt.Fprintf(os.Stderr, ...)` instead. But most
consul log calls are operational events that belong in the event feed.

---

## Task 2: Add timeout to forge RunGates

**File:** `internal/forge/toolbox.go`, lines 63-90

Quality gate commands run with no timeout. A hanging test suite or build blocks the forge
indefinitely. The sentinel has a 30-second timeout for AI assessment — gates need similar.

**Fix:** Add a context with timeout to each gate execution. Use a configurable timeout
from the forge config, with a sensible default.

First, check `internal/config/world_config.go` for the `ForgeSection` struct. Add a
`GateTimeout` field if it doesn't exist:

```go
type ForgeSection struct {
    QualityGates []string `toml:"quality_gates"`
    TargetBranch string   `toml:"target_branch"`
    GateTimeout  string   `toml:"gate_timeout"` // duration string, e.g. "5m"
}
```

In `DefaultWorldConfig`, set the default:

```go
Forge: ForgeSection{
    TargetBranch: "main",
    GateTimeout:  "5m",
},
```

Then in `RunGates`, parse the timeout and apply it:

```go
func (r *Forge) RunGates() ([]GateResult, error) {
    timeout := 5 * time.Minute // default
    if r.cfg.GateTimeout != "" {
        parsed, err := time.ParseDuration(r.cfg.GateTimeout)
        if err == nil {
            timeout = parsed
        }
    }

    var results []GateResult
    for _, gate := range r.cfg.QualityGates {
        start := time.Now()
        ctx, cancel := context.WithTimeout(context.Background(), timeout)
        cmd := exec.CommandContext(ctx, "sh", "-c", gate)
        cmd.Dir = r.worktree
        cmd.Env = append(os.Environ(),
            "SOL_HOME="+config.Home(),
            "SOL_WORLD="+r.world,
        )
        output, err := cmd.CombinedOutput()
        cancel()

        result := GateResult{
            Gate:     gate,
            Passed:   err == nil,
            Output:   truncate(string(output), 4096),
            Duration: time.Since(start),
        }
        if err != nil {
            result.Error = err.Error()
        }
        results = append(results, result)
    }
    return results, nil
}
```

Add `"context"` to imports if needed.

If the `ForgeSection` struct doesn't have room for `GateTimeout` or the config layer
resists the change, hardcode `5 * time.Minute` as the timeout and leave a `// TODO:`
comment for making it configurable.

---

## Task 3: Remove empty TestPushGitOps

**File:** `internal/forge/toolbox_test.go`, lines 317-320

This test does nothing — it runs `git --version` and discards the result.

**Fix:** Delete the entire test function. It gives false confidence in coverage.

---

## Task 4: Fix fire-and-forget goroutine in Resolve

**File:** `internal/dispatch/dispatch.go`, lines 592-596 and 669-672

Two fire-and-forget goroutines launch `mgr.Stop` in the background with no error handling
or tracking. These can outlive shutdown and trigger race detector warnings in tests.

**Fix:** Keep the delayed-stop behavior (the 1-second delay is intentional — it lets the
resolve command finish printing output before the session is killed). But log errors:

```go
// Lines 592-596:
go func() {
    time.Sleep(1 * time.Second)
    if err := mgr.Stop(sessName, true); err != nil {
        fmt.Fprintf(os.Stderr, "resolve: failed to stop session %s: %v\n", sessName, err)
    }
}()
```

Apply the same pattern to lines 669-672 (the `resolveConflictResolution` goroutine).

---

## Task 5: Clean up unused import hack

**File:** `test/integration/loop5_test.go`, line 1273

```go
var _ = fmt.Sprintf
```

This is a leftover hack to prevent the `fmt` import from being removed.

**Fix:** Check if `fmt` is actually used elsewhere in the file. If yes, remove only this
line. If no, remove both this line and the `"fmt"` import.

---

## Task 6: Use gtBin in TestFlockSerialization

**File:** `test/integration/loop1_test.go`, lines 157-162

This test builds its own binary instead of using the shared `gtBin(t)` helper from
`cli_test.go`, causing redundant compilation.

**Fix:** Replace the manual build with `gtBin(t)`:

```go
// Before:
binary := filepath.Join(t.TempDir(), "sol")
buildCmd := exec.Command("go", "build", "-o", binary, "github.com/nevinsm/sol")
if out, err := buildCmd.CombinedOutput(); err != nil {
    t.Fatalf("failed to build: %s: %s", err, out)
}

// After:
binary := gtBin(t)
```

Verify that `gtBin` is accessible from `loop1_test.go` (it should be — both are in
`package integration_test` in the same directory).

---

## Task 7: Fix schema var/const inconsistency

**File:** `internal/store/schema.go`

`worldSchemaV5` (line 68) and `sphereSchemaV6` (line 209) are declared as `var` while all
other schema definitions use `const`.

**Fix:** Change both to `const`:

```go
// Line 68:
// Before: var worldSchemaV5 = `...`
// After:  const worldSchemaV5 = `...`

// Line 209:
// Before: var sphereSchemaV6 = `...`
// After:  const sphereSchemaV6 = `...`
```

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify no `log.Printf` in consul: `grep -n "log.Printf" internal/consul/consul.go` returns nothing
- Verify `"log"` import removed from consul: `grep '"log"' internal/consul/consul.go` returns nothing
- Verify no remaining empty tests: `grep -A3 "func Test.*t \*testing.T" internal/forge/toolbox_test.go`
  should show no test bodies that are just `exec.Command("git", "--version")`

## Commit

```
fix(consul,forge,dispatch,test): arc 1 review-4 — structured logging, gate timeout, cleanup
```

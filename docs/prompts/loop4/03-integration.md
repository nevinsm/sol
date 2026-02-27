# Prompt 03: Loop 4 — Integration and Acceptance

You are wiring the Loop 4 components (workflow engine, caravans) into the
existing dispatch pipeline and verifying the complete Loop 4 feature set
with integration tests.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 4 prompts 01 and 02 are complete.

Read all existing code first. Understand:
- `internal/dispatch/dispatch.go` — `Cast()`, `Prime()`, `Done()`
- `internal/protocol/claudemd.go` — CLAUDE.md generation
- `internal/workflow/` — the new workflow engine (prompt 01)
- `internal/store/caravans.go`, `dependencies.go` — caravan/deps (prompt 02)
- `cmd/cast.go`, `cmd/prime.go`, `cmd/done.go` — existing CLI

Read `docs/target-architecture.md` Loop 4 definition of done and Section
3.10 (Outpost — propulsion loop, crash recovery).

---

## Task 1: Extend Cast for Workflow Instantiation

When `--formula` is provided, `Cast` should instantiate a workflow for
the assigned agent after creating the worktree.

### SlingOpts Extension

In `internal/dispatch/dispatch.go`, extend `SlingOpts`:

```go
type SlingOpts struct {
    WorkItemID  string
    World         string
    AgentName   string            // optional
    SourceRepo  string
    Formula     string            // optional: formula name for workflow
    Variables   map[string]string // optional: workflow variables
}
```

### Cast Flow Change

In the `Cast` function, after step 8 (install tethers) and before step 9
(start session), add workflow instantiation:

```go
// 8b. Instantiate workflow if formula provided.
if opts.Formula != "" {
    vars := opts.Variables
    if vars == nil {
        vars = map[string]string{}
    }
    // Always set "issue" variable to the work item ID.
    if _, ok := vars["issue"]; !ok {
        vars["issue"] = opts.WorkItemID
    }
    if _, err := workflow.Instantiate(opts.World, agent.Name, opts.Formula, vars); err != nil {
        rollback()
        return nil, fmt.Errorf("failed to instantiate workflow %q: %w", opts.Formula, err)
    }
}
```

This creates the `.workflow/` directory inside the agent's outpost dir
before the session starts. When `sol prime` fires on session start, it
will find the workflow and inject step context.

### SlingResult Extension

Add the workflow info to the result:

```go
type SlingResult struct {
    WorkItemID  string
    AgentName   string
    SessionName string
    WorktreeDir string
    Formula     string // empty if no workflow
}
```

### CLI Extension

In `cmd/cast.go`, add flags:

```go
castCmd.Flags().String("formula", "", "Workflow formula to instantiate")
castCmd.Flags().StringSlice("var", nil, "Workflow variable (key=val, repeatable)")
```

Parse `--var` flags into a `map[string]string` (split on first `=`).
Pass to `SlingOpts`.

---

## Task 2: Extend Prime for Workflow Context

When an agent has an active workflow, `Prime` should inject the current
step instructions instead of just the work item description.

### Prime Changes

In `internal/dispatch/dispatch.go`, modify `Prime()`:

```go
func Prime(world, agentName string, worldStore WorldStore) (*PrimeResult, error) {
    // Forge gets special context (unchanged).
    if agentName == "forge" {
        return primeRefinery(world)
    }

    // Read the tether file.
    workItemID, err := tether.Read(world, agentName)
    if err != nil {
        return nil, fmt.Errorf("failed to read tether: %w", err)
    }
    if workItemID == "" {
        return &PrimeResult{Output: "No work tethered"}, nil
    }

    // Get the work item.
    item, err := worldStore.GetWorkItem(workItemID)
    if err != nil {
        return nil, fmt.Errorf("failed to get work item %q: %w", workItemID, err)
    }

    // Check for active workflow.
    state, err := workflow.ReadState(world, agentName)
    if err != nil {
        return nil, fmt.Errorf("failed to read workflow state: %w", err)
    }

    if state != nil && state.Status == "running" {
        return primeWithWorkflow(world, agentName, item, state)
    }

    // No workflow — standard prime (existing behavior).
    output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Work Item: %s
Title: %s
Status: %s

Description:
%s

Instructions:
Execute this work item. When complete, run: sol resolve
If stuck, run: sol escalate "description"
=== END CONTEXT ===`, agentName, world, item.ID, item.Title, item.Status, item.Description)

    return &PrimeResult{Output: output}, nil
}
```

### Workflow Prime

```go
func primeWithWorkflow(world, agentName string, item *store.WorkItem,
    state *workflow.State) (*PrimeResult, error) {

    step, err := workflow.ReadCurrentStep(world, agentName)
    if err != nil {
        return nil, fmt.Errorf("failed to read current step: %w", err)
    }
    if step == nil {
        // Workflow exists but no current step — treat as complete.
        return &PrimeResult{
            Output: fmt.Sprintf("Workflow complete for %s. Run: sol resolve", item.ID),
        }, nil
    }

    // Count progress.
    completed := len(state.Completed)
    instance, _ := workflow.ReadInstance(world, agentName)
    formula := ""
    if instance != nil {
        formula = instance.Formula
    }

    output := fmt.Sprintf(`=== WORK CONTEXT ===
Agent: %s (world: %s)
Work Item: %s
Title: %s

Workflow: %s (step %d/%d+%d: %s)

--- CURRENT STEP ---
%s
--- END STEP ---

Propulsion loop:
1. Execute the step above
2. When done: sol workflow advance --world=%s --agent=%s
3. Check progress: sol workflow status --world=%s --agent=%s
4. After final step: sol resolve
=== END CONTEXT ===`,
        agentName, world, item.ID, item.Title,
        formula, completed+1, completed, 1, step.Title,
        step.Instructions,
        world, agentName, world, agentName)

    return &PrimeResult{Output: output}, nil
}
```

The key difference from standard prime: instead of a generic
"Execute this work item" instruction, the agent gets the specific step
markdown and the propulsion loop commands.

---

## Task 3: Extend CLAUDE.md for Workflow Agents

When a workflow is instantiated, the CLAUDE.md should include workflow
commands in the agent's protocol.

### ClaudeMDContext Extension

In `internal/protocol/claudemd.go`, extend `ClaudeMDContext`:

```go
type ClaudeMDContext struct {
    AgentName   string
    World         string
    WorkItemID  string
    Title       string
    Description string
    HasWorkflow bool // if true, include workflow commands
}
```

### GenerateClaudeMD Changes

When `HasWorkflow` is true, add workflow commands to the Commands and
Protocol sections:

```go
func GenerateClaudeMD(ctx ClaudeMDContext) string {
    workflowSection := ""
    if ctx.HasWorkflow {
        workflowSection = fmt.Sprintf(`
## Workflow Commands
- ` + "`sol workflow current --world=%s --agent=%s`" + ` — Read current step instructions
- ` + "`sol workflow advance --world=%s --agent=%s`" + ` — Mark step complete, advance to next
- ` + "`sol workflow status --world=%s --agent=%s`" + ` — Check progress
`, ctx.World, ctx.AgentName, ctx.World, ctx.AgentName, ctx.World, ctx.AgentName)
    }

    // ... existing CLAUDE.md content ...
    // Include workflowSection after ## Commands
    // Update ## Protocol to reference the propulsion loop when HasWorkflow
}
```

When `HasWorkflow` is true, the Protocol section should say:

```markdown
## Protocol
1. Read your current step: `sol workflow current --world=<world> --agent=<name>`
2. Execute the step instructions.
3. When the step is complete: `sol workflow advance --world=<world> --agent=<name>`
4. Repeat from step 1 until all steps are done.
5. When the workflow is complete, run `sol resolve`.
```

### Cast Wiring

In the `Cast` function, set `HasWorkflow` on the `ClaudeMDContext`:

```go
ctx := protocol.ClaudeMDContext{
    AgentName:   agent.Name,
    World:         opts.World,
    WorkItemID:  opts.WorkItemID,
    Title:       item.Title,
    Description: item.Description,
    HasWorkflow: opts.Formula != "",
}
```

---

## Task 4: Extend Done for Workflow Cleanup

When an agent with a workflow calls `sol resolve`, clean up the workflow
directory.

In `internal/dispatch/dispatch.go`, in the `Done` function, after
clearing the tether file (step 6) and before stopping the session (step 7):

```go
// 6b. Clean up workflow if present.
if _, err := workflow.ReadState(opts.World, opts.AgentName); err == nil {
    workflow.Remove(opts.World, opts.AgentName) // best-effort cleanup
}
```

This is best-effort — if cleanup fails, the directory is harmless and
can be cleaned manually. The `.workflow/` directory lives in the
outpost's dir, which may also be cleaned by worktree removal.

---

## Task 5: Wire Caravan Launch to Dispatch

In `cmd/caravan.go`, the `launch` subcommand should dispatch ready items
via the dispatch package:

```go
// For each ready item in the target world:
for _, item := range readyItems {
    slingOpts := dispatch.SlingOpts{
        WorkItemID: item.WorkItemID,
        World:        world,
        SourceRepo: sourceRepo,
    }
    if formula != "" {
        slingOpts.Formula = formula
        slingOpts.Variables = vars
    }
    result, err := dispatch.Cast(slingOpts, worldStore, sphereStore, mgr, logger)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Warning: failed to cast %s: %v\n",
            item.WorkItemID, err)
        continue // best-effort: skip failures, dispatch what we can
    }
    fmt.Printf("Dispatched %s → %s\n", item.WorkItemID, result.AgentName)
}
```

After dispatching, call `TryCloseConvoy` to auto-close if all items are
done.

---

## Task 6: Event Instrumentation

Emit events for new Loop 4 operations. Add event type constants to
`internal/events/events.go`:

```go
const (
    EventWorkflowInstantiate = "workflow_instantiate"
    EventWorkflowAdvance     = "workflow_advance"
    EventWorkflowComplete    = "workflow_complete"
    EventConvoyCreated       = "convoy_created"
    EventConvoyLaunched      = "convoy_launched"
    EventConvoyClosed        = "convoy_closed"
)
```

Add formatter cases in `cmd/feed.go`'s `formatEventDescription`:

```go
case events.EventWorkflowInstantiate:
    return fmt.Sprintf("Workflow %s instantiated for %s", get("formula"), get("work_item_id"))
case events.EventWorkflowAdvance:
    return fmt.Sprintf("Advanced to step: %s (%s)", get("step"), get("work_item_id"))
case events.EventWorkflowComplete:
    return fmt.Sprintf("Workflow complete: %s", get("work_item_id"))
case events.EventConvoyCreated:
    return fmt.Sprintf("Caravan created: %s (%s items)", get("name"), get("count"))
case events.EventConvoyLaunched:
    return fmt.Sprintf("Caravan launched: %s dispatched in %s", get("dispatched"), get("world"))
case events.EventConvoyClosed:
    return fmt.Sprintf("Caravan closed: %s", get("name"))
```

Emit from:
- `Cast()` — after workflow instantiation (if formula provided)
- `workflow.Advance()` — on step advance and workflow completion
- `caravan create` CLI — on caravan creation
- `caravan launch` CLI — after dispatch
- `TryCloseConvoy()` — on auto-close

Pass the logger through function parameters, same pattern as existing
code. Nil logger is always safe.

---

## Task 7: Integration Tests

Create `test/integration/loop4_test.go`:

### Workflow Integration Tests

```go
func TestWorkflowInstantiateAndAdvance(t *testing.T)
    // 1. Create temp SOL_HOME with formula directory
    // 2. Instantiate workflow
    // 3. ReadCurrentStep → first step
    // 4. Advance → second step
    // 5. Advance → third step
    // 6. Advance → resolve
    // 7. ReadState → status="done"

func TestWorkflowCrashRecovery(t *testing.T)
    // 1. Instantiate workflow, advance to step 2
    // 2. Simulate crash: delete in-memory state (but state.json persists)
    // 3. ReadState from disk → current_step is step 2
    // 4. ReadCurrentStep → step 2 instructions
    // 5. Advance → step 3 (workflow resumed correctly)

func TestSlingWithWorkflow(t *testing.T)
    // 1. Set up world + sphere stores, session mock, formula
    // 2. Cast with formula="default-work"
    // 3. Verify .workflow/ directory created in agent's outpost dir
    // 4. Verify state.json exists with current_step set
    // 5. Verify CLAUDE.md includes workflow commands

func TestPrimeWithWorkflow(t *testing.T)
    // 1. Set up stores, tether, and instantiated workflow
    // 2. Call Prime()
    // 3. Verify output contains current step instructions
    // 4. Verify output contains propulsion loop commands

func TestPrimeWithoutWorkflow(t *testing.T)
    // 1. Set up stores and tether, no workflow
    // 2. Call Prime()
    // 3. Verify output is standard format (no workflow section)
    // 4. Backwards-compatible with existing behavior

func TestDoneWithWorkflowCleanup(t *testing.T)
    // 1. Set up stores, tether, workflow, mock session
    // 2. Call Done()
    // 3. Verify .workflow/ directory is removed
```

### Caravan Integration Tests

```go
func TestConvoyCreateAndCheck(t *testing.T)
    // 1. Create work items with dependencies: A, B, C where C→A and C→B
    // 2. Create caravan with all 3
    // 3. CheckConvoyReadiness → A and B ready, C blocked
    // 4. Mark A as "done"
    // 5. CheckConvoyReadiness → B ready, C still blocked (B not done)
    // 6. Mark B as "done"
    // 7. CheckConvoyReadiness → C now ready

func TestConvoyAutoClose(t *testing.T)
    // 1. Create caravan with 2 items (no deps)
    // 2. Mark both items as "closed"
    // 3. TryCloseConvoy → returns true
    // 4. GetConvoy → status="closed", closed_at set

func TestConvoyMultiRig(t *testing.T)
    // 1. Create items in rig1 and rig2
    // 2. Create caravan spanning both worlds
    // 3. CheckConvoyReadiness → correct status from both worlds
```

### End-to-End Workflow Test

```go
func TestWorkflowPropulsionLoop(t *testing.T)
    // Simulate the full agent propulsion loop:
    // 1. Create work item, create formula with 3 steps
    // 2. Cast with formula (mock session)
    // 3. Prime → get step 1 instructions
    // 4. workflow advance → step 2
    // 5. Prime again → get step 2 instructions (crash recovery sim)
    // 6. workflow advance → step 3
    // 7. workflow advance → complete
    // 8. Done → workflow cleaned up, work item marked done
```

### CLI Smoke Tests

Extend `test/integration/cli_loop4_test.go` with remaining CLI tests:

```go
func TestCLISlingFormulaHelp(t *testing.T)
    // Verify --formula and --var flags appear in cast help
```

---

## Task 8: Acceptance Checklist

Create `docs/prompts/loop4/acceptance.md`:

```markdown
# Loop 4 Acceptance Checklist

## Workflow Engine
- [ ] `sol workflow instantiate` creates .workflow/ with manifest.json,
      state.json, and step files
- [ ] `sol workflow current` outputs current step's rendered markdown
- [ ] `sol workflow advance` marks step complete and moves to next ready step
- [ ] `sol workflow status` shows progress (human and JSON output)
- [ ] DAG dependencies work (branching steps, not just linear)
- [ ] Variable substitution ({{var}}) works in step instructions
- [ ] Cycle detection rejects circular step dependencies
- [ ] Default default-work formula extracted from embedded defaults

## Dispatch Integration
- [ ] `sol cast --formula=default-work` instantiates workflow during dispatch
- [ ] `sol prime` injects workflow step instructions when workflow is active
- [ ] `sol prime` falls back to standard output when no workflow exists
- [ ] `sol resolve` cleans up .workflow/ directory
- [ ] CLAUDE.md includes workflow commands when formula is provided
- [ ] Workflow state survives simulated crash (state.json persistence)

## Caravans
- [ ] `sol caravan create` creates caravan record in sphere.db
- [ ] `sol caravan add` adds items to existing caravan
- [ ] `sol caravan check` shows ready vs blocked items
- [ ] `sol caravan status` shows summary of all open caravans
- [ ] `sol caravan launch` dispatches ready items via cast
- [ ] Caravan auto-closes when all items are done/closed
- [ ] Multi-world caravans work (items from different worlds)

## Dependencies
- [ ] `sol store dep add` creates dependency relationship
- [ ] `sol store dep list` shows deps and dependents
- [ ] Cycle detection rejects circular dependencies
- [ ] IsReady checks dependency status correctly

## Conflict Resolution (existing, verify still works)
- [ ] Complex merge conflict → forge creates resolution task
- [ ] `sol resolve --force-with-lease` unblocks original MR
- [ ] `sol forge check-unblocked` auto-unblocks resolved MRs

## Events
- [ ] Workflow events (instantiate, advance, complete) emitted
- [ ] Caravan events (created, launched, closed) emitted
- [ ] `sol feed` formats new event types correctly

## Build
- [ ] `make build` succeeds
- [ ] `make test` passes (all packages)
- [ ] `go vet ./...` clean
```

---

## Task 9: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. `go vet ./...` — clean
4. Walk through the acceptance checklist manually
5. Commit with message:
   `feat: integrate workflows and caravans into dispatch pipeline`

---

## Guidelines

- **Backwards compatibility is critical.** Cast, Prime, and Done must
  work exactly as before when no formula is provided. All existing tests
  must pass unchanged.
- The workflow is **optional** — an agent can be cast without a formula
  and everything works like Loop 0-3. The formula is an enhancement, not
  a requirement.
- `sol prime` is the crash recovery mechanism. When a session crashes and
  restarts, `sol prime` reads `state.json` from disk and re-injects the
  current step. The agent doesn't know it crashed — it just gets
  instructions (GUPP + CRASH principles).
- Caravan `launch` is best-effort — if one item fails to cast (e.g., no
  idle agent), the others still dispatch. Print warnings for failures.
- Event emission follows the same nil-logger-safe pattern as existing
  code.
- Don't over-engineer the status integration — a simple count summary
  in `sol status` output is sufficient for Loop 4.
- All existing tests must continue to pass.

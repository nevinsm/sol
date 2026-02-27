# Prompt 03: Arc 1 — Hard Gate Enforcement

You are wiring the world lifecycle gate into every world-scoped command.
After this prompt, no command can operate on a world that hasn't been
initialized with `sol world init`.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 prompts 01 and 02 are complete (config types,
schema V5, world store CRUD, RequireWorld, world CLI commands).

Read all existing code first. Understand:
- `internal/config/config.go` — `RequireWorld()` gate function
- All `cmd/*.go` files — identify every command that takes a world arg

---

## Task 1: Hard Gate — All World-Scoped Commands

Add `config.RequireWorld(world)` as the **first check** after
extracting the world name in each command's `RunE`. This is explicit —
no middleware, no cobra `PersistentPreRunE` chaining.

The pattern for each command:

```go
// Right after extracting world name:
if err := config.RequireWorld(world); err != nil {
    return err
}
```

Add the import `"github.com/nevinsm/sol/internal/config"` to any
file that doesn't already have it.

### Commands to gate

The following is the complete list of commands that take a world
argument. Add the `RequireWorld` check to each one.

**`cmd/cast.go`** (1 command):
- `castCmd` — world = `args[1]`

**`cmd/status.go`** (1 command):
- `statusCmd` — world = `args[0]`

**`cmd/forge.go`** (14 commands):
- `forgeStartCmd` — world = `args[0]`
- `forgeStopCmd` — world = `args[0]`
- `forgeAttachCmd` — world = `args[0]`
- `forgeQueueCmd` — world = `args[0]`
- `forgeReadyCmd` — world = `args[0]`
- `forgeBlockedCmd` — world = `args[0]`
- `forgeClaimCmd` — world = `args[0]`
- `forgeReleaseCmd` — world = `args[0]`
- `forgeRunGatesCmd` — world = `args[0]`
- `forgePushCmd` — world = `args[0]`
- `forgeMarkMergedCmd` — world = `args[0]`
- `forgeMarkFailedCmd` — world = `args[0]`
- `forgeCreateResCmd` — world = `args[0]`
- `forgeCheckUnblockedCmd` — world = `args[0]`

**`cmd/sentinel.go`** (4 commands):
- `sentinelRunCmd` — world = `args[0]`
- `sentinelStartCmd` — world = `args[0]`
- `sentinelStopCmd` — world = `args[0]`
- `sentinelAttachCmd` — world = `args[0]`

**`cmd/store.go`** (6 commands):
- `storeCreateCmd` — world from `cmd.Flag("world")`
- `storeGetCmd` — world from `cmd.Flag("world")`
- `storeListCmd` — world from `cmd.Flag("world")`
- `storeUpdateCmd` — world from `cmd.Flag("world")`
- `storeCloseCmd` — world from `cmd.Flag("world")`
- `storeQueryCmd` — world from `cmd.Flag("world")`

**`cmd/store_dep.go`** (3 commands):
- `storeDepAddCmd` — world from `cmd.Flag("world")`
- `storeDepRemoveCmd` — world from `cmd.Flag("world")`
- `storeDepListCmd` — world from `cmd.Flag("world")`

**`cmd/agent.go`** (2 commands):
- `agentCreateCmd` — world = `agentCreateWorld`
- `agentListCmd` — world = `agentListWorld`

**`cmd/prime.go`** (1 command):
- `primeCmd` — world = `primeWorld`

**`cmd/workflow.go`** (4 commands):
- `wfInstantiateCmd` — world = `wfWorld`
- `wfCurrentCmd` — world = `wfWorld`
- `wfAdvanceCmd` — world = `wfWorld`
- `wfStatusCmd` — world = `wfWorld`

**`cmd/resolve.go`** (1 command):
- `resolveCmd` — world resolved from flag or `SOL_WORLD` env var

**`cmd/handoff.go`** (1 command):
- `handoffCmd` — world resolved from flag or `SOL_WORLD` env var

**`cmd/caravan.go`** (3 commands — gate only when world is provided):
- `caravanCreateCmd` — world = `caravanWorld`, only gate if non-empty
  (world is optional on create when no items are being added)
- `caravanAddCmd` — world = `caravanWorld` (required)
- `caravanLaunchCmd` — world = `caravanWorld` (required)

**Total: ~40 insertion points across 11 files.**

### Placement rules

- For commands that validate `world == ""` before proceeding, add
  `RequireWorld` right after the empty check (the gate only fires for
  non-empty world names).
- For positional arg commands (`world := args[0]`), add `RequireWorld`
  immediately after the assignment.
- For `resolve` and `handoff` which fall back to `SOL_WORLD` env var,
  add `RequireWorld` after the final world value is determined (after
  the env var fallback logic).
- For caravan commands where world is optional, wrap the gate:
  ```go
  if caravanWorld != "" {
      if err := config.RequireWorld(caravanWorld); err != nil {
          return err
      }
  }
  ```

---

## Task 2: Tests

Add integration tests to `test/integration/hard_gate_test.go`:

```go
func TestHardGateStoreCreate(t *testing.T)
    // No world initialized
    // Run: sol store create --world=noworld --title="test"
    // Error output contains "does not exist"

func TestHardGateStoreGet(t *testing.T)
    // Run: sol store get sol-00000000 --world=noworld
    // Error output contains "does not exist"

func TestHardGateCast(t *testing.T)
    // Run: sol cast sol-00000000 noworld
    // Error output contains "does not exist"

func TestHardGateForgeQueue(t *testing.T)
    // Run: sol forge queue noworld
    // Error output contains "does not exist"

func TestHardGateStatus(t *testing.T)
    // Run: sol status noworld
    // Error output contains "does not exist"

func TestHardGatePreArc1World(t *testing.T)
    // Create DB manually (world exists in store but no world.toml)
    // Run: sol store create --world=legacy --title="test"
    // Error output contains "before world lifecycle"

func TestHardGatePassesAfterInit(t *testing.T)
    // Run: sol world init myworld
    // Run: sol store create --world=myworld --title="test"
    // → succeeds (gate passes)
```

Use the existing test helpers from `test/integration/helpers_test.go`:
`setupTestEnv`, `runGT`, `gtBin`.

---

## Task 3: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Catch-all verification — no ungated `OpenWorld` calls remain:
   ```bash
   grep -rn 'store.OpenWorld\|OpenWorld(' cmd/*.go | grep -v RequireWorld
   ```
   Every call to `OpenWorld` in `cmd/` should be preceded by a
   `RequireWorld` check in the same function. Review any matches to
   ensure nothing was missed.
4. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   mkdir -p /tmp/sol-test/.store

   # Uninitiated world should fail
   bin/sol store create --world=noworld --title="test" 2>&1
   # → error: "does not exist"

   # Init and try again
   bin/sol world init myworld
   bin/sol store create --world=myworld --title="test"
   # → succeeds

   # Forge command also gated
   bin/sol forge queue noworld 2>&1
   # → error: "does not exist"

   # Status also gated
   bin/sol status noworld 2>&1
   # → error: "does not exist"
   ```
5. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The hard gate is **explicit per-command** — no middleware, no
  `PersistentPreRunE` chaining. This is intentional: each command's
  `RunE` shows exactly what validation happens. Middleware-based
  approaches are fragile with nested cobra command groups.
- Do NOT modify any behavior beyond adding the `RequireWorld` check.
  No config loading, no source repo changes — that's prompt 04.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(world): enforce world init gate on all world-scoped commands`

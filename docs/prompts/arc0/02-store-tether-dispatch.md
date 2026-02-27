# Arc 0, Prompt 2: Store, Tether, and Dispatch Rename

## Context

We are renaming the `gt` system to `sol`. Prompt 1 renamed the module, binary, and environment variables. This prompt renames the core data layer and dispatch pipeline — the concepts that agents interact with directly.

Read `docs/naming.md` for the full naming glossary.

**Important:** After prompt 1, the module is `github.com/nevinsm/sol`, the binary is `bin/sol`, and env vars are `SOL_HOME`/`SOL_WORLD`/`SOL_AGENT`.

## What To Change

### 1. Hook Package → Tether Package

Rename the `internal/hook/` directory to `internal/tether/`.

```bash
git mv internal/hook internal/tether
```

In `internal/tether/` (all files in the package):
- Package declaration: `package hook` → `package tether`
- `HookPath()` → `TetherPath()`
- `IsHooked()` → `IsTethered()`
- `Read()`, `Write()`, `Clear()` — keep names (they're generic)
- All path references: `".hook"` → `".tether"`
- All path references: `"polecats"` → `"outposts"`
- All comments referencing "hook" in the gt sense → "tether"
- All comments referencing `$GT_HOME` → `$SOL_HOME` (if any remain)

Update all consumers:
- Every file that imports `"github.com/nevinsm/sol/internal/hook"` → `"github.com/nevinsm/sol/internal/tether"`
- Every call to `hook.HookPath(...)` → `tether.TetherPath(...)`
- Every call to `hook.IsHooked(...)` → `tether.IsTethered(...)`
- Every call to `hook.Read(...)` → `tether.Read(...)`
- Every call to `hook.Write(...)` → `tether.Write(...)`
- Every call to `hook.Clear(...)` → `tether.Clear(...)`

### 2. Store Renames

**File:** `internal/store/store.go`

- `OpenRig()` → `OpenWorld()`
- `OpenTown()` → `OpenSphere()`
- `"town.db"` → `"sphere.db"`
- All comments: "rig database" → "world database", "town database" → "sphere database"
- All comments: `$GT_HOME` → `$SOL_HOME`

**File:** `internal/store/workitems.go`

- ID prefix: `"gt-"` → `"sol-"` in the ID generation function

**File:** `internal/store/convoys.go`

- ID prefix: `"convoy-"` → `"car-"` in the ID generation function
- `Convoy` struct → `Caravan`
- `ConvoyItem` struct → `CaravanItem`
- `ConvoyItemStatus` type → `CaravanItemStatus`
- All methods on these types: update receiver names and comments
- All store methods: `CreateConvoy` → `CreateCaravan`, `GetConvoy` → `GetCaravan`, `ListConvoys` → `ListCaravans`, `AddConvoyItem` → `AddCaravanItem`, etc. (rename all convoy-related methods)

**File:** `internal/store/messages.go`

- `PolecatDonePayload` → `AgentDonePayload`
- `ConvoyNeedsFeedingPayload` → `CaravanNeedsFeedingPayload`
- Any protocol message type constants referencing old names: `POLECAT_DONE` → check if this is a string constant and update
- `CONVOY_NEEDS_FEEDING` → `CARAVAN_NEEDS_FEEDING` (if it's a string constant used as a protocol message type)

**File:** `internal/store/escalations.go`

- Check for any references to old naming in comments

**File:** `internal/store/schema.go`

- Comments only — table/column names in SQL DDL stay as-is (they're generic: `agents`, `work_items`, `convoys`, `convoy_items`)
- **BUT** if there are SQL references to `"convoys"` and `"convoy_items"` table names, those stay (they're database table names, renaming them would require a migration). The Go types change but the SQL table names remain.

### 3. Dispatch Renames

**File:** `internal/dispatch/dispatch.go`

- `Sling()` → `Cast()`
- `SlingResult` → `CastResult`
- `SlingOpts` → `CastOpts`
- `Done()` → `Resolve()`
- `DoneOpts` → `ResolveOpts`
- `DoneResult` → `ResolveResult`
- `TownStore` interface → `SphereStore`
- `RigStore` interface → `WorldStore`
- Session name format: `fmt.Sprintf("gt-%s-%s", rig, agentName)` → `fmt.Sprintf("sol-%s-%s", rig, agentName)`
- Directory paths: `"polecats"` → `"outposts"`
- **Worktree directory**: The final `"rig"` path component in `WorktreePath()` → `"worktree"` (this is the literal directory name for the git worktree checkout, NOT the rig concept)
- Branch convention: `"polecat/"` → `"outpost/"` in branch name generation (e.g., `fmt.Sprintf("polecat/%s/%s", ...)` → `fmt.Sprintf("outpost/%s/%s", ...)`)
- Agent role: `"polecat"` → `"agent"` (hardcoded role string when auto-provisioning agents)
- All `rig` parameter names → `world` (function parameters, local variables named `rig`)
- All comments: "rig" → "world", "sling" → "cast", "polecat" → "outpost"
- All error messages referencing "rig" → "world"

**CAUTION:** The `rig` parameter rename must be done carefully. Only rename the parameter/variable, not unrelated uses of "rig" (which shouldn't exist in this file, but check). The function `RigDir()` in config is used but is renamed separately in config.go.

### 4. Config Updates

**File:** `internal/config/config.go`

- `RigDir()` → `WorldDir()` (function name and comment)
- Parameter: `rig string` → `world string`

### 5. Protocol / CLAUDE.md Generation

**File:** `internal/protocol/claudemd.go`

This file generates the CLAUDE.md that gets injected into agent sessions. It contains many hardcoded command references. Update all of them:

- `gt done` → `sol resolve`
- `gt workflow current` → `sol workflow current`
- `gt workflow advance` → `sol workflow advance`
- `gt workflow status` → `sol workflow status`
- `gt escalate` → `sol escalate`
- `gt handoff` → `sol handoff`
- All references to `GT_HOME` → `SOL_HOME`
- All references to `GT_RIG` → `SOL_WORLD`
- All references to `GT_AGENT` → `SOL_AGENT`
- References to "rig" in generated text → "world"
- References to "hook" in generated text → "tether"
- References to "polecat" in generated text → "outpost"
- `--rig` flags in generated commands → `--world`
- Refinery-related generated content: `gt refinery` → `sol forge` (command references in refinery CLAUDE.md)

### 6. CLI Flag and Argument Renames

**All cmd files** that have `--rig` flags:

- `--rig` → `--world` (flag name)
- Flag help text: `"rig name..."` → `"world name..."`
- Variable names: `xxxRig` → `xxxWorld` (e.g., `slingRig` → `castWorld`, `doneRig` → `resolveWorld`)
- Positional argument descriptions in `Use` field: where `<rig>` appears → `<world>`

Files that likely have `--rig` flags (check all `cmd/*.go` files):
- `cmd/sling.go` — positional arg `<rig>` and/or `--rig` flag
- `cmd/done.go` — `--rig` flag (already partially updated in prompt 1 for env var)
- `cmd/prime.go` — `--rig` flag
- `cmd/agent.go` — `--rig` flag
- `cmd/store.go` — `--db` flag (this is the rig name, but flag name is `--db` — leave as-is or consider `--world`)
- `cmd/supervisor.go` — flags
- `cmd/refinery.go` — positional `<rig>` args
- `cmd/witness.go` — positional `<rig>` args
- `cmd/status.go` — positional `<rig>` arg
- `cmd/workflow.go` — `--rig` flag
- `cmd/convoy.go` — `--rig` flag
- `cmd/mail.go` — flags
- `cmd/feed.go` — flags
- `cmd/curator.go` — flags
- `cmd/deacon.go` — flags
- `cmd/handoff.go` — flags
- `cmd/escalate.go` — flags

For `cmd/store.go`: The `--db` flag is used to specify which rig/world database. Rename to `--world` for consistency.

### 7. Other Internal Packages

Update `TownStore`/`RigStore` interface references in:
- `internal/supervisor/supervisor.go` — `TownStore` → `SphereStore`
- `internal/refinery/refinery.go` — `TownStore` → `SphereStore`, `RigStore` → `WorldStore`
- `internal/witness/witness.go` — `TownStore` → `SphereStore`, `RigStore` → `WorldStore`
- `internal/deacon/deacon.go` — `TownStore` → `SphereStore`
- `internal/handoff/handoff.go` — check for store interface references
- `internal/status/status.go` — check for store interface references
- `internal/events/*.go` — check for store interface references

Also update `rig` parameter names → `world` in these packages where they refer to the rig concept.

**File:** `internal/refinery/refinery.go`
- `RefineryWorktreePath()` — the final `"rig"` path component → `"worktree"`
- `rig` parameters → `world`

### 8. Test Updates

Update all test files for the changes made in this prompt:
- Import path: `"github.com/nevinsm/sol/internal/hook"` → `"github.com/nevinsm/sol/internal/tether"`
- All `hook.` calls → `tether.` calls
- All `store.OpenRig()` → `store.OpenWorld()`
- All `store.OpenTown()` → `store.OpenSphere()`
- All `dispatch.Sling(...)` → `dispatch.Cast(...)`
- All `dispatch.Done(...)` → `dispatch.Resolve(...)`
- All type references: `dispatch.SlingResult` → `dispatch.CastResult`, etc.
- All `store.Convoy` → `store.Caravan`, etc.
- All `--rig` in CLI test commands → `--world`
- All `--db` in CLI test commands → `--world`
- All `"polecats"` path references → `"outposts"`
- All `".hook"` path references → `".tether"`
- All `"polecat/"` branch references → `"outpost/"`
- ID assertions: `"gt-"` prefix → `"sol-"` prefix
- Convoy ID assertions: `"convoy-"` prefix → `"car-"` prefix
- `t.Setenv("GT_HOME"...)` should already be `SOL_HOME` from prompt 1
- Rig variable names in tests: `rig` → `world` where they represent the concept
- Helper function `openStores` comments: "rig store" → "world store", "town store" → "sphere store"
- Makefile `test-e2e` target: `polecats` → `outposts`, `.hook` → `.tether`, `--rig` → `--world`

## What NOT To Change (Yet)

These are handled in later prompts:
- Package directories: `internal/supervisor/`, `internal/refinery/`, `internal/witness/`, `internal/deacon/` (prompt 3)
- Command names: `sling`, `done`, `supervisor`, `refinery`, `witness`, `curator`, `deacon`, `convoy` (prompt 3)
- Cmd file names: `cmd/sling.go`, `cmd/done.go`, etc. (prompt 3)
- Exported types in process packages: `Supervisor`, `Refinery`, `Witness`, `Deacon`, `Curator` (prompt 3)
- Workflow formula: `polecat-work` → `default-work` (prompt 3)
- Documentation files (prompt 4)

**Note on SQL table names:** The database tables `convoys` and `convoy_items` keep their SQL names. Only the Go types change. This avoids needing a schema migration. The Go types `Caravan`/`CaravanItem` map to SQL tables `convoys`/`convoy_items`.

## Acceptance Criteria

```bash
make build && make test         # passes
bin/sol cast --help             # still works (command not renamed yet, but binary is sol)
bin/sol store create --world=testrig --title="test"  # --world flag works
grep -rn 'OpenTown\|OpenRig' --include='*.go' .      # no hits
grep -rn '"\.hook"' --include='*.go' .                # no hits
grep -rn '"polecats"' --include='*.go' .              # no hits
grep -rn '"gt-"' --include='*.go' .                   # no hits
grep -rn '"town\.db"' --include='*.go' .              # no hits
grep -rn '"polecat"' --include='*.go' .               # no hits (except maybe "polecat-work" which is prompt 3)
grep -rn -- '--rig' --include='*.go' cmd/             # no hits
```

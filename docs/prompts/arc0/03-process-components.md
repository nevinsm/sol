# Arc 0, Prompt 3: Process Component Rename

## Context

We are renaming the `gt` system to `sol`. Prompts 1-2 renamed the module, binary, environment, store, tether (hook), and dispatch. This prompt renames all the supervisory/monitoring process packages, their CLI commands, and the workflow defaults.

Read `docs/naming.md` for the full naming glossary.

**State after prompt 2:** Module is `github.com/nevinsm/sol`, binary is `bin/sol`, env vars are `SOL_HOME`/`SOL_WORLD`/`SOL_AGENT`, store uses `OpenWorld`/`OpenSphere`, dispatch uses `Cast`/`Resolve`, hook is now tether, flags use `--world`, paths use `outposts/` and `.tether`.

## What To Change

### 1. Package Directory Renames

Rename these directories using `git mv`:

```bash
git mv internal/supervisor internal/prefect
git mv internal/refinery internal/forge
git mv internal/witness internal/sentinel
git mv internal/deacon internal/consul
```

**Note:** `internal/events/` stays as-is (the package name `events` is generic). Only the curator types within it are renamed.

### 2. Prefect (was Supervisor)

**Package:** `internal/prefect/` (was `internal/supervisor/`)

- Package declaration: `package supervisor` → `package prefect`
- `Supervisor` struct → `Prefect`
- `supervisor.Config` → `prefect.Config`
- `supervisor.TownStore` → already `prefect.SphereStore` (renamed in prompt 2)
- `supervisor.SessionManager` → `prefect.SessionManager`
- All methods on `Supervisor` → methods on `Prefect` (receiver rename)
- `New()` function — update return type
- `NewLogger()`, `WritePID()`, `ReadPID()`, `ClearPID()`, `IsRunning()`, `DefaultConfig()` — keep names (generic)
- All comments: "supervisor" → "prefect"
- All session name patterns: `"gt-supervisor"` → `"sol-prefect"` (if any)
- All `rig` variable names → `world` (if any remain from prompt 2)

**File:** `cmd/supervisor.go` → rename to `cmd/prefect.go`

```bash
git mv cmd/supervisor.go cmd/prefect.go
```

- Cobra `Use`: `"supervisor"` → `"prefect"`
- Cobra `Short`: update description
- Variable names: `supervisorCmd` → `prefectCmd`
- All subcommand registrations
- Session names in start/stop logic
- `rootCmd.AddCommand(supervisorCmd)` → `rootCmd.AddCommand(prefectCmd)`

### 3. Forge (was Refinery)

**Package:** `internal/forge/` (was `internal/refinery/`)

- Package declaration: `package refinery` → `package forge`
- `Refinery` struct → `Forge`
- `refinery.Config` → `forge.Config`
- `refinery.GateResult` → `forge.GateResult`
- `RefineryWorktreePath()` → `forge.WorktreePath()` (drop the "Refinery" prefix since the package name provides context)
- `RefineryBranch()` → `forge.ForgeBranch()`
- `refinery.TownStore` → already `forge.SphereStore`
- `refinery.RigStore` → already `forge.WorldStore`
- `LoadQualityGates()`, `DefaultConfig()`, `New()` — keep names (generic)
- `EnsureWorktree()` — keep name
- All methods on `Refinery` → methods on `Forge`
- All comments: "refinery" → "forge"
- Directory path: `"refinery"` → `"forge"` in path construction (e.g., `filepath.Join(config.Home(), world, "forge", "worktree")`)
- Session name: `"gt-refinery-%s"` → `"sol-forge-%s"`

**File:** `cmd/refinery.go` → rename to `cmd/forge.go`

```bash
git mv cmd/refinery.go cmd/forge.go
```

- Cobra `Use`: `"refinery"` → `"forge"`
- Variable names: `refineryCmd` → `forgeCmd`
- All subcommand Use fields: `"start <rig>"` → `"start <world>"`, etc.
- All subcommand variable names
- Session names
- `rootCmd.AddCommand(refineryCmd)` → `rootCmd.AddCommand(forgeCmd)`

### 4. Sentinel (was Witness)

**Package:** `internal/sentinel/` (was `internal/witness/`)

- Package declaration: `package witness` → `package sentinel`
- `Witness` struct → `Sentinel`
- `witness.Config` → `sentinel.Config`
- `witness.AssessmentResult` → `sentinel.AssessmentResult`
- `witness.TownStore` → already `sentinel.SphereStore`
- `witness.RigStore` → already `sentinel.WorldStore`
- `witness.SessionChecker` → `sentinel.SessionChecker`
- All methods on `Witness` → methods on `Sentinel`
- All comments: "witness" → "sentinel"
- Session name: `"gt-witness-%s"` → `"sol-sentinel-%s"`

**File:** `cmd/witness.go` → rename to `cmd/sentinel.go`

```bash
git mv cmd/witness.go cmd/sentinel.go
```

- Cobra `Use`: `"witness"` → `"sentinel"`
- Variable names: `witnessCmd` → `sentinelCmd`
- All subcommand Use fields
- Session names
- `rootCmd.AddCommand(witnessCmd)` → `rootCmd.AddCommand(sentinelCmd)`

### 5. Chronicle (was Curator)

**Package:** `internal/events/` (directory stays, only types rename)

- `Curator` struct → `Chronicle`
- `CuratorConfig` → `ChronicleConfig`
- `CuratorOption` → `ChronicleOption`
- `DefaultCuratorConfig()` → `DefaultChronicleConfig()`
- `NewCurator()` → `NewChronicle()`
- `WithLogger()` — keep (generic)
- All methods on `Curator` → methods on `Chronicle`
- All comments: "curator" → "chronicle"

**File:** `cmd/curator.go` → rename to `cmd/chronicle.go`

```bash
git mv cmd/curator.go cmd/chronicle.go
```

- Cobra `Use`: `"curator"` → `"chronicle"`
- Variable names: `curatorCmd` → `chronicleCmd`
- Session name: `"gt-curator"` → `"sol-chronicle"`
- All subcommand registrations
- `rootCmd.AddCommand(curatorCmd)` → `rootCmd.AddCommand(chronicleCmd)`

### 6. Consul (was Deacon)

**Package:** `internal/consul/` (was `internal/deacon/`)

- Package declaration: `package deacon` → `package consul`
- `Deacon` struct → `Consul`
- `deacon.Config` → `consul.Config`
- `deacon.Heartbeat` → `consul.Heartbeat`
- `deacon.HeartbeatPath()` → `consul.HeartbeatPath()`
- `deacon.WriteHeartbeat()` → `consul.WriteHeartbeat()`
- `deacon.ReadHeartbeat()` → `consul.ReadHeartbeat()`
- `deacon.TownStore` → already `consul.SphereStore`
- `deacon.SessionChecker` → `consul.SessionChecker`
- `deacon.RigOpener` → `consul.WorldOpener` (type name)
- All methods on `Deacon` → methods on `Consul`
- All comments: "deacon" → "consul"
- Session name: `"gt-deacon"` → `"sol-consul"`

**File:** `cmd/deacon.go` → rename to `cmd/consul.go`

```bash
git mv cmd/deacon.go cmd/consul.go
```

- Cobra `Use`: `"deacon"` → `"consul"`
- Variable names: `deaconCmd` → `consulCmd`
- Session name
- `rootCmd.AddCommand(deaconCmd)` → `rootCmd.AddCommand(consulCmd)`

### 7. Caravan Command (was Convoy)

**File:** `cmd/convoy.go` → rename to `cmd/caravan.go`

```bash
git mv cmd/convoy.go cmd/caravan.go
```

- Cobra `Use`: `"convoy"` → `"caravan"`
- Variable names: `convoyCmd` → `caravanCmd`
- All subcommand variables and registrations
- All references to convoy types → caravan types (already renamed in store in prompt 2)
- `rootCmd.AddCommand(convoyCmd)` → `rootCmd.AddCommand(caravanCmd)`

### 8. Cast Command (was Sling)

**File:** `cmd/sling.go` → rename to `cmd/cast.go`

```bash
git mv cmd/sling.go cmd/cast.go
```

- Cobra `Use`: `"sling"` → `"cast"`
- Variable names: `slingCmd` → `castCmd`
- All references to dispatch functions (already renamed in prompt 2)
- `rootCmd.AddCommand(slingCmd)` → `rootCmd.AddCommand(castCmd)`

### 9. Resolve Command (was Done)

**File:** `cmd/done.go` → rename to `cmd/resolve.go`

```bash
git mv cmd/done.go cmd/resolve.go
```

- Cobra `Use`: `"done"` → `"resolve"`
- Variable names: `doneCmd` → `resolveCmd`
- All references to dispatch functions (already renamed in prompt 2)
- `rootCmd.AddCommand(doneCmd)` → `rootCmd.AddCommand(resolveCmd)`

### 10. Workflow Defaults

**Directory:** `internal/workflow/defaults/polecat-work/` → `internal/workflow/defaults/default-work/`

```bash
git mv internal/workflow/defaults/polecat-work internal/workflow/defaults/default-work
```

**File:** `internal/workflow/defaults.go`

- `go:embed` directives: `defaults/polecat-work/...` → `defaults/default-work/...`
- `knownDefaults` map: `"polecat-work": true` → `"default-work": true`
- Any comments referencing "polecat-work"

**Files in `internal/workflow/defaults/default-work/`:**

- `manifest.toml`: `name = "polecat-work"` → `name = "default-work"`
- Step markdown files: check for any references to old naming and update

**Other workflow files** (`internal/workflow/*.go`):
- Any references to `"polecat-work"` → `"default-work"`
- Any references to `"polecat"` → update contextually

### 11. Status Package

**File:** `internal/status/status.go`

- `rig` parameter names → `world`
- Comments referencing "rig" → "world"
- Any store interface references (should already be renamed in prompt 2)

### 12. Handoff Package

**File:** `internal/handoff/handoff.go`

- `rig` parameter names → `world`
- Comments referencing "rig" → "world"
- Any references to old command names in generated content

### 13. Escalation Package

**File:** `internal/escalation/*.go`

- Check for any `rig` parameter names → `world`
- **CAUTION:** `webhook.go` uses "hook" in the webhook context — do NOT rename this

### 14. Protocol Updates

**File:** `internal/protocol/claudemd.go`

Update any remaining references not caught in prompt 2:
- `gt supervisor` → `sol prefect`
- `gt refinery` → `sol forge`
- `gt witness` → `sol sentinel`
- `gt curator` → `sol chronicle`
- `gt deacon` → `sol consul`
- `gt convoy` → `sol caravan`
- `gt sling` → `sol cast`
- Any references to "supervisor", "refinery", "witness", "curator", "deacon" in generated CLAUDE.md content

### 15. Cross-Package References

After renaming, update all import paths:
- `"github.com/nevinsm/sol/internal/supervisor"` → `"github.com/nevinsm/sol/internal/prefect"`
- `"github.com/nevinsm/sol/internal/refinery"` → `"github.com/nevinsm/sol/internal/forge"`
- `"github.com/nevinsm/sol/internal/witness"` → `"github.com/nevinsm/sol/internal/sentinel"`
- `"github.com/nevinsm/sol/internal/deacon"` → `"github.com/nevinsm/sol/internal/consul"`

These appear in:
- `cmd/` files (the renamed ones above)
- `internal/supervisor/supervisor.go` may reference witness, refinery, deacon
- `internal/deacon/deacon.go` may reference supervisor
- Test files

### 16. Test Updates

All test files that reference renamed packages, types, or commands:

- Import paths for renamed packages
- Type references: `supervisor.Supervisor` → `prefect.Prefect`, etc.
- Function calls: `events.NewCurator(...)` → `events.NewChronicle(...)`, etc.
- Session name assertions: `"gt-supervisor"` → `"sol-prefect"`, etc.
- CLI command tests: `"bin/sol sling"` → `"bin/sol cast"`, `"bin/sol done"` → `"bin/sol resolve"`, etc.
- The `mockSessionChecker` in test helpers references `witness.SessionChecker` — update to `sentinel.SessionChecker`
- Convoy test assertions → caravan
- Any `"polecat-work"` references → `"default-work"`
- Makefile `test-e2e`: `bin/sol sling` → `bin/sol cast`, `bin/sol done` → `bin/sol resolve`

## What NOT To Change (Yet)

- Documentation files (prompt 4)

## Acceptance Criteria

```bash
make build && make test     # passes
bin/sol --help              # shows all new command names
bin/sol prefect --help     # works
bin/sol forge --help        # works
bin/sol sentinel --help     # works
bin/sol chronicle --help    # works
bin/sol consul --help       # works
bin/sol caravan --help      # works
bin/sol cast --help         # works
bin/sol resolve --help      # works

# No remaining old names in Go source:
grep -rn 'package supervisor' --include='*.go' .     # no hits
grep -rn 'package refinery' --include='*.go' .       # no hits
grep -rn 'package witness' --include='*.go' .        # no hits
grep -rn 'package deacon' --include='*.go' .         # no hits
grep -rn 'package hook' --include='*.go' .           # no hits
grep -rn '"polecat-work"' --include='*.go' .         # no hits
grep -rn 'Curator' --include='*.go' .                # no hits (except maybe comments about the concept)
grep -rn '"supervisor"' --include='*.go' cmd/        # no hits
grep -rn '"refinery"' --include='*.go' cmd/          # no hits
grep -rn '"sling"' --include='*.go' cmd/             # no hits
grep -rn '"done"' --include='*.go' cmd/              # no hits (as command name)
```

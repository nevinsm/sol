# Prompt 02: Arc 1 — World Lifecycle Commands

You are extending the `sol` orchestration system with explicit world
management commands. This prompt builds the CLI layer: a `RequireWorld`
gate function, agent cleanup for world deletion, and four `sol world`
subcommands (init, list, status, delete).

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 prompt 01 is complete (config types, schema V5,
world store CRUD).

Read all existing code first. Understand:
- `internal/config/config.go` — `Home()`, `StoreDir()`, `WorldDir()`,
  `EnsureDirs()`
- `internal/config/world_config.go` — `WorldConfig`, `LoadWorldConfig`,
  `WriteWorldConfig`, `WorldConfigPath`, `DefaultWorldConfig`
- `internal/store/store.go` — `OpenWorld`, `OpenSphere`
- `internal/store/worlds.go` — `RegisterWorld`, `GetWorld`, `ListWorlds`,
  `RemoveWorld`
- `internal/store/agents.go` — `ListAgents`, `CreateAgent`
- `internal/dispatch/dispatch.go` — `DiscoverSourceRepo()`
- `internal/status/status.go` — `Gather()`, `WorldStatus` struct
- `cmd/status.go` — existing `sol status <world>` command
- `cmd/forge.go` — how quality gates are loaded from flat file
  (`$SOL_HOME/{world}/forge/quality-gates.txt`)

---

## Task 1: RequireWorld Gate Function

**Modify** `internal/config/config.go`.

Add a function that validates a world has been initialized. The gate
checks for `world.toml` existence (file is source of truth, not DB):

```go
// RequireWorld checks that a world has been initialized.
// Returns nil if world.toml exists at $SOL_HOME/{world}/world.toml.
//
// Distinguishes two error cases:
// - Pre-Arc1 world (DB exists but no world.toml): tells user to run
//   "sol world init <world>" to adopt the existing world.
// - Nonexistent world: tells user to run "sol world init <world>".
func RequireWorld(world string) error
```

Implementation:

```go
func RequireWorld(world string) error {
    path := WorldConfigPath(world)
    if _, err := os.Stat(path); os.IsNotExist(err) {
        // Check if this is a pre-Arc1 world (DB exists but no config).
        dbPath := filepath.Join(StoreDir(), world+".db")
        if _, err := os.Stat(dbPath); err == nil {
            return fmt.Errorf("world %q was created before world lifecycle management; "+
                "run: sol world init %s", world, world)
        }
        return fmt.Errorf("world %q does not exist; run: sol world init %s", world, world)
    } else if err != nil {
        return fmt.Errorf("failed to check world %q: %w", world, err)
    }
    return nil
}
```

The gate is a **file existence check** — no DB connection needed,
GLASS-friendly (verifiable with `ls`), fast. This function will be
called by every world-scoped command (wired in prompt 03).

---

## Task 2: Delete Agents for World

**Modify** `internal/store/agents.go`.

Add a function to remove all agents for a world. This is needed by
`sol world delete`:

```go
// DeleteAgentsForWorld removes all agent records for the given world.
// Used during world deletion to clean up sphere state.
func (s *Store) DeleteAgentsForWorld(world string) error
```

Implementation: `DELETE FROM agents WHERE world = ?`. No error if
there are no agents for the world.

Add a test in the existing `internal/store/agents_test.go` (or wherever
agent tests live):

```go
func TestDeleteAgentsForWorld(t *testing.T)
    // Create agents in world "alpha" and "beta"
    // DeleteAgentsForWorld("alpha")
    // ListAgents("alpha", "") → empty
    // ListAgents("beta", "") → still there

func TestDeleteAgentsForWorldEmpty(t *testing.T)
    // DeleteAgentsForWorld("nonexistent") → no error
```

---

## Task 3: World CLI Commands

**Create** `cmd/world.go` with the `sol world` command group and four
subcommands.

### sol world init

```
sol world init <name> [--source-repo=<path>]
```

Args: `cobra.ExactArgs(1)`, name = `args[0]`.

**Behavior:**

1. Check if `world.toml` already exists at
   `$SOL_HOME/{name}/world.toml`:
   - If yes and this is NOT a pre-Arc1 adoption → error:
     `"world %q is already initialized"`
   - To detect adoption: DB exists but no world.toml was the state
     *before* this call. Since we just checked world.toml and it
     exists, the world is already fully initialized.

2. Determine source repo:
   - If `--source-repo` flag provided: use that value
   - Else: try `dispatch.DiscoverSourceRepo()` from cwd (best-effort,
     empty string is OK if this fails — not all worlds have a source
     repo at init time)

3. Create directory tree:
   ```
   $SOL_HOME/{name}/
   $SOL_HOME/{name}/outposts/
   ```
   Use `os.MkdirAll` (idempotent).

4. Ensure `.store/` directory exists: call `config.EnsureDirs()`.

5. Create world database: `store.OpenWorld(name)` → close immediately.
   This triggers the schema migration and creates the DB file if it
   doesn't exist.

6. Register in sphere.db: open `store.OpenSphere()`, call
   `RegisterWorld(name, sourceRepo)`, close.

7. Build `WorldConfig` from defaults and set `SourceRepo`:
   ```go
   cfg := config.DefaultWorldConfig()
   cfg.World.SourceRepo = sourceRepo
   ```

8. **Pre-Arc1 migration** — if the world DB already existed before
   this init (the DB file existed but world.toml didn't), migrate
   legacy config files:
   - `.quality-gates` file at `$SOL_HOME/{name}/forge/quality-gates.txt`:
     if it exists, read its contents using `forge.LoadQualityGates()`,
     and set `cfg.Forge.QualityGates` to the loaded gates.
   - `names.txt` at `$SOL_HOME/{name}/names.txt`: if it exists, set
     `cfg.Agents.NamePoolPath` to that file's absolute path.

   Flag pre-Arc1: check if `$SOL_HOME/.store/{name}.db` existed
   *before step 5* by checking for it before calling `OpenWorld`.

9. Write `world.toml`: `config.WriteWorldConfig(name, cfg)`.

10. Print confirmation:
    ```
    World "myworld" initialized.
      Config:   $SOL_HOME/myworld/world.toml
      Database: $SOL_HOME/.store/myworld.db
      Source:   /home/user/myproject  (or "none" if empty)

    Next steps:
      sol store create --world=myworld --title="First task"
      sol cast <work-item-id> myworld
    ```

### sol world list

```
sol world list [--json]
```

Args: `cobra.NoArgs`.

**Behavior:**

1. Open sphere store.
2. Call `ListWorlds()`.
3. If no worlds registered: print `"No worlds initialized."`, exit 0.

**Human output:**
```
NAME        SOURCE REPO               CREATED
myworld     /home/user/myproject      2026-02-27T10:30:00Z
testworld   /home/user/testproject    2026-02-27T11:00:00Z

2 world(s)
```

Use `text/tabwriter` for alignment (same pattern as other commands).

**JSON output:** JSON array of objects with `name`, `source_repo`,
`created_at` fields.

### sol world status

```
sol world status <name> [--json]
```

Args: `cobra.ExactArgs(1)`, name = `args[0]`.

**Behavior:**

1. Call `config.RequireWorld(name)` — fail if not initialized.
2. Load world config: `config.LoadWorldConfig(name)`.
3. Delegate to existing `status.Gather()` for agent/session/queue info.
4. Also print config summary.

**Human output** (extends existing status output):
```
World: myworld

Config:
  Source repo:   /home/user/myproject
  Agent capacity: 10
  Model tier:    opus
  Quality gates: 2
  Name pool:     (default)

Prefect: running (pid 12345)
Forge: running (sol-myworld-forge)
... (rest of existing status output)
```

**JSON output:** Same as existing `sol status` JSON but with an
additional `config` object containing the resolved world config.

Note: the existing `sol status <world>` command remains as-is and is
not modified. `sol world status` is a superset that includes config
information.

### sol world delete

```
sol world delete <name> --confirm
```

Args: `cobra.ExactArgs(1)`, name = `args[0]`.

The `--confirm` flag is **required** — no interactive prompts.
If `--confirm` is not set, print the deletion plan and exit:
```
This will permanently delete world "myworld":
  - World database: $SOL_HOME/.store/myworld.db
  - World directory: $SOL_HOME/myworld/
  - Agent records for world "myworld"

Run with --confirm to proceed.
```

**Behavior (when --confirm is set):**

1. Call `config.RequireWorld(name)` — fail if not initialized.

2. Check for active sessions: list tmux sessions matching
   `sol-{name}-*` pattern. If any are running, refuse:
   ```
   Cannot delete world "myworld": 3 active sessions.
   Stop sessions first: sol session stop sol-myworld-Toast
   ```
   Use `session.New().List()` and filter by prefix.

3. Open sphere store.

4. Remove agents for this world:
   `sphereStore.DeleteAgentsForWorld(name)`.

5. Remove world record: `sphereStore.RemoveWorld(name)`.

6. Close sphere store.

7. Delete world database file:
   `os.Remove($SOL_HOME/.store/{name}.db)`.
   Ignore "not found" errors.

8. Delete world directory tree:
   `os.RemoveAll($SOL_HOME/{name}/)`.
   Ignore "not found" errors.

9. Print: `World "myworld" deleted.`

---

## Task 4: Wire Commands

In `cmd/world.go`'s `init()` function, register all commands:

```go
func init() {
    rootCmd.AddCommand(worldCmd)
    worldCmd.AddCommand(worldInitCmd)
    worldCmd.AddCommand(worldListCmd)
    worldCmd.AddCommand(worldStatusCmd)
    worldCmd.AddCommand(worldDeleteCmd)

    worldInitCmd.Flags().StringVar(&worldInitSourceRepo, "source-repo",
        "", "path to source git repository")
    worldListCmd.Flags().BoolVar(&worldListJSON, "json", false,
        "output as JSON")
    worldStatusCmd.Flags().BoolVar(&worldStatusJSON, "json", false,
        "output as JSON")
    worldDeleteCmd.Flags().BoolVar(&worldDeleteConfirm, "confirm", false,
        "confirm deletion")
}
```

---

## Task 5: Tests

### Unit Tests

**Create** `internal/config/config_test.go` (or extend if it exists):

```go
func TestRequireWorldExists(t *testing.T)
    // Create $SOL_HOME/{world}/world.toml
    // RequireWorld → nil

func TestRequireWorldNotExists(t *testing.T)
    // No DB, no world.toml
    // RequireWorld → error containing "does not exist"

func TestRequireWorldPreArc1(t *testing.T)
    // Create $SOL_HOME/.store/{world}.db but NO world.toml
    // RequireWorld → error containing "before world lifecycle"
```

### Integration Tests

**Create** `test/integration/world_lifecycle_test.go`:

```go
func TestWorldInitBasic(t *testing.T)
    // Run: sol world init myworld
    // Verify: world.toml exists
    // Verify: myworld.db exists
    // Verify: myworld/ directory exists
    // Verify: myworld/outposts/ directory exists

func TestWorldInitWithSourceRepo(t *testing.T)
    // Run: sol world init myworld --source-repo=/tmp/fakerepo
    // Verify: world.toml contains source_repo="/tmp/fakerepo"

func TestWorldInitAlreadyExists(t *testing.T)
    // Init once → success
    // Init again → error "already initialized"

func TestWorldInitPreArc1World(t *testing.T)
    // Create a world DB manually (sol store create --world=legacy ...)
    // Run: sol world init legacy
    // Verify: world.toml created (adoption succeeded)
    // Verify: no error

func TestWorldList(t *testing.T)
    // Init two worlds
    // Run: sol world list
    // Output contains both world names

func TestWorldListEmpty(t *testing.T)
    // No worlds → output: "No worlds initialized."

func TestWorldListJSON(t *testing.T)
    // Init a world
    // Run: sol world list --json
    // Parse JSON output → valid array with correct name

func TestWorldStatusBasic(t *testing.T)
    // Init a world
    // Run: sol world status myworld
    // Output contains "Config:" section

func TestWorldStatusNotInitialized(t *testing.T)
    // Run: sol world status nonexistent
    // Error: "does not exist"

func TestWorldDeleteBasic(t *testing.T)
    // Init a world
    // Run: sol world delete myworld --confirm
    // Verify: world.toml gone
    // Verify: myworld.db gone
    // Verify: myworld/ directory gone

func TestWorldDeleteNoConfirm(t *testing.T)
    // Init a world
    // Run: sol world delete myworld (no --confirm)
    // Output shows deletion plan but does NOT delete
    // Verify: world.toml still exists

func TestWorldDeleteNotInitialized(t *testing.T)
    // Run: sol world delete nonexistent --confirm
    // Error: "does not exist"
```

Use the existing test helpers from `test/integration/helpers_test.go`:
`setupTestEnv`, `runGT`, `gtBin`.

---

## Task 6: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   mkdir -p /tmp/sol-test/.store

   # Init a world
   bin/sol world init myworld --source-repo=/tmp/fakerepo
   cat /tmp/sol-test/myworld/world.toml

   # List worlds
   bin/sol world list
   bin/sol world list --json

   # Create a work item (should work)
   bin/sol store create --world=myworld --title="Test item"

   # Status
   bin/sol world status myworld

   # Delete
   bin/sol world delete myworld
   # → shows plan, doesn't delete
   bin/sol world delete myworld --confirm
   # → deletes everything
   bin/sol world list
   # → no worlds

   # Pre-Arc1 adoption
   bin/sol store create --world=legacy --title="Old item"
   # This creates legacy.db without world.toml
   bin/sol world init legacy
   cat /tmp/sol-test/legacy/world.toml
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The `world.toml` file is the source of truth for whether a world is
  initialized. The `worlds` table in sphere.db is a cache/registry.
  `RequireWorld` checks the file, not the DB. This is GLASS-friendly —
  you can verify with `ls`.
- `sol world init` is idempotent in the pre-Arc1 case: running it on
  an existing DB without world.toml creates the config and registers
  the world. Running it again after that fails with "already initialized."
- `sol world delete --confirm` is the only destructive operation. It
  requires an explicit flag (no interactive prompts) for scripting
  safety.
- The deletion sequence handles partial state gracefully: each step
  ignores "not found" errors. If the DB was already deleted manually,
  the command still removes the directory and agent records.
- Active session detection before deletion prevents orphaned processes.
- The existing `sol status <world>` command is NOT modified. `sol world
  status` is a superset. Both can coexist.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(world): add world lifecycle commands (init, list, status, delete)`

# Prompt 09: Arc 2 — Status Command Overhaul

You are overhauling the `sol status` command to support both sphere
overview (no args) and per-world detail (with world arg), using the
lipgloss renderers from prompt 08.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 2 prompt 08 is complete (lipgloss rendering in
`internal/status/render.go`).

Read the existing code first. Understand:
- `cmd/status.go` — current `sol status <world>` command with
  `printWorldStatus()` function
- `cmd/world.go` — `worldStatusCmd` (sol world status <name>)
- `internal/status/status.go` — `Gather()`, `GatherSphere()`,
  `WorldStatus`, `SphereStatus`
- `internal/status/render.go` — `RenderSphere()`, `RenderWorld()`
- `internal/store/store.go` — `OpenSphere()`, `OpenWorld()`

---

## Task 1: Make World Arg Optional

**Modify** `cmd/status.go`.

Change `sol status` from requiring a world argument to accepting an
optional one:

```go
var statusCmd = &cobra.Command{
    Use:   "status [world]",
    Short: "Show sphere or world status",
    Long: `Show system status.

Without arguments, shows a sphere-level overview of all worlds and processes.
With a world name, shows detailed status for that specific world.

Exit codes (world mode only):
  0 = healthy
  1 = unhealthy
  2 = degraded`,
    Args:          cobra.MaximumNArgs(1),
    SilenceErrors: true,
    SilenceUsage:  true,
    RunE:          runStatus,
}
```

---

## Task 2: Dispatch Logic

**Modify** `cmd/status.go`.

Replace the existing `RunE` inline function with a named function that
dispatches to sphere or world mode:

```go
func runStatus(cmd *cobra.Command, args []string) error {
    if len(args) == 0 {
        return runSphereStatus()
    }
    return runWorldStatus(args[0])
}
```

### Sphere Status

```go
func runSphereStatus() error {
    sphereStore, err := store.OpenSphere()
    if err != nil {
        return err
    }
    defer sphereStore.Close()

    mgr := session.New()

    result := status.GatherSphere(sphereStore, sphereStore, mgr,
        gatedWorldOpener, sphereStore)

    if statusJSON {
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(result)
    }

    fmt.Print(status.RenderSphere(result))
    return nil
}
```

**Note:** The `GatherSphere` call above uses `sphereStore` for multiple
interface parameters (it implements both `SphereStore` and `WorldLister`
and `CaravanStore`). Verify the actual interface implementations in the
store package and adjust the parameters accordingly. If `store.Store`
doesn't implement `WorldLister`, you may need to adapt.

The `gatedWorldOpener` function already exists in `cmd/status.go` (used
by `GatherCaravans`). Reuse it.

### World Status

```go
func runWorldStatus(world string) error {
    if err := config.RequireWorld(world); err != nil {
        return err
    }

    sphereStore, err := store.OpenSphere()
    if err != nil {
        return err
    }
    defer sphereStore.Close()

    worldStore, err := store.OpenWorld(world)
    if err != nil {
        return err
    }
    defer worldStore.Close()

    mgr := session.New()

    result, err := status.Gather(world, sphereStore, worldStore, worldStore, mgr)
    if err != nil {
        return err
    }

    status.GatherCaravans(result, sphereStore, gatedWorldOpener)

    if statusJSON {
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        return enc.Encode(result)
    }

    fmt.Print(status.RenderWorld(result))

    // Exit with health code.
    os.Exit(result.Health())
    return nil
}
```

---

## Task 3: Remove printWorldStatus

Delete the `printWorldStatus()` function from `cmd/status.go`. It is
replaced by `status.RenderWorld()`.

Also update `cmd/world.go` — `worldStatusCmd` currently calls
`printWorldStatus(result)`. Replace it with:

```go
fmt.Print(status.RenderWorld(result))
```

And add the config section rendering before the world status render
in `worldStatusCmd`. The config section should be rendered with
lipgloss styles too:

```go
// In worldStatusCmd RunE, replace the plain fmt.Printf config section with:
fmt.Print(status.RenderWorldConfig(name, cfg))
fmt.Print(status.RenderWorld(result))
```

Add `RenderWorldConfig` to `internal/status/render.go`:

```go
// RenderWorldConfig renders the config section for sol world status.
func RenderWorldConfig(world string, cfg config.WorldConfig) string {
    var b strings.Builder

    b.WriteString(headerStyle.Render("Config"))
    b.WriteString("\n")

    sourceDisplay := cfg.World.SourceRepo
    if sourceDisplay == "" {
        sourceDisplay = dimStyle.Render("(none)")
    }
    b.WriteString(fmt.Sprintf("  Source repo:    %s\n", sourceDisplay))

    if cfg.Agents.Capacity == 0 {
        b.WriteString(fmt.Sprintf("  Agent capacity: %s\n", dimStyle.Render("unlimited")))
    } else {
        b.WriteString(fmt.Sprintf("  Agent capacity: %d\n", cfg.Agents.Capacity))
    }
    b.WriteString(fmt.Sprintf("  Model tier:     %s\n", cfg.Agents.ModelTier))
    b.WriteString(fmt.Sprintf("  Quality gates:  %d\n", len(cfg.Forge.QualityGates)))

    namePool := dimStyle.Render("(default)")
    if cfg.Agents.NamePoolPath != "" {
        namePool = cfg.Agents.NamePoolPath
    }
    b.WriteString(fmt.Sprintf("  Name pool:      %s\n", namePool))
    b.WriteString("\n")

    return b.String()
}
```

This requires adding an import for `config` in `render.go`:

```go
import "github.com/nevinsm/sol/internal/config"
```

---

## Task 4: Ensure gatedWorldOpener Exists

The `gatedWorldOpener` function should already exist in `cmd/status.go`.
Verify it's defined and accessible. It opens a world store with the
RequireWorld gate:

```go
func gatedWorldOpener(world string) (*store.Store, error) {
    if err := config.RequireWorld(world); err != nil {
        return nil, err
    }
    return store.OpenWorld(world)
}
```

If it doesn't exist, create it. If it's defined somewhere else
(e.g., `cmd/world.go`), it may need to be shared. Place it in
whichever file makes it accessible to both `statusCmd` and
`worldStatusCmd`.

---

## Task 5: Tests

### Integration tests

**Add** to `test/integration/` (create `status_test.go` or extend
existing):

```go
func TestStatusSphereOverview(t *testing.T)
    // Set up SOL_HOME with 2 initialized worlds.
    // Run: sol status
    // Verify: output contains "Sol Sphere"
    // Verify: output contains both world names
    // Verify: output contains "Processes" section
    // Verify: exit code 0

func TestStatusSphereJSON(t *testing.T)
    // Set up SOL_HOME with 1 world.
    // Run: sol status --json
    // Parse as JSON.
    // Verify: has "sol_home", "worlds" array, "health" field.

func TestStatusSphereEmpty(t *testing.T)
    // Set up SOL_HOME with no worlds.
    // Run: sol status
    // Verify: output contains "No worlds initialized."

func TestStatusWorldDetail(t *testing.T)
    // Set up SOL_HOME with 1 world and a work item.
    // Run: sol status myworld
    // Verify: output contains world name
    // Verify: output contains "Processes" section
    // Verify: output contains "Merge Queue"

func TestStatusWorldJSON(t *testing.T)
    // Existing test pattern — verify JSON output still works.

func TestStatusWorldNotFound(t *testing.T)
    // Run: sol status nonexistent
    // Should error with "does not exist"

func TestWorldStatusStillWorks(t *testing.T)
    // Verify: sol world status <name> still works with lipgloss output.
    // Verify: output contains "Config" section.
```

---

## Task 6: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-status-test
   rm -rf /tmp/sol-status-test

   # Set up
   bin/sol init --name=alpha --skip-checks
   bin/sol init --name=beta --skip-checks  # will fail: init creates SOL_HOME
   # Actually need: sol world init beta
   bin/sol world init beta

   bin/sol store create --world=alpha --title="Test 1"
   bin/sol store create --world=alpha --title="Test 2"

   # Sphere overview
   bin/sol status
   # Should show styled output with both worlds

   # World detail
   bin/sol status alpha
   # Should show styled per-world output

   # JSON
   bin/sol status --json
   bin/sol status alpha --json

   # World status (should still work)
   bin/sol world status alpha

   rm -rf /tmp/sol-status-test
   ```

---

## Guidelines

- The overhaul is **additive for sphere mode, replacement for world
  mode.** `sol status` (no args) is new. `sol status <world>` replaces
  the old `printWorldStatus` with `RenderWorld`.
- **`--json` is unchanged.** JSON output uses `encoding/json` directly,
  no lipgloss. The sphere JSON output uses the `SphereStatus` struct.
- **Exit codes only apply in world mode.** Sphere mode always exits 0
  (the sphere overview is informational, not a health gate).
- `sol world status <name>` still works and now uses the lipgloss
  renderer too (plus config section). Don't break it.
- The `gatedWorldOpener` function is shared between status and world
  commands. Make sure it's accessible to both.
- `RenderWorldConfig` is separate from `RenderWorld` because world
  config is only shown in `sol world status`, not `sol status <world>`.
  `sol status <world>` shows runtime state; `sol world status <name>`
  adds config on top.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(status): overhaul status command with sphere overview and lipgloss rendering`

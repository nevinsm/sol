# Prompt 04: Arc 1 — Config Consumer Wiring

You are connecting the subsystems that consume world configuration to
read from `WorldConfig` (loaded from `world.toml`) instead of hardcoded
paths and CWD-based discovery. After this prompt, cast, forge, dispatch,
and protocol all read their settings from the world's config file.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 prompts 01–03 are complete (config types,
schema V5, world store CRUD, RequireWorld, world CLI commands, hard
gate enforcement).

Read all existing code first. Understand:
- `internal/config/world_config.go` — `LoadWorldConfig()`, `WorldConfig`
- `internal/dispatch/dispatch.go` — `autoProvision()` (name pool,
  agent listing), `DiscoverSourceRepo()`, `Cast()`
- `internal/protocol/claudemd.go` — `ClaudeMDContext`,
  `ForgeClaudeMDContext`, `GenerateClaudeMD()`
- `cmd/cast.go` — source repo from `DiscoverSourceRepo()`
- `cmd/forge.go` — quality gates from flat file at
  `$SOL_HOME/{world}/forge/quality-gates.txt`, source repo from CWD
- `internal/forge/forge.go` — `LoadQualityGates()`, `DefaultConfig()`

---

## Task 1: Cast — Source Repo from Config

**Modify** `cmd/cast.go`.

Replace CWD-based source repo discovery with config-first approach:

```go
// After RequireWorld check:
worldCfg, err := config.LoadWorldConfig(world)
if err != nil {
    return err
}
sourceRepo := worldCfg.World.SourceRepo
if sourceRepo == "" {
    // Fallback to cwd discovery for convenience.
    sourceRepo, err = dispatch.DiscoverSourceRepo()
    if err != nil {
        return fmt.Errorf("no source_repo in world.toml and not in a git repo: %w", err)
    }
}
```

The existing `dispatch.DiscoverSourceRepo()` call is replaced with
this config-first logic. The fallback exists for convenience —
operators who always `cd` into their repo before running `cast` don't
need to set `source_repo` in `world.toml`.

---

## Task 2: Forge — Quality Gates + Source Repo from Config

**Modify** `cmd/forge.go`.

### forgeStartCmd

After the RequireWorld check, load the world config and use it for
quality gates, target branch, and source repo:

```go
worldCfg, err := config.LoadWorldConfig(world)
if err != nil {
    return err
}

cfg := forge.DefaultConfig()
if len(worldCfg.Forge.QualityGates) > 0 {
    cfg.QualityGates = worldCfg.Forge.QualityGates
} else {
    // Fallback to flat file for backwards compatibility.
    gatesPath := filepath.Join(config.WorldDir(world), "forge", "quality-gates.txt")
    gates, err := forge.LoadQualityGates(gatesPath, cfg.QualityGates)
    if err != nil {
        return fmt.Errorf("failed to load quality gates: %w", err)
    }
    cfg.QualityGates = gates
}

if worldCfg.Forge.TargetBranch != "" {
    cfg.TargetBranch = worldCfg.Forge.TargetBranch
}
```

Also read source repo from config (same pattern as cast):
```go
sourceRepo := worldCfg.World.SourceRepo
if sourceRepo == "" {
    sourceRepo, err = dispatch.DiscoverSourceRepo()
    if err != nil {
        return err
    }
}
```

### openForge helper

Apply the same config-first pattern to the `openForge()` helper
function (used by all forge toolbox subcommands). The helper takes
a `world` string — add config loading at the start and use config
values for quality gates, target branch, and source repo:

```go
func openForge(world string) (*forge.Forge, *store.Store, *store.Store, error) {
    worldCfg, err := config.LoadWorldConfig(world)
    if err != nil {
        return nil, nil, nil, err
    }

    // ... existing store opens ...

    sourceRepo := worldCfg.World.SourceRepo
    if sourceRepo == "" {
        sourceRepo, err = dispatch.DiscoverSourceRepo()
        if err != nil {
            worldStore.Close()
            sphereStore.Close()
            return nil, nil, nil, err
        }
    }

    cfg := forge.DefaultConfig()
    if len(worldCfg.Forge.QualityGates) > 0 {
        cfg.QualityGates = worldCfg.Forge.QualityGates
    } else {
        gatesPath := filepath.Join(config.WorldDir(world), "forge", "quality-gates.txt")
        gates, err := forge.LoadQualityGates(gatesPath, cfg.QualityGates)
        if err != nil {
            worldStore.Close()
            sphereStore.Close()
            return nil, nil, nil, fmt.Errorf("failed to load quality gates: %w", err)
        }
        cfg.QualityGates = gates
    }
    if worldCfg.Forge.TargetBranch != "" {
        cfg.TargetBranch = worldCfg.Forge.TargetBranch
    }

    // ... rest of function (pass sourceRepo and cfg to forge.New) ...
}
```

---

## Task 3: Dispatch — Name Pool Path + Capacity from Config

**Modify** `internal/dispatch/dispatch.go`, specifically the
`autoProvision` function.

### Name pool path

Replace the hardcoded name pool path with config-driven path:

```go
func autoProvision(world string, sphereStore SphereStore) (*store.Agent, error) {
    worldCfg, _ := config.LoadWorldConfig(world)

    overridePath := worldCfg.Agents.NamePoolPath
    if overridePath == "" {
        overridePath = filepath.Join(config.Home(), world, "names.txt")
    }
    pool, err := namepool.Load(overridePath)
    // ...
```

The fallback to `$SOL_HOME/{world}/names.txt` preserves backwards
compatibility.

### Agent capacity enforcement

After listing agents (which already happens in `autoProvision`),
check agent count against capacity before allocating a name:

```go
    agents, err := sphereStore.ListAgents(world, "")
    if err != nil {
        return nil, fmt.Errorf("failed to list agents for world %q: %w", world, err)
    }

    // Enforce agent capacity.
    if worldCfg.Agents.Capacity > 0 && len(agents) >= worldCfg.Agents.Capacity {
        return nil, fmt.Errorf("world %q has reached agent capacity (%d)", world, worldCfg.Agents.Capacity)
    }

    usedNames := make([]string, len(agents))
    // ... rest of function unchanged
```

---

## Task 4: Protocol — Model Tier Field

**Modify** `internal/protocol/claudemd.go`.

Add a `ModelTier` field to `ClaudeMDContext`:

```go
type ClaudeMDContext struct {
    AgentName   string
    World       string
    WorkItemID  string
    Title       string
    Description string
    HasWorkflow bool
    ModelTier   string // "sonnet", "opus", "haiku" — informational
}
```

In `GenerateClaudeMD()`, if `ModelTier` is non-empty, include it in
the generated CLAUDE.md:

```go
// After the agent identity section:
if ctx.ModelTier != "" {
    fmt.Fprintf(&b, "\n## Model\nConfigured model tier: %s\n", ctx.ModelTier)
}
```

**Wire up in dispatch:** Find where `ClaudeMDContext` is built in the
dispatch pipeline (likely in `Cast()` in `internal/dispatch/dispatch.go`
or where `protocol.InstallClaudeMD` is called). Load the world config
and populate `ModelTier`:

```go
worldCfg, _ := config.LoadWorldConfig(opts.World)
claudeCtx := protocol.ClaudeMDContext{
    // ... existing fields ...
    ModelTier: worldCfg.Agents.ModelTier,
}
```

If the config is already loaded earlier in the `Cast` flow (e.g., by
`autoProvision`), consider loading it once and threading it through
rather than loading it multiple times.

---

## Task 5: Tests

### Config Consumer Integration Tests

Add to `test/integration/world_lifecycle_test.go` or create
`test/integration/config_consumer_test.go`:

```go
func TestCastUsesConfigSourceRepo(t *testing.T)
    // Init world with --source-repo pointing to the test source repo
    // Run cast from a different directory (e.g., /tmp)
    // → cast should succeed using source_repo from config
    // (This tests that CWD fallback isn't needed when config is set)

func TestForgeUsesConfigQualityGates(t *testing.T)
    // Init world
    // Write quality_gates = ["echo gate-ok"] to world.toml
    // Start forge (or call forge run-gates directly)
    // Verify the config-defined gate is used (not flat file)

func TestDispatchCapacityEnforced(t *testing.T)
    // Init world
    // Write capacity = 1 to world.toml
    // Create 2 work items
    // Cast first item → succeeds (1 agent created)
    // Cast second item → fails with "reached agent capacity"

func TestDispatchCapacityZeroUnlimited(t *testing.T)
    // Init world (default capacity=0)
    // Cast multiple items → all succeed (no capacity limit)

func TestDispatchNamePoolFromConfig(t *testing.T)
    // Init world
    // Create custom name pool file at a non-default path
    // Write name_pool_path to world.toml pointing to custom file
    // Cast item → agent name comes from the custom pool
```

### Unit Tests

Add to `internal/protocol/protocol_test.go`:

```go
func TestGenerateClaudeMDWithModelTier(t *testing.T)
    // ClaudeMDContext with ModelTier="opus"
    // GenerateClaudeMD → output contains "model tier: opus"

func TestGenerateClaudeMDWithoutModelTier(t *testing.T)
    // ClaudeMDContext with ModelTier="" (empty)
    // GenerateClaudeMD → output does NOT contain "Model" section
```

---

## Task 6: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   rm -rf /tmp/sol-test
   mkdir -p /tmp/sol-test/.store

   # Init a world with source repo
   bin/sol world init myworld --source-repo=$(pwd)

   # Verify config-driven quality gates
   cat >> /tmp/sol-test/myworld/world.toml << 'EOF'

   [forge]
   quality_gates = ["echo gate-passed"]
   EOF

   cat /tmp/sol-test/myworld/world.toml
   # Should show quality_gates under [forge]

   # Verify config-driven capacity
   cat >> /tmp/sol-test/myworld/world.toml << 'EOF'

   [agents]
   capacity = 2
   model_tier = "opus"
   EOF
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- Config consumers always have a **fallback** to the pre-Arc1 behavior:
  - Source repo: falls back to `DiscoverSourceRepo()` (CWD-based)
  - Quality gates: falls back to `quality-gates.txt` flat file
  - Name pool: falls back to `$SOL_HOME/{world}/names.txt`
  This ensures backwards compatibility for worlds initialized with
  `sol world init` that haven't configured these settings yet.
- Agent capacity of 0 means unlimited (no enforcement). This is the
  default in `DefaultWorldConfig()`.
- The `ModelTier` field is informational in the CLAUDE.md for now.
  Actual model selection is a future enhancement.
- Avoid loading `WorldConfig` multiple times in the same code path.
  If `autoProvision` and `Cast` both need config, load it once in
  `Cast` and pass the relevant values through.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(world): wire config consumers for source repo, gates, capacity, and model tier`

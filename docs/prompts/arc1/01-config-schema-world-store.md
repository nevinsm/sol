# Prompt 01: Arc 1 — Config Foundation + Schema + World Store

You are extending the `sol` orchestration system with explicit world
lifecycle management. This prompt builds the data layer: configuration
types with TOML loading, a new sphere schema migration for tracking
worlds, and CRUD operations for world records.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 0 is complete. Loops 0–5 are complete.

Read all existing code first. Understand:
- `internal/config/config.go` — SOL_HOME resolution, path helpers
- `internal/config/world_config.go` — **already exists** from a previous
  partial implementation. Verify it matches the spec below; adjust if needed.
- `internal/store/schema.go` — migration pattern (world V1–V4, sphere V1–V4)
- `internal/store/store.go` — `OpenWorld`, `OpenSphere`, `open`, pragmas
- `internal/store/agents.go` — agent CRUD pattern (reference for worlds.go)
- `go.mod` — `BurntSushi/toml` is currently an indirect dependency

---

## Task 1: Config Types and TOML Loading

A file `internal/config/world_config.go` already exists from a previous
session. Verify it matches the spec below and fix any discrepancies.

### Types

```go
// internal/config/world_config.go
package config

type WorldConfig struct {
    World  WorldSection  `toml:"world"`
    Agents AgentsSection `toml:"agents"`
    Forge  ForgeSection  `toml:"forge"`
}

type WorldSection struct {
    SourceRepo string `toml:"source_repo"`
}

type AgentsSection struct {
    Capacity     int    `toml:"capacity"`       // 0 = unlimited
    NamePoolPath string `toml:"name_pool_path"` // empty = embedded default
    ModelTier    string `toml:"model_tier"`      // "sonnet", "opus", "haiku"
}

type ForgeSection struct {
    TargetBranch string   `toml:"target_branch"`
    QualityGates []string `toml:"quality_gates"`
}
```

### Functions

```go
// DefaultWorldConfig returns a WorldConfig with built-in defaults.
// Defaults: capacity=0 (unlimited), model_tier="sonnet", target_branch="main".
func DefaultWorldConfig() WorldConfig

// WorldConfigPath returns $SOL_HOME/{world}/world.toml.
func WorldConfigPath(world string) string

// GlobalConfigPath returns $SOL_HOME/sol.toml.
func GlobalConfigPath() string

// LoadWorldConfig loads configuration by layering:
// defaults → sol.toml → world.toml.
// Missing files are not an error (returns layer accumulated so far).
// Uses toml.DecodeFile which only overwrites fields present in the file.
func LoadWorldConfig(world string) (WorldConfig, error)

// WriteWorldConfig writes a world's configuration to world.toml.
// Creates parent directories if needed.
func WriteWorldConfig(world string, cfg WorldConfig) error
```

### Config layering

`LoadWorldConfig` applies three layers in order:

1. `DefaultWorldConfig()` — built-in defaults
2. `sol.toml` — global overrides (same struct, file at `$SOL_HOME/sol.toml`)
3. `world.toml` — per-world overrides (file at `$SOL_HOME/{world}/world.toml`)

Each `toml.DecodeFile` call only overwrites fields present in the TOML
file. Missing files are silently skipped (not an error).

### world.toml example

```toml
[world]
source_repo = "/home/user/myproject"

[agents]
capacity = 10
name_pool_path = ""
model_tier = "opus"

[forge]
target_branch = "main"
quality_gates = [
    "make test",
    "make vet",
]
```

### sol.toml example (global defaults only)

```toml
[agents]
model_tier = "sonnet"

[forge]
target_branch = "main"
```

### Dependency

`BurntSushi/toml` is currently indirect in `go.mod`. Run `go mod tidy`
after implementation to promote it to a direct dependency.

---

## Task 2: Config Tests

Create `internal/config/world_config_test.go`.

All tests use `t.TempDir()` for `SOL_HOME`. Set the `SOL_HOME` env var
in each test and restore it with `t.Cleanup`.

```go
func TestDefaultWorldConfig(t *testing.T)
    // Defaults: capacity=0, model_tier="sonnet", target_branch="main"
    // quality_gates is nil, source_repo is empty

func TestLoadWorldConfigNoFiles(t *testing.T)
    // No sol.toml or world.toml → returns defaults, no error

func TestLoadWorldConfigGlobalOnly(t *testing.T)
    // Write sol.toml with model_tier="opus"
    // LoadWorldConfig → model_tier="opus", target_branch still "main"

func TestLoadWorldConfigWorldOverridesGlobal(t *testing.T)
    // Write sol.toml with model_tier="opus"
    // Write world.toml with model_tier="haiku"
    // LoadWorldConfig → model_tier="haiku" (world wins)

func TestLoadWorldConfigPartialOverride(t *testing.T)
    // Write world.toml with only [world] source_repo="/path"
    // LoadWorldConfig → source_repo="/path", all other fields are defaults

func TestLoadWorldConfigQualityGates(t *testing.T)
    // Write world.toml with quality_gates=["make test", "make vet"]
    // LoadWorldConfig → quality_gates has 2 entries

func TestWriteWorldConfigRoundTrip(t *testing.T)
    // Build a WorldConfig with all fields set
    // WriteWorldConfig → LoadWorldConfig → matches original

func TestLoadWorldConfigInvalidTOML(t *testing.T)
    // Write invalid TOML to world.toml
    // LoadWorldConfig → returns error

func TestWorldConfigPath(t *testing.T)
    // Verify path is $SOL_HOME/{world}/world.toml

func TestGlobalConfigPath(t *testing.T)
    // Verify path is $SOL_HOME/sol.toml
```

---

## Task 3: Schema V5 — Worlds Table

**Modify** `internal/store/schema.go`.

Add the sphere schema V5 migration — a `worlds` table for tracking
initialized worlds:

```go
const sphereSchemaV5 = `
CREATE TABLE IF NOT EXISTS worlds (
    name        TEXT PRIMARY KEY,
    source_repo TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);
`
```

Update `migrateSphere()`:

1. Add an `if v < 5` block after the V4 block:
   ```go
   if v < 5 {
       if _, err := s.db.Exec(sphereSchemaV5); err != nil {
           return fmt.Errorf("failed to apply sphere schema v5: %w", err)
       }
   }
   ```

2. Update the version insert/update targets from `4` to `5`:
   ```go
   if v < 1 {
       if _, err := s.db.Exec("INSERT INTO schema_version VALUES (5)"); err != nil {
   ```
   ```go
   } else if v < 5 {
       if _, err := s.db.Exec("UPDATE schema_version SET version = 5"); err != nil {
   ```

Follow the exact same pattern used for V2, V3, V4.

No changes to the world DB schema (stays at V4).

---

## Task 4: World Store Operations

Create `internal/store/worlds.go` with CRUD operations for world records.

### Types

```go
package store

import "time"

// World represents a registered world in the sphere database.
type World struct {
    Name       string
    SourceRepo string
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

### Functions

```go
// RegisterWorld creates a world record in the sphere DB.
// Uses INSERT OR IGNORE — idempotent, safe for re-init of existing worlds.
// If the world already exists, this is a no-op (does not update fields).
func (s *Store) RegisterWorld(name, sourceRepo string) error

// GetWorld returns a world by name. Returns nil, nil if not found.
func (s *Store) GetWorld(name string) (*World, error)

// ListWorlds returns all registered worlds, ordered by name.
func (s *Store) ListWorlds() ([]World, error)

// UpdateWorldRepo updates the source_repo for a world.
// Also updates updated_at.
func (s *Store) UpdateWorldRepo(name, sourceRepo string) error

// RemoveWorld deletes a world record from the sphere DB.
// Does NOT delete the world database file or directory — that's the
// CLI's responsibility.
func (s *Store) RemoveWorld(name string) error
```

### Implementation notes

- All timestamps are RFC3339 UTC (same pattern as agents, work items).
- `RegisterWorld` uses `INSERT OR IGNORE INTO worlds ...` — calling it
  twice for the same world name does nothing on the second call.
- `GetWorld` returns `nil, nil` for not-found (not an error), following
  the same pattern as `FindIdleAgent`.
- `ListWorlds` always returns all worlds, ordered by `name ASC`.
- `RemoveWorld` uses `DELETE FROM worlds WHERE name = ?`. No error if
  the world doesn't exist.

---

## Task 5: World Store Tests

Create `internal/store/worlds_test.go`.

Use `t.TempDir()` for database files. Each test opens a fresh sphere
store via `openTestSphere(t)` helper (create one if it doesn't already
exist in the test file, or use the existing test patterns).

```go
func TestRegisterWorld(t *testing.T)
    // Register → GetWorld → matches name, source_repo, timestamps
    // Verify created_at and updated_at are valid RFC3339

func TestRegisterWorldIdempotent(t *testing.T)
    // Register twice with same name → no error
    // GetWorld → original values preserved (not overwritten)

func TestGetWorldNotFound(t *testing.T)
    // GetWorld("nonexistent") → nil, nil

func TestListWorlds(t *testing.T)
    // Register 3 worlds → ListWorlds → 3 entries, ordered by name

func TestListWorldsEmpty(t *testing.T)
    // No worlds registered → ListWorlds → empty slice, no error

func TestUpdateWorldRepo(t *testing.T)
    // Register with repo A → UpdateWorldRepo to repo B → GetWorld → repo B
    // Verify updated_at changed

func TestRemoveWorld(t *testing.T)
    // Register → RemoveWorld → GetWorld → nil

func TestRemoveWorldNonexistent(t *testing.T)
    // RemoveWorld("nonexistent") → no error

func TestSchemaV5Migration(t *testing.T)
    // Open sphere store → verify worlds table exists
    // Verify schema_version is 5
```

---

## Task 6: Verify

1. `make build` — compiles cleanly
2. `make test` — all existing and new tests pass
3. Verify with SQLite:
   ```bash
   export SOL_HOME=/tmp/sol-test
   mkdir -p /tmp/sol-test/.store
   bin/sol store create --world=testworld --title="test"
   sqlite3 /tmp/sol-test/.store/sphere.db ".tables"
   # Should show: agents caravans caravan_items escalations messages schema_version worlds
   sqlite3 /tmp/sol-test/.store/sphere.db "SELECT version FROM schema_version"
   # Should show: 5
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The existing `world_config.go` was created by a previous session that
  started implementing instead of writing prompts. Verify it matches
  this spec before building on it. Adjust any discrepancies.
- Follow the exact migration pattern established in V1–V4. The version
  numbering must be sequential and the `INSERT/UPDATE` targets must
  match the latest version.
- `RegisterWorld` is idempotent by design. This supports the "adopt
  pre-Arc1 world" flow: running `sol world init` on an existing world
  calls `RegisterWorld` which succeeds without overwriting.
- World store operations are on the **sphere** store (not world stores).
  The worlds table is a registry of all initialized worlds.
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(config): add world config types, schema V5 worlds table, and world store CRUD`

# Arc 1 Review — Post-Completion Audit

**Date:** 2026-02-28
**Scope:** Full codebase review after Arc 1 (World Lifecycle) completion
**Method:** Four parallel review passes — world package, store/schema, CLI/integration, config consumer wiring

---

## HIGH — Shared Cobra Flag Variables (5 instances, same class)

Cobra persistent flag binding bugs where a single `var` is shared across multiple
subcommands — later registrations overwrite earlier ones:

| Variable | File | Commands affected |
|----------|------|-------------------|
| `worldFlag` | `cmd/store.go` | 9 store subcommands |
| `forgeToolboxJSON` | `cmd/forge.go` | 6 forge toolbox commands |
| `caravanJSON` | `cmd/caravan.go` | 2 caravan commands |
| `caravanWorld` | `cmd/caravan.go` | 3 caravan commands |
| `wfWorld`/`wfAgent` | `cmd/workflow.go` | 4 workflow commands |

These may not manifest as bugs today if each flag is re-bound per command correctly,
but the pattern is fragile.

---

## MEDIUM — Functional Issues (8)

### M1. Missing `"formulas"` in reserved world names
- **File:** `internal/config/config.go:39`
- `sol world init formulas` would create `$SOL_HOME/formulas/world.toml` and
  `$SOL_HOME/formulas/outposts/`, colliding with the formula directory at
  `$SOL_HOME/formulas/`.

### M2. `session start` missing `RequireWorld` gate
- **File:** `cmd/session.go`
- Only world-scoped command without the guard. All other world-scoped commands
  correctly call `config.RequireWorld()`.

### M3. `workflow instantiate --item` value never passed to `Instantiate()`
- **File:** `cmd/workflow.go`
- The `--item` flag (`wfItem`) is captured but never injected into the `vars` map
  passed to `workflow.Instantiate()`. The work item ID is only used in the
  success message, never persisted. Users must manually pass `--var issue=<id>`
  for the work item to be associated.

### M4. Migrations not wrapped in transactions
- **File:** `internal/store/schema.go`
- Both `migrateWorld()` and `migrateSphere()` execute DDL statements and version
  updates as separate `s.db.Exec()` calls with no transaction wrapper. If the
  process crashes between executing a migration step and updating `schema_version`,
  the DB will be in a partially-migrated state.
- V1/V2/V5 use `CREATE TABLE IF NOT EXISTS` (idempotent, safe on re-run).
- V3 world and V4 sphere use `ALTER TABLE` (NOT idempotent, will fail on re-run).
- The `TestMigrationIdempotent` test only tests the "already at latest version"
  case, not the "partial migration" case.

### M5. N+1 query in `ListWorkItems`
- **File:** `internal/store/workitems.go:264`
- After fetching work items, labels are fetched one-by-one in a loop. With N
  work items, this results in N+1 queries. A single query with `GROUP_CONCAT`
  or batched `WHERE work_item_id IN (...)` would be more efficient.

### M6. `GetEscalation` masks real DB errors as "not found"
- **File:** `internal/store/escalations.go:70`
- Returns a "not found" error message for ANY error from the query, including
  actual database errors. Should check for `sql.ErrNoRows` specifically.

### M7. Silently swallowed `time.Parse` errors
- **Files:** `store/workitems.go`, `store/merge_requests.go`, `store/caravans.go`,
  `store/escalations.go`, `store/messages.go`
- These files use `w.CreatedAt, _ = time.Parse(...)` — errors are silently
  discarded, producing zero-value timestamps on corrupt data.
- Contrast with `store/worlds.go` and `store/agents.go` which properly return
  parse errors.

### M8. `parseVarFlags` silently drops malformed `--var` values
- **Files:** `cmd/cast.go:84`, `cmd/caravan.go:469`, `cmd/workflow.go:39`
- If a user passes `--var foo` (without `=value`), it is silently ignored.
- Three separate duplicate implementations of the same logic.

---

## LOW — Code Quality / Correctness Smells (18)

### L1. Double-close in `WriteWorldConfig`
- **File:** `internal/config/world_config.go:117`
- `f.Close()` called via both `defer` and explicit `return f.Close()`.

### L2. `DeleteWorldData` doesn't clean up messages/escalations
- **File:** `internal/store/worlds.go:119`
- Deletes caravan_items, agents, and world record, but orphans messages and
  escalations that reference agents in the deleted world.

### L3. Dead code: `RemoveWorld` never called outside tests
- **File:** `internal/store/worlds.go:142`
- Simpler version of `DeleteWorldData` that only deletes the world record
  without cleanup. Could mislead future developers.

### L4. `RegisterWorld` no-op on re-init doesn't update `source_repo`
- **File:** `internal/store/worlds.go:17`
- Uses `INSERT OR IGNORE`. If the sphere.db record persists but world.toml was
  deleted and re-created with a different `--source-repo`, the DB record keeps
  the old value. `world list` would show stale data.

### L5. `store.World` struct lacks JSON tags
- **File:** `internal/store/worlds.go:10`
- Would serialize with PascalCase if ever passed directly to a JSON encoder.
  The cmd layer works around this with a local `worldJSON` struct.

### L6. No world name length limit
- **File:** `internal/config/config.go:37`
- Extremely long names could exceed filesystem path limits or create absurdly
  long tmux session names.

### L7. No git repo validation for `--source-repo`
- **File:** `cmd/world.go:65`
- Validates the path exists and is a directory, but doesn't check it's actually
  a git repository. Any directory is accepted.

### L8. Validation doesn't check `TargetBranch` or `NamePoolPath`
- **File:** `internal/config/world_config.go:91`
- Only validates `Capacity` and `ModelTier`. Invalid values in other fields
  surface as errors downstream rather than at config-load time.

### L9. `os.Exit()` inside `RunE` skips deferred cleanup
- **Files:** `cmd/status.go:67`, `cmd/consul.go:93`, `cmd/session.go:155`,
  `cmd/workflow.go:75`, `cmd/mail.go:153`, `cmd/forge.go:469`
- `os.Exit` terminates immediately without running defers, leaking store
  connections opened earlier in the function.

### L10. Hardcoded `.store` in dry-run message
- **File:** `cmd/world.go:302`
- Uses `filepath.Join(home, ".store", name+".db")` instead of
  `config.StoreDir()`. If `StoreDir()` ever changes, the dry-run message
  would be wrong while the actual deletion would be correct.

### L11. Inconsistent "not found" error patterns
- **Files:** Multiple store files
- Two different patterns: some return `nil, fmt.Errorf("not found")`, others
  return `nil, nil`. The distinction is undocumented.

### L12. Missing indexes for scale
- **File:** `internal/store/schema.go`
- No index on `agents(world, state)`, `escalations(status)`,
  `merge_requests(blocked_by)`, `caravan_items(world)`.

### L13. No `ON DELETE CASCADE` for FK relationships
- **File:** `internal/store/schema.go`
- `labels`, `dependencies`, and `merge_requests` FK references to `work_items`
  have no cascade behavior. Deleting a work item (if implemented) would require
  manual cleanup.

### L14. Dead config field `consul.Config.SourceRepo`
- **File:** `internal/consul/consul.go:25`
- Field exists in the struct with a comment but is never set in `cmd/consul.go`
  and never read in the consul implementation.

### L15. `world delete` can leave partial state on failure
- **File:** `cmd/world.go:338`
- Deletion proceeds: sphere data → world DB → world directory. If any step
  fails after the first, the world is in a half-deleted state with no rollback.

### L16. Duplicate `parseVarFlags` implementations
- **Files:** `cmd/cast.go:84`, `cmd/caravan.go:469`, `cmd/workflow.go:39`
- Three separate implementations of "split key=val pairs". Should be
  consolidated into a shared helper.

### L17. `DeleteWorldData` leaves orphaned empty caravans
- **File:** `internal/store/worlds.go:119`
- Deletes caravan_items for the world but not the parent caravan records,
  which may now be empty.

### L18. Inconsistent `--world` help text
- **Files:** Multiple cmd files
- Some use `"world database name"`, others use `"world name"`.

---

## INFORMATIONAL — Config Wiring Observations

| Item | Status |
|------|--------|
| `ModelTier` is informational only — not enforced at session start | By design, but undocumented |
| `Forge.TargetBranch` defaults redundantly in both config and forge | Correct behavior, dead fallback |
| Unknown TOML keys silently ignored — typos go undetected | UX concern |
| Different JSON shapes between `sol world status` and `sol status` | Likely intentional |

---

## TEST GAPS — Top Priority

### T1. `DeleteWorldData` has zero unit tests
- Critical cascade delete affecting three tables is only exercised through
  integration tests. No unit test verifies transactional behavior or error paths.

### T2. `internal/config/config.go` has no test file
- `Home()`, `StoreDir()`, `EnsureDirs()`, and `ValidateWorldName("")` are
  entirely untested. `Home()` has branching logic (env var → UserHomeDir →
  temp dir fallback).

### T3. `WriteWorldConfig` error paths untested
- Only the happy-path round-trip is tested. No tests for write failures,
  permission errors, or the double-close pattern.

### T4. `world delete` with active sessions untested
- The safety check that refuses deletion when tmux sessions are running
  (cmd/world.go:311-329) has no integration test.

### T5. `os.Setenv` in store test helpers (not parallel-safe)
- `internal/store/store_test.go`, `caravans_test.go`, `status/status_test.go`
  use `os.Setenv` instead of `t.Setenv`. Prevents safe `t.Parallel()` usage.

### T6. Session manager tests have 32 hardcoded `time.Sleep` calls
- `internal/session/manager_test.go` — inherently flaky on slow CI machines.

### T7. Integration tests silently ignore errors from setup commands
- `world_lifecycle_test.go:145-146` — `runGT` return values for `world init`
  are discarded. If init fails, subsequent tests pass vacuously.

---

## What's Clean

- All SQL uses parameterized queries — zero injection risk
- SQLite pragmas (WAL, busy_timeout, foreign_keys) correctly set in DSN
- Config three-layer resolution (defaults → sol.toml → world.toml) works correctly
- Every world-scoped command gates through `RequireWorld` (except session start)
- Config consumers all actually USE the config they load — no dead wiring
- Error messages consistently include context (`"failed to <verb> <noun> %q: %w"`)
- `worlds` V5 schema is idempotent (`CREATE TABLE IF NOT EXISTS`)
- Integration test coverage for config consumers (capacity, name pool, quality gates, source repo) is solid
- No TODOs, FIXMEs, or placeholder code found
- Thread safety is adequate for current usage patterns

# Prompt 01: Arc 1 Review — Bugs and Store Hardening

You are fixing bugs and hardening the store layer found during the Arc 1
review pass. These are correctness issues that should be fixed before
moving to Arc 2.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 prompts 01–05 are complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Fix `world list --json` with zero worlds

**File:** `cmd/world.go`, `worldListCmd.RunE`

**Bug:** When there are no worlds, the empty-list check (prints
`"No worlds initialized."`) runs before the `--json` check. So
`sol world list --json` with no worlds prints plain text — invalid JSON.

**Fix:** Move the `--json` check above the empty-list check, or handle
the empty case inside the JSON branch:

```go
if worldListJSON {
    if len(worlds) == 0 {
        fmt.Println("[]")
        return nil
    }
    // ... existing JSON encoding ...
} else {
    if len(worlds) == 0 {
        fmt.Println("No worlds initialized.")
        return nil
    }
    // ... existing table output ...
}
```

---

## Task 2: Fix `caravan launch` source repo resolution

**File:** `cmd/caravan.go`, `caravanLaunchCmd.RunE`

**Bug:** `caravan launch` uses `dispatch.DiscoverSourceRepo()` (CWD-based)
instead of `dispatch.ResolveSourceRepo(worldCfg)` (config-first). This is
the only world-scoped dispatch path that ignores `world.toml`'s
`source_repo`. Running `sol caravan launch` from outside a git repo fails
even when `world.toml` has `source_repo` configured.

**Fix:** Load world config and use `ResolveSourceRepo`, matching the
pattern in `cmd/cast.go` and `cmd/forge.go`:

1. After the `RequireWorld` check, load world config:
   ```go
   worldCfg, err := config.LoadWorldConfig(caravanWorld)
   if err != nil {
       return err
   }
   ```
2. Replace `dispatch.DiscoverSourceRepo()` with
   `dispatch.ResolveSourceRepo(worldCfg)`
3. Remove the old error message that says "must run sol caravan launch
   from within a git repository" — the new function has its own error.
4. Pass `WorldConfig: &worldCfg` in each `CastOpts` inside the
   dispatch loop to avoid redundant config reloading inside `Cast()`.

Add the `"github.com/nevinsm/sol/internal/config"` import if not present.

---

## Task 3: Fix `WriteWorldConfig` discarding close errors

**File:** `internal/config/world_config.go`, `WriteWorldConfig`

**Bug:** `defer f.Close()` after `toml.Encode` silently discards the
close error. On certain filesystems, `Close()` can fail if data wasn't
flushed.

**Fix:** Close explicitly on the success path:

```go
f, err := os.Create(path)
if err != nil {
    return fmt.Errorf("failed to create %s: %w", path, err)
}
defer f.Close()

enc := toml.NewEncoder(f)
if err := enc.Encode(cfg); err != nil {
    return fmt.Errorf("failed to write %s: %w", path, err)
}
return f.Close()
```

Also fix the `MkdirAll` error message to include the path:

```go
return fmt.Errorf("failed to create config directory %q: %w", dir, err)
```

---

## Task 4: Fix `LoadWorldConfig` swallowing non-NotExist stat errors

**File:** `internal/config/world_config.go`, `LoadWorldConfig`

**Bug:** If `os.Stat(sol.toml)` returns a permission-denied error, it's
silently treated as "file doesn't exist" and defaults are used. Same for
`world.toml`.

**Fix:** Check for `os.IsNotExist` explicitly on both stat calls:

```go
// Layer global config.
globalPath := GlobalConfigPath()
if _, err := os.Stat(globalPath); err == nil {
    if _, err := toml.DecodeFile(globalPath, &cfg); err != nil {
        return cfg, fmt.Errorf("failed to parse %s: %w", globalPath, err)
    }
} else if !os.IsNotExist(err) {
    return cfg, fmt.Errorf("failed to check %s: %w", globalPath, err)
}

// Layer world config.
worldPath := WorldConfigPath(world)
if _, err := os.Stat(worldPath); err == nil {
    if _, err := toml.DecodeFile(worldPath, &cfg); err != nil {
        return cfg, fmt.Errorf("failed to parse %s: %w", worldPath, err)
    }
} else if !os.IsNotExist(err) {
    return cfg, fmt.Errorf("failed to check %s: %w", worldPath, err)
}
```

---

## Task 5: Fix silent `time.Parse` errors in store layer

**File:** `internal/store/worlds.go` — `GetWorld` and `ListWorlds`

**Bug:** `time.Parse(time.RFC3339, ...)` errors are silently discarded
with `_`. Corrupt timestamps become zero-value `time.Time` with no signal.

**Fix:** Return parse errors in both functions. In `GetWorld`:

```go
w.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
if err != nil {
    return nil, fmt.Errorf("failed to parse created_at for world %q: %w", name, err)
}
w.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
if err != nil {
    return nil, fmt.Errorf("failed to parse updated_at for world %q: %w", name, err)
}
```

In `ListWorlds`, return the error with the world name from `w.Name`
(which is already scanned at that point):

```go
w.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
if err != nil {
    return nil, fmt.Errorf("failed to parse created_at for world %q: %w", w.Name, err)
}
w.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
if err != nil {
    return nil, fmt.Errorf("failed to parse updated_at for world %q: %w", w.Name, err)
}
```

You need to declare `var err error` at the top of the `for rows.Next()`
loop body or restructure to avoid the short `:=` for `rows.Scan`.

**Note:** The same silent `time.Parse` pattern exists in
`internal/store/agents.go` (lines 54-55, 117-118, 156-157). Fix those
too — apply the same pattern: return `fmt.Errorf` with context including
the agent ID/name.

---

## Task 6: Rename stale migration test names

**File:** `internal/store/store_test.go`

`TestMigrateSphereV4` now tests V5. `TestMigrateSphereV1ToV4` now tests
V1-to-V5. Rename them:

- `TestMigrateSphereV4` → `TestMigrateSphereV5`
- `TestMigrateSphereV1ToV4` → `TestMigrateSphereV1ToV5`

---

## Task 7: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Manual verification of the `world list --json` fix:
   ```bash
   export SOL_HOME=/tmp/sol-test-review
   mkdir -p /tmp/sol-test-review/.store
   bin/sol world list --json
   # → should output []
   rm -rf /tmp/sol-test-review
   ```

---

## Guidelines

- Fix each task in order. Each is independent — commit after all are done.
- Do not refactor beyond the scope of each fix.
- All existing tests must continue to pass.
- Commit with message:
  `fix(world): arc 1 review — bugs, store hardening, error handling`

# Prompt 03: Arc 1 Review — Validation and Test Coverage

You are adding input validation to the config layer and filling test
coverage gaps found during the Arc 1 review.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review prompts 01–02 are complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Add `WorldConfig` validation

**File:** `internal/config/world_config.go`

Add a `Validate` method to `WorldConfig` that catches invalid values at
load time instead of letting them cause confusing downstream errors.

```go
// Validate checks that config values are within acceptable ranges.
func (c WorldConfig) Validate() error {
    if c.Agents.Capacity < 0 {
        return fmt.Errorf("agents.capacity must be >= 0, got %d", c.Agents.Capacity)
    }
    if c.Agents.ModelTier != "" {
        switch c.Agents.ModelTier {
        case "sonnet", "opus", "haiku":
            // valid
        default:
            return fmt.Errorf("agents.model_tier must be sonnet, opus, or haiku; got %q", c.Agents.ModelTier)
        }
    }
    return nil
}
```

Call `Validate()` at the end of `LoadWorldConfig`, before returning:

```go
if err := cfg.Validate(); err != nil {
    return cfg, fmt.Errorf("invalid world config for %q: %w", world, err)
}
return cfg, nil
```

---

## Task 2: Add reserved world name check

**File:** `internal/config/config.go`, `ValidateWorldName`

World names like `store` or `runtime` pass the regex but would create
directories that collide with internal paths (`$SOL_HOME/store/` vs
`$SOL_HOME/.store/`). While the dot-prefixed paths (`.store`, `.runtime`)
are correctly rejected by the regex, non-dot versions should be reserved.

Add a reserved name check after the regex check:

```go
var reservedWorldNames = map[string]bool{
    "store":   true,
    "runtime": true,
    "sol":     true,
}

func ValidateWorldName(name string) error {
    if name == "" {
        return fmt.Errorf("world name must not be empty")
    }
    if !validWorldName.MatchString(name) {
        return fmt.Errorf("invalid world name %q: must match [a-zA-Z0-9][a-zA-Z0-9_-]*", name)
    }
    if reservedWorldNames[name] {
        return fmt.Errorf("world name %q is reserved", name)
    }
    return nil
}
```

---

## Task 3: Add `--source-repo` path validation in `world init`

**File:** `cmd/world.go`, `worldInitCmd.RunE`

When `--source-repo` is provided, validate that the path exists and is
a directory before proceeding. A typo'd path should fail at init time,
not at cast time.

After extracting `sourceRepo` and before any store operations, add:

```go
if sourceRepo != "" {
    info, err := os.Stat(sourceRepo)
    if err != nil {
        return fmt.Errorf("source repo path %q: %w", sourceRepo, err)
    }
    if !info.IsDir() {
        return fmt.Errorf("source repo path %q is not a directory", sourceRepo)
    }
}
```

---

## Task 4: Expand hard gate test coverage

**File:** `test/integration/hard_gate_test.go`

The existing tests cover 5 commands. Replace the individual test
functions with a single table-driven test that covers all gated command
families. Keep the existing `TestHardGatePreArc1World` and
`TestHardGatePassesAfterInit` tests as-is.

```go
func TestHardGateAllCommands(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    gtHome := t.TempDir()
    os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

    cases := []struct {
        name string
        args []string
    }{
        // store commands
        {"store create", []string{"store", "create", "--world=noworld", "--title=test"}},
        {"store get", []string{"store", "get", "sol-00000000", "--world=noworld"}},
        {"store list", []string{"store", "list", "--world=noworld"}},
        {"store update", []string{"store", "update", "sol-00000000", "--world=noworld", "--status=closed"}},
        {"store close", []string{"store", "close", "sol-00000000", "--world=noworld"}},
        {"store query", []string{"store", "query", "--world=noworld", "--status=open"}},
        // store dep commands
        {"store dep add", []string{"store", "dep", "add", "sol-00000001", "sol-00000002", "--world=noworld"}},
        {"store dep remove", []string{"store", "dep", "remove", "sol-00000001", "sol-00000002", "--world=noworld"}},
        {"store dep list", []string{"store", "dep", "list", "sol-00000001", "--world=noworld"}},
        // core commands
        {"cast", []string{"cast", "sol-00000000", "noworld"}},
        {"status", []string{"status", "noworld"}},
        {"prime", []string{"prime", "--world=noworld", "--agent=test", "--item=sol-00000000"}},
        // agent commands
        {"agent create", []string{"agent", "create", "--world=noworld", "--name=test", "--role=dev"}},
        {"agent list", []string{"agent", "list", "--world=noworld"}},
        // forge commands
        {"forge queue", []string{"forge", "queue", "noworld"}},
        {"forge ready", []string{"forge", "ready", "noworld"}},
        {"forge blocked", []string{"forge", "blocked", "noworld"}},
        // sentinel commands
        {"sentinel run", []string{"sentinel", "run", "noworld"}},
        // workflow commands
        {"workflow current", []string{"workflow", "current", "--world=noworld", "--agent=test"}},
        {"workflow status", []string{"workflow", "status", "--world=noworld", "--agent=test"}},
        // world commands (that require existing world)
        {"world status", []string{"world", "status", "noworld"}},
        {"world delete", []string{"world", "delete", "noworld", "--confirm"}},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            out, err := runGT(t, gtHome, tc.args...)
            if err == nil {
                t.Fatalf("expected error, got success: %s", out)
            }
            if !strings.Contains(out, "does not exist") {
                t.Fatalf("expected 'does not exist' error, got: %s", out)
            }
        })
    }
}
```

Remove the now-redundant individual gate tests (`TestHardGateStoreCreate`,
`TestHardGateStoreGet`, `TestHardGateCast`, `TestHardGateForgeQueue`,
`TestHardGateStatus`) since the table-driven test covers them.

Note: Some commands may require extra flags to get past argument
validation before hitting the gate. Adjust args as needed — the test
should verify the gate fires, not test argument parsing. If a command
fails with a usage error before reaching `RequireWorld`, it means the
args need adjustment, not that the gate is missing.

---

## Task 5: Add `world init` invalid name test

**File:** `test/integration/world_lifecycle_test.go`

```go
func TestWorldInitInvalidName(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    gtHome := t.TempDir()
    os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

    cases := []struct {
        name    string
        match   string
    }{
        {".hidden", "invalid world name"},
        {"has spaces", "invalid world name"},
        {"", "world name must not be empty"},  // may get cobra arg error instead
        {"store", "reserved"},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            args := []string{"world", "init", tc.name}
            if tc.name == "" {
                args = []string{"world", "init"}  // cobra will reject missing arg
            }
            out, err := runGT(t, gtHome, args...)
            if err == nil {
                t.Fatalf("expected error for name %q, got success: %s", tc.name, out)
            }
            if !strings.Contains(out, tc.match) {
                t.Fatalf("expected %q in error for name %q, got: %s", tc.match, tc.name, out)
            }
        })
    }
}
```

---

## Task 6: Add config validation tests

**File:** `internal/config/world_config_test.go`

Add unit tests for the new `Validate` method:

```go
func TestWorldConfigValidateModelTier(t *testing.T) {
    // Valid tiers: "sonnet", "opus", "haiku", "" (empty is valid — uses default)
    // Invalid: "gpt-4", "claude", "fast"

func TestWorldConfigValidateCapacity(t *testing.T) {
    // Valid: 0, 1, 100
    // Invalid: -1, -100

func TestValidateWorldNameReserved(t *testing.T) {
    // "store", "runtime", "sol" should return error
    // "mystore", "store1" should pass
```

Also add the missing test for invalid `sol.toml` (the existing
`TestLoadWorldConfigInvalidTOML` only tests invalid `world.toml`):

```go
func TestLoadWorldConfigInvalidGlobalTOML(t *testing.T) {
    // Write invalid TOML to sol.toml
    // LoadWorldConfig should return error mentioning sol.toml path
```

---

## Task 7: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test-review
   mkdir -p /tmp/sol-test-review/.store

   # Invalid name
   bin/sol world init ".bad" 2>&1
   # → error: invalid world name

   # Reserved name
   bin/sol world init store 2>&1
   # → error: reserved

   # Invalid source-repo
   bin/sol world init myworld --source-repo=/nonexistent 2>&1
   # → error: no such file or directory

   # Bad model tier (write it manually)
   bin/sol world init myworld --source-repo=/tmp
   echo 'model_tier = "gpt-4"' >> /tmp/sol-test-review/myworld/world.toml
   bin/sol world status myworld 2>&1
   # → error: model_tier must be sonnet, opus, or haiku

   rm -rf /tmp/sol-test-review
   ```

---

## Guidelines

- Validation should be strict but not surprising. The three model tiers
  are documented in the struct comment — enforce them.
- The table-driven gate test is the highest-value change in this prompt.
  Get the args right for each command so the test actually exercises
  `RequireWorld`, not cobra argument validation.
- All existing tests must continue to pass.
- Commit with message:
  `fix(world): arc 1 review — validation, reserved names, test coverage`

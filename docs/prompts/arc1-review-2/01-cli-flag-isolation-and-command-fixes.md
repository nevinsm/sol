# Prompt 01: CLI Flag Isolation and Command Correctness

You are fixing cobra flag binding bugs and CLI command correctness issues
found during the second Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** All Arc 1 review-1 prompts (01–04) are complete. All tests pass.

Read all files mentioned below before making changes.

---

## Task 1: Fix shared flag variables in `cmd/forge.go`

**File:** `cmd/forge.go`

**Bug:** A single `forgeToolboxJSON` bool variable (line ~264) is bound to
6 different subcommands via a loop (line ~697-702). Each `RunE` reads
`forgeToolboxJSON` directly. While this works for single-shot CLI
invocations, it is a correctness bug if `cobra.Command.Execute()` is
called more than once per process (e.g., in integration tests).

**Fix:** Give each command its own local flag variable. Replace the shared
loop binding with per-command flags, and read them via
`cmd.Flags().GetBool("json")` inside each `RunE`:

```go
// Remove the package-level var:
//   var forgeToolboxJSON bool

// Remove the loop in init():
//   for _, cmd := range [...] {
//       cmd.Flags().BoolVar(&forgeToolboxJSON, "json", false, "output as JSON")
//   }

// Instead, register independently:
func init() {
    // ... existing registrations ...

    for _, cmd := range []*cobra.Command{
        forgeReadyCmd, forgeBlockedCmd, forgeClaimCmd,
        forgeRunGatesCmd, forgeCreateResolutionCmd, forgeCheckUnblockedCmd,
    } {
        cmd.Flags().Bool("json", false, "output as JSON")
    }
}
```

Then in each `RunE`, replace `if forgeToolboxJSON {` with:

```go
jsonOut, _ := cmd.Flags().GetBool("json")
if jsonOut {
```

Do this for all 6 forge toolbox commands that currently read
`forgeToolboxJSON`.

---

## Task 2: Fix shared flag variables in `cmd/caravan.go`

**File:** `cmd/caravan.go`

**Bug:** `caravanWorld` is shared across `caravanCreateCmd`,
`caravanAddCmd`, and `caravanLaunchCmd`. `caravanJSON` is shared across
`caravanCheckCmd` and `caravanStatusCmd`. All are read directly in RunE.

**Fix:** Same approach — read via `cmd.Flags().GetString("world")` and
`cmd.Flags().GetBool("json")` inside each `RunE` instead of reading the
shared package-level variables. Then remove the unused package-level vars.

For `caravanWorld`: replace `caravanWorld` references in each RunE with:

```go
world, _ := cmd.Flags().GetString("world")
```

For `caravanJSON`: replace `caravanJSON` references with:

```go
jsonOut, _ := cmd.Flags().GetBool("json")
```

Remove the package-level `caravanWorld` and `caravanJSON` declarations
once no RunE reads them directly. Keep the flag registrations in `init()`
— they still need `&caravanWorld` for `MarkFlagRequired` to work, OR
switch to registering without a backing var:

```go
caravanAddCmd.Flags().String("world", "", "world for the items")
caravanAddCmd.MarkFlagRequired("world")
```

Pick whichever approach is simpler. The key requirement is that each RunE
reads its own command's flag value, not a shared global.

---

## Task 3: Fix shared flag variables in `cmd/workflow.go`

**File:** `cmd/workflow.go`

**Bug:** `wfWorld` and `wfAgent` are shared across 4 subcommands. All
are read directly in RunE.

**Fix:** Same approach as Tasks 1 and 2. In each RunE, read via:

```go
world, _ := cmd.Flags().GetString("world")
agent, _ := cmd.Flags().GetString("agent")
```

Remove the shared `wfWorld` and `wfAgent` package-level vars once no
RunE reads them directly. Keep `wfItem`, `wfVars`, and `wfJSON` if they
are each only used by a single command — verify this before removing.

---

## Task 4: Clean up `cmd/store.go` shared variable

**File:** `cmd/store.go`

**Context:** The `worldFlag` variable is shared across 9 commands, but
every RunE reads via `cmd.Flag("world").Value.String()` — so there is
no functional bug. However, the shared variable is misleading dead code.

**Fix:** Remove the package-level `var worldFlag string` declaration.
Change the flag registration to not use a backing var:

```go
// Before:
storeCreateCmd.Flags().StringVar(&worldFlag, "world", "", "world database name")

// After:
storeCreateCmd.Flags().String("world", "", "world name")
```

Apply to all 9 store/dep commands. Verify no RunE references `worldFlag`
directly (they should all use `cmd.Flag("world")`).

---

## Task 5: Add `RequireWorld` gate to `session start`

**File:** `cmd/session.go`, `sessionStartCmd`

**Bug:** `session start` accepts `--world` but never calls
`config.RequireWorld()`. It is the only world-scoped command without the
gate.

**Fix:** Add the gate when `--world` is provided. In `sessionStartCmd`'s
RunE, after reading the `startWorld` flag value, add:

```go
if startWorld != "" {
    if err := config.RequireWorld(startWorld); err != nil {
        return err
    }
}
```

This keeps `--world` optional (session start is a low-level primitive)
but validates the world exists when specified.

Add the `"github.com/nevinsm/sol/internal/config"` import if not present.

---

## Task 6: Fix `workflow instantiate --item` not passed to `Instantiate()`

**File:** `cmd/workflow.go`, `workflowInstantiateCmd`

**Bug:** The `--item` flag value (`wfItem`) is captured but never
injected into the `vars` map passed to `workflow.Instantiate()`. The
work item ID only appears in the success message, not in the workflow
instance metadata. Users must manually pass `--var issue=<id>` to
associate a work item.

**Fix:** After building the `vars` map (from `--var` flags), inject the
`--item` value if provided:

```go
vars := parseVarFlags(wfVars) // or however vars are built at this point
item, _ := cmd.Flags().GetString("item")
if item != "" {
    vars["issue"] = item
}
```

This should happen before the call to `workflow.Instantiate()`. Read the
Instantiate function in `internal/workflow/workflow.go` to verify that
it reads `vars["issue"]` for the work item ID.

---

## Task 7: Consolidate `parseVarFlags` and add error on malformed input

**Files:** `cmd/cast.go`, `cmd/caravan.go`, `cmd/workflow.go`

**Bug:** Three separate implementations of "split key=val pairs", and
all silently drop entries without `=`.

**Fix:** Create a single shared helper in `cmd/helpers.go` (or a
suitable existing file in `cmd/`):

```go
// parseVarFlags parses key=value flag entries. Returns an error if any
// entry does not contain "=".
func parseVarFlags(vars []string) (map[string]string, error) {
    m := make(map[string]string, len(vars))
    for _, v := range vars {
        parts := strings.SplitN(v, "=", 2)
        if len(parts) != 2 {
            return nil, fmt.Errorf("invalid --var %q: must be key=value", v)
        }
        m[parts[0]] = parts[1]
    }
    return m, nil
}
```

Update all three callers to use this shared function and handle the
returned error. Remove the duplicate implementations from `cast.go`,
`caravan.go`, and `workflow.go`.

Note: The signature changes from `func([]string) map[string]string` to
`func([]string) (map[string]string, error)`. Update all call sites.

---

## Task 8: Remove dead `consul.Config.SourceRepo` field

**File:** `internal/consul/consul.go`

**Bug:** `SourceRepo string` exists in `consul.Config` with a comment
but is never set in `cmd/consul.go` and never read anywhere in the
consul implementation.

**Fix:** Remove the `SourceRepo` field from `consul.Config`. Verify no
code references it (grep for `SourceRepo` in `internal/consul/` and
`cmd/consul.go`).

---

## Task 9: Standardize `--world` help text

**Files:** `cmd/store.go`, `cmd/store_dep.go`, `cmd/caravan.go`,
`cmd/workflow.go`, `cmd/session.go`

**Bug:** Some commands use `"world database name"`, others use
`"world name"`, others use `"world for the items"`.

**Fix:** Standardize all `--world` flag descriptions to `"world name"`.
This is the most concise and accurate description.

---

## Task 10: Verify

1. `make build` — compiles cleanly
2. `make test` — all tests pass
3. Grep verification:
   ```bash
   # No shared flag vars read directly in forge toolbox
   grep -n 'forgeToolboxJSON' cmd/forge.go
   # → should only appear in comments or not at all

   # No shared flag vars read directly in caravan
   grep -n 'caravanWorld\|caravanJSON' cmd/caravan.go
   # → should only appear in flag registration, not in RunE bodies

   # No shared flag vars read directly in workflow
   grep -n 'wfWorld\|wfAgent' cmd/workflow.go
   # → should only appear in flag registration, not in RunE bodies

   # Shared parseVarFlags is in one place
   grep -rn 'func parseVarFlags\|func parseCaravanVarFlags' cmd/
   # → should show exactly one function definition

   # consul.Config has no SourceRepo
   grep -n 'SourceRepo' internal/consul/consul.go
   # → no matches

   # All --world flags say "world name"
   grep -n '"world' cmd/store.go cmd/store_dep.go cmd/caravan.go cmd/workflow.go cmd/session.go | grep -i 'database'
   # → no matches
   ```

---

## Guidelines

- The flag isolation changes are the highest-priority items. Get those
  right — each RunE must read its own command's flag, not a shared global.
- When removing shared vars, make sure no code (including tests) reads
  them directly. Grep thoroughly before deleting.
- The `parseVarFlags` consolidation changes the return signature — update
  every call site.
- All existing tests must continue to pass.
- Commit with message:
  `fix(cli): arc 1 review-2 — flag isolation, RequireWorld gate, var parsing`

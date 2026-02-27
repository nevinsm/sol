# Arc 0 Cleanup, Prompt 3: Test Helpers, Function Names, Fixture Strings

## Context

After prompts 1-2 cleaned up production code, this prompt renames test setup helpers, test function names, CLI test error messages, and test fixture data strings that still use old naming.

The codebase compiles and tests pass. Every change is in test code — no behavioral change.

## What To Change

### 1. setupTown → setupSphere (definition + all callers)

**File:** `internal/store/store_test.go`
- Line 29 comment: `setupTown creates a temporary sphere store` → `setupSphere creates a temporary sphere store`
- Line 30: `func setupTown(t *testing.T) *Store {` → `func setupSphere(t *testing.T) *Store {`

Update all callers (use replace_all for `setupTown(` → `setupSphere(` in each file):
- `internal/store/store_test.go` — ~6 callers
- `internal/store/messages_test.go` — ~9 callers
- `internal/store/convoys_test.go` — ~7 callers
- `internal/store/escalations_test.go` — ~8 callers

### 2. setupRig → setupWorld (definition + all callers)

**File:** `internal/store/store_test.go`
- Line 11 comment: `setupRig creates a temporary world store` → `setupWorld creates a temporary world store`
- Line 12: `func setupRig(t *testing.T) *Store {` → `func setupWorld(t *testing.T) *Store {`

Update all callers (use replace_all for `setupRig(` → `setupWorld(` in each file):
- `internal/store/store_test.go` — ~10 callers
- `internal/store/merge_requests_test.go` — ~12 callers
- `internal/store/dependencies_test.go` — ~9 callers

### 3. Test function names

**File:** `internal/store/store_test.go`
- `TestTownSchemaCreation` → `TestSphereSchemaCreation`
- `TestMigrateTownV2` → `TestMigrateSphereV4`
- `TestMigrateTownV1ToV2` → `TestMigrateSphereV1ToV4`

**File:** `internal/store/convoys_test.go`
- `TestGetConvoyNotFound` → `TestGetCaravanNotFound`
- Any stale "GetConvoy" or "Verify with GetConvoy" in comments → "GetCaravan"

### 4. "gt " → "sol " in CLI test error messages

Use replace_all for `"gt ` → `"sol ` in each file:

**File:** `test/integration/cli_loop1_test.go` — ~11 occurrences
(e.g., `"gt prefect run --help failed"` → `"sol prefect run --help failed"`)

**File:** `test/integration/cli_loop2_test.go` — ~9 occurrences

**File:** `test/integration/cli_loop3_test.go` — ~14 occurrences

**File:** `test/integration/cli_loop4_test.go` — ~11 occurrences

**File:** `test/integration/cli_loop5_test.go` — ~8 occurrences

### 5. "gt-" → "sol-" ID prefixes

**File:** `internal/store/dependencies_test.go`
- Line 42: `"gt-nonexist"` → `"sol-nonexist"`
- Line 47: `"gt-nonexist"` → `"sol-nonexist"`

**File:** `internal/forge/refinery_test.go`
- Line 162: `fmt.Sprintf("gt-%08x", ...)` → `fmt.Sprintf("sol-%08x", ...)`

**File:** `internal/handoff/handoff_test.go`
- Search for any remaining `"gt-"` prefixed IDs and replace with `"sol-"`

### 6. "refinery/Forge" → "forge/Forge"

**File:** `internal/store/merge_requests_test.go`
- Replace all `"refinery/Forge"` → `"forge/Forge"` (~13 occurrences, use replace_all)

### 7. convoy → caravan in test fixture name strings

**File:** `internal/store/convoys_test.go`
- `"test-convoy"` → `"test-caravan"` (use replace_all)
- `"convoy-1"` → `"caravan-1"`, `"convoy-2"` → `"caravan-2"`, `"convoy-3"` → `"caravan-3"`

**File:** `test/integration/loop4_test.go`
- `"test-convoy"` → `"test-caravan"`
- `"multi-rig-convoy"` → `"multi-world-caravan"`

**File:** `test/integration/loop5_test.go`
- `"feed-convoy"` → `"feed-caravan"`
- `"nodup-convoy"` → `"nodup-caravan"`
- `"e2e-convoy"` → `"e2e-caravan"`

## What NOT To Change

- Production code — already done in prompts 1-2
- Test mock signatures (`rig` params) — prompt 4
- Test variable names (`rigName`, `rigStore`, `gtHome`) — prompt 4
- World names (`testrig`, `myrig`) — prompts 5-6
- `runGT`/`gtBin`/`gtHome` in `cli_test.go` and `helpers_test.go` (setupTestEnv) — intentionally kept

## Acceptance Criteria

```bash
make build && make test     # passes

grep -rn 'setupTown\|setupRig\b' --include='*_test.go' .               # no hits
grep -rn 'TestTown\|TestMigrateTown\|TestGetConvoy' --include='*_test.go' .  # no hits
grep -rn '"gt ' --include='*_test.go' test/integration/cli_*            # no hits
grep -rn '"gt-' --include='*_test.go' .                                 # no hits
grep -rn 'refinery/Forge' --include='*_test.go' .                      # no hits
grep -rn '".*-convoy' --include='*_test.go' .                          # no hits
```

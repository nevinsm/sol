# Arc 0 Cleanup, Prompt 4: Test Mock Signatures + Variable Renames

## Context

After prompts 1-3 cleaned up production code, test helpers, and fixture strings, this prompt renames `rig` parameters in all test mock implementations, local variables (`rigName`, `rigStore`, `gtHome`), and stale "rig"/"town" references in test comments and error strings.

The codebase compiles and tests pass. Every change is a variable/parameter rename or comment fix in test code — no behavioral change.

## What To Change

### 1. rig → world in mock method signatures

For each mock, rename the `rig` parameter to `world` in the signature and all body references:

**File:** `internal/prefect/supervisor_test.go`
- Line 34: `func (m *mockSessions) Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

**File:** `internal/sentinel/witness_test.go`
- Line 53: `func (m *mockSessions) Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

**File:** `internal/dispatch/dispatch_test.go`
- Line 29: `func (m *mockSessionManager) Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

**File:** `internal/handoff/handoff_test.go`
- Line 344: `func (m *mockSessionMgr) Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`
- Line 345: `m.started = append(m.started, startCall{name, workdir, cmd, env, role, rig})` → `role, world})`

**File:** `internal/forge/refinery_test.go`
- Line 197: `func (m *mockSphereStore) CreateAgent(name, rig, role string) (string, error)` → `name, world, role`
- Line 200: `id := rig + "/" + name` → `id := world + "/" + name`
- Line 204: `World: rig,` → `World: world,`

**File:** `internal/status/status_test.go`
- Line 27: `func (m *mockSphereStore) ListAgents(rig, state string)` → `world, state`
- Line 33: `if rig != "" && a.World != rig {` → `if world != "" && a.World != world {`

**File:** `test/integration/helpers_test.go`
- Line 301: `func (m *mockSessionChecker) Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

**File:** `test/integration/loop5_test.go`
- Line 972: `func (m *mockPrefectSessions) Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

### 2. setupGTHome → setupSolHome

**File:** `internal/handoff/handoff_test.go`
- Line 15: `func setupGTHome(t *testing.T) string {` → `func setupSolHome(t *testing.T) string {`
- Replace all callers: `setupGTHome(` → `setupSolHome(` (~10 occurrences, use replace_all)

### 3. rigStore → worldStore

**File:** `internal/forge/toolbox_test.go`
- Replace all `rigStore` → `worldStore` (~30 occurrences, use replace_all)

### 4. rigName → worldName

**File:** `internal/consul/deacon_test.go`
- Replace all `rigName` → `worldName` (~62 occurrences, use replace_all)

### 5. gtHome → solHome (local variables in non-CLI test files)

Replace all `gtHome` → `solHome` in each file. Do NOT touch `test/integration/cli_test.go` or `test/integration/helpers_test.go` (setupTestEnv) — those are intentionally kept.

- `internal/consul/deacon_test.go` — ~20 refs
- `internal/consul/heartbeat_test.go` — ~10 refs
- `internal/dispatch/dispatch_test.go` — ~1 ref
- `internal/workflow/workflow_test.go` — ~40 refs
- `internal/session/manager_test.go` — `gtHome` var (~2 refs) + rename `"gt"` directory name to `"sol"` if used as path component
- `test/integration/loop3_test.go` — ~15 refs
- `test/integration/loop5_test.go` — ~20 refs

### 6. "rig" → "world" in test event payloads

**File:** `internal/events/events_test.go`
- Line 22: `"rig": "myrig"` → `"world": "myrig"` (the world name `myrig` changes to `haven` in prompt 6)

**File:** `test/integration/loop3_test.go`
- Lines 190, 339: `"rig": "testrig"` → `"world": "testrig"` (the world name `testrig` changes to `ember` in prompt 5)

### 7. rig → world in SetWorldOpener callbacks

**File:** `test/integration/loop5_test.go`
- All `func(rig string) (*store.Store, error) {` → `func(world string) (*store.Store, error) {` (~8 occurrences)
- Also rename any `rig` usage inside those callbacks

### 8. Stale "rig"/"town" in test comments and error strings

**File:** `test/integration/loop0_test.go`
- Line 345: `"rig DB"` → `"world DB"` (or similar — fix any comment/error referencing "rig")

**File:** `test/integration/loop1_test.go`
- Line 646: `"create rig dir"` → `"create world dir"`

**File:** `test/integration/loop4_test.go`
- Fix all ~15 occurrences: `"rig store"` → `"world store"`, `"rig"` in comments → `"world"`, `"multi-rig"` → `"multi-world"`, `"Alpha task", "Task in alpha rig"` → `"Task in alpha world"`, etc.

**File:** `test/integration/loop5_test.go`
- Fix ~8 occurrences: `"open rig store"` → `"open world store"`

**File:** `test/integration/helpers_test.go`
- Line 72: `"open rig store"` → `"open world store"`

**File:** `test/integration/cli_loop2_test.go`
- Line 176: `"ensure rig DB exists"` → `"ensure world DB exists"`

**File:** `internal/store/convoys_test.go`
- Lines 165, 247: `"rig store"` → `"world store"` in comments

**File:** `internal/forge/refinery_test.go`
- Line 297: `"Quality gates for this rig"` → `"Quality gates for this world"`
- Line 341: path with `/rig` component — if this is a test fixture directory name, rename to `/world` or another fitting name

**File:** `internal/sentinel/witness_test.go`
- Line 381: if comment still says `"no hook"` → `"no tether"`

### 9. "town" → "cove" (world name)

**File:** `test/integration/loop5_test.go`
- Line 916: `sphereStore.CreateAgent("consul", "town", "consul")` → `sphereStore.CreateAgent("consul", "cove", "consul")`
- Fix any other `"town"` references in the same test that serve as a world name

## What NOT To Change

- Production code — already done in prompts 1-2
- `runGT`/`gtBin`/`gtHome` in `test/integration/cli_test.go` — intentionally kept
- `gtHome` in `test/integration/helpers_test.go` (setupTestEnv function) — intentionally kept
- World names `testrig`/`myrig` — prompts 5-6
- Schema SQL old column names — must keep for migration

## Acceptance Criteria

```bash
make build && make test     # passes

grep -rn 'rigStore\|rigName\|setupGTHome' --include='*_test.go' .     # no hits
grep -rn '"rig"' --include='*_test.go' .                               # no hits (payload keys gone)
grep -rn 'gtHome' --include='*_test.go' internal/                      # no hits

# Check remaining rig refs are only in world names (testrig/myrig — prompts 5-6):
grep -rn '\brig\b' --include='*_test.go' . | grep -v 'testrig\|myrig\|caravanrig\|patrolrig\|ember\|haven\|drift\|vigil\|cli_test.go\|helpers_test.go'  # no hits
```

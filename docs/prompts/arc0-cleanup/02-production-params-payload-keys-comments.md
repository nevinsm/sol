# Arc 0 Cleanup, Prompt 2: Production Params, Payload Keys, Comments

## Context

After prompt 1 renamed exported struct fields and types, this prompt renames all remaining old naming in production (non-test) code: function parameter names, event payload keys, and stale comments.

Go does not require parameter names to match between an interface definition and its implementation. These renames are non-breaking — test mocks keep their `rig` parameter names until prompt 4.

The codebase compiles and tests pass. Every change is a parameter rename, string literal change, or comment fix — no behavioral change.

## What To Change

### 1. gtHome → solHome (parameter names)

**File:** `internal/sentinel/witness.go`
- Line 31: `func DefaultConfig(world, sourceRepo, gtHome string) Config {` → param `solHome`
- Line 39: `SolHome: gtHome,` → `SolHome: solHome,`

**File:** `internal/events/curator.go`
- Line 28: `func DefaultChronicleConfig(gtHome string) ChronicleConfig {` → param `solHome`
- Lines 30-31: all `gtHome` refs in body → `solHome`

**File:** `internal/events/reader.go`
- Line 29: `func NewReader(gtHome string, curated bool) *Reader {` → param `solHome`
- Line 35: `gtHome` in body → `solHome`

**File:** `internal/consul/heartbeat.go`
- Line 23: `func HeartbeatPath(gtHome string) string {` → param `solHome`
- Line 24: body ref → `solHome`
- Line 29: `func WriteHeartbeat(gtHome string, hb *Heartbeat) error {` → param `solHome`
- Lines 30, 41, 45: body refs → `solHome`
- Line 54: `func ReadHeartbeat(gtHome string) (*Heartbeat, error) {` → param `solHome`
- Line 55: body ref → `solHome`

### 2. townStore → sphereStore (parameter names)

**File:** `internal/escalation/mail.go`
- Line 10 comment: `via the town store` → `via the sphere store`
- Line 16: `func NewMailNotifier(townStore *store.Store) *MailNotifier {` → param `sphereStore`
- Line 17: `return &MailNotifier{store: townStore}` → `sphereStore`

**File:** `internal/escalation/router.go`
- Line 56: `func DefaultRouter(logger *events.Logger, townStore *store.Store, webhookURL string) *Router {` → param `sphereStore`
- Line 58: `mailN := NewMailNotifier(townStore)` → `NewMailNotifier(sphereStore)`

### 3. rig → world (interface params, function params, local vars)

**File:** `internal/sentinel/witness.go`
- Line 46: `ListAgents(rig string, state string)` → `ListAgents(world string, state string)`
- Line 48: `CreateAgent(name, rig, role string)` → `CreateAgent(name, world, role string)`
- Line 62: `Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

**File:** `internal/prefect/supervisor.go`
- Line 24: `Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`
- Line 31: `ListAgents(rig string, state string)` → `ListAgents(world string, state string)`

**File:** `internal/handoff/handoff.go`
- Line 36: `Start(name, workdir, cmd string, env map[string]string, role, rig string) error` → `role, world string`

**File:** `internal/forge/refinery.go`
- Line 35: `CreateAgent(name, rig, role string)` → `CreateAgent(name, world, role string)`

**File:** `internal/session/manager.go`
- Line 115: `func (m *Manager) Start(name, workdir, cmd string, env map[string]string, role, rig string) error {` → `role, world string`
- Line 144: `World: rig,` → `World: world,`

**File:** `internal/workflow/workflow.go` (8 functions — use find-and-replace for the parameter name `rig` in function signatures and bodies)
- Line 67: `func WorkflowDir(rig, agentName string)` → `world, agentName`
- Line 68: `filepath.Join(config.Home(), rig,` → `world,`
- Line 222: `func Instantiate(rig, agentName, formulaName string,` → `world, agentName,`
- Lines in body: `WorkflowDir(rig,` → `WorkflowDir(world,`
- Line 332: `func ReadState(rig, agentName string)` → `world, agentName`
- Line 351: `func ReadCurrentStep(rig, agentName string)` → `world, agentName`
- Lines in body: `ReadState(rig,` → `ReadState(world,`
- Line 365: `func ReadInstance(rig, agentName string)` → `world, agentName`
- Line 383: `func ListSteps(rig, agentName string)` → `world, agentName`
- Line 419: `func Advance(rig, agentName string)` → `world, agentName`
- Line 428: error message `"in rig %q"` → `"in world %q"`
- Line 506: `func Remove(rig, agentName string)` → `world, agentName`
- All body references to the `rig` variable → `world`

**File:** `internal/protocol/hooks.go`
- Line 30: comment `--rig={rig}` → `--world={world}`
- Line 32: `func InstallHooks(worktreeDir, rig, agentName string)` → `worktreeDir, world, agentName`

**File:** `internal/namepool/namepool.go`
- Line 47: comment `in the given rig` → `in the given world`

### 4. "rig" → "world" (event payload keys)

**File:** `internal/prefect/supervisor.go`
- Line 289: `"rig": agent.World,` → `"world": agent.World,`

**File:** `internal/sentinel/witness.go`
- Line 158: `"rig": w.config.World,` → `"world": w.config.World,`

**File:** `cmd/feed.go`
- Line 135: `get("rig")` → `get("world")` (EventRespawn display)
- Line 143: `get("rig")` → `get("world")` (EventConsulPatrol display)
- Line 182: `get("rig")` → `get("world")` (respawn_batch display)

### 5. Stale comments

**File:** `internal/dispatch/flock.go`
- Line 58: `a rig's merge slot` → `a world's merge slot`

**File:** `internal/store/merge_requests.go`
- Line 11: `in the rig database` → `in the world database`
- Line 17: `refinery agent ID` → `forge agent ID`

## What NOT To Change

- Test files — prompts 3-6
- Struct field names (already done in prompt 1)
- Schema SQL in `schema.go` — must keep old names for migration path

## Acceptance Criteria

```bash
make build && make test     # passes

# No old param names in production code:
grep -rn 'gtHome\|townStore' --include='*.go' internal/ cmd/ | grep -v _test.go  # no hits
grep -rn 'func.*[( ,]rig[) ,]' --include='*.go' internal/ cmd/ | grep -v _test.go  # no hits (only test mocks remain)
grep -n 'get("rig")' cmd/feed.go  # no hits
grep -rn '"rig":' --include='*.go' internal/ | grep -v _test.go  # no hits
grep -rn 'rig database\|rig.s\|town store\|refinery agent' --include='*.go' internal/  # no hits
```

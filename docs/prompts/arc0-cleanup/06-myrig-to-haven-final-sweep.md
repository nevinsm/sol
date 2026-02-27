# Arc 0 Cleanup, Prompt 6: myrig → haven + Final Sweep

## Context

After prompts 1-5, this is the final cleanup prompt. It renames the last test world name ("myrig" → "haven") and runs a comprehensive sweep to verify no old naming remains anywhere in the codebase.

The sol naming theme is "space-faring civilization." **haven** — a safe harbor, refuge in the commonwealth.

The codebase compiles and tests pass. Every change is a string literal replacement in test code — no behavioral change.

## What To Change

### 1. myrig → haven

For each file, use `replace_all: true` for `"myrig"` → `"haven"`:

| File | Est. Occurrences |
|------|-----------------|
| `internal/prefect/supervisor_test.go` | ~66 |
| `internal/status/status_test.go` | ~37 |
| `internal/workflow/workflow_test.go` | ~30 |
| `internal/store/store_test.go` | ~28 |
| `test/integration/loop5_test.go` | ~25 |
| `internal/store/messages_test.go` | ~20 |
| `internal/handoff/handoff_test.go` | ~20 |
| `internal/store/convoys_test.go` | ~15 |
| `internal/events/events_test.go` | ~10 |
| `internal/events/curator_test.go` | ~10 |
| `internal/store/escalations_test.go` | ~10 |

### Examples

```
"myrig"           → "haven"
"myrig/Toast"     → "haven/Toast"
"sol-myrig-Toast" → "sol-haven-Toast"
```

### 2. Final Sweep

After the replace, run every grep below. If any returns hits (other than documented intentional leftovers), fix them.

```bash
# ---- Core concept names ----
grep -rn '\brig\b' --include='*.go' . | grep -v schema.go | grep -v prompts/
# Expected: no hits (except maybe "rig" as part of a struct tag or something unexpected)

grep -rn '\btown\b' --include='*.go' . | grep -v prompts/
# Expected: no hits

grep -rn '\bsling\b' --include='*.go' . | grep -v prompts/
# Expected: no hits

grep -rn '"hooked"' --include='*.go' . | grep -v schema.go
# Expected: no hits

grep -rn 'hook_item\|HookItem' --include='*.go' . | grep -v schema.go
# Expected: no hits

grep -rn 'EventSling\|EventDone\b\|EventDeacon' --include='*.go' .
# Expected: no hits

grep -rn 'GT_' --include='*.go' . | grep -v prompts/
# Expected: no hits

# ---- Variable/function names ----
grep -rn 'setupTown\|setupRig\|setupGTHome' --include='*.go' .
# Expected: no hits

grep -rn 'rigStore\|rigName\|townStore' --include='*.go' .
# Expected: no hits

grep -rn 'gtHome\|GTHome' --include='*.go' . | grep -v prompts/ | grep -v cli_test.go | grep -v helpers_test.go
# Expected: no hits

grep -rn 'Refinery' --include='*.go' . | grep -v schema.go | grep -v prompts/
# Expected: no hits

# ---- Test fixture data ----
grep -rn '"gt-' --include='*_test.go' . | grep -v cli_test.go
# Expected: no hits

grep -rn '"gt ' --include='*_test.go' . | grep -v cli_test.go
# Expected: no hits

grep -rn 'convoy' --include='*_test.go' . | grep -v prompts/
# Expected: no hits (file names like convoys_test.go are fine — they match the Go package name)

grep -rn 'testrig\|myrig\|caravanrig\|patrolrig' --include='*.go' .
# Expected: no hits
```

Fix any unexpected hits found by the sweep.

## Intentional Leftovers (do NOT change)

These are expected to remain and are correct:

- `runGT`/`gtBin`/`gtHome` in `test/integration/cli_test.go` and `helpers_test.go` (setupTestEnv) — test binary helper names, tied to binary build path
- `hook_item` and `rig` in `internal/store/schema.go` sphereSchemaV1/V3 — old column names required for migration SQL
- `ALTER TABLE ... RENAME` in sphereSchemaV4 — migration SQL must reference old names
- `flock` anywhere — refers to POSIX flock() syscall, not old component name
- Everything in `docs/prompts/` — historical reference, never modify
- Go file names (`witness.go`, `refinery.go`, `curator.go`, `deacon.go`, `supervisor.go`, `flock.go`, `flock_test.go`, `convoys.go`, `convoys_test.go`) — internal file naming, not user-visible

## Acceptance Criteria

```bash
make build && make test     # passes

# ALL sweep greps above return no hits (only intentional leftovers listed above)
```

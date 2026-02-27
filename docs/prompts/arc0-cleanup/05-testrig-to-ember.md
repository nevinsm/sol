# Arc 0 Cleanup, Prompt 5: testrig → ember

## Context

After prompts 1-4 cleaned up all variable names, mock signatures, and fixture strings, this prompt renames the test world names that still contain "rig". These are purely test fixture strings — the world name a test uses when creating agents, work items, and sessions.

The sol naming theme is "space-faring civilization." Test world names should be short, evocative words fitting the theme:
- **ember** — a glowing remnant, star-themed
- **drift** — movement through space, fits caravan/transport tests
- **vigil** — watchfulness, fits patrol/monitoring tests

The codebase compiles and tests pass. Every change is a string literal replacement in test code — no behavioral change.

## What To Change

### Replacement Order

Replace in this exact order to avoid partial matches (e.g., "caravanrig" contains "rig" but must not become "caravanembe"):

1. `"caravanrig"` → `"drift"` (also catches caravanrig2→drift2, caravanrig3→drift3)
2. `"patrolrig"` → `"vigil"`
3. `"testrig"` → `"ember"` (also catches testrig2→ember2, testrig3→ember3)

### Files

For each file, use `replace_all: true` for each replacement string. Process each file through all three replacements in order.

| File | Est. Occurrences |
|------|-----------------|
| `internal/consul/deacon_test.go` | ~62 (after prompt 4's rigName→worldName) |
| `internal/dispatch/dispatch_test.go` | ~64 |
| `internal/sentinel/witness_test.go` | ~61 |
| `test/integration/loop3_test.go` | ~61 |
| `test/integration/loop5_test.go` | ~44 |
| `test/integration/loop4_test.go` | ~41 |
| `test/integration/loop0_test.go` | ~34 |
| `test/integration/loop1_test.go` | ~30 |
| `test/integration/loop2_test.go` | ~10 |
| `test/integration/helpers_test.go` | ~20 |
| `internal/handoff/handoff_test.go` | ~15 |
| `internal/prefect/supervisor_test.go` | ~10 |
| `internal/events/events_test.go` | ~5 |
| `internal/events/curator_test.go` | ~5 |
| `internal/store/convoys_test.go` | ~10 |
| `internal/workflow/workflow_test.go` | ~5 |
| `internal/forge/refinery_test.go` | ~5 |
| `internal/forge/toolbox_test.go` | ~5 |

### Examples of What Changes

String `"testrig"` as a world name:
```
"testrig"           → "ember"
"testrig/Toast"     → "ember/Toast"
"sol-testrig-Toast" → "sol-ember-Toast"
```

String `"testrig2"` (caught by replacing "testrig"):
```
"testrig2"          → "ember2"
"testrig2/Alpha"    → "ember2/Alpha"
```

String `"caravanrig"`:
```
"caravanrig"        → "drift"
"caravanrig2"       → "drift2"
```

## What NOT To Change

- Production code — no "testrig" exists in production
- `docs/prompts/` — historical reference
- The world name "myrig" — prompt 6

## Acceptance Criteria

```bash
make build && make test     # passes

grep -rn 'testrig\|caravanrig\|patrolrig' --include='*.go' .  # no hits
```

# Arc 0 Review, Prompt 3: Test Code Cleanup

## Context

After prompts 1–2 of this review series, all production code uses the new naming. Event constants, user-visible strings, schema columns, and status values are all updated. This prompt cleans up all remaining old-name references in test files.

The codebase compiles and all tests pass before starting this prompt.

## What To Change

### 1. Work Item ID Prefix: "gt-" → "sol-"

Many test fixtures use hardcoded work item IDs with the old `"gt-"` prefix. Update all to `"sol-"`:

**Files and patterns:**
```
"gt-abc12345"  → "sol-abc12345"
"gt-a1b2c3d4"  → "sol-a1b2c3d4"
"gt-12345678"  → "sol-12345678"
"gt-aaa11111"  → "sol-aaa11111"
"gt-bbb22222"  → "sol-bbb22222"
"gt-ghost123"  → "sol-ghost123"
```

Search all `*.go` files for `"gt-` and update. The exact IDs don't matter — just the prefix.

**Affected files (non-exhaustive):**
- `internal/prefect/supervisor_test.go`
- `internal/status/status_test.go`
- `internal/handoff/handoff_test.go`
- `internal/store/store_test.go`
- `internal/consul/deacon_test.go`
- `test/integration/loop3_test.go`
- `test/integration/loop4_test.go`

### 2. Event Source: "gt" → "sol"

Test files emit events with `Source: "gt"` or pass `"gt"` as the source parameter. Production code already uses `"sol"`. Update all test fixtures:

**Files:**
- `internal/events/events_test.go` — all `"gt"` source strings and assertions
- `internal/events/curator_test.go` — all `Source: "gt"` in event structs
- `test/integration/loop3_test.go` — all `logger.Emit(..., "gt", ...)` calls

Replace `Source: "gt"` → `Source: "sol"` and `"gt"` source parameters → `"sol"`.

### 3. "sling" → "cast" in Test Comments and Error Strings

Replace throughout all test files:
- Comments: `// Sling` → `// Cast`, `// sling` → `// cast`, `// Re-sling` → `// Re-cast`
- Error strings: `t.Fatalf("sling: %v"` → `t.Fatalf("cast: %v"`, `t.Fatalf("sling item 1: %v"` → `t.Fatalf("cast item 1: %v"`, etc.
- `t.Error("tmux session does not exist after sling")` → `"...after cast"`
- Test function/subtest names referencing "sling" (if any)

**Affected files:**
- `test/integration/loop0_test.go`
- `test/integration/loop1_test.go`
- `test/integration/loop2_test.go`
- `test/integration/loop3_test.go`
- `test/integration/loop4_test.go`
- `test/integration/loop5_test.go`
- `internal/events/curator_test.go`
- `internal/sentinel/witness.go` (any remaining comments)

### 4. "hook file" → "tether file" in Test Comments and Error Strings

Replace throughout all test files:
- `"hook file does not exist after sling"` → `"tether file does not exist after cast"`
- `"hook file still exists after done"` → `"tether file still exists after resolve"`
- `"hook file missing after crash"` → `"tether file missing after crash"`
- `"Write hook file."` → `"Write tether file."`
- `"Verify hook file still exists."` → `"Verify tether file still exists."`
- `"hook after re-sling"` → `"tether after re-cast"`
- `"hook_item"` in error messages → `"tether_item"`
- `"Set up hook file"` → `"Set up tether file"`
- `"failed to write hook"` → `"failed to write tether"`

**Affected files:**
- `test/integration/loop0_test.go`
- `test/integration/loop1_test.go`
- `test/integration/loop3_test.go`
- `test/integration/loop5_test.go`
- `internal/handoff/handoff_test.go`

### 5. "witness" → "sentinel" in Test Data

Replace agent IDs and role strings:
- `"myrig/witness"` → `"myrig/sentinel"` (as message recipients, escalation sources)
- `"testrig/witness"` → `"testrig/sentinel"`
- Role string `"witness"` → `"sentinel"` when used as an agent role

**Affected files:**
- `test/integration/loop3_test.go`
- `internal/store/messages_test.go`
- `internal/store/store_test.go`
- `internal/store/escalations_test.go`
- `internal/escalation/notifier_test.go`

### 6. "refinery" → "forge" in Test Data

Replace:
- `"myrig/refinery"` → `"myrig/forge"` (as `CreatedBy` values)
- `"testrig/refinery"` → `"testrig/forge"`

**Affected files:**
- `internal/store/store_test.go`
- `internal/dispatch/dispatch_test.go`

### 7. "polecat" → "agent" (Role Value)

- `internal/store/store_test.go`: `'polecat'` role in SQL INSERT → `'agent'`

### 8. "convoy" → "caravan" Variable Names

Rename local variables in test files:
- `convoyID` → `caravanID`
- `convoy` → `caravan` (when used as a variable holding a Caravan struct)

**Affected files:**
- `test/integration/loop4_test.go`
- `test/integration/loop5_test.go`
- `internal/store/convoys_test.go`

### 9. "convoy" → "caravan" in Test Comments

- `"convoy stays open"` → `"caravan stays open"`
- `"convoy to not be closed"` → `"caravan to not be closed"`
- `"expected convoy to be closed"` → `"expected caravan to be closed"`
- `"Verify convoy status"` → `"Verify caravan status"`
- `"Verify convoy tables exist"` → `"Verify caravan tables exist"`
- `t.Fatalf("convoy ID %q does not match..."` → `t.Fatalf("caravan ID %q does not match..."`
- Table name check strings: `[]string{"convoys", "convoy_items"}` → `[]string{"caravans", "caravan_items"}`

**Affected files:**
- `internal/store/convoys_test.go`

### 10. "rig" → "world" in Test Comments

- `"rig stores"` → `"world stores"`
- `"open town store: %v"` → `"open sphere store: %v"` (in `helpers_test.go`, `loop3_test.go`, `loop4_test.go`, `loop5_test.go`)

### 11. "curator" → "chronicle" in Local Variable Names

**File:** `internal/events/curator_test.go`

Local variables named `curator` (e.g., `curator := NewChronicle(cfg)`) should be renamed to `chronicle`. This is cosmetic but maintains consistency. Apply throughout the file:
- `curator :=` → `chronicle :=`
- `curator.` → `chronicle.`
- Comments: `"Stop curator"` → `"Stop chronicle"`, `"Start new curator"` → `"Start new chronicle"`, `"Start curator with cancellable context"` → `"Start chronicle with cancellable context"`
- Comment: `// Write 10 sling events` → `// Write 10 cast events`
- Comment: `// Simulate curator truncation` → `// Simulate chronicle truncation`

Also in `internal/events/events_test.go`:
- `Source: "curator"` → `Source: "chronicle"`
- `Actor: "curator"` → `Actor: "chronicle"`

### 12. "sling-formula" → "cast-formula" in Test Fixtures

**File:** `test/integration/loop4_test.go`

The test creates a formula named `"sling-formula"` as a test fixture. Rename to `"cast-formula"` throughout.

### 13. POLECAT_DONE → AGENT_DONE in Test Strings

Search for `POLECAT_DONE` in test files and replace with `AGENT_DONE` (or whatever the current protocol message type is — check `internal/store/messages.go` for the actual constant value).

**Affected files:**
- `test/integration/loop3_test.go`
- `internal/store/messages_test.go`

### 14. "done" → "resolve" in Test Comments (Command Context)

Where test comments reference the `done` command:
- `// done` → `// resolve` (when clearly referring to the CLI command)
- `"done: %v"` → `"resolve: %v"` (in error strings from dispatch.Resolve calls)
- `"after done"` → `"after resolve"`

Be careful: `"done"` also appears as a work item status value meaning "completed" — do NOT change those. Only change references to the `done` CLI command.

### 15. CLI Test Helper Names

**File:** `test/integration/cli_test.go`

Per arc0 prompt 1, `runGT`/`gtBin`/`gtHome` were explicitly left unchanged. However, the following error messages should still be updated:
- `t.Fatalf("gt --help failed: ...")` → `t.Fatalf("sol --help failed: ...")`
- `t.Fatalf("gt store --help failed: ...")` → `t.Fatalf("sol store --help failed: ...")`

## Approach

Work through files systematically. For each file:
1. Read it
2. Apply all applicable substitutions
3. Verify the result compiles

Run `make build && make test` periodically as you go, and always at the end.

## What NOT To Change

- `runGT`, `gtBin`, `gtHome` function/variable names — intentionally left per arc0
- Work item status `"done"` / `"closed"` — these are valid status values, not old command names
- Anything in `docs/prompts/` — those are historical

## Acceptance Criteria

```bash
make build && make test     # passes

# No old test fixture data:
grep -rn '"gt-[a-f0-9]' --include='*.go' .          # no hits (old work item IDs)
grep -rn 'Source: "gt"' --include='*.go' .           # no hits
grep -rn '"myrig/witness"' --include='*.go' .        # no hits
grep -rn '"testrig/witness"' --include='*.go' .      # no hits
grep -rn '"polecat"' --include='*.go' .              # no hits
grep -rn 'hook file' --include='*.go' .              # no hits
grep -rn 'POLECAT_DONE' --include='*.go' .           # no hits
grep -rn 'sling-formula' --include='*.go' .          # no hits

# Spot check — no "sling" in test comments (excluding docs/prompts):
grep -rn '"sling' --include='*.go' . | grep -v docs/prompts  # no hits
grep -rn '// [Ss]ling' --include='*.go' .                     # no hits
```

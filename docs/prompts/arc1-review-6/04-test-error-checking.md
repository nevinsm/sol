# Prompt 04: Arc 1 Review-6 — Integration Test Error Checking

You are adding missing error checks to integration test setup paths. Silent failures in test setup invalidate assertions — a test can pass for wrong reasons if setup didn't actually work.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 03 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `test/integration/helpers_test.go` — test helpers and setup patterns
- `test/integration/loop3_test.go` — worst offender for unchecked errors
- `test/integration/loop1_test.go` — some unchecked errors
- `test/integration/loop4_test.go` — some unchecked errors
- `test/integration/loop5_test.go` — some unchecked errors

The pattern for all fixes is the same: find calls where the error return is ignored (assigned to `_` or not captured at all), and add `t.Fatal` / `t.Fatalf` checks.

---

## Task 1: Fix `loop3_test.go`

This file has the most unchecked errors. Search for every instance where error-returning functions are called without checking the error. The pattern to apply:

**Before (unchecked):**
```go
tether.Write(world, agentName, workItemID)
```

**After (checked):**
```go
if err := tether.Write(world, agentName, workItemID); err != nil {
	t.Fatalf("failed to write tether: %v", err)
}
```

Known unchecked calls to find and fix (search for these patterns — line numbers are approximate):

### `tether.Write()` calls (multiple instances)
Search for `tether.Write(` in loop3_test.go. Every call should check the error.

### `os.MkdirAll()` calls
Search for `os.MkdirAll(` — ensure all calls in test setup check the error.

### `DB().Exec()` calls
Search for `.DB().Exec(` — these are raw SQL executions used to set up test state. Every call should check the error:

```go
// Before:
worldStore.DB().Exec("UPDATE work_items SET status = 'working' WHERE id = ?", itemID)

// After:
if _, err := worldStore.DB().Exec("UPDATE work_items SET status = 'working' WHERE id = ?", itemID); err != nil {
	t.Fatalf("failed to update work item: %v", err)
}
```

### `sphereStore.AddCaravanItem()` calls
Search for `AddCaravanItem(` — check the error return.

### `worldStore.UpdateWorkItem()` calls
Search for `UpdateWorkItem(` in test setup (not in assertions) — check the error return.

### `dispatch.Prime()` calls
Search for `dispatch.Prime(` — check the error return.

### `worldStore.Close()` calls
Search for `.Close()` where the error is discarded — if in test cleanup (defer), the error can be safely ignored. If in test setup flow, check it.

---

## Task 2: Fix `loop1_test.go`

Search for unchecked errors:

### `exec.Command(...).Run()` calls
Tmux kill-session commands in test cleanup — these can remain unchecked (best-effort cleanup).

### `tether.Read()` calls
If the result is used in an assertion, the error must be checked first:

```go
// Before:
item, _ := tether.Read(world, agentName)

// After:
item, err := tether.Read(world, agentName)
if err != nil {
	t.Fatalf("failed to read tether: %v", err)
}
```

### `worldStore.GetWorkItem()` calls
Same pattern — check error before using the result.

---

## Task 3: Fix `loop4_test.go` and `loop5_test.go`

### `os.WriteFile()` calls
Search for `os.WriteFile(` in both files — check all error returns in test setup:

```go
// Before:
os.WriteFile(path, data, 0o644)

// After:
if err := os.WriteFile(path, data, 0o644); err != nil {
	t.Fatalf("failed to write file %s: %v", path, err)
}
```

---

## Task 4: Fix `loop0_test.go` and `loop2_test.go`

Scan these files for any remaining unchecked errors in test setup paths. Apply the same pattern. The errors here are fewer, but check:

- `dispatch.Cast()` — should have error checked before using result
- `dispatch.Prime()` — check error
- `dispatch.Resolve()` — if used in setup (not as the thing being tested), check error

**Important:** Do NOT add error checks to the function call that IS the thing being tested. For example, if a test is `TestResolveRollback` and it calls `dispatch.Resolve()` to test the rollback behavior, that call's error IS the assertion — don't wrap it in `t.Fatal`.

---

## Guidelines

- Use `t.Fatalf` (not `t.Errorf`) for setup failures — if setup fails, the rest of the test is meaningless
- Keep error messages descriptive: include what operation failed
- Don't change test logic or assertions — only add error checking to setup/teardown paths
- For `defer` cleanup calls (like `store.Close()`), it's OK to leave the error unchecked — these are best-effort
- For `exec.Command("tmux", "kill-session", ...).Run()` in cleanup, leave unchecked — best-effort cleanup

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Search for unchecked error patterns and verify coverage:
  - `grep -n 'tether.Write(' test/integration/loop3_test.go` — all should have error checks
  - `grep -n '\.DB().Exec(' test/integration/loop3_test.go` — all should have error checks
  - `grep -n 'os.WriteFile(' test/integration/` — all setup calls should have error checks

## Commit

```
fix(test): arc 1 review-6 — add error checking to integration test setup paths
```

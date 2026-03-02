# Prompt 02: Arc 1 Review-4 — Error Handling and Lifecycle Fixes

You are fixing error handling gaps and lifecycle inconsistencies found during the fourth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 01 of arc1-review-4 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/store/schema.go` — migration helpers, V4 migration
- `internal/store/messages.go` — `CountUnread`, `ReadMessage`
- `internal/store/workitems.go` — `UpdateWorkItem`
- `internal/store/agents.go` — `RowsAffected` pattern
- `internal/store/merge_requests.go` — `RowsAffected` pattern
- `internal/store/escalations.go` — `RowsAffected` pattern
- `internal/store/worlds.go` — `RowsAffected` pattern
- `internal/sentinel/sentinel.go` — patrol error handling
- `internal/consul/consul.go` — `Register()`, `log.Printf` usage
- `internal/forge/toolbox.go` — `Push()` error wrapping
- `cmd/world.go` — `world init` stat check
- `cmd/root.go` — `PersistentPreRunE`

---

## Task 1: Fix swallowed errors in V4 migration

**File:** `internal/store/schema.go`, lines 281-315

Six calls to `columnExists`/`tableExists` discard errors with `_`. If these checks fail
(disk I/O, DB corruption), migration steps are silently skipped, leaving the database in
an inconsistent state.

**Fix:** Check each error and return it:

```go
// Pattern for each check — replace the 6 occurrences:
// Before:
if exists, _ := columnExists(s.db, "agents", "hook_item"); exists {
// After:
exists, err := columnExists(s.db, "agents", "hook_item")
if err != nil {
    return fmt.Errorf("V4 migration: failed to check column agents.hook_item: %w", err)
}
if exists {
```

Apply this pattern to all six checks on lines 281, 287, 293, 299, 305, 311. Each error
message should reference the specific table/column being checked.

---

## Task 2: Fix incorrect RowsAffected comments

**Files:** `internal/store/messages.go` (lines 87, 135), `internal/store/escalations.go` (line 151),
`internal/store/worlds.go` (line 107)

The comment `// RowsAffected is always nil for modernc.org/sqlite.` is factually wrong.
`RowsAffected()` returns `(int64, error)`, not a pointer, and modernc.org/sqlite does
support it correctly. The code depends on it working (checks `n == 0` for "not found").

**Fix:** Replace each occurrence of the incorrect comment with:

```go
// RowsAffected error is unlikely with modernc.org/sqlite but check defensively.
n, raErr := result.RowsAffected()
if raErr != nil {
    return fmt.Errorf("failed to check rows affected: %w", raErr)
}
```

Apply to these locations:
- `messages.go` line 87 (`ReadMessage`)
- `messages.go` line 135 (`AckMessage`)
- `escalations.go` line 151 (`ResolveEscalation`)
- `worlds.go` line 107 (`UpdateWorldRepo`)

For the remaining `RowsAffected` calls across the store package (in `agents.go`, `workitems.go`,
`merge_requests.go`, `caravans.go`), apply the same pattern: check the error return instead of
discarding it with `_`. There are approximately 10 more locations. Search for `result.RowsAffected()`
to find them all.

---

## Task 3: Rename CountUnread to CountPending

**File:** `internal/store/messages.go`, line 142

The function `CountUnread` actually counts messages with `delivery = 'pending'`, not messages
with `read = 0`. The existing test explicitly validates that read status does not affect the count.
The name is misleading.

**Fix:** Rename `CountUnread` to `CountPending` throughout the codebase:
- `internal/store/messages.go` — function definition and doc comment
- `internal/store/messages_test.go` — test references
- Search for all callers with `grep -r "CountUnread" .` and update them

Update the doc comment:
```go
// CountPending returns the number of pending (unacknowledged) messages for a recipient.
```

---

## Task 4: Add work item status validation

**File:** `internal/store/workitems.go`, around line 326

`UpdateWorkItem` accepts any arbitrary string as a status value. Add validation.

**Fix:** Add a set of valid statuses and check against it:

```go
// validWorkItemStatuses is the set of allowed work item status values.
var validWorkItemStatuses = map[string]bool{
    "open":     true,
    "tethered": true,
    "working":  true,
    "resolve":  true,
    "done":     true,
    "closed":   true,
}
```

In `UpdateWorkItem`, after the `updates.Status != ""` check (line 326), add:

```go
if updates.Status != "" {
    if !validWorkItemStatuses[updates.Status] {
        return fmt.Errorf("invalid work item status %q", updates.Status)
    }
    sets = append(sets, "status = ?")
    args = append(args, updates.Status)
}
```

Note: include both `"done"` and `"closed"` as valid statuses. The dispatch layer currently uses
`"done"` for resolved items, and `"closed"` appears in conflict resolution. Both are legitimate
terminal states. The documented lifecycle in CLAUDE.md mentions `open → tethered → working →
resolve → closed` but the code uses `"done"` — harmonizing the documentation is deferred.

Add a test:

```go
func TestUpdateWorkItemInvalidStatus(t *testing.T)
```

Verify that an invalid status like `"banana"` returns an error.

---

## Task 5: Log sentinel patrol errors instead of swallowing

**File:** `internal/sentinel/sentinel.go`

Four locations discard patrol sub-operation errors with `_ =` (lines 200, 206, 212, 218).
Failed respawns, zombie cleanup, and progress checks become invisible.

**Fix:** Log the errors using the sentinel's event logger. The sentinel has a `logger` field
(`w.logger`). For each swallowed error, replace `_ =` with a conditional log:

```go
// Line 200 — checkProgress:
if err := w.checkProgress(agent, sessionName); err != nil {
    if w.logger != nil {
        w.logger.Log("sentinel_error", map[string]any{
            "agent": agent.ID, "action": "check_progress", "error": err.Error(),
        })
    }
}

// Line 206 — handleStalled (first occurrence):
if err := w.handleStalled(agent); err != nil {
    if w.logger != nil {
        w.logger.Log("sentinel_error", map[string]any{
            "agent": agent.ID, "action": "handle_stalled", "error": err.Error(),
        })
    }
}
```

Apply the same pattern to lines 212 (`handleZombie`) and 218 (`handleStalled` second occurrence).
Keep the action name descriptive: `"handle_stalled"`, `"handle_zombie"`, `"check_progress"`.

---

## Task 6: Fix consul Register() error handling

**File:** `internal/consul/consul.go`, lines 100-107

`Register()` discards the error from `GetAgent()`. A database error is treated as
"not registered", leading to confusing duplicate-creation attempts.

**Fix:**

```go
func (d *Consul) Register() error {
    agent, err := d.sphereStore.GetAgent("sphere/consul")
    if err == nil && agent != nil {
        return nil // already registered
    }
    // If GetAgent failed (e.g., DB error), fall through to CreateAgent
    // which will fail cleanly if the agent already exists (unique constraint).
    _, createErr := d.sphereStore.CreateAgent("consul", "sphere", "consul")
    return createErr
}
```

Apply the same fix to sentinel's `Register()` in `internal/sentinel/sentinel.go` (around line 126).
The pattern is identical.

---

## Task 7: Fix forge Push() error wrapping

**File:** `internal/forge/toolbox.go`, line 102

`Push()` discards the original exec error entirely — uses `%s` with the output string
and never includes `err`.

**Fix:**

```go
// Before:
return fmt.Errorf("push rejected: %s", strings.TrimSpace(string(out)))
// After:
return fmt.Errorf("push rejected: %s: %w", strings.TrimSpace(string(out)), err)
```

---

## Task 8: Fix world init stat error swallowing

**File:** `cmd/world.go`, lines 44-47

`os.Stat` errors other than "not exist" (e.g., permission denied) are silently ignored,
causing init to proceed on a possibly-existing but inaccessible world.

**Fix:**

```go
// Before:
tomlPath := config.WorldConfigPath(name)
if _, err := os.Stat(tomlPath); err == nil {
    return fmt.Errorf("world %q is already initialized", name)
}

// After:
tomlPath := config.WorldConfigPath(name)
if _, err := os.Stat(tomlPath); err == nil {
    return fmt.Errorf("world %q is already initialized", name)
} else if !os.IsNotExist(err) {
    return fmt.Errorf("failed to check world config %q: %w", tomlPath, err)
}
```

---

## Task 9: Skip EnsureDirs for help and version

**File:** `cmd/root.go`, lines 15-17

`PersistentPreRunE` runs `config.EnsureDirs()` for every command, including `--help` and
`--version`. This creates directories as a side effect of read-only operations.

**Fix:** Gate `EnsureDirs` on whether the command actually needs it:

```go
PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
    // Don't create directories for help or version output.
    if cmd.Name() == "help" || cmd.Name() == "sol" {
        return nil
    }
    return config.EnsureDirs()
},
```

Actually, a cleaner approach: Cobra sets `cmd.CalledAs()` to empty string for `--help` on root.
The simplest fix is to check whether a `RunE`/`Run` function exists on the command — help-only
commands don't have one:

```go
PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
    if cmd.RunE == nil && cmd.Run == nil {
        return nil
    }
    return config.EnsureDirs()
},
```

Choose whichever approach works with the existing command tree. Verify that `sol --help`,
`sol --version`, and `sol world --help` do NOT create directories, while `sol status` still does.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify renamed function: `grep -r "CountUnread" .` returns no results (only `CountPending`)
- Verify no remaining `RowsAffected` errors are swallowed: `grep -n "result.RowsAffected()" internal/store/*.go` — none should discard the error with `_`

## Commit

```
fix(store,sentinel,consul,cmd): arc 1 review-4 — error handling, lifecycle validation, naming
```

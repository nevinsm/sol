# Prompt 01: Arc 1 Review-4 — Bugs in Store, Session, and Cmd

You are fixing confirmed bugs found during the fourth Arc 1 review pass.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 review-3 is complete. `make build && make test` passes.

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/store/worlds.go` — `DeleteWorldData` with LIKE patterns
- `internal/session/manager.go` — all tmux `-t` usage
- `cmd/store.go` — `printQueryTable`, `printQueryJSON`, SELECT-only guard
- `cmd/session.go` — env var parsing
- `cmd/escalation.go` — status label capitalization

---

## Task 1: Fix LIKE prefix matching in DeleteWorldData

**File:** `internal/store/worlds.go`, lines 126-132

The `DeleteWorldData` function uses `LIKE` with pattern `world + "/%"` for cleaning up
messages and escalations. This is a data corruption bug: deleting world `"ha"` would also
delete messages from world `"haven"` because SQL `LIKE 'ha/%'` matches `'haven/Agent'`
(the `%` wildcard consumes `ven/Agent`).

**Fix:** Replace the LIKE queries with exact world-prefix matching. Use `substr` + `instr`
to extract the world portion of the sender/recipient/source and compare exactly:

```go
// Replace the message deletion (lines 126-129):
worldPrefix := world + "/"
if _, err := tx.Exec(
    `DELETE FROM messages WHERE
        (length(sender) > ? AND substr(sender, 1, ?) = ?)
        OR (length(recipient) > ? AND substr(recipient, 1, ?) = ?)`,
    len(worldPrefix), len(worldPrefix), worldPrefix,
    len(worldPrefix), len(worldPrefix), worldPrefix,
); err != nil {
    return fmt.Errorf("failed to delete messages for world %q: %w", world, err)
}

// Replace the escalation deletion (lines 131-133):
if _, err := tx.Exec(
    `DELETE FROM escalations WHERE length(source) > ? AND substr(source, 1, ?) = ?`,
    len(worldPrefix), len(worldPrefix), worldPrefix,
); err != nil {
    return fmt.Errorf("failed to delete escalations for world %q: %w", world, err)
}
```

The key idea: instead of `LIKE 'ha/%'` (which matches `haven/...`), check that the
string starts with exactly `"ha/"` by comparing `substr(source, 1, 3) = 'ha/'`.

Add a test in `internal/store/worlds_test.go` that verifies the fix:

```go
func TestDeleteWorldDataDoesNotAffectSimilarWorlds(t *testing.T)
```

- Create two worlds: `"dev"` and `"dev-staging"`
- Register agents in both: `"dev/Agent"` and `"dev-staging/Agent"`
- Send messages between agents in both worlds
- Create escalations sourced from both worlds
- Delete world `"dev"`
- Verify: `"dev-staging"` agents, messages, and escalations still exist
- Verify: `"dev"` data is gone

---

## Task 2: Fix tmux `-t` prefix matching in session manager

**File:** `internal/session/manager.go`

tmux's `-t` flag uses prefix matching by default. Session `sol-world-Toast` would match
a query for `sol-world-Toast2` (or vice versa). In a system running 10-30+ concurrent agents,
prefix collisions are plausible.

**Fix:** Add `=` prefix for exact matching on all tmux `-t` targets. The `=` prefix tells
tmux to require an exact match.

Create a helper function for building the exact-match target:

```go
// tmuxExactTarget returns a tmux target string that forces exact session matching.
// Without the "=" prefix, tmux uses prefix matching which can target the wrong session.
func tmuxExactTarget(name string) string {
    return "=" + name
}
```

Update all tmux `-t` usage to use this helper. Affected lines:

| Line | Function | Current | Change to |
|------|----------|---------|-----------|
| 148 | Start (set-environment) | `"-t", name` | `"-t", tmuxExactTarget(name)` |
| 188 | Stop (send-keys) | `"-t", name` | `"-t", tmuxExactTarget(name)` |
| 203 | Stop (kill-session) | `"-t", name` | `"-t", tmuxExactTarget(name)` |
| 257 | List (display-message) | `"-t", meta.Name` | `"-t", tmuxExactTarget(meta.Name)` |
| 283 | Health (list-panes) | `"-t", name` | `"-t", tmuxExactTarget(name)` |
| 334 | Capture (capture-pane) | `"-t", name` | `"-t", tmuxExactTarget(name)` |
| 366 | Inject (send-keys) | `"-t", name` | `"-t", tmuxExactTarget(name)` |
| 377 | Exists (has-session) | `"-t", name` | `"-t", tmuxExactTarget(name)` |

**Exception:** `Attach` (line 356) uses `syscall.Exec` with raw args. Change the arg there too:
```go
return syscall.Exec(tmuxPath, []string{"tmux", "attach-session", "-t", tmuxExactTarget(name)}, os.Environ())
```

Also fix the spurious empty string in Stop's send-keys (line 188):
```go
// Before:
interrupt, interruptCancel := tmuxCmd("send-keys", "-t", name, "C-c", "")
// After:
interrupt, interruptCancel := tmuxCmd("send-keys", "-t", tmuxExactTarget(name), "C-c")
```

Existing tests should continue to pass since they use unique session names. No new tests needed
for this fix — the existing `TestStartStop`, `TestCapture`, `TestInject`, etc. already exercise
these code paths.

---

## Task 3: Add rows.Err() check to printQueryTable and printQueryJSON

**File:** `cmd/store.go`

Both `printQueryTable` (line 354) and `printQueryJSON` (line 382) iterate over rows without
checking `rows.Err()` after the loop. If iteration is cut short by a database error, partial
results are silently returned as success.

**Fix:** The function signatures accept a narrow interface. Expand the interface to include `Err()`:

```go
type scanRows interface {
    Next() bool
    Scan(dest ...interface{}) error
    Err() error
}
```

Update both function signatures to use `scanRows`. After the `for rows.Next()` loop in each,
add:

```go
if err := rows.Err(); err != nil {
    return fmt.Errorf("error iterating rows: %w", err)
}
```

In `printQueryTable`, add this check between the loop end and `tw.Flush()`.
In `printQueryJSON`, add this check between the loop end and `return printJSON(results)`.

Then update the caller in the `storeQueryCmd` RunE (around line 296) — the `rows` returned by
`db.Query()` already implements `Err()`, so no change needed there.

---

## Task 4: Harden SELECT-only guard in store query

**File:** `cmd/store.go`, lines 286-289

The current guard only checks `HasPrefix(trimmed, "SELECT")`. A multi-statement query like
`SELECT 1; DROP TABLE work_items` passes the guard.

**Fix:** Add a semicolon check:

```go
trimmed := strings.TrimSpace(strings.ToUpper(querySQL))
if !strings.HasPrefix(trimmed, "SELECT") {
    return fmt.Errorf("only SELECT queries are allowed")
}
if strings.Contains(querySQL, ";") {
    return fmt.Errorf("multi-statement queries are not allowed")
}
```

This is intentionally conservative — no legitimate SELECT query needs a semicolon.

---

## Task 5: Fix silent env var drop in session start

**File:** `cmd/session.go`, lines 55-63

The `--env` parsing loop silently ignores entries without `=`. The `parseVarFlags` helper
in `cmd/helpers.go` already handles this correctly with an error.

**Fix:** Replace the manual parsing loop with `parseVarFlags`:

```go
env, err := parseVarFlags(startEnv)
if err != nil {
    return err
}
```

Verify that `parseVarFlags` exists in `cmd/helpers.go` and returns `(map[string]string, error)`.
If it doesn't exist or has a different signature, create it:

```go
func parseVarFlags(vars []string) (map[string]string, error) {
    result := make(map[string]string)
    for _, v := range vars {
        idx := strings.Index(v, "=")
        if idx < 0 {
            return nil, fmt.Errorf("invalid env var %q: must be KEY=VALUE", v)
        }
        result[v[:idx]] = v[idx+1:]
    }
    return result, nil
}
```

---

## Task 6: Fix capitalization crash in escalation list

**File:** `cmd/escalation.go`, lines 73-74

The ASCII trick `statusLabel[0]-32` crashes on non-lowercase-alpha input.

**Fix:** Replace with a safe approach:

```go
if len(statusLabel) > 0 {
    r := []rune(statusLabel)
    r[0] = unicode.ToUpper(r[0])
    statusLabel = string(r)
}
```

Add `"unicode"` to the imports.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Run the new `TestDeleteWorldDataDoesNotAffectSimilarWorlds` in isolation:
  `go test -v -run TestDeleteWorldDataDoesNotAffectSimilarWorlds ./internal/store/`

## Commit

```
fix(store,session,cmd): arc 1 review-4 — LIKE prefix, tmux exact match, query hardening
```

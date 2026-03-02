# Prompt 04: Arc 1 Review-7 — ID Documentation, Error Format Sweep, Sentinel Pruning

You are fixing documentation mismatches, inconsistent error formats, and a memory leak in sentinel.

**Working directory:** `~/gt-src/`
**Prerequisite:** Prompt 03 of this review is committed (or current main passes `make build && make test`).

Read all files mentioned below before making changes.

---

## Context

Read these files first:
- `internal/store/workitems.go` — `generateWorkItemID()` and its comment
- `internal/store/merge_requests.go` — `generateMergeRequestID()` and its comment
- `internal/store/messages.go` — `generateMessageID()` and its comment
- `internal/store/escalations.go` — `generateEscalationID()` and its comment
- `internal/store/caravans.go` — `generateCaravanID()` and its comment
- `internal/dispatch/flock.go` — error messages
- `internal/sentinel/sentinel.go` — `lastCaptures` map, patrol cycle
- `CLAUDE.md` — conventions section for work item ID format

---

## Task 1: Fix ID length documentation

The ID generation functions use `make([]byte, 8)` + `hex.EncodeToString(b)`, which produces **16 hex characters** (8 bytes = 16 hex digits). But the comments and `CLAUDE.md` say "8 hex chars."

There are two options. Pick the one that matches the existing generated IDs in any test databases: since the code generates 16 hex chars, **update the documentation to match the code**.

### Update CLAUDE.md

Find the line:
```
- Work item IDs: "sol-" + 8 hex chars (e.g., sol-a1b2c3d4)
```

Replace with:
```
- Work item IDs: "sol-" + 16 hex chars (e.g., sol-a1b2c3d4e5f6a7b8)
```

**Note:** The store comments already correctly say "16 hex chars" — only CLAUDE.md needs updating.

---

## Task 2: Fix error format in dispatch/flock.go

**File:** `internal/dispatch/flock.go`

The convention is `"failed to <action> <resource> %q: %w"` — using `%q` (quoted) for resource identifiers. Scan the file and fix any error messages that use `%s` instead of `%q` for the resource identifier. For example:

```go
// Before:
return nil, fmt.Errorf("failed to acquire lock for work item %s: %w", itemID, err)
// After:
return nil, fmt.Errorf("failed to acquire lock for work item %q: %w", itemID, err)
```

Apply this to all error messages in the file where a resource identifier (item ID, agent name, path) uses `%s` instead of `%q`.

---

## Task 3: Prune sentinel lastCaptures on idle transition

**File:** `internal/sentinel/sentinel.go`

The `lastCaptures` map (declared around line 92) stores output hashes for every agent that's been checked. When agents are deleted or their tethers are cleared, old entries remain forever, causing unbounded memory growth on long-running sentinels with agent churn.

Find the place in the patrol cycle where the sentinel processes agents. After iterating through working agents, add cleanup for agents no longer in the working set.

Add a helper method:

```go
// pruneCaptures removes hash entries for agents that are no longer working.
func (w *Sentinel) pruneCaptures(workingAgentIDs map[string]bool) {
    for key := range w.lastCaptures {
        if !workingAgentIDs[key] {
            delete(w.lastCaptures, key)
        }
    }
}
```

Then call it at the end of the patrol cycle. Build the `workingAgentIDs` set from the agents being iterated. The key used in `lastCaptures` is the agent ID — verify this by reading how entries are added to the map.

**Important:** Also prune the `respawnAttempts` map if it exists and uses a similar pattern. Read the code to check.

---

## Task 4: Fix webhook response body drain

**File:** `internal/escalation/webhook.go`, `Notify` method (around line 70-78)

The response body should be drained before close to avoid leaking HTTP connections:

```go
resp, err := n.Client.Do(req)
if err != nil {
    return fmt.Errorf("webhook request failed: %w", err)
}
defer func() {
    io.Copy(io.Discard, resp.Body)
    resp.Body.Close()
}()
```

Make sure `io` is imported.

---

## Verification

- `make build && make test` passes with no failures
- `go vet ./...` passes
- Verify CLAUDE.md ID format matches the actual code output
- Verify all flock.go error messages use `%q` for identifiers
- Manually inspect sentinel pruneCaptures is called during patrol

## Commit

```
fix(store,dispatch,sentinel,escalation): arc 1 review-7 — ID docs, error format, capture pruning, webhook drain
```

# Arc 0 Review, Prompt 1: Event Constants and Production Strings

## Context

Arc 0 renamed the system from `gt` to `sol` with new naming across 4 prompts. Post-review found leftover old names in event constants, user-visible strings, and production code comments. This prompt fixes all production (non-test, non-schema) code.

The codebase compiles and tests pass. Every change here is a rename of a constant, string literal, comment, or function name — no behavioral change.

## What To Change

### 1. Event Constants

**File:** `internal/events/events.go`

Rename these constants and their string values:
```
EventSling = "sling"                 → EventCast = "cast"
EventDone  = "done"                  → EventResolve = "resolve"
EventDeaconPatrol = "deacon_patrol"  → EventConsulPatrol = "consul_patrol"
EventDeaconStaleTether = "deacon_stale_tether" → EventConsulStaleTether = "consul_stale_tether"
EventDeaconCaravanFeed = "deacon_caravan_feed" → EventConsulCaravanFeed = "consul_caravan_feed"
```

Fix these comments (same file):
- Line 14: `// "gt", agent ID, or component name` → `// "sol", agent ID, or component name`
- Line 23: `// work dispatched to agent` — keep
- Line 24: `// agent completed work` — keep
- Lines 25–28: four comments saying `(emitted by refinery CLI toolbox)` → `(emitted by forge CLI toolbox)`
- Line 31: `// supervisor respawned agent` → `// prefect respawned agent`
- Line 35: `// witness patrol completed` → `// sentinel patrol completed`
- Line 39: `// message sent (reserved for Loop 5 Deacon)` → `// message sent (reserved for Loop 5 Consul)`

Fix the parameter name on `NewLogger`:
- Line 67: `func NewLogger(gtHome string)` → `func NewLogger(solHome string)`
- Line 69: `path: filepath.Join(gtHome, ".events.jsonl")` → `path: filepath.Join(solHome, ".events.jsonl")`

### 2. All Consumers of Renamed Constants

After renaming the event constants, update every file that references them. Use your IDE or grep to find all usages:

- `events.EventSling` → `events.EventCast` (in `dispatch.go`, test files, `feed.go`, `curator_test.go`, etc.)
- `events.EventDone` → `events.EventResolve` (same)
- `events.EventDeaconPatrol` → `events.EventConsulPatrol`
- `events.EventDeaconStaleTether` → `events.EventConsulStaleTether`
- `events.EventDeaconCaravanFeed` → `events.EventConsulCaravanFeed`

Note: `EventSling` and `EventDone` are referenced in `internal/events/curator.go` (chronicle aggregation config), test files, and `cmd/feed.go`. Be thorough.

### 3. Feed Command Display Strings

**File:** `cmd/feed.go`

Fix user-visible output strings:
- `"Convoy created: ..."` → `"Caravan created: ..."`
- `"Convoy launched: ..."` → `"Caravan launched: ..."`
- `"Convoy closed: ..."` → `"Caravan closed: ..."`
- `"Deacon patrol #%s: %s stale hooks, %s convoy feeds"` → `"Consul patrol #%s: %s stale tethers, %s caravan feeds"`
- `"Stale hook recovered: ..."` → `"Stale tether recovered: ..."`
- `"Convoy needs feeding: ..."` → `"Caravan needs feeding: ..."`
- `"Sling burst: ..."` → `"Cast burst: ..."`
- `case "sling_batch":` → `case "cast_batch":`

Also fix the payload key references in this file:
- `get("stale_hooks")` → `get("stale_tethers")` (appears twice in the consul patrol line)
- `get("convoy_feeds")` → `get("caravan_feeds")`
- `get("convoy_id")` → `get("caravan_id")`
- `get("rig")` → `get("world")` (in the Caravan launched and batch lines)

### 4. Dispatch — Refinery References

**File:** `internal/dispatch/dispatch.go`

- Line 318: comment `// Refinery gets a special prime context.` → `// Forge gets a special prime context.`
- Line 319: `if agentName == "refinery"` → `if agentName == "forge"`
- Line 320: `return primeRefinery(world)` → `return primeForge(world)`
- Line 468: comment → update
- Line 469: `func primeRefinery(world string)` → `func primeForge(world string)`
- Line 470: `=== REFINERY CONTEXT ===` → `=== FORGE CONTEXT ===`
- Line 472: `Role: refinery (merge queue processor)` → `Role: forge (merge queue processor)`
- Line 184: comment `// 5. Update work item: status → hooked` → `// 5. Update work item: status → tethered` (comment only — the actual status string change is prompt 2)

### 5. CLI Help Text and Flags

**File:** `cmd/resolve.go`
- `Short: "Signal work completion — push branch, update state, clear hook"` → `"Signal work completion — push branch, update state, clear tether"`

**File:** `cmd/consul.go`
- Flag help: `"stale hook timeout"` → `"stale tether timeout"`

**File:** `cmd/agent.go`
- Table header: `"ID\tNAME\tWORLD\tROLE\tSTATE\tHOOK ITEM\n"` → `"ID\tNAME\tWORLD\tROLE\tSTATE\tTETHER ITEM\n"`

### 6. Comments in Production Code

**File:** `internal/consul/deacon.go`
- `// Skip non-agent agents (don't recover witness/refinery/consul).` → `// Skip non-agent agents (don't recover sentinel/forge/consul).`

**File:** `internal/sentinel/witness.go`
- `// The sentinel does NOT re-sling or re-prime.` → `// The sentinel does NOT re-cast or re-prime.`
- `// Idle agent with live session and no hook — zombie.` → `// Idle agent with live session and no tether — zombie.`
- Any other comments referencing "hook" in the tether sense → "tether"
- Any other comments referencing "sling" → "cast"

**File:** `internal/handoff/handoff.go`
- `// 1. Read hook file to get work item ID.` → `// 1. Read tether file to get work item ID.`

**File:** `internal/events/reader.go`
- `// Detect file replacement (e.g., curator truncation).` → `// Detect file replacement (e.g., chronicle truncation).`
- `// The curator atomically renames a new file over the feed path.` → `// The chronicle atomically renames a new file over the feed path.`

**File:** `internal/store/workitems.go`
- Line 12: `// WorkItem represents a tracked work item in a rig database.` → `// WorkItem represents a tracked work item in a world database.`

**File:** `internal/store/messages.go`
- `// Message represents a message in the town database.` → `// Message represents a message in the sphere database.`

### 7. Chronicle Aggregation Config

**File:** `internal/events/curator.go`

The chronicle's aggregation rules likely reference `EventSling` (now `EventCast`) and related constants. After the constant renames, verify the aggregation batch type names:
- If the chronicle produces `"sling_batch"` as an aggregated event type, change it to `"cast_batch"`
- Any comment references to "curator" → "chronicle"

## What NOT To Change (Yet)

- DB schema columns (`hook_item`, `rig`, `convoys`/`convoy_items` tables) — prompt 2
- Work item status value `"hooked"` → `"tethered"` — prompt 2
- Test comments, test fixture data, test variable names — prompt 3
- Event payload keys emitted by consul (`stale_hooks` field name in the emitting code) — prompt 2 if in consul/deacon.go, or prompt 3 if in test code

## Acceptance Criteria

```bash
make build && make test     # passes

# No old event constant names:
grep -rn 'EventSling\b' --include='*.go' .        # no hits
grep -rn 'EventDone\b' --include='*.go' .          # no hits (EventDone specifically)
grep -rn 'EventDeacon' --include='*.go' .          # no hits

# No old display strings in feed.go:
grep -n 'Convoy\|Deacon\|Sling burst\|Stale hook' cmd/feed.go  # no hits
grep -n '"refinery"' internal/dispatch/dispatch.go              # no hits
grep -n 'HOOK ITEM' cmd/agent.go                                # no hits
grep -n 'clear hook' cmd/resolve.go                             # no hits
grep -n 'stale hook' cmd/consul.go                              # no hits
```

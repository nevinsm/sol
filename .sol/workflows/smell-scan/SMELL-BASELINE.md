# Smell-Scan Baseline

The baseline suppresses recurring false-smells and records accepted-but-deferred
smells so the scan does not re-litigate operator decisions every run. Analysis
agents check it before reporting; adversarial triage proposes new candidates for
human review.

This is the smell scan's counterpart to the codebase-scan baseline, with its own
ID namespace so the two never collide.

## Schema

`baseline.json` is a JSON array of entry objects:

| Field       | Type     | Description |
|-------------|----------|-------------|
| `id`        | string   | `SE-{n}` for smell-exempt (false smell), `AS-{n}` for accepted smell (real, deferred). |
| `file`      | string   | Path relative to repo root, or a glob/area for cross-cutting entries. |
| `functions` | string[] | Functions/methods/types involved. May be empty for file- or area-level patterns. |
| `pattern`   | string   | Short description of the pattern that triggers the finding. |
| `decision`  | string   | Why it is baselined: the reasoning for exemption (SE) or deferral (AS). |
| `category`  | string   | `smell_exempt` or `accepted_smell`. |
| `added`     | string   | ISO 8601 date (YYYY-MM-DD). |

### Categories

- **`smell_exempt`** (`SE-{n}`) — the pattern superficially resembles a smell but
  is justified. The code is correct and reasonable as written. Example: a
  per-component cache that ZFC explicitly exempts (a stale read cannot cause
  divergent decisions). These would be re-flagged every scan without an entry.
- **`accepted_smell`** (`AS-{n}`) — a real smell the operator has reviewed and
  intentionally deferred (the fix is not worth it now, or is blocked on a larger
  decision). Tracked so it is not re-surfaced as new.

### Example Entries

```json
[
  {
    "id": "SE-1",
    "file": "internal/ledger/ledger.go",
    "functions": ["sessionKey lookup"],
    "pattern": "In-memory sessionKey to history_id map read across calls",
    "decision": "ZFC scope (principles.md) exempts caches reconstructible from a source of truth. This map rebuilds lazily from durable rows on restart and a stale read cannot cause divergent cross-agent decisions. Not a ZFC violation.",
    "category": "smell_exempt",
    "added": "2026-06-25"
  },
  {
    "id": "AS-1",
    "file": "internal/example/big.go",
    "functions": ["DoEverything"],
    "pattern": "Long multi-responsibility function",
    "decision": "Real SPRAWL smell, but the split is blocked on the pending ADR-00XX interface change. Defer until that lands to avoid refactoring twice.",
    "category": "accepted_smell",
    "added": "2026-06-25"
  }
]
```

The example entries above are illustrative. `baseline.json` ships empty; the
operator populates it from triage candidates after the first run.

## How Analysis Agents Use It

Before reporting, read `baseline.json`. For each candidate finding, compare file
+ (functions or pattern) semantically (not exact string). On a match:

- **`smell_exempt`** — do not report. Reviewed, justified.
- **`accepted_smell`** — do not report as new. Already tracked and deferred.

Note in your output how many findings the baseline suppressed so triage can
confirm it is not over-suppressing.

## How Triage Produces Candidates

Findings rejected as false-smells (or accepted-but-deferred) become candidates,
written to `baseline-candidates.json` using this schema, with:

- `id` = next free `SE-{n}` or `AS-{n}` (check `baseline.json` for the current
  max; never reuse a removed ID).
- `decision` = the triage reasoning.
- `added` = the run date.

Candidates are **not** auto-added. A human reviews and promotes entries to
`baseline.json`. This keeps humans in the loop for every suppression.

## Maintenance

- **Review periodically.** Refactors can invalidate entries. If a baselined area
  is reworked, recheck the entry.
- **Remove stale entries.** When an `accepted_smell` is finally fixed, remove its
  entry so the scan can confirm it is gone. When an `smell_exempt` pattern is
  refactored away, remove it too.
- **Keep IDs stable.** Never reuse an ID. Always increment.

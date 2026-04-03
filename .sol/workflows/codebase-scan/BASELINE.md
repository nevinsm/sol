# Codebase Scan Baseline

The baseline system suppresses known false positives and tracks acknowledged issues so they don't recur in every scan run. Analysis agents check the baseline before reporting, and adversarial triage produces new baseline candidates for human review.

## Schema

`baseline.json` is a JSON array of entry objects. Each entry has these fields:

| Field       | Type     | Description |
|-------------|----------|-------------|
| `id`        | string   | Unique identifier. `CS-{n}` for false positives, `KI-{n}` for known issues. |
| `file`      | string   | File path relative to repo root where the pattern occurs. |
| `functions` | string[] | Function or method names involved. May be empty for file-level patterns. |
| `pattern`   | string   | Short description of the code pattern that triggers the finding. |
| `decision`  | string   | Why this was baselined — the reasoning for suppression. |
| `category`  | string   | One of: `false_positive`, `known_issue`. |
| `added`     | string   | ISO 8601 date (YYYY-MM-DD) when the entry was added. |

### Categories

- **`false_positive`** — The analysis reports an issue that does not actually exist. The code is correct as written, but the pattern superficially resembles a bug. ID format: `CS-{n}` (codebase-scan, sequential).
- **`known_issue`** — The issue is real but intentionally deferred. It has been reviewed and accepted for now. ID format: `KI-{n}` (known-issue, sequential).

### Example Entry

```json
{
  "id": "CS-1",
  "file": "internal/store/world.go",
  "functions": ["OpenWorld"],
  "pattern": "Deferred rows.Close() after rows.Err() check",
  "decision": "Close is called in defer; the err check before Close is intentional for early return logging, not a missed close.",
  "category": "false_positive",
  "added": "2026-04-03"
}
```

## How Analysis Agents Use the Baseline

Before reporting a finding, analysis agents should:

1. Read `baseline.json` from the workflow directory.
2. For each potential finding, check whether it matches a baseline entry by comparing:
   - The file path (`file` field)
   - The function(s) involved (`functions` field)
   - The pattern description (`pattern` field — semantic match, not exact string match)
3. If a match is found:
   - **`false_positive`**: Do not report the finding. It has been reviewed and is not a real issue.
   - **`known_issue`**: Do not report the finding. It is already tracked and intentionally deferred.
4. If no match is found, report the finding normally.

Agents should note in their output how many findings were suppressed by the baseline, so triage can verify the baseline is working correctly and not over-suppressing.

## How Adversarial Triage Produces Baseline Candidates

During adversarial triage, findings that are rejected as false positives become candidates for baseline inclusion. The triage step writes these to a `baseline-candidates.json` file in its output directory using the same schema as `baseline.json`, except:

- The `id` field uses the next available `CS-{n}` or `KI-{n}` number (check existing baseline for the highest current number).
- The `decision` field contains the triage agent's reasoning for rejection.
- The `added` field is the date of the triage run.

These candidates are **not** automatically added to the baseline. A human reviews `baseline-candidates.json` and promotes entries to `baseline.json` if they agree with the suppression rationale. This keeps humans in the loop for all baseline changes.

## Maintenance

- **Review periodically.** Code changes may invalidate baseline entries. If a baselined file is significantly refactored, check whether the entry still applies.
- **Remove stale entries.** If a `known_issue` is fixed, remove its baseline entry so the scan can verify the fix.
- **Keep IDs stable.** Never reuse an ID after an entry is removed. Always increment to the next number.

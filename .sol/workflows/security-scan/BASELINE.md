# Security Scan Baseline

The `baseline.json` file tracks previously-triaged security findings so they aren't re-created as writs on subsequent scan runs.

## Entry Format

Each entry in the `entries` array:

```json
{
  "id": "unique-identifier",
  "disposition": "accepted | false_positive | deferred",
  "rule": "G201 | CVE-2024-XXXXX | MANUAL",
  "cwe": "CWE-89",
  "file": "internal/store/queries.go",
  "pattern": "optional code snippet for fuzzy matching (handles line drift)",
  "reason": "why this disposition was chosen",
  "date": "2026-03-31",
  "expires": "2026-09-30 (optional — for accepted/deferred, when to re-evaluate)"
}
```

## Dispositions

- **accepted**: Known vulnerability, risk accepted by operator. Will not generate fix writs. Should include an expiry date for periodic re-evaluation.
- **false_positive**: Confirmed not a real vulnerability. Will be skipped entirely during triage.
- **deferred**: Real vulnerability, fix deferred. Will still appear in triage findings with a note, but won't generate P0/P1 writs unless severity has changed.

## Workflow

1. Run `sol workflow manifest security-scan --world=sol-dev`
2. After commission completes, review `baseline-updates.md` in the commission output
3. Apply recommended changes to `baseline.json` as appropriate
4. Commit the updated baseline

## Matching

The triage step matches findings to baseline entries by:
1. File path (exact match, with tolerance for line number changes)
2. Rule/CVE ID
3. Code pattern (substring match against the finding's quoted code)

A finding matches if file + rule match, OR if file + pattern match.

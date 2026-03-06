# Scan the Merge Queue

```
echo "=== STEP 2/10: SCAN QUEUE ==="
sol forge ready --world={{world}} --json
```

**If the queue is empty** (empty JSON array `[]`):
- Run `sol forge await --world={{world}} --timeout=30` (blocks until nudge or 30s timeout)
- Go back to step 1 (unblock)

**If MRs are listed**: proceed to claim.

Do NOT proceed without at least one MR in the ready queue.
Do NOT investigate why the queue is empty.
Do NOT explore the codebase while waiting — just run the await command.

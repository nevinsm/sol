# Scan the Merge Queue

```
echo "=== STEP 2/10: SCAN QUEUE ==="
sol forge await --world={{world}} --timeout=30
sol forge ready --world={{world}} --json
```

**Default action**: Run `sol forge await --world={{world}} --timeout=30` first. This blocks until a nudge arrives or 30 seconds elapse. Then check the queue with `sol forge ready --world={{world}} --json`.

**If MRs are listed**: proceed to claim.

**If the queue is empty** (empty JSON array `[]`):
- Run `sol forge await --world={{world}} --timeout=30` again
- Check `sol forge ready --world={{world}} --json` again
- Repeat until MRs appear

Do NOT proceed without at least one MR in the ready queue.
Do NOT investigate why the queue is empty.
Do NOT explore the codebase while waiting — just await and check.

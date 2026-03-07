# Scan the Merge Queue

```
echo "=== STEP 2/10: SCAN QUEUE ==="
sol forge await --world={{world}} --timeout=120
sol forge ready --world={{world}} --json
```

**Default action**: Run `sol forge await --world={{world}} --timeout=120` first. This blocks until a nudge arrives or 120 seconds elapse. Then check the queue with `sol forge ready --world={{world}} --json`.

**If MRs are listed**: proceed to claim.

**If the queue is empty** (empty JSON array `[]`):
- Run `sol forge await --world={{world}} --timeout=120` again
- Check `sol forge ready --world={{world}} --json` again
- Repeat until MRs appear

Do NOT proceed without at least one MR in the ready queue.
Do NOT investigate why the queue is empty.
Do NOT explore the codebase while waiting — just await and check.

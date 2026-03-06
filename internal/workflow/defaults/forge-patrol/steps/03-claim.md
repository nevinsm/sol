# Claim Next MR

```
echo "=== STEP 3/10: CLAIM ==="
sol forge claim --world={{world}} --json
```

Save `mr_id` and `branch` from the JSON response. Both are needed for subsequent steps.

**If claim returns nothing** (no claimable MRs): go back to scan (step 2).

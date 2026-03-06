# Claim Next MR

First, check if the forge is paused:
```
echo "=== STEP 3/10: CLAIM ==="
sol forge status {{world}} --json
```

**If `"paused": true`**:
- Log "forge paused, waiting for resume"
- Run `sol forge await --world={{world}} --timeout=60` (wait for FORGE_RESUMED nudge)
- Go back to step 1 (unblock) — do NOT claim while paused

**If not paused**, claim the next MR:
```
sol forge claim --world={{world}} --json
```

Save `mr_id` and `branch` from the JSON response. Both are needed for subsequent steps.

**If claim returns nothing** (no claimable MRs): go back to scan (step 2).

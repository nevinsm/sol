# Run Quality Gates

```
echo "=== STEP 6/10: GATES ==="
{{gate_command}}
```

**If gates pass**: proceed to push (step 7).

**If gates fail**: apply the Scotty Test to determine fault.

## Scotty Test — Branch-Caused vs Pre-Existing Failure

The Scotty Test determines whether a gate failure was introduced by the outpost
branch or already existed on `{{target_branch}}`.

1. Stash the merged state: `git stash`
2. Run gates on the base branch: `{{gate_command}}`
3. Evaluate:

**Base branch also fails** — pre-existing failure:
- Run `git stash pop` to restore the merged state
- The outpost branch did not cause this failure — proceed to push (step 7)

**Base branch passes** — branch-caused failure:
- Run `git stash drop` to discard the merged state
- Run `sol forge mark-failed --world={{world}} <mr_id>`
- Go back to scan (step 2)

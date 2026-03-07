# Squash-Merge the Outpost Branch

```
echo "=== STEP 5/10: MERGE ==="
git merge --squash origin/<branch>
```

Replace `<branch>` with the branch from the claim response.

**If the merge succeeds cleanly**, check whether it produced any actual changes:

```
git diff --cached --quiet
```

- **Exit code 1** (there ARE staged changes): proceed to gates (step 6).
- **Exit code 0** (NO staged changes — the branch's changes are already on main):
  1. Run `git reset --hard HEAD` to undo the squash-merge staging
  2. Run `sol forge mark-merged --world={{world}} <mr_id>` to mark the MR as already merged
  3. Go back to scan (step 2)

Do NOT proceed to gates or push if the diff is empty. This happens when multiple outposts touch overlapping files and an earlier merge already included the same changes.

**If there are conflicts**:
1. Run `git merge --abort` to clean up
2. Run `sol forge create-resolution --world={{world}} <mr_id>` to send the MR back for conflict resolution
3. Go back to scan (step 2)

Do NOT attempt to resolve conflicts yourself. You are not a developer.

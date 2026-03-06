# Squash-Merge the Outpost Branch

```
echo "=== STEP 5/10: MERGE ==="
git merge --squash origin/<branch>
```

Replace `<branch>` with the branch from the claim response.

**If the merge succeeds cleanly**: proceed to gates (step 6).

**If there are conflicts**:
1. Run `git merge --abort` to clean up
2. Run `sol forge create-resolution --world={{world}} <mr_id>` to send the MR back for conflict resolution
3. Go back to scan (step 2)

Do NOT attempt to resolve conflicts yourself. You are not a developer.

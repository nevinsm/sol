# Sync to Target Branch

```
echo "=== STEP 4/10: SYNC ==="
git fetch origin
git reset --hard origin/{{target_branch}}
```

Ensure the worktree matches the current remote state of `{{target_branch}}` before
attempting the merge. All local state is replaced with the remote branch head.

# Push to Target Branch

```
echo "=== STEP 7/10: PUSH ==="
git diff --cached --quiet
```

**If exit code 0** (no staged changes — empty commit): the branch's changes are already on main. Do NOT commit or push. Run `sol forge mark-merged --world={{world}} <mr_id>` and go back to scan (step 2).

**If exit code 1** (there ARE staged changes): commit and push:

```
git commit -m "<MR title> (<writ_id>)"
git push origin HEAD:{{target_branch}}
```

Use the MR title as the commit message. Include the writ ID for traceability.

**Verify the push**: confirm the remote SHA matches what you pushed.

```
git rev-parse HEAD
git ls-remote origin {{target_branch}}
```

Both SHAs must match. If they do not, something went wrong — mark failed and go back to scan.

**If push is rejected** (another merge landed first):
- Run `sol forge release --world={{world}} <mr_id>`
- Go back to scan (step 2) — the MR will re-enter the queue

Do NOT force-push. Do NOT debug rejection. Release and retry.

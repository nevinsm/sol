# Push to Target Branch

```
echo "=== STEP 7/10: PUSH ==="
git commit -m "<MR title> (<work_item_id>)"
git push origin HEAD:{{target_branch}}
```

Use the MR title as the commit message. Include the work item ID for traceability.

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

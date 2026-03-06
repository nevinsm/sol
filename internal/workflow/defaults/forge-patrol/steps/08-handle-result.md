# Handle Merge Result

If you reached this step, the merge succeeded and was pushed. Mark it merged:

```
echo "=== STEP 8/10: MARK MERGED ==="
sol forge mark-merged --world={{world}} <mr_id>
```

Confirm mark-merged returned successfully before proceeding to loop.

Branching for conflicts, gate failures, and push rejections is handled inline
in earlier steps — those paths go back to scan directly.

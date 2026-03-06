# Unblock Resolved MRs

```
echo "=== STEP 1/10: UNBLOCK ==="
sol forge check-unblocked --world={{world}}
```

Release MRs whose blocking dependencies have been resolved.
No output means nothing was unblocked — that is normal, proceed to scan.

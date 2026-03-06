# Context Health Check

```
echo "=== STEP 10/10: HEALTH CHECK ==="
```

Assess whether you should continue or hand off to a fresh session.

**Continue** if:
- Context is manageable
- No errors have accumulated
- You are still processing MRs effectively

**Hand off** if:
- Context has grown very large (many MRs processed)
- Repeated errors suggest degraded operation
- Tool use is becoming unreliable

To hand off, stop the patrol and let the system respawn a fresh session.
If no handoff is needed, go back to step 1 (unblock).

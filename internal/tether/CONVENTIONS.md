# internal/tether Conventions

Conventions for callers of the tether package. The tether is the durability
primitive that binds a writ to an agent; incorrect use leads to writs silently
picked up by the wrong agent or stuck in tethered state after an agent dies.

## 1. `Read` is single-tether only — do not use for multi-tether agents

`tether.Read` returns the *first* file in the tether directory sorted
lexicographically. It is correct only for agents expected to hold at most one
tether at a time (outpost agents).

**Do not use `tether.Read` for:**

- Envoys (can hold zero or one tether, but `Read` silently picks the wrong
  one if a race deposits two files).
- Forge (holds multiple concurrent tethers for parallel merge tasks).
- Any new agent type that may acquire more than one writ simultaneously.

For those callers, use `tether.ReadSingle` (which returns `ErrMultipleTethers`
when ambiguous) or resolve the active writ from the sphere store via
`agent.ActiveWrit`:

```go
// Safe for agents that may hold multiple tethers:
writID, err := tether.ReadSingle(world, agentName, role)
if errors.Is(err, tether.ErrMultipleTethers) {
    // Disambiguate via the sphere store's authoritative active_writ field.
    writID, err = sphereStore.GetAgentActiveWrit(agentID)
}

// Or: enumerate all tethers and decide in the caller:
ids, err := tether.List(world, agentName, role)
```

## 2. Write/Clear/ClearOne require the dispatch lock

`tether.Write`, `tether.Clear`, and `tether.ClearOne` provide no internal
locking. Callers must hold the appropriate dispatch lock (`AgentLock` or
`WritLock`) before calling any of these. `tether.List` and `tether.IsTethered`
are safe for concurrent reads without a lock.

## 3. Tether files are the source of truth for crash recovery

The tether directory is written to disk before the session is started and
cleared only after the session stops. This means the tether directory outlives
crashes: if a tether file exists after a crash, the writ is still considered
bound to that agent. Sentinel and consul use `tether.IsTethered` /
`tether.List` to detect stale bindings and reap them.

Do not clear tether files as a speculative cleanup step — clear only after
the associated session has been confirmed stopped or the writ has been
definitively resolved.

**See also:** ADR-0025 (`docs/decisions/0025-*.md`) for the directory-per-writ
tether design and crash-recovery rationale.

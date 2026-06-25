# Lens: Named-Principle Adherence

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. Then read
`docs/principles.md` in full: it defines each named principle, its rationale,
its scope, and its enforcement. This lens sweeps the whole tree for code that
drifts from those principles. Category for all findings here: usually **DRIFT**
(or **DIVERGE** for multi-truth, **COUPLE** for layering).

This is a whole-codebase sweep, not an area read. Use `grep`/`rg` to find
candidate sites for each principle, then read the surrounding code before
judging. A principle violation is high-value because it erodes a guarantee the
whole system relies on, so a returning maintainer reasons from a false premise.

## The Principles to Check

### ZFC (Zero Filesystem Cache)

Coordination state (writs, agent state, tether contents, MRs, escalations) must
be derived fresh at point of use, never cached in memory across call
boundaries. Look for in-memory maps/structs that hold coordination state and
are read after a window in which another agent could have mutated the source.

**Scope discipline:** ZFC exempts caches reconstructible from a source of truth.
`principles.md` explicitly names the ledger session map and broker probe cache
as legitimate. Do not flag those (they are baseline `SE` entries). The test is:
can a stale read of this cache cause two agents to make divergent decisions? If
not, it is not a ZFC finding.

### GLASS (Glass Box Operations)

State must be inspectable with `sqlite3`, `cat`, `ls`, `jq`; logs structured and
greppable. Look for state that lives only in memory or in an opaque encoding
where the autarch could not answer "what is each agent doing / what failed and
why" with standard tools. Look for autarch-facing state that violates the
file-primary, DB-as-registry split.

### CRASH (Crash Recovery As Standard Handling)

Every component answers: what survives a crash, what is lost, how it recovers,
recovery time. Look for components holding important state with no documented or
visible re-derivation path on restart; in-memory pending operations that a crash
would silently drop with no recovery.

### DEGRADE (Graceful Degradation)

When a subsystem is down, the system continues in reduced capacity. Look for
code that hard-fails (or blocks execution) on a dependency the manifesto says
should degrade. The tether-is-a-local-file invariant: agent execution must
depend only on tether + worktree, never on a live daemon or the DB.

### EVOLVE (Explicit Migration Paths)

Schemas/configs/layouts versioned; migrations numbered, sequential, idempotent,
forward-only. Look for schema or on-disk-format changes with no migration, or
migrations that are not idempotent (would corrupt on re-run).

### GUPP (Universal Propulsion)

An agent with work on its tether executes immediately. Look for confirmation
loops, polling, or waits inserted where propulsion should be immediate.

## What Makes a Finding (not just a deviation)

Apply the DOCTRINE gates. A principle deviation is a finding only when it
imposes real orientation/reasoning cost: it makes a maintainer trust a guarantee
that does not hold, or it has already produced (or plainly will produce)
divergence. A theoretical, never-triggered deviation in a corner with a
defensible local reason is not worth churn. Quote the code and name the
guarantee it breaks.

## Out of Scope

- Documented coding conventions (error-wrapping format, identifier formats):
  `lens-conventions`.
- Rejected architectural patterns (universal bus, supervision depth, command
  bloat): `lens-rejected-patterns`.

Write findings to `review.md` per the DOCTRINE schema.

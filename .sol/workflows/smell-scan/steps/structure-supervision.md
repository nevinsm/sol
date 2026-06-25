# Structural Review: Supervision and Messaging

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first.

## Focus

Production `.go` files in:

- `internal/sentinel/`, `internal/consul/`, `internal/prefect/`
- `internal/broker/`
- `internal/service/`, `internal/daemon/`, `internal/heartbeat/`
- `internal/nudge/`, `internal/inbox/`, `internal/escalation/`
- `internal/sessionsave/`, `internal/softfail/`

This is the supervision tree (prefect to sentinel/consul to outposts) plus the
messaging and daemon-lifecycle plumbing. The manifesto explicitly rejects
supervision deeper than two levels; this layer is where that creep would appear.

## Look For (local smells)

- **SPRAWL** — patrol loops or health-check functions that have grown many
  responsibilities; a sentinel/consul method that handles several unrelated
  recovery cases in one body.
- **OVERBUILD** — generalized retry/backoff/lifecycle machinery used by one
  caller; configurability nobody sets.
- **DEADWEIGHT** — recovery branches that can never fire, unused heartbeat
  fields, dead daemon states.
- **MISLEAD** — health/liveness functions whose names overstate what they
  actually verify (e.g., an `AllOK` that is vacuously true); comments describing
  removed supervision layers.
- **INCONSIST (within this layer)** — the several daemons (prefect, sentinel,
  consul, broker, ledger, chronicle, forge) handle pidfile/heartbeat/lifecycle
  in shapes that should match but do not. Note in-layer divergence here; the
  cross-tree pattern is `lens-consistency`'s.

## Out of Scope (owned elsewhere)

- DEGRADE / CRASH adherence as a principle: `lens-principles`.
- Supervision-depth-creep as a *rejected pattern*: `lens-rejected-patterns`
  owns the architectural finding. Report concrete in-area structural smells
  here.
- Convention drift: `lens-conventions`.
- Test code: `structure-tests`.

Write findings to `review.md` per the DOCTRINE schema.

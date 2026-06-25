# Lens: Documented-Convention Adherence

Read `.sol/workflows/smell-scan/steps/DOCTRINE.md` first. This lens checks the
whole tree against the project's *written* conventions. The standard is the
documented rule, not your preference: a finding here always cites the doc it
deviates from. Category: usually **DRIFT** (or **INCONSIST**). This is a sweep,
not an area read.

## The Conventions to Check

Read each of these and check the code against its rules:

- `CLAUDE.md` — Conventions, Design Conventions, Testing sections (timestamps in
  RFC3339 UTC, writ ID / session name formats, error-message context format,
  SQLite pragma set, destructive-command `--confirm` vs `--force`, worktree
  excludes, new-component requirements).
- `cmd/CONVENTIONS.md` — `Args:` on every leaf, flag-binding style,
  `SilenceUsage`, command groups, exit-code documentation.
- `internal/runtime/CONVENTIONS.md` — descriptor + interface-method pattern,
  `SOL_SESSION_COMMAND` override, symmetric implementation across runtimes.
- `internal/tether/CONVENTIONS.md` — `Read` single-tether rule, dispatch-lock
  requirements.
- `internal/service/CONVENTIONS.md` — Status exit-code contract, Restart
  rollback.
- `docs/conventions/error-handling.md` (CC-6) — never swallow errors silently;
  ENOENT vs corruption; `store.ErrNotFound` vs transient; `softfail` usage.
- `docs/conventions/state-mutation.md` (CC-7, CC-8) — multi-step mutations
  transactional or with explicit rollback; failure-during-step-2 test required.

## What to Look For

- Code that violates a stated rule (wrong timestamp format, error not wrapped
  with context, a leaf command missing `Args:`, a destructive command bypassing
  `--confirm`, a multi-step mutation with no rollback or transaction).
- **Convention rot in the docs themselves** is also a finding (category OPAQUE,
  but report here): a `CONVENTIONS.md` rule that the code has uniformly moved
  away from, so the doc now misleads. Either the code drifted or the doc is
  stale; flag the mismatch and say which side looks authoritative.
- New components missing a required artifact (status representation, ADR, cli.md
  entry, failure-modes entry, worktree excludes) per `CLAUDE.md`.

## Discipline

Cite the exact rule and the exact violating site(s). If a convention is silent
on something, it is not a convention finding (it may belong to `lens-consistency`
as idiom drift). Do not invent conventions. Apply the DOCTRINE gates: a single
harmless deviation with a local reason is not worth a writ; a pattern of
deviation that makes the convention unreliable is.

## Out of Scope

- Named principles (ZFC/GLASS/etc.): `lens-principles`.
- Undocumented idiom drift: `lens-consistency`.
- ADR currency and CLAUDE.md index accuracy: `lens-orientation`.

Write findings to `review.md` per the DOCTRINE schema.

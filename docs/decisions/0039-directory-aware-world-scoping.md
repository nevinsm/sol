# ADR-0039: Directory-Aware World Scoping for CLI Commands

Status: accepted
Date: 2026-04-10

## Context

sol's CLI has many commands that take a `--world` flag. Operators run them
from various places: inside an envoy worktree, from `$SOL_HOME`, or from
anywhere on the filesystem. Requiring an explicit `--world` on every
invocation is friction, especially inside a worktree where the operator is
clearly already "in" a world.

`internal/config/config.go` (`ResolveWorld`) already implements
directory-aware resolution with the following precedence: explicit flag >
`SOL_WORLD` env > cwd detection (cwd under `$SOL_HOME/{world}/`). Most
commands already call `ResolveWorld` — but several historically bypassed it
and used the raw flag value directly, producing inconsistent operator
experience. Known drift sites at the time of writing: `cmd/cost.go`,
`cmd/migrate.go` (`migrate run`), `cmd/session.go` (`session start`),
`cmd/writ_trace.go`, and `cmd/envoy.go` (`envoy list`).

This ADR codifies the convention so new commands follow it by default and
reviewers have something concrete to cite when it drifts again.

## Decision

1. **Convention**: every CLI command that takes a `--world` flag MUST call
   `config.ResolveWorld(flagValue)` and use its return value, not the flag
   value directly.

2. **Precedence** (canonical — do not change without a superseding ADR):
   1. Explicit `--world` flag (highest priority)
   2. `SOL_WORLD` environment variable
   3. Detected from cwd if cwd is under `$SOL_HOME/{world}/`
   4. Empty string → command-specific behavior: most commands error with a
      helpful message; sphere-wide commands (e.g. `sol cost` with no
      `--world`, `sol migrate run` for sphere migrations) treat empty as
      "all worlds" or "sphere-scoped".

3. **Help text contract**: every `--world` flag's help string must read
   approximately `world name (defaults to $SOL_WORLD or detected from
   current worktree)`. Sphere-wide commands document the empty-world
   fallback explicitly in their `Long` description.

4. **`--all` flag**: for list commands that previously listed across all
   worlds when `--world` was empty (e.g. `envoy list`, `agent list`),
   provide an explicit `--all` flag to preserve cross-world listing as a
   deliberate operator choice rather than an empty-string accident.

## Consequences

- One enforcement sweep is needed to bring the known drift sites in line —
  handled by the `cli-surface-polish-2026-04` caravan (W0.2 and phase 1).
- New CLI commands have a clear template to follow; reviewers can cite
  ADR-0039 when a new command bypasses `ResolveWorld`.
- Backwards-compatible — no operator workflow breaks; the change only adds
  convenience where operators were already typing `--world` redundantly.
- Edge cases pinned:
  - **Nested worktrees**: cwd under `$SOL_HOME/{world}/repo/subdir`
    resolves to `{world}` (the outermost match wins).
  - **Deleted world**: cwd matching a deleted world causes `ResolveWorld`
    to return an error referencing the missing world, not a silent
    fallback.
  - **Overlapping paths**: not possible by construction — worlds are
    siblings under `$SOL_HOME`.
  - **`SOL_WORLD` pointing at a non-existent world**: error, do not
    silently fall back to cwd.
- Sphere-wide commands (`cost`, `migrate`, `status` without args) document
  the empty-world semantics in `--help`.

## Alternatives considered

- **No convention, leave each command to decide**: rejected — that is
  exactly the state we just cleaned up. It produced inconsistent UX and
  silent surprises.
- **Cwd > env precedence**: rejected because operators frequently set
  `SOL_WORLD` in shell rcfiles to lock their working world; cwd would
  override that unexpectedly.
- **Infer world from git remote URL**: rejected as overreach. Too magic,
  and source repos don't map 1:1 to worlds.

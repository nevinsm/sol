# ADR-0005: Refinery as Claude Session + Go Toolbox

Status: accepted (supersedes ADR-0002)
Date: 2026-02-26
Loop: 2 (revision)

## Context

ADR-0002 implemented the refinery as a pure Go process. Every step in the
merge pipeline (poll, claim, merge, test, push) was deterministic — no AI
judgment required. Conflicts resulted in `phase=failed`, deferred to a
future rework pipeline.

At scale (10-30 concurrent polecats), this breaks down:

- **Conflicts are routine**, not exceptional. Branches go stale while
  waiting in queue. The pure Go refinery can't resolve them.
- **Test failures can't be attributed.** Was it the branch or a
  pre-existing issue? The Go process can't make that judgment.
- **No delegation capability.** Complex conflicts that need a developer
  (polecat) to resolve have no path back into the system.

The Gastown prototype solved this with a split architecture: Go code
provides the mechanical toolbox (queue management, claiming, gates, push,
state updates) while Claude runs the patrol loop (rebase with interactive
conflict resolution, test failure attribution, delegation of complex
conflicts to polecats).

## Decision

Refinery becomes a Claude session backed by Go CLI subcommands.

**Claude handles:**
- The patrol loop (scan queue, claim, rebase, test, push, repeat)
- Rebase execution (where conflicts surface)
- Conflict judgment: trivial (resolve directly) vs complex (delegate)
- Test failure attribution
- Wait/retry decisions

**Go handles (as CLI subcommands):**
- `gt refinery ready/blocked/claim/release` — queue management
- `gt refinery run-gates` — quality gate execution
- `gt refinery push` — merge slot acquisition and push
- `gt refinery mark-merged/mark-failed` — state updates
- `gt refinery create-resolution` — conflict delegation
- `gt refinery check-unblocked` — resolution tracking

The `gt refinery run` Go poll loop has been removed. The system has not
been released, so there are no backward-compatibility concerns.

## Consequences

**Benefits:**
- Merge conflicts are resolvable: trivial ones directly, complex ones
  delegated to polecats via conflict-resolution work items
- Test failures can be attributed (branch vs pre-existing)
- The "senior engineer" model: handles easy stuff directly, delegates
  hard stuff intelligently
- All mechanical Go code preserved as CLI subcommands — no logic lost

**Tradeoffs:**
- API cost proportional to queue activity (but gated: only active when
  MRs are in queue)
- Non-deterministic conflict resolution (mitigated by the judgment
  framework in CLAUDE.md and the "when in doubt, delegate" rule)
- Requires Claude API access for functionality (no Go fallback)

**Schema change:**
- `merge_requests.blocked_by` (TEXT, nullable) — tracks which
  conflict-resolution work item is blocking an MR
- `done` flow detects `conflict-resolution` label and uses
  `--force-with-lease` push, skips MR creation, unblocks original MR

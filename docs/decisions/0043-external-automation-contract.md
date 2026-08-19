# ADR-0043: External Automation Contract — SOL_VIA Origin Convention and Cursor-Based Feed Reads

Status: Accepted
Date: 2026-08-19

## Context

Sol's CLI is the natural integration surface for external automations:
notification bridges, CI pipelines, operator-side tooling, agent
frameworks scripting sol from outside. Prior evaluation of external agent
tooling (2026-06) established the boundary that works: external tools run
as separate processes consuming the sol CLI; sol imports, depends on, and
knows nothing about them. Several such consumers are now materializing,
and three gaps in the CLI surface make them harder to build well than
they should be:

1. **No way to read the event stream incrementally.** `sol feed` renders
   events for humans; an external consumer needs a resumable, lossless
   read ("give me everything since my last cursor") or it must choose
   between hammering the CLI and missing events.
2. **Mail is invisible to the event stream.** The `mail_sent` event
   constant exists and the feed can render it, but nothing emits it — so
   an event-driven consumer cannot observe the arrival of mail, one of
   the things most worth observing.
3. **No way to distinguish the channel behind a principal.** When an
   automation sends mail on the autarch's behalf, the audit trail should
   distinguish it from the autarch at a terminal. Mail's schema also has
   a `thread_id` column that the CLI neither sets nor surfaces, so
   conversations cannot round-trip through an external channel.

## Options Considered

For origin attribution specifically:

- **Compound identities** (e.g. `autarch/toolname` as the sender).
  Rejected: collides with the `{world}/{agent}` identity format, and
  conflates the principal (who is speaking) with the channel (how it
  arrived).
- **Per-command flags** (each command grows a `--proxy` flag with its own
  semantics). Rejected: N commands invent N conventions.
- **A generic environment convention (chosen)**: one env var declaring
  the origin channel for every sol command the automation runs, in the
  same family as `SOL_WORLD`/`SOL_AGENT`.

## Decision

1. **`SOL_VIA` origin convention.** An automation driving the sol CLI
   sets `SOL_VIA=<name>` in its process environment (per-command override:
   `--via`). Identity resolution is unchanged — the principal remains
   whatever it is — and records that support it store the origin
   separately. Mail implements it first: a `via` column on messages,
   surfaced in `mail read` and `mail inbox --json`. Other record types
   (writs, escalations, events) may adopt `via` incrementally.

   **Trust statement**: `via` is audit metadata, not a security boundary.
   Sol's CLI has no authentication between local processes; sender
   identity is already self-declared via environment. The security
   boundary remains the host account and whatever authentication the
   external tool applies on its own inbound surface.

2. **Cursor-based feed contract.** `sol feed --json --since=<cursor>`
   returns events plus a next-cursor token, with documented exit codes.
   This is the supported way for an external consumer to observe the
   sphere: resumable, lossless, poll-friendly. The cursor is opaque to
   the consumer.

3. **Mail completes its event and threading plumbing.** `mail send` emits
   `mail_sent` to the event log (and escalation creation is verified to
   emit likewise). `mail send` gains `--thread`; `read`/`inbox --json`
   surface `thread_id`. Sol remains the system of record for
   conversations; external channels map their own threading onto mail's.

4. **A "scripting sol" documentation page** becomes the contract's
   reference: which commands emit `--json`, exit-code semantics,
   read-only vs mutating vs `--confirm`-gated commands, the `SOL_VIA`
   convention, and the feed cursor contract.

## Consequences

- The CLI surface named above becomes a deliberately supported contract:
  changes to it are breaking changes for external consumers and should be
  treated with the same care as any public interface.
- Every future external consumer (bridges, CI, concierge tooling) gets
  origin attribution and event observation for free, identically — no
  tool-specific plumbing accumulates in sol.
- Two independent audit trails become reconcilable by convention: sol's
  event log and the external tool's own log, joined on `via` and event
  ids.
- Sol gains no new dependencies, processes, or knowledge of any specific
  external tool. Specific consumers document their own architecture in
  their own repositories.
- `mail_sent` emission slightly increases event-log volume; the feed and
  chronicle already handle this event type by design.

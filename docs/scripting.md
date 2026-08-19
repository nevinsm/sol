# Scripting sol

Reference for external processes that drive sol from outside: notification
bridges, CI pipelines, operator-side tooling, agent frameworks. This page is
the contract named by [ADR-0043](decisions/0043-external-automation-contract.md)
decision 4 — the `SOL_VIA` origin convention, the feed cursor contract, and
the `--json`/exit-code shape of the commands an external consumer actually
needs.

It is deliberately narrow. For the broader "every structured command gets
`--json`" story and the full contract-tested schema catalog, see
[docs/integration-api.md](integration-api.md) and [docs/api/](api/README.md).
This page covers exactly the surface ADR-0043 named: `SOL_VIA`, `sol feed`'s
cursor mode, and the mail/escalation/status commands an automation polls or
drives. Every flag and exit code below was checked against the built `sol`
binary, not against source comments — if behavior here ever looks wrong,
trust a fresh run of the command over this file and file a fix.

## Command classes

Every command on this page is one of three classes:

- **Read-only** — no state change. Safe to poll on any interval.
- **Mutating** — changes durable state (sends mail, acks a message,
  acknowledges/resolves an escalation, creates an escalation). Runs once its
  side effect has happened; no confirmation step.
- **Confirm-gated** — a mutating, hard-to-undo command that previews its
  effect and exits 1 without acting until you pass `--confirm`. `sol mail
  purge` is the only command of this class on this page; see [the exit-code
  convention in CLAUDE.md](../CLAUDE.md) for the general pattern (`sol world
  delete` is the reference implementation elsewhere in the CLI).

Unless stated otherwise below, every command follows sol's general exit-code
convention: **0 = success, 1 = failure** (including "not found" and
validation errors). Commands with a different or extended contract
(`mail check`'s inverted 0/1, `status`'s 0/1/2 health levels, `feed`'s
cursor-specific 1) document it explicitly in their own `--help` output, and
that documentation is reproduced in the table below.

## The `SOL_VIA` origin convention

An automation driving the sol CLI should set `SOL_VIA=<name>` in its process
environment — a sibling of `SOL_WORLD`/`SOL_AGENT` — so records that support
it can record *how* the request arrived, separately from *who* it claims to
be. Per-command override: `--via`.

```bash
export SOL_VIA=notification-bridge
sol mail send --to autarch --subject "Build failed" --body "..." --json
```

`via` is validated with the same restrictive charset as agent names (letters,
digits, `.`, `_`, `-` — no `/`): it is a channel label, not a compound
identity. An empty `via` is valid and means "no origin channel recorded" —
sol's own internal callers never set one.

**Trust statement** (from ADR-0043): `via` is audit metadata, not a security
boundary. Sol's CLI has no authentication between local processes; sender
identity is already self-declared via environment. The security boundary
remains the host account and whatever authentication the external tool
applies on its own inbound surface. Don't build access control on top of
`via` — build an audit trail on top of it.

**Identity, separately from channel.** `SOL_VIA` names how a request
arrived; it never changes who the request is attributed to. Attribution
(mail sender, writ `created_by`, caravan owner) comes from `SOL_AGENT`/
`SOL_WORLD` if both are set (resolving to the agent identity `world/agent`),
else `autarch`. External consumers already set `SOL_VIA`; they should
usually leave `SOL_AGENT`/`SOL_WORLD` unset, since setting them makes the
consumer's own process an agent principal rather than the autarch acting
through a channel.

Today `via` is implemented on mail only: a `via` column on messages,
surfaced in `mail read` (human and `--json`), `mail inbox --json`, and
`mail send --json`. Other record types (writs, escalations) may adopt it
incrementally — check this page or `--help` before assuming a `--via` flag
exists on a command that doesn't list one below.

## The feed cursor contract

`sol feed` has two unrelated `--since` modes, distinguished by shape, not by
a separate flag:

- **A duration** — `"1h"`, `"30m"` — events from that far back. Works with
  or without `--json`, and with `--follow`. This is the human/interactive
  mode; it is not resumable.
- **An opaque cursor token** — the `next_cursor` a previous cursor read
  returned. This is the resumable mode ADR-0043 decision 2 asks for: "give
  me everything since my last read, losslessly." Cursor mode requires
  `--json` and cannot be combined with `--follow`; output is a single JSON
  object (`{"events": [...], "next_cursor": "..."}`) instead of one JSON
  line per event.

**Bootstrapping a cursor.** A consumer that has never read the feed before
has no cursor to pass. Enter the cursor contract with an *explicitly empty*
`--since`:

```bash
sol feed --json --since=''
```

This is different from leaving `--since` off entirely — `sol feed --json`
(no `--since` at all) stays on the plain mode and prints one JSON line per
event, which is the older, already-widely-used shape for humans and simple
tailing scripts. Only an explicit empty string opts into the cursor
envelope. Both forms read the same events; only the output shape and the
presence of `next_cursor` differ. Save `next_cursor` from the bootstrap
call and pass it back as `--since=<cursor>` on every subsequent call:

```bash
cursor=$(sol feed --json --since='' | jq -r .next_cursor)
while sleep 30; do
  page=$(sol feed --json --since="$cursor")
  echo "$page" | jq -c '.events[]'   # process new events
  cursor=$(echo "$page" | jq -r .next_cursor)
done
```

An increment with no new events returns an empty `"events": []` (never
`null`) and the same, or an advanced, `next_cursor` — that is a normal,
successful outcome, not an error.

**The cursor is opaque.** Do not parse it, do not construct one by hand from
an event's `id`/`occurred_at` fields (both appear in plain JSONL output, but
the cursor's wire encoding is not a public contract and may change between
sol versions) — only ever pass back a token verbatim from a prior
`next_cursor`.

**Invalid or expired cursor.** If the referenced event can no longer be
found in the feed — most commonly because chronicle rotated it out of
retention (both the raw and curated feed files rotate by truncating their
head in place, so a dropped event is gone for good) — the read fails with
exit 1 and an error naming the problem. There is no partial-recovery path:
re-sync by restarting from a fresh bootstrap (`--json --since=''`), same as
the first-run case. A consumer that polls faster than the feed's retention
window should not normally see this; it's the signal that your poll
interval fell too far behind.

Exit codes for `sol feed`:

| Exit | Meaning |
|------|---------|
| 0 | Read succeeded, including an empty increment |
| 1 | Invalid `--since` (bad duration, or a cursor that can't be decoded or whose event has rotated out of the feed), or another error |

## Consumer-facing surface

All commands below accept `--json`. Exit codes are `0`/`1` (general
convention) unless noted.

| Command | Class | `--json` | Exit codes | Notes |
|---|---|---|---|---|
| `sol mail send` | mutating | yes | 0/1 | Emits `mail_sent` to the event feed. Recipient must resolve to `world/agent` or `autarch` — pass `--world` or set `SOL_WORLD` if `--to` is a bare agent name. |
| `sol mail inbox` | read-only | yes | 0/1 | Lists pending (unacked) messages for an identity. `--identity` defaults to `SOL_WORLD`/`SOL_AGENT` if set, else `autarch`. |
| `sol mail read <id>` | mutating | yes | 0/1 | Marks the message read as a side effect of reading it. `--json` support added by this writ. |
| `sol mail ack <id>` | mutating | yes | 0/1 | Marks acknowledged (implicitly marks read too). |
| `sol mail check` | read-only | no | **0 = unread exist, 1 = none** | Inverted convention — built for `sol mail check && sol mail inbox --json`-style scripting, not a health probe. |
| `sol mail purge` | confirm-gated | no | 0 success / **1 = preview-only (no `--confirm`) or bad args** | Deletes acknowledged messages. Without `--confirm`, previews the count and exits 1; nothing is deleted. `--json` is not currently available — see follow-ups. |
| `sol escalate <description>` | mutating | yes | 0/1 | Creates an escalation and routes it (event log, optional webhook — routing failure is a warning, not a command failure). |
| `sol escalation list` | read-only | yes (array) | 0/1 | Defaults to open+acknowledged only; `--all` includes resolved; `--status` filters explicitly. |
| `sol escalation ack <id>` | mutating | yes | 0/1 | |
| `sol escalation resolve <id>` | mutating | yes | 0/1 | |
| `sol status [world]` | read-only | yes | **sphere-only: always 0. World/combined: 0 = healthy, 1 = unhealthy, 2 = degraded** | With no world argument and none detected from `SOL_WORLD`/cwd, shows the sphere overview and always exits 0 regardless of what it finds degraded. |
| `sol feed` | read-only | yes | 0/1 (cursor-specific — see above) | See the cursor contract section above; `--json` without `--since` is plain JSONL, not the cursor envelope. |

### A JSON shape you should not assume is symmetric

`sol escalate --json`, `sol escalation ack --json`, and `sol escalation
resolve --json` all emit a single escalation using field names `component`
(the source/sender) and `message` (the description). `sol escalation list
--json` emits an *array* using different field names for the same data:
`source` and `description`. This is intentional — the single-record and
list shapes were named independently — but it means you cannot reuse one
JSON-decoding struct for both; match field names to the specific subcommand
you called. Separately, `sol escalation ack`/`resolve --json` always emit
`"world": ""` (the field exists but is never populated on those two
commands), while `sol escalate --json` populates it from `SOL_WORLD` at
creation time — don't rely on `world` being present outside of `escalate`'s
own output.

## Audit findings from this writ

Auditing the surface above against the built binary (not source comments)
turned up one functional gap, fixed in this writ, and two shape
inconsistencies left as follow-ups (both cosmetic — no external consumer of
this contract yet exists to break):

- **Fixed:** `sol mail read` had no `--json` flag — the one command in the
  writ's explicit table that couldn't emit structured output. Added,
  mirroring `mail ack`'s existing JSON shape (`internal/cliapi/mail.Message`
  with a `read_at` timestamp set).
- **Fixed:** the feed cursor contract had no CLI-reachable bootstrap. `sol
  feed --json --since=<cursor>` worked once you had a cursor, but nothing
  in the CLI could hand a first-time consumer their first one — the
  documented escape hatch ("restart with `--since` omitted or `--since=""`
  for a fresh cursor") produced plain JSONL with no `next_cursor` in both
  cases, since the code never distinguished an explicitly empty `--since`
  from an omitted one. Fixed by keying off `cmd.Flags().Changed("since")`:
  an explicit `--since=''` now bootstraps the cursor envelope; `--since`
  left off entirely keeps its existing plain-JSONL behavior unchanged (see
  `TestFeedCmd_OmittedSinceStaysPlainJSONL`/`TestFeedCmd_ExplicitEmptySinceBootstrapsCursor`
  in `cmd/feed_test.go`).
- **Follow-up (not fixed here):** `sol escalation ack`/`resolve --json`
  hardcode `"world": ""` instead of resolving it the way `sol escalate
  --json` does. Low urgency — the field is documented as unreliable above —
  but worth fixing in a future pass over `internal/cliapi/escalations` for
  consumers that do want it.
- **Follow-up (not fixed here):** `sol mail purge` has no `--json`. Its
  output today is a one-line human count message; adding `--json` (a
  `{"deleted": N}`-shaped result, or the preview count when not
  `--confirm`ed) would round out the mail surface for scripting but wasn't
  in the writ's named table and touches confirm-gated-command JSON
  conventions this writ didn't otherwise need to establish.
- **Observed, not a gap:** `CLAUDE.md`'s "Worktree excludes" paragraph lists
  seven files and four directories as sol-managed; the actual source of
  truth, `setup.SolManagedPaths()` (see below), also includes
  `.resolution.md` and `.brief/`, which aren't mentioned there. Out of
  scope for this writ (its CLAUDE.md instruction was a single link line,
  not an audit of that paragraph) but worth a follow-up correction.

## Worktree paths for tools that touch repos

A consumer that clones or inspects a world's managed repo or an agent's
worktree — rather than only talking to the sol CLI — should know that sol
writes a fixed set of local-only files and directories into every worktree.
These are excluded from the project's git history via `.git/info/exclude`
at world-init time, and independently rejected by forge's merge gate if an
agent's branch tries to add them anyway — two checks reading the same list
(`setup.SolManagedPaths()`) so they can't drift apart:

```
.claude/settings.local.json
.claude/system-prompt.md
.claude/skills/
CLAUDE.local.md
.workflow/
.forge-result.json
.forge-injection.md
.guidelines.md
AGENTS.override.md
.agents/skills/
.codex/
.resolution.md
.brief/
```

(Trailing `/` denotes a directory — the entry and everything under it. This
list is generated at world-init time from that function; treat it, not this
page, as authoritative if the two ever disagree.)

Practically: a tool diffing an agent's branch against `main` should expect
these paths to be absent from the diff (forge won't merge a branch that
touches them), and a tool reading a worktree directly on disk should not
try to check them into anything — they are process-local scratch state
(persona files, cached skills, per-writ resolution reports, etc.), not
project content.

## See also

- [ADR-0043: External Automation Contract](decisions/0043-external-automation-contract.md)
  — the decision this page implements.
- [docs/integration-api.md](integration-api.md) — the broader `--json`
  philosophy and the event-webhooks proposal (still future work; `sol feed`
  is the current alternative for observing the sphere from outside).
- [docs/api/README.md](api/README.md) — the growing, contract-tested JSON
  schema catalog for writ/world/agent/caravan commands (a different,
  larger surface than this page's ADR-0043 scope).
- [cmd/CONVENTIONS.md](../cmd/CONVENTIONS.md) — how these commands are built
  internally, if you're extending the surface rather than just consuming it.
- [docs/channels.md](channels.md) — in-band delivery *into* a live claude
  session (channels/doorbell), a different concern from this page's
  outside-in `SOL_VIA`/feed/mail contract for driving sol from outside.

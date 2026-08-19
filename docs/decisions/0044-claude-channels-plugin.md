# ADR-0044: First-Party Claude Code Channels Plugin for In-Band Message Delivery

Status: Accepted

Date: 2026-08-19

## Context

Sol delivers agent-directed messages (mail, doorbell nudges, escalation
replies) via the pane doorbell: `internal/nudge.Deliver` enqueues content
durably, then injects a fixed line into the session's terminal via a
send-keys-style pane write, prompting the agent to run `sol nudge drain` at
its next turn boundary. ADR-0043 formalized the external-automation contract
around this world (origin attribution, cursor-based event reads) but did not
change the delivery mechanism itself — send-keys injection remains sol's
only tool for getting an idle session to notice new content, and it is a
real, if narrow, reliability surface: a swallowed or mistimed pane write
delays notice (never loses content, since the queue is durable), and every
injection is one more place a terminal-emulation quirk could bite.

Claude Code ships a *channels* feature (research preview, `claude` 2.1.227):
a stdio MCP server can declare an experimental `claude/channel` capability
and push `notifications/claude/channel` events directly into a live
session's transcript — no synthesized keystroke, no pane write. Three spike
writs investigated whether this is viable for sol's unattended,
per-agent-isolated dispatch model:

- **sol-159ee545a38d78ac** (base spike): delivery semantics check out well —
  fast (~7s idle-turn latency, dominated by model think time, not
  transport), correctly grouped when busy, byte-exact at multi-KB size,
  and the subprocess lifecycle is a plain stdio MCP child with no durability
  of its own to manage. But the only launch form tested,
  `--dangerously-load-development-channels`, shows an unconditional,
  per-process interactive confirmation dialog with no config-seedable
  bypass reachable from sol's `CLAUDE_CONFIG_DIR` isolation model — a hard
  blocker for unattended sessions. Initial verdict: no-go.
- **sol-d792deb1a2e99eec** (follow-up): the *other* launch form,
  `--channels plugin:<name>@<marketplace>` (the "approved allowlist" path,
  distinct from the dev-flag path), shows **zero** interactive dialogs when
  the plugin is on an allowlist sourced from
  `/etc/claude-code/managed-settings.json` (`channelsEnabled: true` +
  `allowedChannelPlugins`). This reclassifies the prior no-go: with a
  host-installed managed-settings file and sol's existing plugin-seeding
  pipeline (`internal/config.go`'s `seedClaudePlugins`/`mergeEnabledPlugins`,
  already used for operator-installed plugins), a full end-to-end delivery
  succeeded with no consent surface beyond the one-time, operator-run
  `/plugin install` authoring step that already exists for any sphere-wide
  plugin today.
- **sol-0e4943e8366c7b1c** (falsification): directly tested whether
  `channelsEnabled`/`allowedChannelPlugins` could be set from any
  sol-controllable, non-root source instead — `userSettings`,
  `projectSettings`, `localSettings`, and `--settings` (`flagSettings`,
  the most promising candidate, since `BuildCommand` already fully controls
  the launch command). All four were dropped, identically, across eight
  independent launches. The managed-settings file is confirmed to be the
  *only* path to this gate — decompiled from the client binary, and now
  empirically verified from three independent angles.

## Decision

Ship a first-party sol channel plugin and the machinery to use it, gated
behind three independent, operator-controlled switches, off by default:

1. **`sol channel serve`** (`internal/channelserve`, wired up in
   `cmd/channel.go`): a stdio JSON-RPC MCP server, thin and stateless over
   `internal/nudge`'s existing queue — no cursor or durability of its own.
   It declares the `claude/channel` capability, polls the agent's nudge
   queue, and pushes pending messages as channel notifications. A push
   failure re-enqueues the message rather than losing it; a restart simply
   redelivers whatever is still pending — at-least-once, matching the
   queue's own contract, not a new one.
2. **Routing integrated at `nudge.Deliver`'s ring step**
   (`internal/nudge`): `sol channel serve` calls `nudge.MarkChannelAlive` on
   every poll tick as a liveness heartbeat. `Deliver` checks
   `nudge.ChannelAvailable` after enqueueing — if a bridge's heartbeat is
   fresh, the pane doorbell is skipped (the bridge will notice and deliver
   the message itself); otherwise the doorbell fires exactly as before.
   Enqueue itself is unconditional either way. This makes the routing
   decision synchronously testable (fresh/stale/missing heartbeat) without a
   live `claude` process, and fails safe to the doorbell whenever the bridge
   isn't confirmed alive — disabled config, codex agents, or a crashed
   bridge all land on the same universal fallback.
3. **Sol is the *vendor* of this plugin, not an installer of a third-party
   one** (`internal/channelplugin`): `EnsureMarketplace` materializes the
   plugin + marketplace content (`marketplace.json`, `plugin.json`,
   `.mcp.json`) sphere-wide under `.claude-defaults/`, mirroring the exact
   local-directory-marketplace shape the spikes captured from a real
   `claude` binary's own `/plugin install` output. `SeedAgent` merges the
   *per-agent* installation record (`installed_plugins.json`,
   `known_marketplaces.json`, `settings.json`'s `enabledPlugins`) into one
   agent's config dir — called only when that agent's world opted in, so
   the plugin's MCP subprocess is never spawned for an agent that hasn't.
   A schema-drift guard test compares generated output against fixtures
   copied verbatim from the spike artifacts, so a future Claude Code release
   changing these reverse-engineered formats fails loudly instead of
   silently.
4. **`ClaudeRuntime.BuildCommand` appends `--channels
   plugin:sol-channel@sol-official`** only when `agents.channels_enabled` is
   true in that world's config (a new `[agents]` key — the same section as
   `agents.model`/`agents.runtimes`, since this governs session launch
   command construction like they do). With the flag at its default
   (`false`), `BuildCommand`'s output is byte-identical to before this ADR.
   Codex is entirely untouched (CC-9): it has no channel capability and
   keeps the doorbell permanently, not "until channels ship for codex."
5. **The managed-settings.json file stays operator-installed, always.**
   Sol documents the exact recipe (`docs/channels.md`) and validates it
   (`sol doctor`'s new advisory `channels:<world>` check, mirroring
   `docs/credentials.md`'s "sol documents, doctor validates, operator
   installs" precedent) but never writes it — it lives outside `$SOL_HOME`,
   requires root, and is host-wide (affects every `claude` process on the
   host, not just this sphere's agents), the same blast-radius reasoning
   that already keeps sol out of writing credential files directly.
   Deliberately kept generic in this public repo: the recipe names the
   Claude Code path and JSON shape, not any operator's specific
   infrastructure.

Activation therefore requires all three operator-controlled gates in
concert: the config flag, the managed-settings file, and the sol binary
itself being resolvable at the path the plugin's `.mcp.json` points at.
Missing any one of them means agents transparently keep using the pane
doorbell — there is no degraded or broken state, only "channels didn't
activate."

## Consequences

- **Preview-drift risk.** This entire design rests on reverse-engineered,
  undocumented Claude Code behavior (`claude` 2.1.227, "research preview"
  per its own CLI framing) — the dev-channels dialog, the managed-settings
  gate function, and the exact plugin/marketplace JSON shapes could all
  change without notice in a future release. **Re-check trigger:** re-run
  the base spike's Q1 section (or the schema-drift guard test against a
  fresh `claude plugin install` output) whenever a `claude` release's
  changelog mentions "channels," or every ~90 days as a backstop — whichever
  comes first, per the base spike's own stated cadence.
- **Doorbell retained as the universal, permanent fallback.** No code path
  in this ADR requires the doorbell to be removed or made secondary in any
  structural sense — it is what every agent uses when channels aren't
  active for any reason, forever, and is what every codex agent uses
  unconditionally.
- **New host-wide operator dependency for anyone who opts in.** Enabling
  channels for a world now depends on state outside `$SOL_HOME` (a root-only
  file), which `sol doctor` must track as an advisory (not blocking) check,
  the same category as credential presence.
- **One extra MCP subprocess per opted-in agent session.** `sol channel
  serve` runs for the lifetime of any claude session whose world enabled
  channels — negligible resource cost, but a new process class to be aware
  of when reasoning about a session's process tree.
- **No change for any world that doesn't set `agents.channels_enabled`.**
  `BuildCommand`, `Seed`, and `nudge.Deliver`'s routing are all no-ops at
  their respective flag checks, verified by tests asserting byte-identical
  `BuildCommand` output and untouched plugin state when the flag is off.

# Claude Code Channels (Research Preview)

## The Model

Sol's message delivery is layered, from most to least reliable:

1. **Verified send-keys** (the floor). `session.Manager.NudgeSession` is
   sol's one primitive for waking an idle terminal: it types a message and
   Enter into the pane, then re-captures the pane after each Enter to
   confirm the text actually landed and submitted — retrying rather than
   trusting a single keystroke went through. Every layer above this one is
   built on top of it working.
2. **The pane doorbell** (the standard path). `internal/nudge.Deliver`
   enqueues message content durably first, then uses verified send-keys to
   inject one fixed, content-free line into the session's terminal telling
   it to run `sol nudge drain`. The terminal pane is a rendering surface,
   not a transport: content always goes through the durable queue, so a
   swallowed or mistimed doorbell delays an agent noticing new mail, it
   never loses it (ADR-0043). This is what every agent uses today, and what
   every codex agent and every claude agent without channels active
   continues to use, always.
3. **Channels** (primary, when active). Claude Code's *channels* feature
   offers an in-band alternative to the doorbell: a stdio MCP server can
   push content directly into a live session's transcript via
   `notifications/claude/channel`, without a synthesized keystroke at all.
   Sol ships a first-party channel plugin (`sol-channel@sol-official`)
   whose server is `sol channel serve` — a thin, stateless bridge over the
   same `internal/nudge` queue the doorbell already reads from. See
   [ADR-0044](decisions/0044-claude-channels-plugin.md) for the design
   decision and [docs/decisions/0043](decisions/0043-external-automation-contract.md)
   for the context this builds on.

**This is a research-preview feature: off by default, and gated behind three
independent, operator-controlled switches.** No world's behavior changes
unless an operator deliberately opts in, and every gate failure falls back
to the doorbell (layer 2) — never to a broken or degraded state.

## The Three Gates

Channel delivery for a world's claude-runtime agents requires all three:

1. **`agents.channels_enabled = true`** in that world's `world.toml`
   (`[agents]` section). Default: `false`. With it off, `sol cast` produces
   byte-identical session launch commands to before this feature existed.
2. **An operator-installed `managed-settings.json`** allowlisting sol's
   plugin — see [below](#installing-managed-settingsjson). This is the only
   settings source Claude Code's channels gate reads (confirmed by direct
   testing across all four other settings sources — user, project, local,
   `--settings`; see ADR-0044's cited spike findings). Sol never writes this
   file itself: it's outside `$SOL_HOME`, requires root, and is host-wide —
   the same "sol documents, `sol doctor` validates, operator installs"
   precedent as [docs/credentials.md](credentials.md).
3. **The `sol` binary resolvable on the host.** Sol's channel plugin's
   `.mcp.json` launches `sol channel serve` at the absolute path resolved
   when the plugin content was last materialized (`os.Executable()` at that
   time). If sol is reinstalled at a different path, re-run any command that
   triggers `ClaudeRuntime.Seed` (e.g. `sol cast`) to refresh it.

With any gate missing, agents fall back to the pane doorbell exactly as
before — nothing is lost, delivery is just less immediate.

## Installing managed-settings.json

Claude Code reads managed settings from a fixed, platform-specific path:

| Platform | Path |
|----------|------|
| Linux | `/etc/claude-code/managed-settings.json` |
| macOS | `/Library/Application Support/ClaudeCode/managed-settings.json` |
| Windows | not yet validated by `sol doctor` — see the caveat below |

Content (the exact fixture `sol doctor`'s Fix text renders — see
`internal/doctor.ManagedSettingsJSON`):

```json
{
  "channelsEnabled": true,
  "allowedChannelPlugins": [
    {
      "plugin": "sol-channel",
      "marketplace": "sol-official"
    }
  ]
}
```

**`channelsEnabled: true` is required, not optional, the moment this file
exists at all.** Claude Code's gate is asymmetric: no file present defaults
to *open* (channels usable by any approved plugin), but a file present
*without* this key is *more* restrictive than no file — it blocks channels
outright. This is an editing-regression trap: an operator who later edits
this file for an unrelated reason (adding another tool's policy, say) and
drops the `channelsEnabled` key without realizing it silently *breaks*
channels rather than leaving them as they were. `sol doctor` calls this out
as a distinct warning if it happens.

If your organization already manages this file (an MDM/enterprise policy, or
another tool), add the `sol-channel`/`sol-official` entry to your existing
`allowedChannelPlugins` array rather than overwriting the file —
`allowedChannelPlugins` *replaces* Anthropic's own remote default allowlist
rather than merging with it, so whatever your existing file grants must stay
present alongside sol's entry. For example, if your host also needs
Anthropic's own first-party plugins allowlisted, list them explicitly
alongside sol's:

```json
{
  "channelsEnabled": true,
  "allowedChannelPlugins": [
    {"plugin": "sol-channel", "marketplace": "sol-official"},
    {"plugin": "some-anthropic-plugin", "marketplace": "anthropic"}
  ]
}
```

Install it once per host:

```sh
sudo install -D -m 0644 /dev/stdin /etc/claude-code/managed-settings.json <<'EOF'
{
  "channelsEnabled": true,
  "allowedChannelPlugins": [
    {
      "plugin": "sol-channel",
      "marketplace": "sol-official"
    }
  ]
}
EOF
```

Then run `sol doctor` — it validates the file exists, parses, has
`channelsEnabled: true`, and allowlists sol's plugin, for every world that
has `agents.channels_enabled = true`.

**Drop-in alternative.** Claude Code also reads a `managed-settings.d/`
directory alongside `managed-settings.json` (e.g.
`/etc/claude-code/managed-settings.d/` on Linux). If you'd rather not touch
an existing `managed-settings.json`, drop a single JSON file in there
containing both `channelsEnabled: true` and the `allowedChannelPlugins`
entry above — `sol doctor` accepts either form. It checks fragments
independently rather than merging them: one fragment must provide both keys
on its own for the check to pass.

## Scope Caveat: Host-Wide, Not Per-Agent

`managed-settings.json` applies to **every** `claude` process on the host —
every sol agent regardless of world, and any human operator's own
interactive session. This is deliberate (a single-autarch sphere makes
host-wide the natural granularity, the same precedent as operator-managed
credentials), but it is a real, host-wide grant, not a per-agent one. The
allowlist entry itself is narrow in scope: it only permits sol's plugin to
receive unsolicited channel notifications — it grants no additional tool or
file access.

If your host runs `claude` sessions for more than one operator or purpose,
be aware that enabling this for one sol sphere enables it host-wide.

## Enabling for a World

```toml
# world.toml
[agents]
channels_enabled = true
```

Then re-cast (or let the next natural respawn/handoff pick it up) — Claude
Code's `--channels plugin:sol-channel@sol-official` flag and the per-agent
plugin installation record are both applied at session launch / `Seed` time.

## Gotcha: Per-Notification Silent-Drop Semantics

Even with all three gates satisfied, an *individual* channel notification
can still be silently dropped by Claude Code's client — for example, one
sent while the client itself is misconfigured or mid-restart. Two things to
know when debugging a delivery that never showed up:

- **`sol channel serve`'s own log cannot see the drop.** The rejection
  happens client-side, after the bridge's write to stdout has already
  succeeded at the transport level — the exact "server-side write succeeds,
  client silently discards" signature the channels spikes characterized for
  the allowlist-gated case (see ADR-0044's cited findings). The absence of a
  `channelserve: push failed` line in the bridge's log is *not* proof a
  notification actually reached the agent's transcript; it only proves sol
  handed it to Claude Code's stdio pipe successfully.
- **The stable rejection signature, if you need to confirm it directly.**
  A rejected channel notification produces a fixed string from the client —
  `plugin <name>@<marketplace> is not on the approved channels allowlist
  (use --dangerously-load-development-channels for local dev)` — for sol's
  plugin, literally `plugin sol-channel@sol-official is not on the approved
  channels allowlist (use --dangerously-load-development-channels for local
  dev)`. This is a transient, positioned terminal write (Claude Code's TUI
  redraws over it within about a second), so a plain `tmux capture-pane`
  taken after the fact shows nothing. To
  capture it reliably, start `tmux pipe-pane -o "cat >> <logfile>"` on the
  session immediately after it launches, then grep the raw stream for the
  string above. In practice this level of manual capture is rarely needed —
  `sol doctor`'s `channels:<world>` check validates the managed-settings
  gate that produces this rejection, so a clean doctor pass on that check
  means this signature won't occur for sol's plugin.

## Verifying It's Working

- `sol doctor` reports `channels:<world>` as a clean pass once
  `managed-settings.json` is installed and correct.
- A live agent's `/status` inside its Claude Code session should list
  `Enterprise managed settings (file)` as a settings source.
- Deliveries that land via channel show up in-transcript as
  `<channel source="sol-channel" ...>` blocks instead of a doorbell line
  followed by `sol nudge drain` output.

## Third-Party Channel Plugins

`sol-channel` is sol's own first-party plugin, but Claude Code's plugin
system is general — an operator can install and allowlist any other channel
plugin the same way. Sol doesn't manage third-party plugin installation
directly; that happens through the same sphere-wide Claude Code defaults
session used for any other plugin: `sol config claude` launches an
interactive `claude` session rooted at `$SOL_HOME/.claude-defaults/`, where
`/install` and `/uninstall` manage plugins available to every agent across
every world. See `sol config claude --help` (its `Long` text) for the file
ownership rules that session operates under — `settings.json` is sol-owned,
`settings.local.json` is where an installed plugin's `enabledPlugins` entry
must also be verified to persist across sol restarts.

A third-party channel plugin still needs its own entry in
`allowedChannelPlugins` in `managed-settings.json` (see
[above](#installing-managed-settingsjson)) — installing it via
`sol config claude` makes it available to agents' Claude Code config, but
does not itself satisfy the host-wide allowlist gate.

## Codex

Codex has no channels capability. `internal/runtime/codex/` is entirely
untouched by this feature (CC-9 runtime symmetry) — codex agents always use
the pane doorbell, regardless of `agents.channels_enabled`.

## Preview Status: Removable Scaffolding

Everything in this document — the managed-settings.json policy file
requirement, the three-gate activation dance, the plugin/marketplace JSON
shapes `internal/channelplugin` materializes — is scaffolding around a
Claude Code *research preview* feature, not a permanent architectural
commitment. It exists because the only way sol found to use channels
unattended today is the reverse-engineered, host-wide managed-settings
allowlist path; none of it should be assumed stable.

**Re-check trigger:** re-verify this design whenever a `claude` release's
changelog mentions "channels" (the base spike's cited cadence), or every
~90 days as a backstop, whichever comes first. Re-verification means
re-running the base spike's launch-dialog check (or the schema-drift guard
test in `internal/channelplugin` against a fresh `claude plugin install`
output) to confirm the gate and plugin shapes haven't silently changed.

**What to revisit once channels leave research preview:** if Claude Code
ships a stable, documented, non-managed-settings way to approve a channel
plugin (a per-project or per-user config Anthropic supports and versions),
the entire managed-settings.json recipe on this page — and the host-wide
scope caveat it carries — becomes removable scaffolding: replace it with
whatever the stable mechanism turns out to be, and drop the "research
preview" framing from this document's title.

## See Also

- [docs/scripting.md](scripting.md) — the external-automation delivery
  contract (`SOL_VIA`, mail, feed); channels is an in-band delivery
  mechanism for live sessions, a different concern from that page's
  outside-in automation surface.
- [CLAUDE.md](../CLAUDE.md) — architecture overview; see the Nudge/Mail
  entries this feature builds on.

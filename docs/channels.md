# Claude Code Channels (Research Preview)

## The Model

Sol delivers most agent-directed messages (mail, doorbell nudges, escalation
replies) through the pane doorbell: a fixed line injected into an idle
session's terminal telling it to run `sol nudge drain`. That works, but it's
one more send-keys interaction on top of an already-real reliability arc
(ADR-0043). Claude Code's *channels* feature offers an in-band alternative:
a stdio MCP server can push content directly into a live session's transcript
via `notifications/claude/channel`, without a synthesized keystroke.

Sol ships a first-party channel plugin (`sol-channel@sol-official`) whose
server is `sol channel serve` — a thin, stateless bridge over the same
`internal/nudge` queue the doorbell already reads from. See
[ADR-0044](decisions/0044-claude-channels-plugin.md) for the design decision
and [docs/decisions/0043](decisions/0043-external-automation-contract.md) for
the context this builds on.

**This is a research-preview feature: off by default, and gated behind three
independent, operator-controlled switches.** No world's behavior changes
unless an operator deliberately opts in.

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

Content:

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
outright. `sol doctor` calls this out as a distinct warning if it happens.

If your organization already manages this file (an MDM/enterprise policy, or
another tool), add the `sol-channel`/`sol-official` entry to your existing
`allowedChannelPlugins` array rather than overwriting the file —
`allowedChannelPlugins` *replaces* Anthropic's own remote default allowlist
rather than merging with it, so whatever your existing file grants must stay
present alongside sol's entry.

Install it once per host:

```sh
sudo install -D -m 0644 /dev/stdin /etc/claude-code/managed-settings.json <<'EOF'
{
  "channelsEnabled": true,
  "allowedChannelPlugins": [
    {"plugin": "sol-channel", "marketplace": "sol-official"}
  ]
}
EOF
```

Then run `sol doctor` — it validates the file exists, parses, has
`channelsEnabled: true`, and allowlists sol's plugin, for every world that
has `agents.channels_enabled = true`.

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

## Verifying It's Working

- `sol doctor` reports `channels:<world>` as a clean pass once
  `managed-settings.json` is installed and correct.
- A live agent's `/status` inside its Claude Code session should list
  `Enterprise managed settings (file)` as a settings source.
- Deliveries that land via channel show up in-transcript as
  `<channel source="sol-channel" ...>` blocks instead of a doorbell line
  followed by `sol nudge drain` output.

## Codex

Codex has no channels capability. `internal/runtime/codex/` is entirely
untouched by this feature (CC-9 runtime symmetry) — codex agents always use
the pane doorbell, regardless of `agents.channels_enabled`.

## Preview Status

This integrates with a Claude Code *research preview* feature. Anthropic's
own dev-channels dialog, allowlist gate, and managed-settings schema could
change without notice in a future release. See ADR-0044's consequences
section for the re-check trigger.

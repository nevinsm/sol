# ADR-0018: Agent Config Directory Isolation

Status: accepted
Date: 2026-03-06

## Context

Claude Code maintains persistent state in `~/.claude/projects/` keyed by
working directory. This state includes:

- **Auto-memory** (`memory/MEMORY.md`): Claude writes observations across
  sessions. These memories are loaded into every new session's context.
- **Session transcripts** (`.jsonl` files): Full conversation history,
  used for session resumption and context recovery after compaction.

When multiple agents share the default `~/.claude/` config directory, their
state cross-contaminates:

- **Stale memories poison behavior.** The forge accumulated memories about
  `sol forge merge` (a removed pipeline command) and old CLI patterns.
  These competed with the workflow's instructions, causing the
  agent to deviate from prescribed steps.
- **Transcript bloat.** Outposts are ephemeral but their transcripts
  persist indefinitely — Nova accumulated 127 sessions (62MB) across
  multiple writs assigned to different Nova instances.
- **No role isolation.** A forge memory could theoretically influence an
  outpost session working in the same directory, and vice versa.

The Gastown prototype solved this with `CLAUDE_CONFIG_DIR`, a Claude Code
environment variable that redirects all persistent state to an alternate
directory. Each account (agent identity) gets its own config directory
with independent settings, memories, and transcripts.

## Decision

Set `CLAUDE_CONFIG_DIR` for every agent session, pointing to a
world-scoped directory:

```
<world-dir>/.claude-config/<role>/<name>/
```

Examples:
- `/home/ubuntu/sol/sol-dev/.claude-config/forge/forge/`
- `/home/ubuntu/sol/sol-dev/.claude-config/outposts/Nova/`
- `/home/ubuntu/sol/sol-dev/.claude-config/envoys/Meridian/`
- `/home/ubuntu/sol/sol-dev/.claude-config/governor/governor/`

The chancellor (world-less) uses `<sol-home>/.claude-config/chancellor/chancellor/`.

**Provisioning:**
- The config directory is created (`mkdir -p`) before session start.
- `CLAUDE_CONFIG_DIR` is added to the environment map at all agent spawn
  points and flows through `session/manager.go` via `tmux set-environment`.
- Existing `settings.local.json` installation (hooks) remains unchanged —
  it writes to the agent's working directory, not the config directory.

**Lifecycle:**
- Forge, envoy, and governor config dirs are persistent — memories and
  transcripts accumulate across sessions for the same agent identity.
- Outpost config dirs are not cleaned up on resolve. Transcripts may be
  useful for debugging or future analysis tooling.

## Consequences

- Each agent's auto-memory is scoped to its own identity. Forge memories
  cannot influence outpost behavior or vice versa.
- Stale memories from removed features only affect the agent that wrote
  them, and can be cleared by deleting that agent's config directory.
- Session transcripts are organized by agent identity, making it possible
  to analyze per-agent behavior patterns.
- Outpost transcript accumulation is a known trade-off — config dirs for
  ephemeral outposts will grow over time. This is acceptable given
  transcripts are small relative to disk and may have future analytical
  value.
- The world directory gains a `.claude-config/` tree. This should be
  added to `.gitignore` if the world directory is version-controlled.

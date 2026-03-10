# ADR-0019: Account & Quota Management

Status: accepted
Date: 2026-03-06

## Context

Claude Code agents authenticate via OAuth tokens stored in
`.credentials.json`. ADR-0018 gives each agent its own
`CLAUDE_CONFIG_DIR`, and credentials are currently symlinked from
`~/.claude/.credentials.json` into each agent's config directory.

This single-account setup breaks down at scale. Rate limits are
per-account — a high-volume world running a forge, governor, sentinel,
and multiple outposts can exhaust a single account's quota. When the
account hits its rate limit, every agent in the sphere stalls until the
limit resets.

The Gastown prototype addressed this with account rotation, but that
approach had significant limitations:

- **macOS-only** — credential storage relied on the macOS keychain,
  unavailable on Linux.
- **Global symlink swap** — rotating `~/.claude` via a single symlink
  introduces race conditions when concurrent agents read credentials
  simultaneously.
- **Manual OAuth** — per-account login was manual in Gastown and remains
  so here; Claude Code does not expose a programmatic OAuth flow.

## Decision

### Account storage

Account directories live under `$SOL_HOME/.accounts/{handle}/`. Each
directory holds a `.credentials.json` obtained via manual OAuth login:

```
$SOL_HOME/.accounts/
├── alice/
│   └── .credentials.json
├── bob/
│   └── .credentials.json
└── carol/
    └── .credentials.json
```

Keeping credentials in the sol tree (rather than scattered under
`~/.claude-accounts` or similar) makes them discoverable, portable, and
manageable through the sol CLI.

### Credential binding

Agents receive credentials via symlink: each agent's
`CLAUDE_CONFIG_DIR/.credentials.json` symlinks to the assigned account's
`.credentials.json`. This replaces the current symlink to
`~/.claude/.credentials.json`.

### Account resolution

When assigning an account to an agent, resolution follows this priority:

1. Per-dispatch `--account` flag (explicit override)
2. `default_account` in `world.toml` (world-level default)
3. `sol account default` (sphere-level default)
4. `~/.claude/.credentials.json` fallback (single-account compatibility)

### Rate limit detection

The sentinel detects rate limits by scanning tmux pane output for Claude
error patterns (e.g., rate limit error messages). This extends the
sentinel's existing output-monitoring patrol loop (ADR-0001) with
additional pattern matching — no new polling mechanism required.

### Credential rotation

When a rate limit is detected, the sentinel:

1. Selects the next available account (one not currently rate-limited).
2. Swaps credential symlinks for **all agents in the world** (including
   the governor) — partial rotation would leave some agents on the
   exhausted account.
3. Respawns affected sessions with `--continue` to preserve context.

Rotating all agents together avoids the complexity of per-agent account
tracking and ensures the world operates on a single active account at
any given time.

### Quota exhaustion

When no accounts have remaining quota:

- **Autonomous agents** (outposts, forge) are paused — sessions are
  stopped and agents enter a `quota-paused` state. This prevents wasted
  API calls against an exhausted account.
- **Governor is rotated but never paused** — the autarch may need it
  for manual intervention, and it should remain accessible.
- **Senate is autarch-managed** — it is sphere-scoped with no sentinel
  coverage, so the autarch handles its credentials directly.
- The sentinel tracks each account's reset time and restarts paused
  agents when the earliest account becomes available again.

### Account lifecycle

Accounts are managed through the sol CLI:

- `sol account login {handle}` — creates the account directory, opens a
  Claude session with `CLAUDE_CONFIG_DIR` set for the autarch to
  complete OAuth login.
- `sol account list` — shows registered accounts and their status.
- `sol account default {handle}` — sets the sphere-level default.
- `sol account remove {handle}` — removes an account directory.

## Consequences

- Multiple accounts enable sustained operation through rate limits —
  when one account is exhausted, agents rotate to the next.
- Credential rotation is transparent to agents — the symlink swap plus
  `--continue` respawn preserves session context.
- The pause/resume model prevents wasted API calls when all accounts
  are exhausted, rather than letting agents spin against rate limits.
- Account directories accumulate under `$SOL_HOME/.accounts/` — the
  autarch manages their lifecycle via `sol account add/remove`.
- Manual OAuth login is required per account (`CLAUDE_CONFIG_DIR={dir}
  claude`, then `/login`). This is a Claude Code limitation, not a sol
  design choice.
- Per-agent symlinks (from ADR-0018) eliminate the race conditions that
  plagued Gastown's global symlink approach — each agent's credential
  binding is an independent filesystem operation.

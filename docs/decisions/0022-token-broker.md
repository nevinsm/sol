# ADR-0022: Token Broker — Centralized OAuth Refresh

Status: Accepted

## Context

Multiple agents sharing an account via credential file symlinks race on
OAuth refresh tokens. Refresh tokens are single-use — when one agent
refreshes, all other agents' symlinked copy of the refresh token is
invalidated. This causes "OAuth error: Invalid code" on session restart.

Claude Code was verified to work with access-token-only credentials (no
`refreshToken` field): the agent starts, makes API calls, and does not
attempt to inject or manufacture a refresh token.

## Decision

Introduce a **token broker** — a standalone process that centralizes
OAuth refresh handling:

1. Broker holds the refresh token per account (source of truth:
   `$SOL_HOME/.accounts/{handle}/.credentials.json`).
2. Proactively refreshes before `expiresAt` (default: 30 minutes margin).
3. On refresh, writes **access-token-only credentials** (no
   `refreshToken`) to each agent config dir that uses that account.
4. Agents consume the access token; they never see or use refresh tokens.
5. On quota rotation, the rotation logic writes access-token-only files
   (not symlinks) for the new account.

### Agent-to-account tracking

Each agent config dir gets a `.account` metadata file containing the
account handle. The broker discovers agents by scanning
`$SOL_HOME/{world}/.claude-config/` directories and reading `.account`
files.

### Credential flow

- `EnsureClaudeConfigDir` (on cast/start) writes:
  - `.account` — account handle for broker discovery
  - `.credentials.json` — access-token-only copy (no refreshToken)
- `ResolveCurrentAccount` reads `.account` first, falls back to symlink
  for legacy compatibility.
- Quota rotation writes new `.account` + `.credentials.json` (replacing
  the old symlink swap).

### CLI

- `sol broker run` — foreground broker loop
- `sol broker status` — heartbeat-based status

## Consequences

- No agent credentials file contains a `refreshToken`
- Token broker is the sole consumer of refresh tokens
- Agents survive token expiry transparently (broker pre-refreshes)
- No more OAuth errors on session restart when sharing accounts
- Legacy symlink fallback preserved for single-account setups (empty
  account parameter)
- Quota rotation updated from symlink swap to file copy
- New heartbeat file: `$SOL_HOME/.runtime/broker-heartbeat.json`

# ADR-0042: Skip credential symlink when a credential env var is configured

Status: Accepted
Date: 2026-06-29

## Context

ADR-0040 (architectural simplification) removed the token broker and account
registries and replaced them with a static approach: `runtime.EnsureConfigDir`
always plants a symlink in each agent config dir pointing
`.credentials.json → ~/.claude/.credentials.json`. Agents that need a
subscription credential pick it up from that symlink.

That static symlink is a liability when an operator has configured a credential
env var (e.g. `ANTHROPIC_AUTH_TOKEN` or `CLAUDE_CODE_OAUTH_TOKEN`) in
`$SOL_HOME/.env`:

1. **Expiry / degradation.** OAuth access tokens expire within hours. Claude's
   atomic-rename refresh replaces the symlink with a private file. If the token
   expires between sessions the agent 401s with "Please run /login", forcing a
   manual re-login.
2. **Single-use token races.** Multiple agents sharing the symlinked credential
   race on the single-use refresh token embedded in the subscription cred.
3. **Silent fallback.** When an env credential is present but the on-disk cred
   has expired, Claude's precedence (env var wins over on-disk cred) means the
   agent authenticates fine — but the stale symlink is a silent landmine for
   any future session where the env var is absent.

Empirically verified 2026-06-29: interactive Claude Code authenticates correctly
from an env var credential alone with **no** on-disk `.credentials.json` present.
This was confirmed for both `CLAUDE_CODE_OAUTH_TOKEN` and `ANTHROPIC_AUTH_TOKEN`
with the full real envoy config state (`hasCompletedOnboarding`, `oauthAccount`,
`customApiKeyResponses.rejected`) and an expired credential file on disk.

## Decision

When any credential env var listed in `RuntimeDescriptor.CredentialEnvKeys` is
present (non-empty) in the loaded `.env` at startup time, `EnsureConfigDir` must
**not** plant a competing on-disk credential symlink. The env var is the sole
authoritative credential.

`EnsureConfigDir` gains an `env map[string]string` parameter (the merged
sphere+world `.env` loaded by `envfile.LoadEnv`). The credential-symlink block:

1. **Always removes** any pre-existing credential file or symlink (idempotent
   cleanup; removes stale symlinks from persistent envoy config dirs on the
   next session start).
2. **Creates the symlink only** when no credential env var is configured in
   `env`.

`startup.Launch` is updated to load `.env` before step 8 (`EnsureConfigDir`)
instead of step 12 (session environment build), and to reuse the same map at
both sites (no double read).

`ANTHROPIC_AUTH_TOKEN` is added to the Claude descriptor's `CredentialEnvKeys`
(previously only `CLAUDE_CODE_OAUTH_TOKEN` and `ANTHROPIC_API_KEY` were listed).
This is the priority-2 bearer slot recommended for unattended sol use, and
adding it also auto-extends `sol doctor`'s credential check.

## Consequences

**Positive**

- Agents authenticated via an env var credential never see a competing or
  expired on-disk credential.
- Existing stale symlinks in persistent envoy config dirs are silently cleaned
  up on the next session start.
- The `ANTHROPIC_AUTH_TOKEN` slot is now first-class in sol's credential check
  and doctor output.
- No new config knob: the descriptor's existing `CredentialEnvKeys` map drives
  the decision.

**Neutral**

- When no credential env var is configured, behavior is unchanged: the symlink
  to `~/.claude/.credentials.json` is created as before.
- Codex runtime is unaffected: it sets no `CredentialFile`/`GlobalCredsPath`,
  so the symlink block is a no-op for it in both old and new code.

**Negative / Trade-offs**

- Operators must ensure at least one valid credential is present — either the
  global subscription file (for symlink path) or an env var. Sol does not
  validate credential presence at startup time; authentication errors surface
  from Claude Code itself.

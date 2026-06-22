# Credential Management

## The Model

Sol is an orchestration layer above the AI agent runtime. It handles writ
lifecycle, tether durability, session management, forge merge pipeline, and
supervision — but it does not store, rotate, or fetch runtime credentials. Each
runtime (Claude, Codex, etc.) has its own authentication flow; sol reads
whatever credentials the operator places in the environment. This is a
deliberate architectural choice: see [ADR-0040](decisions/0040-architectural-simplification.md).

## Recommendation: Long-Lived Credentials

Sol agents run unattended — a writ can take minutes to hours. If an agent's
credential expires or requires interactive re-authentication mid-writ, the
session stalls with a login prompt that no one is watching. The writ stays
tethered; the sentinel logs the stall; the operator has to intervene manually.

Long-lived credentials (setup tokens, API keys) avoid this entire class of
failure. They are issued once, placed in an env file, and work until explicitly
revoked or expired.

**For unattended sol use, prefer long-lived credentials.**

## Where Sol Reads Credentials

Sol injects credentials through standard environment variables. The env scope
available to each agent session is merged from three sources (later sources
override earlier ones on key collision):

1. **`$SOL_HOME/.env`** — sphere-level (shared across all worlds).
2. **`$SOL_HOME/{world}/.env`** — world-level override for a specific world.
3. **The process environment** inherited by `sol cast` / `sol session start`.

The sphere `.env` file is the recommended location for credentials that apply
everywhere. The world `.env` file is for per-world overrides — for example, if
two worlds use different API keys for the same runtime.

Sol's doctor check (`sol doctor`) reads both files and warns if no credential
env var is present for a world's configured runtime.

See [docs/configuration.md](configuration.md) for the full env-file
specification and layering semantics.

## Per-Runtime Recipes

### Claude (`claude`)

The Claude runtime recognises two credential env vars (highest-precedence first):

| Env var | Type | Notes |
|---------|------|-------|
| `ANTHROPIC_API_KEY` | API key | Issued from console.anthropic.com. No expiration by default. Preferred for unattended use. |
| `CLAUDE_CODE_OAUTH_TOKEN` | OAuth token | Issued by `claude setup-token`. Has an expiration; refresh is interactive. Less suitable for long-running unattended agents. |

**Obtaining a long-lived credential (API key):**

1. Sign in at [console.anthropic.com](https://console.anthropic.com).
2. Navigate to **API Keys** and create a new key.
3. Copy the key and add it to your env file:

```sh
# $SOL_HOME/.env
ANTHROPIC_API_KEY=sk-ant-...
chmod 600 "$SOL_HOME/.env"
```

**Obtaining an OAuth setup token (alternative):**

```sh
claude setup-token
```

Follow the prompts. The token is printed to stdout. Copy it to your env file:

```sh
# $SOL_HOME/.env
CLAUDE_CODE_OAUTH_TOKEN=<token>
chmod 600 "$SOL_HOME/.env"
```

Note: OAuth setup tokens expire. Monitor expiration and refresh before
scheduling long caravan runs.

---

### Codex (`codex`)

The Codex runtime recognises one credential env var:

| Env var | Type | Notes |
|---------|------|-------|
| `OPENAI_API_KEY` | API key | Issued from platform.openai.com. No expiration by default. |

**Obtaining an API key:**

1. Sign in at [platform.openai.com](https://platform.openai.com).
2. Navigate to **API keys** and create a new key.
3. Copy the key and add it to your env file:

```sh
# $SOL_HOME/.env
OPENAI_API_KEY=sk-...
chmod 600 "$SOL_HOME/.env"
```

---

## Trade-offs

Long-lived credentials have a larger blast radius if leaked: they work until
revoked, not just until the next session ends. Mitigate this:

- **File permissions:** `chmod 600 "$SOL_HOME/.env"`. The env file must not be
  readable by group or others. `sol doctor` checks this and fails if it finds
  overly-permissive modes.

- **Version control:** Ensure `$SOL_HOME` is outside any git repository, or
  that `$SOL_HOME/.env` is in `.gitignore` / `.git/info/exclude`. Sol places
  `$SOL_HOME` at `~/sol` by default — outside any project repo.

- **Rotation:** Rotate credentials periodically to limit the window of exposure
  if a credential is compromised. There is no prescribed cadence — balance
  operational overhead against your security posture. Shorter-lived credentials
  mean more frequent rotation but smaller exposure windows.

## Failure Modes Cross-Link

For what happens when a credential expires mid-writ and how to recover, see
[docs/failure-modes.md — Credential Exhaustion](failure-modes.md#credential-exhaustion).

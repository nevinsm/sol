# Upgrade Guide — Migrating from Pre-Simplification Sol

This guide is for operators upgrading a sol installation that was set up
before **2026-06-06**, when the architectural simplification landed
([ADR-0040](decisions/0040-architectural-simplification.md)).

If you installed sol after that date, this guide does not apply.

---

## Summary of the Simplification

The architectural simplification (ADR-0040) removed the multi-account
credential management layer that sol used to own. The removed machinery
included:

- `internal/account/` — OAuth token storage and account registry
- `internal/quota/` — per-account rate-limit state tracking
- `internal/budget/` — daily spend tracking and dispatch gating
- `sol account` and `sol quota` CLI command trees
- Sentinel's rate-limit detection and credential rotation branch
- `broker.Provider` rate-limit detection contract (broker is now a liveness probe only)

The forge daemon was also renamed from **forge** to **forge-merge** as part of
a broader naming cleanup.

---

## What Changed for Operators

### Credential management is now fully operator-managed

Previously, sol stored OAuth tokens under `$SOL_HOME/.accounts/{handle}/`
and automatically rotated per-agent credentials when rate limits were hit.

After the simplification:

- Credentials are managed by **you**, via the runtime's native flow:
  - Claude: `claude login` (OAuth) or `ANTHROPIC_API_KEY` (env var)
  - Codex: native login flow or `OPENAI_API_KEY`
- Sol never stores tokens, never runs OAuth flows, and never rotates credentials.
- Each agent's config dir (`<world>/.claude-config/<role>/<agent>/`) receives a
  single **symlink** named `.credentials.json` at spawn time, pointing at the
  operator-managed global credential file (`~/.claude/.credentials.json`).
  The symlink is created once and never swapped.

### Removed CLI commands

- `sol account` — account registration and management
- `sol quota` — per-account quota inspection and reset

These commands no longer exist. Remove any scripts or automation that invoke them.

### `world.toml` keys that no longer do anything

- `[world]\ndefault_account = "..."` — the credential routing role was removed
  in ADR-0040, but the key is still read by `startup.go` and used as the OTEL
  `account` resource attribute for agent session telemetry. Remove only if you
  do not need account-level telemetry attribution.
- `[budget]` section — not recognized by the current binary.
- `[accounts]` section — not recognized by the current binary.

### Forge daemon rename

The forge daemon was renamed from **forge** to **forge-merge**. If you have
manual scripts that reference the old name, update them. Sol's own CLI and
supervisor use the new name automatically.

---

## Stale State After Upgrading

After you replace the binary on a pre-simplification installation, the
following stale state may remain:

| Location | What it is | Effect |
|---|---|---|
| `$SOL_HOME/.accounts/` | Old account credential bundles | Harmless but unused; may cause confusion |
| `<world>/.claude-config/<role>/<agent>/.credentials.json` as a **regular file** | Pre-simplification credential copy | Causes 401 errors when the cached token expires |
| `[budget]` or `[accounts]` in `world.toml` | Dead config sections | Silently ignored; cosmetic noise |
| `world.default_account = "..."` in `world.toml` | Telemetry label key | Used for OTEL `account` resource attribute; credential routing role removed in ADR-0040. Remove only if you don't need account-level telemetry attribution. |
| `<world>/.claude-config/forge/` | Old forge daemon config dir | Unused; harmless but stale |

The most urgent item is the **regular-file `.credentials.json`** case. When sol
used the old account system, it wrote real credential files. The new code
creates symlinks. An old regular file has no refresh path — when the cached
OAuth token in it expires, every agent using that config dir gets a 401 error
with no recovery path.

---

## Cleanup Story: sol doctor

Run `sol doctor` to detect all stale state in one pass:

```
$ sol doctor
  ✓ tmux          tmux 3.5a (/usr/local/bin/tmux)
  ✓ git           git version 2.43.0 (/usr/bin/git)
  ...
  ⚠ credential_symlink:myworld:outposts:Toast
                  /home/me/sol/myworld/.claude-config/outposts/Toast/.credentials.json
                  is a regular file (not a symlink) — stale pre-simplification credential
                  that will cause 401 errors when it expires
  ⚠ obsolete_accounts_dir
                  /home/me/sol/.accounts exists — stale directory from pre-simplification
                  account management (ADR-0019/ADR-0040); no longer used by sol
  ⚠ dead_config_keys:myworld
                  /home/me/sol/myworld/world.toml: 1 dead/no-op config key(s) from
                  pre-simplification architecture: world.default_account
  ⚠ defunct_config_dir:myworld:forge
                  /home/me/sol/myworld/.claude-config/forge is a defunct role directory
                  (replaced by forge-merge (renamed in the architectural simplification,
                  ADR-0040)) (1 agent config dir(s))
```

Run `sol doctor --fix` to auto-remediate what can be safely automated:

```
$ sol doctor --fix
  ... (check output as above) ...

4 fixable issue(s) found:
  ⚠ credential_symlink:myworld:outposts:Toast
    Delete the file (the next session start will recreate it as the correct symlink)
  ⚠ obsolete_accounts_dir
    Move or remove the directory (backup recommended in case you need to recover OAuth tokens)
  ⚠ defunct_config_dir:myworld:forge
    Move the directory

Apply 3 remediation(s)? [y/N] y

Applying remediations...
  Fixing credential_symlink:myworld:outposts:Toast...
  Removed /home/me/sol/myworld/.claude-config/outposts/Toast/.credentials.json
  ✓ credential_symlink:myworld:outposts:Toast: done
  Fixing obsolete_accounts_dir...
  Moved /home/me/sol/.accounts → /home/me/sol/.accounts.bak.20260610T120000Z
  ✓ obsolete_accounts_dir: done
  Fixing defunct_config_dir:myworld:forge...
  Moved /home/me/sol/myworld/.claude-config/forge → /home/me/sol/myworld/.claude-config/forge.bak.20260610T120000Z
  ✓ defunct_config_dir:myworld:forge: done

All remediations applied. Run 'sol doctor' to verify.
```

Note: `dead_config_keys` has no auto-fix because config file mutation is too
risky to automate. Edit `world.toml` manually to remove the dead keys.

To skip the interactive confirmation:

```
sol doctor --fix --yes
```

To preview what `--fix` would do without applying anything:

```
sol doctor --fix --dry-run
```

---

## Remediation Details

### Regular-file `.credentials.json` (auto-fixed)

`sol doctor --fix` **deletes** the regular file. The next time a session starts
for that agent (via `sol cast` or prefect respawn), the claude runtime recreates
it as the correct symlink pointing to `~/.claude/.credentials.json`.

**Safety**: the file is deleted, not backed up, because it is safe to recreate
from the global credential at spawn time. Active agent sessions (detected via
tmux) are skipped.

After deletion, run `sol cast <writ-id> --world=<world>` or let the prefect
respawn the agent to recreate the symlink.

### `$SOL_HOME/.accounts/` (auto-fixed, backed up)

`sol doctor --fix` **moves** `.accounts/` to `.accounts.bak.<timestamp>/`
rather than deleting it. This preserves any OAuth tokens stored there in case
you need them to re-authenticate with `claude login`.

Once you've confirmed you no longer need the backup, delete it:

```bash
rm -rf ~/sol/.accounts.bak.*
```

### Dead `world.toml` keys (manual fix)

Edit `world.toml` directly:

```toml
# Remove or comment out:
# [budget]
# daily_limit = 100

[world]
source_repo = "git@github.com:org/repo.git"
branch = "main"
# default_account = "alice"   ← keep if you want account-level OTEL telemetry
                               #   remove only if you don't need it
```

> **Note on `default_account`**: unlike `[budget]` and `[accounts]`, the
> `default_account` key is **not dead** — it is still read by `startup.go` and
> used as the OTEL `account` resource attribute on every agent session.
> Removing it silences account-level telemetry attribution. Keep the key unless
> you are certain you do not need it.

### Defunct config dirs (auto-fixed, backed up)

`sol doctor --fix` **moves** the defunct role directory to
`<name>.bak.<timestamp>/`. Any active tmux session for an agent in that dir
is skipped as a safety measure.

---

## Manual Cleanup Commands

If you prefer not to use `sol doctor --fix`, here are the manual equivalents:

```bash
# 1. Fix stale regular-file .credentials.json
# Find all regular-file credentials (not symlinks):
find ~/sol/*/. -path '*/.claude-config/*/*/.credentials.json' -not -type l -exec rm {} +

# 2. Move the old .accounts/ directory
mv ~/sol/.accounts ~/sol/.accounts.bak.$(date -u +%Y%m%dT%H%M%SZ)

# 3. Fix world.toml (manual edit — remove dead keys)
$EDITOR ~/sol/<world>/world.toml

# 4. Move defunct forge config dir
mv ~/sol/<world>/.claude-config/forge ~/sol/<world>/.claude-config/forge.bak.$(date -u +%Y%m%dT%H%M%SZ)
```

---

## Rollback Notes

Downgrading the binary after upgrading is **non-trivial**. Sol's database schema
is forward-only (via `internal/migrate/`), and the removed CLI commands are not
available in the old binary layout.

If you need to roll back:

1. **Before upgrading**, make a full backup of `$SOL_HOME/` including all
   databases (`.store/*.db`) and the `.accounts/` directory.
2. Keep the old binary available at a known path.
3. Any migrations applied via `sol migrate run` during the new binary's use
   may leave the database in a state incompatible with the old binary.

**Recommendation**: before upgrading in a production environment, test on a
cloned `$SOL_HOME/` directory with `SOL_HOME=/path/to/clone sol doctor`.

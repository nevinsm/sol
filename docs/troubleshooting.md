# Troubleshooting Guide

This guide uses a **Symptom → Diagnosis → Fix** pattern for common problems. Run `sol doctor` first — it catches most prerequisite issues automatically.

---

## General Diagnostic Commands

Run these to get an overview before diving into specific issues:

| Command | What it shows |
|---|---|
| `sol doctor` | Prerequisite checks (tmux, git, claude, env files, etc.) |
| `sol status <world>` | Overall world health — agents, forge, writs |
| `sol forge log --world=<world> --follow` | Live forge activity |
| `sol session attach <name>` | Watch an agent's terminal session |
| `sol writ list --world=<world>` | Work item status |
| `sol forge queue --world=<world>` | Merge queue state |
| `sol forge blocked --world=<world>` | Blocked merges |

---

## sol doctor Failures

Run `sol doctor` to validate all prerequisites. Each check below corresponds to a named check in the output.

### `tmux` — tmux not found in PATH

**Symptom:** `sol doctor` reports `tmux not found in PATH`

**Diagnosis:** tmux is not installed or not on your `$PATH`.

**Fix:**
```bash
# macOS
brew install tmux

# Linux (Debian/Ubuntu)
apt install tmux
```

---

### `tmux` — tmux found but failed to run

**Symptom:** `sol doctor` reports `tmux found at <path> but failed to run`

**Diagnosis:** tmux binary exists but is corrupted or missing dependencies.

**Fix:** Reinstall tmux using your package manager.

---

### `git` — git not found in PATH

**Symptom:** `sol doctor` reports `git not found in PATH`

**Diagnosis:** git is not installed or not on your `$PATH`.

**Fix:**
```bash
# macOS
brew install git

# Linux (Debian/Ubuntu)
apt install git
```

---

### `claude` — claude CLI not found in PATH

**Symptom:** `sol doctor` reports `claude CLI not found in PATH`

**Diagnosis:** The Claude Code CLI is not installed.

**Fix:**
```bash
npm install -g @anthropic-ai/claude-code
```

Ensure the npm global bin directory is on your `$PATH`.

---

### `jq` — jq not found in PATH

**Symptom:** `sol doctor` reports `jq not found in PATH`

**Diagnosis:** jq is not installed. It is required for reading OAuth tokens from broker-managed credentials files.

**Fix:**
```bash
# macOS
brew install jq

# Linux (Debian/Ubuntu)
apt install jq
```

---

### `sol_home` — SOL_HOME parent not writable

**Symptom:** `sol doctor` reports `SOL_HOME (<path>) does not exist and parent is not writable`

**Diagnosis:** The directory that would contain SOL_HOME doesn't allow writes.

**Fix:**
```bash
mkdir -p ~/sol   # or wherever SOL_HOME points
```

Or set `SOL_HOME` to a writable location:
```bash
export SOL_HOME=/path/to/writable/dir
```

---

### `sol_home` — SOL_HOME is not a directory

**Symptom:** `sol doctor` reports `SOL_HOME (<path>) exists but is not a directory`

**Diagnosis:** A file exists at the SOL_HOME path instead of a directory.

**Fix:**
```bash
rm ~/sol && mkdir -p ~/sol
```

---

### `sol_home` — SOL_HOME is not writable

**Symptom:** `sol doctor` reports `SOL_HOME (<path>) is not writable`

**Diagnosis:** The SOL_HOME directory exists but the current user can't write to it.

**Fix:**
```bash
chmod u+w ~/sol   # replace ~/sol with your SOL_HOME path
```

---

### `sqlite_wal` — WAL mode not supported

**Symptom:** `sol doctor` reports `WAL mode not supported (got journal_mode=<mode>)` or `failed to enable WAL mode`

**Diagnosis:** The filesystem where SOL_HOME lives does not support SQLite WAL locking. This commonly happens when SOL_HOME is on a network filesystem (NFS, CIFS/SMB, some cloud-mounted volumes).

**Fix:** Move SOL_HOME to a local filesystem:
```bash
export SOL_HOME=/local/path/to/sol
sol init
```

---

### `env:sphere` / `env:<world>` — permissions too open

**Symptom:** `sol doctor` reports `permissions <mode> — file is readable by group or others`

**Diagnosis:** The `.env` file at `$SOL_HOME/.env` or `$SOL_HOME/<world>/.env` has group- or world-readable permissions. This is a security risk since the file may contain API keys.

**Fix:**
```bash
chmod 600 $SOL_HOME/.env
chmod 600 $SOL_HOME/<world>/.env
```

---

### `env:sphere` / `env:<world>` — parse error

**Symptom:** `sol doctor` reports `parse error: <details>`

**Diagnosis:** The `.env` file has invalid syntax (e.g., unquoted special characters, missing `=`).

**Fix:** Open the file and check for malformed lines. Each line must follow `KEY=value` format. Lines starting with `#` are comments.

---

### `env:sphere` / `env:<world>` — key with empty value

**Symptom:** `sol doctor` reports `key "<KEY>" has an empty value`

**Diagnosis:** A key is defined in the `.env` file but has no value (e.g., `ANTHROPIC_API_KEY=`).

**Fix:** Either set a value for the key or remove the line entirely:
```bash
# In $SOL_HOME/.env or $SOL_HOME/<world>/.env
ANTHROPIC_API_KEY=sk-ant-...   # set a real value
```

---

## Agent Issues

### Agent appears stuck / not making progress

**Symptom:** An agent has been running for a long time with no visible output or writ progress. `sol status <world>` shows the agent in a stalled state.

**Diagnosis:**
1. Check the agent's live session:
   ```bash
   sol session attach <agent-name>
   ```
   Look for: frozen output, waiting for input, repeated error messages, or a crashed process.

2. Check overall world status for stall detection:
   ```bash
   sol status <world>
   ```
   The sentinel monitors agents for stalls and will surface warnings.

3. Check the writ state:
   ```bash
   sol writ list --world=<world>
   ```

**Fix:**
- If the agent is genuinely stuck and not recoverable, kill the session and let it respawn:
  ```bash
  sol session kill <agent-name>
  ```
  The prefect will respawn the session. The agent will resume from its last committed state.

- If the agent keeps stalling on the same writ, consider escalating or manually reassigning the work.

---

### Session won't start

**Symptom:** An agent session fails to start. `sol status <world>` shows the agent without an active session.

**Diagnosis:** Common causes:

1. **tmux not running or not found** — run `sol doctor` to verify tmux.
2. **claude CLI not found** — run `sol doctor` to check the `claude` check.
3. **Worktree conflict** — a worktree for this agent/writ already exists in a broken state.

**Fix:**
1. Resolve any `sol doctor` failures first.
2. For worktree conflicts, check the managed repo:
   ```bash
   ls $SOL_HOME/<world>/repo/
   git -C $SOL_HOME/<world>/repo worktree list
   ```
   If a stale worktree exists, remove it:
   ```bash
   git -C $SOL_HOME/<world>/repo worktree remove --force <worktree-path>
   ```
3. After clearing the conflict, the prefect will attempt to start the session again.

---

### Agent keeps crashing

**Symptom:** An agent session repeatedly starts and stops. `sol status <world>` shows repeated respawn events.

**Diagnosis:**
1. Attach to the session immediately after it starts to catch the error:
   ```bash
   sol session attach <agent-name>
   ```
2. Check the tether state — the agent's work assignment:
   ```bash
   sol writ list --world=<world>
   ```
   If the tether is in an inconsistent state, the agent may crash on startup.

**Fix:**
- If the agent crashes on a specific writ, consider using `sol escalate` from within the session, or manually marking the writ to unblock the queue.
- Check disk space and permissions in `$SOL_HOME` — a full disk will cause repeated failures.
- Run `sol doctor` to rule out environmental issues.

---

## Forge Issues

### Merge keeps failing

**Symptom:** A writ's branch repeatedly fails to merge. The merge queue shows repeated retry attempts.

**Diagnosis:**
```bash
sol forge log --world=<world> --follow
```
Look for:
- **Gate failures** — a quality gate (lint, test, build) is failing.
- **Merge conflicts** — the branch conflicts with the current main branch.
- **Repeated conflict resolution failures** — the forge attempted AI-assisted conflict resolution but couldn't succeed.

**Fix:**
- **Gate failures:** Check which gate is failing in the forge log. If the failure is pre-existing on main (not caused by the branch), see the [Quality gate failures](#quality-gate-failures) section below.
- **Merge conflicts:** The agent needs to rebase their branch. If the session is still active, it will handle this. If not, escalate the writ.
- If a merge is permanently broken, mark it failed to unblock the queue:
  ```bash
  sol forge mark-failed --world=<world> <mr-id>
  ```

---

### Merge queue is stuck

**Symptom:** Items have been in the merge queue for a long time with no movement. `sol forge queue` shows no active processing.

**Diagnosis:**
```bash
sol forge queue --world=<world>
sol forge blocked --world=<world>
sol forge log --world=<world> --follow
```
Look for: a single failing item blocking all others, forge process errors, or a paused forge.

**Fix:**
1. If the forge is paused:
   ```bash
   sol forge resume --world=<world>
   ```

2. If a specific item is permanently stuck:
   ```bash
   sol forge mark-failed --world=<world> <mr-id>
   ```

3. If the forge process itself is unhealthy, restart it:
   ```bash
   sol forge pause --world=<world>
   sol forge resume --world=<world>
   ```

---

### Quality gate failures

**Symptom:** A merge fails because a gate check (e.g., `make test`, `make build`) fails, but the failure might be pre-existing on the main branch.

**Diagnosis:**
The forge runs gate checks on the merged result. To determine if a failure is pre-existing:
1. Check the forge log for the specific gate that failed:
   ```bash
   sol forge log --world=<world> --follow
   ```
2. Run the same gate command manually on the main branch:
   ```bash
   git -C $SOL_HOME/<world>/repo checkout main
   make test   # or whichever gate failed
   ```

**Fix:**
- **If failure is pre-existing on main:** Fix the main branch first, then retry the merge. Pre-existing failures will block all merges until resolved.
- **If failure is branch-specific:** The agent that created the branch needs to fix their code. The writ will remain in the queue until the branch is fixed and force-pushed.
- Gates are defined in `world.toml`. Review the gate configuration if a gate is consistently misconfigured.

---

## World Issues

### World sync fails

**Symptom:** `sol status <world>` shows sync errors, or operations fail with git-related errors about the managed repo.

**Diagnosis:**
Check the managed repo's state:
```bash
git -C $SOL_HOME/<world>/repo status
git -C $SOL_HOME/<world>/repo remote -v
git -C $SOL_HOME/<world>/repo fetch --dry-run
```

Common causes:
- Remote repository is unreachable (network issue, auth failure, repo moved/deleted)
- Managed repo has uncommitted changes or is in a detached HEAD state
- Disk space exhausted

**Fix:**
1. **Network/auth issues:** Verify you can reach the remote and that credentials are valid.
2. **Dirty repo state:**
   ```bash
   git -C $SOL_HOME/<world>/repo status
   # If uncommitted changes exist and are safe to discard:
   git -C $SOL_HOME/<world>/repo reset --hard HEAD
   ```
3. **Disk space:** Check with `df -h $SOL_HOME`.

---

### World init fails

**Symptom:** `sol world init` fails with an error.

**Diagnosis:**
Common causes:
1. **Prerequisites not met** — run `sol doctor` to check tmux, git, claude, SOL_HOME.
2. **Source repo not accessible** — the repo URL is unreachable or credentials are missing.
3. **World already exists** — a world with that name was already initialized.
4. **SOL_HOME not writable** — check permissions on `$SOL_HOME`.

**Fix:**
1. Run `sol doctor` and resolve all failures before retrying `sol world init`.
2. Verify the source repo URL is correct and accessible:
   ```bash
   git ls-remote <repo-url>
   ```
3. If the world exists in a broken state:
   ```bash
   sol world delete <world> --confirm
   sol world init <world> --repo <repo-url>
   ```

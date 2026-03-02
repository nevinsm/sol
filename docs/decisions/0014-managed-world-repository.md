# ADR-0014: Managed World Repository

Status: accepted
Date: 2026-03-02
Arc: 3.5

## Context

World `source_repo` is stored as a local filesystem path in `world.toml`.
`sol init` validates it with `os.Stat` + `IsDir`, rejecting git URLs outright.
Cast, envoy, and forge create worktrees directly from the user's local repo
using `git -C`, which pollutes it with worktree refs and git state. The governor
maintains a separate clone at `$SOL_HOME/{world}/governor/mirror/` — duplicating
git objects already present in agent worktrees.

A new user running `sol init --source-repo=git@github.com:org/repo.git` gets a
validation error. This is the first thing someone coming in cold would try.

## Decision

Every world maintains a managed git clone at `$SOL_HOME/{world}/repo/`. Sol
clones the source repository (URL or local path) during world initialization.
All worktree operations (cast, envoy, forge) create worktrees from this managed
clone. The governor's separate mirror is eliminated — the governor reads from
the managed clone directly.

### Clone behavior

- **URL input** (HTTPS, SSH): `git clone <url> $SOL_HOME/{world}/repo/`
- **Local path input**: `git clone <path> $SOL_HOME/{world}/repo/`, then adopt
  the local repo's upstream remote as origin (if it has one). This preserves
  push semantics — agents push directly to the real upstream, not back to the
  user's local repo.

### Upstream adoption for local paths

When the source is a local filesystem path:

```
upstream=$(git -C /local/path remote get-url origin)
git clone /local/path $SOL_HOME/{world}/repo/
if upstream != "":
    git -C $SOL_HOME/{world}/repo/ remote set-url origin $upstream
```

This ensures `git push origin HEAD` from any worktree goes to the real upstream
(e.g., GitHub), not the user's local copy.

### Sync

`sol world sync <world>` is the single command for keeping the managed clone
current:

- If `$SOL_HOME/{world}/repo/` doesn't exist but `source_repo` is configured:
  clone it (late initialization)
- If `$SOL_HOME/{world}/repo/` exists: `git fetch origin && git pull --ff-only`

### Governor mirror elimination

The governor's separate mirror (`$SOL_HOME/{world}/governor/mirror/`) is removed.
The governor reads code from `$SOL_HOME/{world}/repo/` directly — the managed
clone's main checkout is always on the target branch. `sol governor refresh-mirror`
is replaced by `sol world sync`.

### Config and path changes

- `config.RepoPath(world)` returns `$SOL_HOME/{world}/repo/`
- `dispatch.ResolveSourceRepo()` simplified to return `config.RepoPath(world)`
- `--source-repo` flags removed from `envoy create`, `governor start`, `forge start`
- `world.toml` `source_repo` field stores the original value (URL or path) for
  reference; the managed clone is the runtime source of truth

### Disk layout

```
$SOL_HOME/{world}/
├── repo/                           # managed git clone (NEW)
│   ├── .git/
│   └── (working tree on target branch)
├── outposts/{agent}/worktree/      # worktree of repo/
├── envoys/{name}/worktree/         # worktree of repo/
├── forge/worktree/                 # worktree of repo/
├── governor/
│   ├── .brief/
│   └── .claude/CLAUDE.md
└── world.toml
```

## Consequences

- Remote URLs (HTTPS, SSH) work as first-class `source_repo` values
- Sol fully owns the git state — user's repo is not polluted with worktree refs
- Shared git objects between all worktrees (agents, envoy, forge) via single clone
- Governor no longer duplicates the entire repo — reads from managed clone
- `sol world sync` is the single command for keeping the clone current
- CWD-based auto-discovery (`git rev-parse --show-toplevel`) preserved for
  convenience when no `--source-repo` flag is provided
- Existing worlds without `repo/` need re-initialization (pre-production, acceptable)

# ADR-0008: World Lifecycle with Dual-Store Design

Status: accepted
Date: 2026-02-27
Arc: 1

## Context

Worlds in sol are the primary isolation boundary — each has its own
database, agent pool, merge pipeline, and directory tree. Before Arc 1,
worlds were implicit: a world came into existence the first time
`store.OpenWorld()` was called, silently creating a database file.
There was no registry, no configuration, and no way to list, inspect,
or delete worlds through the CLI.

This created several operational problems:

- **Discovery**: No way to enumerate worlds. The autarch had to
  `ls .store/*.db` and strip extensions.
- **Configuration**: Quality gates lived in a flat file
  (`forge/quality-gates.txt`), name pool path was hardcoded, source
  repo was discovered from CWD every time.
- **Teardown**: Deleting a world required manual cleanup of DB files,
  directory trees, and agent records across two databases.
- **Accidental creation**: A typo in `--world=myworl` would silently
  create a new empty database.

Three design questions shaped the solution:

1. **Hard gate vs. soft warning**: Should commands refuse to operate
   on uninitialized worlds, or just warn?
2. **Source of truth**: Should configuration live in the database,
   in files, or both?
3. **Config surface**: What should be configurable per-world?

## Decision

### Hard gate

`sol world init` is required before any world operation. Every
world-scoped command calls `config.RequireWorld(world)` as its first
check. This prevents accidental world creation and makes the world
lifecycle explicit.

The gate validates the world name format and checks for `world.toml`
existence — a regex and file check, not a database query. This is GLASS-friendly (verifiable with `ls`), fast,
and requires no database connection.

Pre-Arc1 worlds (database exists but no `world.toml`) get a specific
error message directing the autarch to run `sol world init <name>`
to adopt the existing world.

### Dual-store, file primary

Configuration uses a dual-store design:

- **`world.toml`** (file): Source of truth for world configuration.
  Layered resolution: defaults → `sol.toml` (global) → `world.toml`
  (per-world). Human-editable, GLASS-inspectable.
- **`worlds` table** (sphere.db): Registry of initialized worlds.
  Stores name, source_repo, and timestamps. Used for `sol world list`
  and discovery. Treated as a cache — `world.toml` existence is the
  authoritative signal.

The file-primary design follows GLASS: the autarch can inspect and edit
configuration with standard tools (`cat`, `vim`, `toml` parsers)
without needing to query SQLite.

### Configuration surface

Per-world configuration (`world.toml`):

```toml
[world]
source_repo = "/path/to/repo"   # persistent source repo binding

[agents]
capacity = 10                    # max agents (0 = unlimited)
name_pool_path = ""              # custom name pool file
model_tier = "sonnet"            # model guidance for agents

[forge]
target_branch = "main"           # merge target
quality_gates = ["make test"]    # replaces flat file
```

Global defaults (`sol.toml`): same structure, applied before
world-specific overrides.

Config consumers (cast, forge, dispatch) read from `WorldConfig`
with fallbacks to legacy behavior (CWD-based source repo,
`quality-gates.txt` flat file, hardcoded `names.txt` path).

## Consequences

**Benefits:**
- Worlds are explicit, discoverable, and manageable through the CLI
- Typos in world names fail fast instead of creating orphan databases
- Configuration is centralized, human-readable, and version-controllable
- Pre-Arc1 worlds have a clean adoption path
- Source repo is persistent — no more CWD dependency for cast/forge

**Tradeoffs:**
- Every new world requires `sol world init` (one-time cost per world)
- Dual-store means configuration can theoretically diverge between
  `world.toml` and the `worlds` table (mitigated: file is source of
  truth, DB is cache)
- Config fallbacks to legacy paths add complexity (mitigated: will be
  removed once all existing deployments have migrated)

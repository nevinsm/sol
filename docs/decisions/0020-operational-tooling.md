# ADR-0020: Operational Tooling

Status: accepted
Date: 2026-03-06
Arc: 6

## Context

Sol has reached the point where multiple worlds run concurrently in
production. The operator's toolkit for managing these worlds remains
manual — backing up a world means knowing which files and database
records to copy, restoring means reversing that process by hand, and
there's no way to duplicate a world's configuration for a new project.

Four operational gaps need closing:

1. **No backup/restore.** World state spans two databases (sphere.db
   rows filtered by world, plus the per-world database) and a directory
   tree. There's no single command to capture or restore this state.

2. **No world cloning.** Standing up a new world with the same
   configuration as an existing one requires manual copying of
   `world.toml`, agent setup, and credential binding.

3. **No schema migration visibility.** Migrations run automatically on
   store open (`migrateWorld`, `migrateSphere`), but the operator has
   no way to check current schema versions, verify migration status, or
   understand what version an archive was created at.

4. **No multi-world prefect filtering.** The prefect supervises all
   worlds unconditionally. Operators running mixed environments (some
   worlds in maintenance, some in active development) cannot restrict
   prefect supervision to a subset of worlds without using the `sleeping`
   flag, which also affects other components.

## Decision

### World export: `sol world export <name>`

Export produces a `.tar.gz` archive containing everything needed to
restore the world on the same or different sol instance. The archive
structure is:

```
sol-export-{name}-{timestamp}/
├── manifest.json           # metadata (see below)
├── world.toml              # world configuration
├── world.db                # per-world database (SQLite, checkpointed)
└── sphere-data/
    ├── agents.json          # agent records for this world
    ├── messages.json        # messages sent by/to this world's agents
    ├── escalations.json     # escalations sourced from this world
    ├── caravans.json        # caravans containing items for this world
    └── caravan_items.json   # caravan items scoped to this world
```

**`manifest.json`** contains:

```json
{
  "version": 1,
  "world": "myworld",
  "exported_at": "2026-03-06T14:30:00Z",
  "sol_version": "0.1.0",
  "schema_versions": {
    "world": 7,
    "sphere": 8
  }
}
```

**What is included:**

- The complete per-world database (`{world}.db`), checkpoint-flushed
  before copy to ensure WAL contents are consolidated into the main
  database file.
- World-scoped rows from sphere.db: agents (filtered by `world`),
  messages (filtered by sender/recipient matching world agents),
  escalations (filtered by source), caravan items (filtered by world),
  and their parent caravans.
- The `world.toml` configuration file.

**What is excluded:**

- The managed repository (`repo/`) — reconstructible from `source_repo`
  in `world.toml` via `sol world sync`.
- Worktree directories (`outposts/`, `envoys/`, `forge/`) — ephemeral
  working state recreated on next cast/start.
- Agent config directories (`.claude-config/`) — session transcripts
  and auto-memories are agent-local state (ADR-0018), not world state.
- Brief files (`.brief/`) — agent-maintained, regenerated over time.
- Account credentials (`.accounts/`) — sphere-scoped, not world-scoped,
  and sensitive.
- The `sol.toml` global configuration — sphere-scoped.
- Tether files — represent in-flight work; exporting active tethers
  would create invalid state on import.

**SQLite checkpoint:** Before copying `world.db`, the export runs
`PRAGMA wal_checkpoint(TRUNCATE)` to flush the WAL journal into the
main database file. This ensures the archive contains a single,
self-consistent database file.

**Sphere data as JSON:** World-scoped rows from sphere.db are exported
as JSON rather than as a database slice. This avoids the complexity of
extracting a filtered subset of a shared database and makes the archive
contents inspectable — consistent with the manifesto's emphasis on
operator inspectability.

### World import: `sol world import <archive>`

Import restores a world from an exported archive:

1. Validate the manifest — check `version` is supported, schema
   versions are compatible (target sol binary must be at equal or higher
   schema versions).
2. Check that the world name does not already exist. If it does, refuse
   with an error. The operator must `sol world delete` first or use
   `--name=<newname>` to import under a different name.
3. Register the world in sphere.db (`worlds` table).
4. Create the world directory structure under `$SOL_HOME/{name}/`.
5. Copy `world.db` into place. Run `migrateWorld()` to bring it to the
   current schema version if the archive is from an older sol version.
6. Insert sphere-scoped data (agents, messages, escalations, caravans,
   caravan items) into sphere.db. Agent states are reset to `idle` on
   import — there are no active sessions for imported agents.
7. Copy `world.toml` into the world directory.
8. If `source_repo` is set in `world.toml`, prompt the operator to run
   `sol world sync <name>` to clone the managed repository.

**`--name` flag:** `sol world import <archive> --name=newname` imports
the world under a different name. Agent IDs and caravan item world
references are rewritten to match. This supports the "import as copy"
use case.

**Schema forward-compatibility:** Import refuses archives with schema
versions higher than the running binary supports. The operator must
upgrade sol first. Import from older schema versions succeeds — the
standard migration code brings the world database up to date.

### World clone: `sol world clone <source> <target>`

Clone creates a new world with the same configuration and optionally
the same work item history as an existing world. Two modes:

**Shallow clone (default):** Copies only configuration — `world.toml`
with `source_repo` preserved. Creates a fresh, empty world database.
Registers the new world in sphere.db. This is the common case:
standing up a new world for the same repository.

```
sol world clone myproject myproject-staging
```

**Deep clone (`--deep`):** Equivalent to export-then-import with name
rewriting. Copies the full world database (work items, merge requests,
history, token usage) and sphere-scoped data (agents, messages,
escalations). Agent states reset to `idle`. Tethers are not copied.

```
sol world clone myproject myproject-backup --deep
```

**Credential handling:** Clone does not copy credential bindings.
Agents in the cloned world inherit the account resolution chain
(ADR-0019): per-dispatch flag → `world.toml` `default_account` →
sphere default → `~/.claude` fallback. Since `world.toml` is copied
(including `default_account` if set), the cloned world uses the same
default account unless the operator overrides it.

**Managed repository:** The cloned world shares the same `source_repo`.
The operator runs `sol world sync <target>` to create a separate
managed clone. Each world's managed clone is independent — they fetch
from the same origin but have separate local state.

### Schema migration tooling: `sol schema status`

Expose schema version information and migration state:

```
$ sol schema status
Sphere database: v8 (current)
World databases:
  myworld:   v7 (current)
  staging:   v7 (current)
  legacy:    v5 (needs migration)
```

**`sol schema migrate`** runs migrations on all databases. This is what
`store.Open*` already does automatically, but the explicit command
gives operators visibility and control — particularly useful after a
sol binary upgrade where multiple worlds need migration.

Migrations remain embedded in the binary (the `schema.go` constants).
This is the right choice for sol's deployment model: a single binary
with no external dependencies. A standalone migration tool would add
operational complexity (keeping tool and binary in sync) without
meaningful benefit. The embedded approach is proven across 8 sphere
versions and 7 world versions.

The `manifest.json` in export archives captures schema versions at
export time, enabling import to detect version mismatches before
attempting restoration.

### Multi-world prefect selection: `--worlds` flag

Add a `--worlds` flag to `sol prefect run`:

```
sol prefect run --worlds=frontend,backend,api
```

The flag accepts a comma-separated list of world names. When set, the
prefect only supervises agents in the listed worlds. When omitted, the
prefect supervises all worlds (current behavior, unchanged).

**Implementation:** The prefect's heartbeat loop currently calls
`ListAgents("", "working")` to get all working agents across all
worlds. With `--worlds`, the loop iterates over the specified world
list, calling `ListAgents(world, "working")` for each. The existing
`ListAgents` interface already supports world filtering via its first
parameter.

**Interaction with sleeping:** The `sleeping` flag in `world.toml`
remains the per-world opt-out mechanism. `--worlds` is additive
filtering — a world must be both in the `--worlds` list and not
sleeping to be supervised. This lets operators use `sleeping` for
temporary pauses and `--worlds` for structural partitioning (e.g.,
separate prefect instances for production vs staging worlds).

**No config file equivalent:** The `--worlds` flag is CLI-only, not
persisted in `sol.toml`. Prefect invocation is controlled by systemd
unit files or process managers where the flag belongs. Adding a config
key would create a confusing precedence question (does the flag
override config or merge with it?).

## Consequences

- Operators can backup and restore worlds with single commands,
  eliminating manual file-and-database coordination.
- Archives are self-describing (manifest) and inspectable (JSON for
  sphere data, standard SQLite for world data, plain TOML for config).
- World cloning enables rapid environment replication — staging from
  production, experiment branches from main worlds.
- Schema version visibility prevents surprise migration failures and
  gives operators confidence during upgrades.
- Export excludes ephemeral and reconstructible state (repos, worktrees,
  config dirs, briefs), keeping archives focused on durable world state.
- Multi-world prefect filtering enables structural partitioning without
  overloading the `sleeping` flag.
- Import resets agent states to `idle`, which means any in-flight work
  at export time requires manual re-dispatch after import. This is the
  correct trade-off — importing active tethers would reference
  worktrees and sessions that don't exist on the target.
- The `--name` flag on import and the clone command both rewrite world
  references in sphere data, which requires care around caravan items
  that span multiple worlds. Cross-world caravan items pointing to
  other worlds are preserved as-is — only items belonging to the
  exported world are rewritten.

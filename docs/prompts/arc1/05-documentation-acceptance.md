# Prompt 05: Arc 1 — Documentation + Acceptance

You are completing Arc 1 (World Lifecycle) with documentation and final
acceptance verification. This prompt creates an ADR for the design
decisions, updates the arc roadmap and CLAUDE.md, and runs a full
acceptance sweep.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 prompts 01–04 are complete (config, schema,
world store, CLI commands, hard gate, config consumers).

Read the existing documentation first:
- `docs/decisions/` — all existing ADRs for format reference
- `docs/arc-roadmap.md` — current arc roadmap
- `CLAUDE.md` — project root instructions

---

## Task 1: ADR-0008 — World Lifecycle

**Create** `docs/decisions/0008-world-lifecycle.md`.

Follow the lightweight MADR format used by ADRs 0001–0007.

```markdown
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

- **Discovery**: No way to enumerate worlds. Operators had to
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

The gate checks for `world.toml` existence — a file check, not a
database query. This is GLASS-friendly (verifiable with `ls`), fast,
and requires no database connection.

Pre-Arc1 worlds (database exists but no `world.toml`) get a specific
error message directing the operator to run `sol world init <name>`
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

The file-primary design follows GLASS: operators can inspect and edit
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
```

---

## Task 2: Update Arc Roadmap

**Modify** `docs/arc-roadmap.md`.

Add completion notes to the Arc 1 section. Follow the same format that
Arc 0 would have (if it had completion notes):

```markdown
## Arc 1: World Lifecycle

Explicit world management — the biggest operational gap.

- `sol world init <name>` — create world database, directory structure, optional source repo association
- `sol world list` — discover all worlds from `.store/` directory
- `sol world status <name>` — aggregate view (agents, work items, active sessions, config)
- `sol world delete <name>` — safe teardown with confirmation
- Source repo association — persisted in world.toml, no longer relies on cwd for `cast`
- Configuration files: `sol.toml` (global), `world.toml` (per-world)
- Configuration surface: quality gates, agent capacity, model tier, name pool path
- Hard gate: `sol world init` required before any world operation

**Acceptance:** Operator can fully manage world lifecycle through CLI.
Worlds are explicit, discoverable, and configurable.

**Status:** Complete.
- Schema V5: `worlds` table in sphere.db
- Config: `world.toml` (per-world), `sol.toml` (global), three-layer resolution
- ADR-0008: Dual-store design rationale
```

---

## Task 3: Update CLAUDE.md

**Modify** `CLAUDE.md` (project root).

Add world lifecycle entries to the Key Concepts section:

```markdown
- **World Config**: `world.toml` per-world, `sol.toml` global — layered TOML configuration
- **World Lifecycle**: `sol world init` required before use — explicit world creation
```

Add to the Commits section (or Conventions if more appropriate):

```markdown
- World config path: $SOL_HOME/{world}/world.toml
- Global config path: $SOL_HOME/sol.toml
```

Keep changes minimal — only add what a new developer needs to know.

---

## Task 4: Acceptance Verification

Run the full acceptance sweep. Every check must pass.

### Build and test

```bash
make build && make test
```

### World lifecycle flow

```bash
export SOL_HOME=/tmp/sol-test
rm -rf /tmp/sol-test
mkdir -p /tmp/sol-test/.store

# 1. Init a world
bin/sol world init myworld --source-repo=$(pwd)
test -f /tmp/sol-test/myworld/world.toml && echo "PASS: world.toml exists"
test -f /tmp/sol-test/.store/myworld.db && echo "PASS: world DB exists"
test -d /tmp/sol-test/myworld/outposts && echo "PASS: outposts dir exists"

# 2. List worlds
bin/sol world list
bin/sol world list --json

# 3. World status
bin/sol world status myworld

# 4. Store operations work
bin/sol store create --world=myworld --title="Test item"
bin/sol store list --world=myworld
```

### Hard gate enforcement

```bash
# 5. Uninitialized world fails
bin/sol store create --world=noworld --title="test" 2>&1 | grep -q "does not exist" && echo "PASS: hard gate blocks"

# 6. Pre-Arc1 world gets helpful message
sqlite3 /tmp/sol-test/.store/legacy.db "CREATE TABLE schema_version (version INTEGER NOT NULL); INSERT INTO schema_version VALUES (4);"
bin/sol store create --world=legacy --title="test" 2>&1 | grep -q "before world lifecycle" && echo "PASS: pre-Arc1 detected"

# 7. Adopt pre-Arc1 world
bin/sol world init legacy
bin/sol store create --world=legacy --title="test" && echo "PASS: adopted world works"
```

### Config consumers

```bash
# 8. Config file is valid TOML
cat /tmp/sol-test/myworld/world.toml

# 9. Schema version is 5
sqlite3 /tmp/sol-test/.store/sphere.db "SELECT version FROM schema_version"
# → 5

# 10. Worlds table exists and has entries
sqlite3 /tmp/sol-test/.store/sphere.db "SELECT name, source_repo FROM worlds"
# → myworld and legacy
```

### GLASS principle

```bash
# 11. Everything is inspectable with standard tools
ls /tmp/sol-test/myworld/
cat /tmp/sol-test/myworld/world.toml
sqlite3 /tmp/sol-test/.store/sphere.db ".tables"
sqlite3 /tmp/sol-test/.store/sphere.db "SELECT * FROM worlds"
```

### Teardown

```bash
# 12. Delete world
bin/sol world delete myworld --confirm
test ! -f /tmp/sol-test/.store/myworld.db && echo "PASS: DB removed"
test ! -d /tmp/sol-test/myworld && echo "PASS: directory removed"

# 13. Cleanup
bin/sol world delete legacy --confirm
rm -rf /tmp/sol-test
```

### Grep verification

```bash
# 14. No ungated OpenWorld calls in cmd/
grep -rn 'store.OpenWorld' cmd/*.go
# Each should have a RequireWorld check in the same function

# 15. No stale references
grep -rn 'quality-gates.txt' cmd/*.go
# Should only appear in fallback paths, never as sole source
```

---

## Guidelines

- The ADR follows the lightweight MADR format used by all existing
  ADRs (0001–0007): Context → Decision → Consequences.
- Documentation changes are minimal — only add what's needed for
  developers and operators to understand the world lifecycle.
- The acceptance verification is a script-style checklist. Run each
  command and verify the expected output. All checks must pass.
- Do NOT modify any Go code in this prompt — only documentation files
  and verification.
- Commit after verification passes with message:
  `docs(world): add ADR-0008 and arc roadmap update for world lifecycle`

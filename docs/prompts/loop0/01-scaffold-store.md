# Prompt 01: Loop 0 — Project Scaffold + Store

You are building the foundation of a multi-agent orchestration system called
`sol`. This prompt creates the project scaffold and the SQLite store — the
single source of truth for all coordination state.

**Working directory:** `~/sol-src/`
**Go module:** `github.com/nevinsm/sol`

Read `docs/target-architecture.md` for full context on what this system is
and where it's headed. You are implementing Loop 0 (Section 5).

---

## Task 1: Project Scaffold

Initialize the project:

```
~/sol-src/
├── go.mod                          (github.com/nevinsm/sol)
├── main.go                         (cobra entry point)
├── Makefile                        (build, test, install)
├── CLAUDE.md                       (project-level instructions)
├── internal/
│   ├── config/
│   │   └── config.go               (SOL_HOME resolution)
│   └── store/
│       ├── store.go                (connection, setup, close)
│       ├── schema.go               (DDL, migrations)
│       ├── workitems.go            (work item CRUD)
│       ├── agents.go               (agent record CRUD)
│       └── store_test.go
└── cmd/
    ├── root.go                     (cobra root command)
    └── store.go                    (sol store subcommands)
```

### CLAUDE.md (project root)

Create a `CLAUDE.md` at the project root with the following content:

```markdown
# sol — Multi-Agent Orchestration System

Production-ready system for coordinating concurrent AI coding agents.

## Architecture
- Read `docs/target-architecture.md` for the full system spec
- Read `docs/manifesto.md` for design philosophy

## Build & Test
- Build: `make build` (binary at `bin/sol`)
- Test: `make test`
- Install: `make install`

## Key Concepts
- **SOL_HOME**: Runtime root directory (env var, default ~/sol)
- **Store**: SQLite (WAL mode) — sphere.db for agents, {world}.db for work items
- **Session**: tmux-based process containers for AI agents
- **Tether**: File at $SOL_HOME/{world}/outposts/{name}/.tether — the durability primitive
- **Cast**: Dispatch work to an agent (creates worktree, tethers work, starts session)
- **Prime**: Inject execution context on session start
- **Resolve**: Signal work complete (push branch, clear tether, stop session)

## Conventions
- Go module: github.com/nevinsm/sol
- All timestamps: RFC3339 in UTC
- Work item IDs: "sol-" + 8 hex chars (e.g., sol-a1b2c3d4)
- Session names: sol-{world}-{agentName} (e.g., sol-myworld-Toast)
- Error messages include context: "failed to open world database %q: %w"
- SQLite connections always set: journal_mode=WAL, busy_timeout=5000, foreign_keys=ON
```

### config package

`SOL_HOME` env var determines the runtime root. Default: `~/sol`.

```go
// internal/config/config.go
package config

func Home() string        // $SOL_HOME or ~/sol
func StoreDir() string    // $SOL_HOME/.store/
func RuntimeDir() string  // $SOL_HOME/.runtime/
func RigDir(world string) string  // $SOL_HOME/{world}/

// EnsureDirs creates .store/ and .runtime/ if they don't exist.
func EnsureDirs() error
```

### Makefile

```makefile
.PHONY: build test install clean

build:
	go build -o bin/sol .

test:
	go test ./...

install: build
	cp bin/sol /usr/local/bin/sol

clean:
	rm -rf bin/
```

### main.go

Standard cobra setup. Calls `cmd.Execute()`.

### cmd/root.go

Root command `sol` with version flag. Calls `config.EnsureDirs()` on
`PersistentPreRun` so the runtime directory exists for all subcommands.

---

## Task 2: Store Package

The store wraps SQLite (WAL mode) for all coordination state. Two databases:

- **Sphere DB** (`$SOL_HOME/.store/sphere.db`): agent identities
- **World DB** (`$SOL_HOME/.store/{world}.db`): work items for that world

### Schema

**Sphere DB schema (version 1):**

```sql
CREATE TABLE agents (
    id          TEXT PRIMARY KEY,    -- "myworld/Toast"
    name        TEXT NOT NULL,       -- "Toast"
    world         TEXT NOT NULL,       -- "myworld"
    role        TEXT NOT NULL,       -- outpost|sentinel|forge|crew
    state       TEXT NOT NULL DEFAULT 'idle',  -- idle|working|stalled|stuck|zombie
    hook_item   TEXT,                -- currently tethered work item ID (NULL if idle)
    created_at  TEXT NOT NULL,       -- RFC3339
    updated_at  TEXT NOT NULL        -- RFC3339
);

CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version VALUES (1);
```

**World DB schema (version 1):**

```sql
CREATE TABLE work_items (
    id          TEXT PRIMARY KEY,    -- "sol-" + 8 hex chars
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open', -- open|tethered|working|done|closed
    priority    INTEGER NOT NULL DEFAULT 2,   -- 1=high, 2=normal, 3=low
    assignee    TEXT,                -- agent ID (NULL if unassigned)
    parent_id   TEXT,                -- for sub-items (NULL if top-level)
    created_by  TEXT NOT NULL,       -- "operator" or agent ID
    created_at  TEXT NOT NULL,       -- RFC3339
    updated_at  TEXT NOT NULL,       -- RFC3339
    closed_at   TEXT                 -- RFC3339 (NULL if not closed)
);
CREATE INDEX idx_work_status ON work_items(status);
CREATE INDEX idx_work_assignee ON work_items(assignee);
CREATE INDEX idx_work_priority ON work_items(priority);

CREATE TABLE labels (
    work_item_id TEXT NOT NULL REFERENCES work_items(id),
    label        TEXT NOT NULL,
    PRIMARY KEY (work_item_id, label)
);
CREATE INDEX idx_labels_label ON labels(label);

CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version VALUES (1);
```

### SQLite Configuration

Every connection must set:
```sql
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
```

### Go Interface

```go
// internal/store/store.go
package store

type Store struct {
    db   *sql.DB
    path string
}

// OpenWorld opens (or creates) a world database at $SOL_HOME/.store/{world}.db.
func OpenWorld(world string) (*Store, error)

// OpenSphere opens (or creates) the sphere database at $SOL_HOME/.store/sphere.db.
func OpenSphere() (*Store, error)

// Close closes the database connection.
func (s *Store) Close() error

// Migrate runs pending schema migrations.
func (s *Store) Migrate() error
```

```go
// internal/store/workitems.go
package store

type WorkItem struct {
    ID          string
    Title       string
    Description string
    Status      string
    Priority    int
    Assignee    string   // empty if unassigned
    ParentID    string   // empty if top-level
    CreatedBy   string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    ClosedAt    *time.Time
    Labels      []string
}

type ListFilters struct {
    Status   string   // empty = all
    Assignee string   // empty = all
    Label    string   // empty = all
    Priority int      // 0 = all
}

// CreateWorkItem creates a new work item. Returns the generated ID.
// ID format: "sol-" + 8 random hex chars.
func (s *Store) CreateWorkItem(title, description, createdBy string, priority int, labels []string) (string, error)

// GetWorkItem returns a work item by ID, including its labels.
func (s *Store) GetWorkItem(id string) (*WorkItem, error)

// ListWorkItems returns work items matching the filters.
func (s *Store) ListWorkItems(filters ListFilters) ([]WorkItem, error)

// UpdateWorkItem updates fields on a work item. Only non-zero fields
// in the updates struct are applied. Always updates updated_at.
func (s *Store) UpdateWorkItem(id string, updates WorkItemUpdates) error

type WorkItemUpdates struct {
    Status   string // empty = no change
    Assignee string // empty = no change, "-" = clear
    Priority int    // 0 = no change
}

// CloseWorkItem sets status to "closed" and records closed_at.
func (s *Store) CloseWorkItem(id string) error

// AddLabel adds a label to a work item. No-op if already present.
func (s *Store) AddLabel(itemID, label string) error

// RemoveLabel removes a label from a work item.
func (s *Store) RemoveLabel(itemID, label string) error
```

```go
// internal/store/agents.go
package store

type Agent struct {
    ID        string
    Name      string
    World       string
    Role      string
    State     string
    TetherItem  string // empty if idle
    CreatedAt time.Time
    UpdatedAt time.Time
}

// CreateAgent creates an agent record in the sphere DB.
func (s *Store) CreateAgent(name, world, role string) (string, error)

// GetAgent returns an agent by ID ("world/name").
func (s *Store) GetAgent(id string) (*Agent, error)

// UpdateAgentState updates an agent's state and optionally its hook_item.
func (s *Store) UpdateAgentState(id, state, tetherItem string) error

// ListAgents returns agents for a world, optionally filtered by state.
func (s *Store) ListAgents(world string, state string) ([]Agent, error)

// FindIdleAgent returns the first idle outpost for a world, or nil.
func (s *Store) FindIdleAgent(world string) (*Agent, error)
```

### Work Item ID Generation

Generate IDs as `"sol-" + 8 random hex characters` using `crypto/rand`.
Example: `sol-a1b2c3d4`.

---

## Task 3: CLI Commands

Implement `sol store` with these subcommands:

### `sol store create`

```
sol store create --world=<world> --title="..." [--description="..."] [--priority=N] [--label=<label>]...
```

Creates a work item in the specified world database. Prints the ID to stdout.
`--label` can be repeated for multiple labels. Default priority: 2.
`created_by` is always `"operator"` for CLI-created items.

### `sol store get <id>`

```
sol store get <id> --world=<world> [--json]
```

Prints the work item. Default format: human-readable. `--json` for machine-readable.
Exit 1 if not found.

### `sol store list`

```
sol store list --world=<world> [--status=<status>] [--label=<label>] [--assignee=<agent>] [--json]
```

Lists work items matching filters. Default: all open items.

### `sol store update <id>`

```
sol store update <id> --world=<world> [--status=<status>] [--assignee=<agent>] [--priority=N]
```

Updates work item fields. At least one update flag required.

### `sol store close <id>`

```
sol store close <id> --world=<world>
```

Sets status to "closed" and records `closed_at`.

### `sol store query`

```
sol store query --world=<world> --sql="SELECT ..."
```

Runs a read-only SQL query and prints results as a table (or JSON with `--json`).
This is the escape hatch for complex queries. Only SELECT is allowed.

---

## Task 4: Tests

Write tests in `internal/store/store_test.go` covering:

1. **Schema creation**: Open a new DB, verify tables and indexes exist
2. **Work item CRUD**: Create, get, list, update, close
3. **Labels**: Add, remove, filter by label
4. **ID generation**: Verify format `sol-[0-9a-f]{8}`
5. **Concurrent access**: Open two connections to the same DB, write from
   both, verify no errors (WAL mode test)
6. **Not found**: GetWorkItem with nonexistent ID returns appropriate error
7. **Agent CRUD**: Create, get, list, update state, find idle

Tests should use `t.TempDir()` for database files — no shared state between
tests.

Run tests and fix any issues before completing.

---

## Task 5: Verify

After implementation:

1. `make build` succeeds
2. `make test` passes all tests
3. `SOL_HOME=/tmp/sol-test bin/sol store create --world=testrig --title="Test item"` prints an ID
4. `SOL_HOME=/tmp/sol-test bin/sol store get <id> --world=testrig` prints the item
5. `SOL_HOME=/tmp/sol-test bin/sol store list --world=testrig` shows the item
6. `SOL_HOME=/tmp/sol-test sqlite3 /tmp/sol-test/.store/testrig.db "SELECT * FROM work_items"` shows the row

Clean up `/tmp/sol-test` after verification.

---

## Dependencies

Use `modernc.org/sqlite` for pure-Go SQLite (no CGo). Use
`github.com/spf13/cobra` for CLI. No other external dependencies.

## Guidelines

- Keep it simple. This is Loop 0 — the smallest working foundation.
- No premature abstractions. If something is used once, inline it.
- Error messages should include context: `"failed to open world database %q: %w"`.
- All timestamps are RFC3339 in UTC.
- Initialize the git repo with `git init` and make an initial commit after
  the scaffold is complete, before writing the store code. Make a second
  commit after the store is complete and tests pass.

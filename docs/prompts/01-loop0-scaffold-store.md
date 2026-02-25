# Prompt 01: Loop 0 — Project Scaffold + Store

You are building the foundation of a multi-agent orchestration system called
`gt`. This prompt creates the project scaffold and the SQLite store — the
single source of truth for all coordination state.

**Working directory:** `~/gt-src/`
**Go module:** `github.com/nevinsm/gt`

Read `docs/target-architecture.md` for full context on what this system is
and where it's headed. You are implementing Loop 0 (Section 5).

---

## Task 1: Project Scaffold

Initialize the project:

```
~/gt-src/
├── go.mod                          (github.com/nevinsm/gt)
├── main.go                         (cobra entry point)
├── Makefile                        (build, test, install)
├── CLAUDE.md                       (project-level instructions)
├── internal/
│   ├── config/
│   │   └── config.go               (GT_HOME resolution)
│   └── store/
│       ├── store.go                (connection, setup, close)
│       ├── schema.go               (DDL, migrations)
│       ├── workitems.go            (work item CRUD)
│       ├── agents.go               (agent record CRUD)
│       └── store_test.go
└── cmd/
    ├── root.go                     (cobra root command)
    └── store.go                    (gt store subcommands)
```

### CLAUDE.md (project root)

Create a `CLAUDE.md` at the project root with the following content:

```markdown
# gt — Multi-Agent Orchestration System

Production-ready system for coordinating concurrent AI coding agents.

## Architecture
- Read `docs/target-architecture.md` for the full system spec
- Read `docs/manifesto.md` for design philosophy

## Build & Test
- Build: `make build` (binary at `bin/gt`)
- Test: `make test`
- Install: `make install`

## Key Concepts
- **GT_HOME**: Runtime root directory (env var, default ~/gt)
- **Store**: SQLite (WAL mode) — town.db for agents, {rig}.db for work items
- **Session**: tmux-based process containers for AI agents
- **Hook**: File at $GT_HOME/{rig}/polecats/{name}/.hook — the durability primitive
- **Sling**: Dispatch work to an agent (creates worktree, hooks work, starts session)
- **Prime**: Inject execution context on session start
- **Done**: Signal work complete (push branch, clear hook, stop session)

## Conventions
- Go module: github.com/nevinsm/gt
- All timestamps: RFC3339 in UTC
- Work item IDs: "gt-" + 8 hex chars (e.g., gt-a1b2c3d4)
- Session names: gt-{rig}-{agentName} (e.g., gt-myrig-Toast)
- Error messages include context: "failed to open rig database %q: %w"
- SQLite connections always set: journal_mode=WAL, busy_timeout=5000, foreign_keys=ON
```

### config package

`GT_HOME` env var determines the runtime root. Default: `~/gt`.

```go
// internal/config/config.go
package config

func Home() string        // $GT_HOME or ~/gt
func StoreDir() string    // $GT_HOME/.store/
func RuntimeDir() string  // $GT_HOME/.runtime/
func RigDir(rig string) string  // $GT_HOME/{rig}/

// EnsureDirs creates .store/ and .runtime/ if they don't exist.
func EnsureDirs() error
```

### Makefile

```makefile
.PHONY: build test install clean

build:
	go build -o bin/gt .

test:
	go test ./...

install: build
	cp bin/gt /usr/local/bin/gt

clean:
	rm -rf bin/
```

### main.go

Standard cobra setup. Calls `cmd.Execute()`.

### cmd/root.go

Root command `gt` with version flag. Calls `config.EnsureDirs()` on
`PersistentPreRun` so the runtime directory exists for all subcommands.

---

## Task 2: Store Package

The store wraps SQLite (WAL mode) for all coordination state. Two databases:

- **Town DB** (`$GT_HOME/.store/town.db`): agent identities
- **Rig DB** (`$GT_HOME/.store/{rig}.db`): work items for that rig

### Schema

**Town DB schema (version 1):**

```sql
CREATE TABLE agents (
    id          TEXT PRIMARY KEY,    -- "myrig/Toast"
    name        TEXT NOT NULL,       -- "Toast"
    rig         TEXT NOT NULL,       -- "myrig"
    role        TEXT NOT NULL,       -- polecat|witness|refinery|crew
    state       TEXT NOT NULL DEFAULT 'idle',  -- idle|working|stalled|stuck|zombie
    hook_item   TEXT,                -- currently hooked work item ID (NULL if idle)
    created_at  TEXT NOT NULL,       -- RFC3339
    updated_at  TEXT NOT NULL        -- RFC3339
);

CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version VALUES (1);
```

**Rig DB schema (version 1):**

```sql
CREATE TABLE work_items (
    id          TEXT PRIMARY KEY,    -- "gt-" + 8 hex chars
    title       TEXT NOT NULL,
    description TEXT,
    status      TEXT NOT NULL DEFAULT 'open', -- open|hooked|working|done|closed
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

// OpenRig opens (or creates) a rig database at $GT_HOME/.store/{rig}.db.
func OpenRig(rig string) (*Store, error)

// OpenTown opens (or creates) the town database at $GT_HOME/.store/town.db.
func OpenTown() (*Store, error)

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
// ID format: "gt-" + 8 random hex chars.
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
    Rig       string
    Role      string
    State     string
    HookItem  string // empty if idle
    CreatedAt time.Time
    UpdatedAt time.Time
}

// CreateAgent creates an agent record in the town DB.
func (s *Store) CreateAgent(name, rig, role string) (string, error)

// GetAgent returns an agent by ID ("rig/name").
func (s *Store) GetAgent(id string) (*Agent, error)

// UpdateAgentState updates an agent's state and optionally its hook_item.
func (s *Store) UpdateAgentState(id, state, hookItem string) error

// ListAgents returns agents for a rig, optionally filtered by state.
func (s *Store) ListAgents(rig string, state string) ([]Agent, error)

// FindIdleAgent returns the first idle polecat for a rig, or nil.
func (s *Store) FindIdleAgent(rig string) (*Agent, error)
```

### Work Item ID Generation

Generate IDs as `"gt-" + 8 random hex characters` using `crypto/rand`.
Example: `gt-a1b2c3d4`.

---

## Task 3: CLI Commands

Implement `gt store` with these subcommands:

### `gt store create`

```
gt store create --db=<rig> --title="..." [--description="..."] [--priority=N] [--label=<label>]...
```

Creates a work item in the specified rig database. Prints the ID to stdout.
`--label` can be repeated for multiple labels. Default priority: 2.
`created_by` is always `"operator"` for CLI-created items.

### `gt store get <id>`

```
gt store get <id> --db=<rig> [--json]
```

Prints the work item. Default format: human-readable. `--json` for machine-readable.
Exit 1 if not found.

### `gt store list`

```
gt store list --db=<rig> [--status=<status>] [--label=<label>] [--assignee=<agent>] [--json]
```

Lists work items matching filters. Default: all open items.

### `gt store update <id>`

```
gt store update <id> --db=<rig> [--status=<status>] [--assignee=<agent>] [--priority=N]
```

Updates work item fields. At least one update flag required.

### `gt store close <id>`

```
gt store close <id> --db=<rig>
```

Sets status to "closed" and records `closed_at`.

### `gt store query`

```
gt store query --db=<rig> --sql="SELECT ..."
```

Runs a read-only SQL query and prints results as a table (or JSON with `--json`).
This is the escape hatch for complex queries. Only SELECT is allowed.

---

## Task 4: Tests

Write tests in `internal/store/store_test.go` covering:

1. **Schema creation**: Open a new DB, verify tables and indexes exist
2. **Work item CRUD**: Create, get, list, update, close
3. **Labels**: Add, remove, filter by label
4. **ID generation**: Verify format `gt-[0-9a-f]{8}`
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
3. `GT_HOME=/tmp/gt-test bin/gt store create --db=testrig --title="Test item"` prints an ID
4. `GT_HOME=/tmp/gt-test bin/gt store get <id> --db=testrig` prints the item
5. `GT_HOME=/tmp/gt-test bin/gt store list --db=testrig` shows the item
6. `GT_HOME=/tmp/gt-test sqlite3 /tmp/gt-test/.store/testrig.db "SELECT * FROM work_items"` shows the row

Clean up `/tmp/gt-test` after verification.

---

## Dependencies

Use `modernc.org/sqlite` for pure-Go SQLite (no CGo). Use
`github.com/spf13/cobra` for CLI. No other external dependencies.

## Guidelines

- Keep it simple. This is Loop 0 — the smallest working foundation.
- No premature abstractions. If something is used once, inline it.
- Error messages should include context: `"failed to open rig database %q: %w"`.
- All timestamps are RFC3339 in UTC.
- Initialize the git repo with `git init` and make an initial commit after
  the scaffold is complete, before writing the store code. Make a second
  commit after the store is complete and tests pass.

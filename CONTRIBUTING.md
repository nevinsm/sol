# Contributing to sol

Developer guide for working on the sol codebase.

## Prerequisites

- **Go 1.21+**
- **tmux** (required for integration tests)
- **git**
- **make**

## Project Structure

```
sol/
├── main.go                        Entry point
├── Makefile                       build, test, install, clean
├── cmd/                           Cobra command definitions
│   ├── root.go                    Root command, version
│   ├── init.go                    First-time setup (flag/interactive/guided)
│   ├── doctor.go                  Prerequisite checks
│   ├── world.go                   World lifecycle + sync
│   ├── cast.go, prime.go, resolve.go  Dispatch pipeline
│   ├── agent.go                   Agent management
│   ├── writ.go, writ_dep.go       Writs and dependencies
│   ├── session.go                 tmux session management
│   ├── prefect.go                 Top-level orchestrator
│   ├── forge.go                   Merge pipeline + toolbox
│   ├── status.go                  World status
│   ├── sentinel.go                Per-world health monitor
│   ├── envoy.go                   Persistent human-directed agents
│   ├── governor.go                Per-world coordinator
│   ├── brief.go                   Brief injection hooks
│   ├── feed.go, log_event.go      Event feed
│   ├── chronicle.go               Event chronicle
│   ├── mail.go                    Inter-agent messaging
│   ├── workflow.go                Workflow engine
│   ├── caravan.go                 Batch dispatch
│   ├── escalate.go, escalation.go Escalation management
│   ├── handoff.go                 Session continuity
│   ├── ledger.go                  Token tracking
│   ├── quota.go                   Quota management
│   └── consul.go                  Sphere-level patrol
├── internal/
│   ├── account/                   Account and token quota management
│   ├── adapter/                   RuntimeAdapter interface (AI runtime abstraction)
│   ├── brief/                     Brief file management, size enforcement
│   ├── broker/                    Token broker
│   ├── chronicle/                 Event log maintenance
│   ├── config/                    SOL_HOME resolution, world config
│   ├── consul/                    Sphere-level patrol, heartbeat
│   ├── dash/                      Dashboard rendering
│   ├── dispatch/                  Cast/prime/resolve core logic
│   ├── docgen/                    CLI documentation generation
│   ├── doctor/                    Prerequisite check engine
│   ├── envfile/                   Environment file handling
│   ├── envoy/                     Envoy lifecycle, worktree, hooks
│   ├── escalation/                Notifier interface, log/mail/webhook
│   ├── events/                    JSONL event feed + chronicle
│   ├── fileutil/                  File system helpers (atomic writes, etc.)
│   ├── forge/                     Merge queue, quality gates
│   ├── git/                       Git utilities
│   ├── governor/                  Governor lifecycle, hooks, world sync
│   ├── handoff/                   Session continuity, capture/exec
│   ├── heartbeat/                 Component heartbeat tracking
│   ├── inbox/                     Agent inbox and messaging
│   ├── ledger/                    OTel OTLP receiver for token tracking
│   ├── logutil/                   Logging utilities
│   ├── namepool/                  Agent name generation
│   ├── nudge/                     Agent nudging
│   ├── prefect/                   Agent respawn, health checks
│   ├── processutil/               Process utilities
│   ├── protocol/                  CLAUDE.md + tether script generation
│   ├── quota/                     Quota enforcement
│   ├── sentinel/                  Stall detection, AI assessment
│   ├── service/                   Service lifecycle utilities
│   ├── session/                   tmux: start, stop, health, capture, inject
│   ├── setup/                     Init flow, managed repo cloning
│   ├── startup/                   Agent startup sequencing
│   ├── status/                    World status gathering
│   ├── store/                     SQLite: writs, agents, messages, escalations
│   ├── style/                     Terminal styling (lipgloss helpers)
│   ├── tether/                    Tether file read/write/clear
│   ├── trace/                     Writ trace rendering
│   ├── workflow/                  Directory-based state machine, workflows
│   ├── worldexport/               World export operations
│   └── worldsync/                 World sync operations
├── test/integration/              End-to-end tests
└── docs/
    ├── manifesto.md               Design philosophy
    ├── failure-modes.md           Crash recovery and degradation
    ├── naming.md                  Naming glossary
    ├── cli.md                     Full CLI reference
    └── decisions/                 Architecture Decision Records
```

## Development

```bash
make build       # Build binary to bin/sol
make test        # Run all unit tests
make install     # Install to /usr/local/bin
make clean       # Remove build artifacts
```

## Running Tests

### Unit tests

```bash
make test
# or equivalently:
go test ./...
```

### Integration tests

```bash
go test ./test/integration/ -v -count=1
```

**Requirements:**
- `tmux` must be available on PATH
- Tests use `SOL_SESSION_COMMAND="sleep 300"` to avoid spawning real claude processes

**Isolation:** Integration tests create isolated tmux servers via `TMUX_TMPDIR` — they will not affect any running sol sessions or the real tmux server.

All tests that create tmux sessions must use `setupTestEnv()` or `setupTestEnvWithRepo()` from `test/integration/helpers_test.go`. These helpers enforce three critical isolation rules:

1. `TMUX_TMPDIR` — isolates the tmux server socket
2. `TMUX=""` — unsets the inherited tmux variable; without this, tmux commands connect to the real server
3. `SOL_SESSION_COMMAND="sleep 300"` — prevents tests from spawning real `claude` processes

## Conventions

### Commits

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add session manager
fix: handle nil agent in dispatch
refactor: extract store helpers
test: add concurrent WAL access tests
docs: update architecture spec
chore: update dependencies
```

Use scope when helpful: `feat(store): add label filtering`

### Identifiers and Formatting

| Convention | Format | Example |
|---|---|---|
| Writ IDs | `sol-` + 16 hex chars | `sol-a1b2c3d4e5f6a7b8` |
| Session names | `sol-{world}-{agentName}` | `sol-myworld-Toast` |
| Timestamps | RFC 3339 in UTC | `2026-01-15T10:30:00Z` |

### Error Messages

Always include context:

```go
fmt.Errorf("failed to open world database %q: %w", path, err)
```

### SQLite

Every connection must set:

```go
journal_mode=WAL
busy_timeout=5000
foreign_keys=ON
```

### Config Paths

- Per-world config: `$SOL_HOME/{world}/world.toml`
- Global config: `$SOL_HOME/sol.toml`

### Destructive Commands

Commands that delete data or are hard to undo require a `--confirm` flag. Without `--confirm`, the command previews what would happen and exits 1 (dry-run pattern). `--force` is reserved for behavioral escalation (e.g., stop active sessions before deleting), not confirmation bypass. See `sol world delete` as the reference implementation.

## Architecture Decision Records

Significant architectural choices are recorded as ADRs in `docs/decisions/`. See [docs/decisions/README.md](docs/decisions/README.md) for the full index.

**Format:** Lightweight MADR — `Context → Options Considered → Decision → Consequences`

**When to write one:** Significant architectural choices — new components, storage layout changes, cross-cutting patterns. Not routine implementation decisions.

## Adding a New CLI Command

1. Commands live in `cmd/` as [Cobra](https://github.com/spf13/cobra) commands
2. Follow existing patterns — pick a similar command as reference
3. Update `docs/cli.md` — run `sol docs generate` to regenerate
4. Document exit codes in the command's `Long` field:
   - `0` — success
   - `1` — failure, "not found", or "not running"
   - `2` — context-specific (blocked by guard, degraded status)

## Adding a New Component

New components must satisfy these requirements before merging:

- **Status representation** — appears in `sol status` (sphere overview and/or per-world detail)
- **Failure mode** — defined in `docs/failure-modes.md`
- **ADR** — if architecturally significant (new process, new storage, cross-cutting change)

## Design Documents

- [Manifesto](docs/manifesto.md) — Design philosophy and what we learned from the Gastown prototype
- [Failure Modes](docs/failure-modes.md) — Per-component crash recovery and graceful degradation
- [Naming Glossary](docs/naming.md) — Sol naming conventions
- [CLI Reference](docs/cli.md) — Full command reference
- [Architecture Decision Records](docs/decisions/) — Records of significant architectural choices

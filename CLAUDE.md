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

## Commits
Use [Conventional Commits](https://www.conventionalcommits.org/):
- `feat: add session manager` — new feature
- `fix: handle nil agent in dispatch` — bug fix
- `refactor: extract store helpers` — restructure without behavior change
- `test: add concurrent WAL access tests` — tests only
- `docs: update architecture spec` — documentation only
- `chore: update dependencies` — maintenance
- Use scope when helpful: `feat(store): add label filtering`

## Conventions
- Go module: github.com/nevinsm/gt
- All timestamps: RFC3339 in UTC
- Work item IDs: "gt-" + 8 hex chars (e.g., gt-a1b2c3d4)
- Session names: gt-{rig}-{agentName} (e.g., gt-myrig-Toast)
- Error messages include context: "failed to open rig database %q: %w"
- SQLite connections always set: journal_mode=WAL, busy_timeout=5000, foreign_keys=ON

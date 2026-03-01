# sol — Multi-Agent Orchestration System

Production-ready system for coordinating concurrent AI coding agents.

## Architecture
- Read `docs/target-architecture.md` for the full system spec
- Read `docs/manifesto.md` for design philosophy
- Read `docs/naming.md` for the naming glossary
- Read `docs/arc-roadmap.md` for the arc roadmap

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
- **World Config**: `world.toml` per-world, `sol.toml` global — layered TOML configuration
- **World Lifecycle**: `sol world init` required before use — explicit world creation

## Commits
Use [Conventional Commits](https://www.conventionalcommits.org/):
- `feat: add session manager` — new feature
- `fix: handle nil agent in dispatch` — bug fix
- `refactor: extract store helpers` — restructure without behavior change
- `test: add concurrent WAL access tests` — tests only
- `docs: update architecture spec` — documentation only
- `chore: update dependencies` — maintenance
- Use scope when helpful: `feat(store): add label filtering`

## Design Conventions
- New components must have status representation in `sol status` (sphere overview and/or per-world detail)
- New agent roles get their own section in per-world status display
- New sphere-level processes appear in the sphere processes section

## Conventions
- Go module: github.com/nevinsm/sol
- All timestamps: RFC3339 in UTC
- Work item IDs: "sol-" + 8 hex chars (e.g., sol-a1b2c3d4)
- Session names: sol-{world}-{agentName} (e.g., sol-myworld-Toast)
- Error messages include context: "failed to open world database %q: %w"
- SQLite connections always set: journal_mode=WAL, busy_timeout=5000, foreign_keys=ON
- World config path: $SOL_HOME/{world}/world.toml
- Global config path: $SOL_HOME/sol.toml

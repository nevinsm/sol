# sol — Multi-Agent Orchestration System

Production-ready system for coordinating concurrent AI coding agents.

## Architecture
- Read `docs/manifesto.md` for design philosophy
- Read `docs/failure-modes.md` for crash recovery and degradation behavior
- Read `docs/naming.md` for the naming glossary
- Read `docs/decisions/` for ADRs (architectural decision records)

## Build & Test
- Build: `make build` (binary at `bin/sol`)
- Test: `make test`
- Install: `make install`

## Key Concepts
- **SOL_HOME**: Runtime root directory (env var, default ~/sol)
- **Store**: SQLite (WAL mode) — sphere.db for agents, {world}.db for writs
- **Session**: tmux-based process containers for AI agents
- **Tether**: Directory at $SOL_HOME/{world}/{role}s/{agent}/.tether/ — contains one file per bound writ. See ADR-0025.
- **Cast**: Dispatch work to an agent (creates worktree, tethers work, starts session)
- **Prime**: Inject execution context on session start
- **Resolve**: Signal work complete (push branch, clear tether, stop session)
- **World Config**: `world.toml` per-world, `sol.toml` global — layered TOML configuration
- **World Lifecycle**: `sol world init` required before use — explicit world creation
- **Caravan**: Batch of related writs across worlds, with phase-based sequencing
- **Managed Repo**: Clone at $SOL_HOME/{world}/repo/ — source for all worktrees
- **Brief**: Agent-maintained context file (`.brief/memory.md`) persisted across sessions
- **Doctor**: Prerequisite validator — checks tmux, git, claude, SOL_HOME, SQLite WAL
- **Init**: First-time setup — creates SOL_HOME, first world (flag/interactive/guided modes)

## Components (built)
- **Prefect**: Sphere-wide orchestrator — respawns sessions, health checks
- **Forge**: Per-world merge pipeline — deterministic Go process + targeted AI callouts (ADR-0027)
- **Sentinel**: Per-world health monitor — Go process + AI callouts (ADR-0001)
- **Consul**: Sphere-level patrol — stale tethers, stranded caravans (ADR-0007)
- **Chronicle**: Event log maintenance
- **Ledger**: Sphere-scoped OTel OTLP receiver for agent token tracking (ADR-0016)
- **Doctor**: Prerequisite check engine (`internal/doctor/`)
- **Status**: Sphere overview + per-world detail, lipgloss-styled rendering
- **Envoy**: Persistent human-directed agent with brief system (Arc 3, ADR-0009)
- **Brief**: Agent-maintained context files, hook-based injection (Arc 3, ADR-0013)
- **Broker**: Sphere-level health probe for AI provider runtimes (claude, codex) — discovers configured runtimes and tracks availability
- **Dash**: Live TUI dashboard for the sphere (`sol dash`)
- **Inbox**: Unified TUI for autarch escalations and unread mail (`sol inbox`)
- **Account**: Manages registered AI provider credentials (Claude OAuth tokens, API keys)
- **Quota**: Tracks per-account rate limit state and rotates rate-limited agents to available accounts
- **Handoff**: Cycles a session before context exhaustion — preserves committed code and writ binding so a successor session resumes with full git history (ADR-0023)
- **Nudge**: Per-agent message queue drained on session start; injects autarch and inter-agent prompts as system messages
- **Mail**: Asynchronous inter-agent and autarch messaging with priority and notification (`sol mail send/list/read`)
- **Escalation**: Agent-initiated request-for-help surfaced in `sol inbox` for the autarch (`sol escalate`)
- **Skills**: Progressive-disclosure tool documentation discovered by agents at runtime under `.claude/skills/` (ADR-0026)
- **Persona**: Three-tier persona template resolution (`internal/persona/{defaults,resolve}.go`) — embedded → user → project — selected at envoy creation via `--persona` (see `docs/personas.md`)

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
- Architectural decisions get an ADR in `docs/decisions/`
- ADR format: lightweight MADR — Context → Options Considered (when warranted) → Decision → Consequences
- CLI changes (new commands, changed flags, removed subcommands) must be reflected in `docs/cli.md`
- Exit code conventions:
  - Exit 0: success
  - Exit 1: failure, "not found", or "not running" (general non-success)
  - Exit 2: context-specific (blocked by guard, degraded status) — document in Long field
  - Commands used for scripting (status checks, health probes) MUST document exit codes in their Long field
- **Worktree excludes**: Sol-managed local files (`.claude/settings.local.json`, `.claude/system-prompt.md`, `CLAUDE.local.md`, `.forge-result.json`, `.forge-injection.md`, `.guidelines.md`, `AGENTS.override.md`) and sol-specific directories (`.claude/skills/`, `.brief/`, `.workflow/`, `.agents/skills/`, `.codex/`) are excluded from git via `.git/info/exclude` in the managed repo (`setup.InstallExcludes`). The shared `.claude/` contents (`settings.json`, `CLAUDE.md`, `agents/`, `rules/`) are NOT excluded — they belong to the project's version control. Agent persona files are written to `CLAUDE.local.md` at the worktree root (the local variant) so the project's shared instructions are preserved and Claude Code's upward directory walk discovers the file. If you add a new sol-managed path that gets written inside worktrees, add it to the exclude list in `internal/setup/setup.go`.
- **Destructive command confirmation**: Commands that delete data or are hard to undo require a `--confirm` flag. Without `--confirm`, the command previews what would happen and exits 1 (dry-run pattern). `--force` is reserved for behavioral escalation (e.g., stop active sessions before deleting, close despite unmerged items), not for confirmation bypass. See `sol world delete` as the reference implementation.

## Testing
- Tests that create tmux sessions MUST use `setupTestEnv()` or `setupTestEnvWithRepo()` from `test/integration/helpers_test.go`
- These helpers enforce three critical isolation rules:
  1. **`TMUX_TMPDIR`** — isolates the tmux server socket so test sessions don't touch the real server
  2. **`TMUX=""`** — unsets the inherited tmux variable; without this, tmux commands connect to the real server and test cleanup kills all live `sol-*` sessions
  3. **`SOL_SESSION_COMMAND="sleep 300"`** — prevents tests from spawning real `claude` processes (resource exhaustion)
- Never hardcode `"claude --dangerously-skip-permissions"` — use `config.SessionCommand()` which respects `SOL_SESSION_COMMAND`
- The one exception is `TestWorldDeleteRefusesWithActiveSessions` which intentionally uses the real tmux server for its test, creates a single session by exact name, and cleans up only that session

## Conventions
- Go module: github.com/nevinsm/sol
- All timestamps: RFC3339 in UTC
- Writ IDs: "sol-" + 16 hex chars (e.g., sol-a1b2c3d4e5f6a7b8)
- Session names: sol-{world}-{agentName} (e.g., sol-myworld-Toast)
- Error messages include context: "failed to open world database %q: %w"
- SQLite connections always set: journal_mode=WAL, busy_timeout=5000, foreign_keys=ON
- World config path: $SOL_HOME/{world}/world.toml
- Global config path: $SOL_HOME/sol.toml
- Dependencies: charmbracelet/lipgloss (terminal styling), charmbracelet/huh (interactive prompts)

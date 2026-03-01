# Prompt 01: Arc 2 — ADR-0012 + Charmbracelet Dependencies

You are extending the `sol` orchestration system with operator onboarding
features. This prompt establishes the architectural decision for adopting
Charmbracelet libraries and adds the Go dependencies.

**Working directory:** `~/gt-src/`
**Prerequisite:** Arc 1 is complete. Loops 0–5 are complete.

Read the existing ADRs first to understand format:
- `docs/decisions/0011-senate-sphere-scoped-planner.md` — latest ADR
- `docs/decisions/0007-consul-as-go-process.md` — component decision format
- `docs/decisions/0005-forge-claude-session.md` — pattern ADR format
- `go.mod` — current dependencies

---

## Task 1: ADR-0012 — Charmbracelet Adoption

**Create** `docs/decisions/0012-charmbracelet-adoption.md`.

Follow the lightweight MADR format used by ADRs 0001–0011.

```markdown
# ADR-0012: Charmbracelet Libraries for Terminal UI

Status: accepted
Date: 2026-03-01
Arc: 2

## Context

Arc 2 introduces two operator-facing features that need polished terminal
output: `sol doctor` (prerequisite validation with pass/fail indicators)
and `sol status` (sphere overview with styled tables and colored status
indicators). The existing status output uses raw `fmt.Printf` and
`text/tabwriter` — functional but visually flat.

Additionally, `sol init` needs interactive prompts (world name, source
repo path, model tier selection) for operators who run it without flags.
Today the codebase has no interactive input capability.

Three approaches were evaluated for terminal rendering:

1. **Keep fmt/tabwriter** — zero dependencies, but no color, no
   structure. Adequate for machine output, poor for operator experience.
   Interactive prompts would require manual stdin handling.

2. **ANSI escape codes directly** — color support without dependencies,
   but fragile across terminals, no Windows support, and interactive
   input is a massive effort to build correctly.

3. **Charmbracelet ecosystem** — lipgloss for styled rendering, huh for
   interactive forms. Widely adopted, well-maintained, MIT-licensed.
   Adds dependencies but provides a complete terminal UI toolkit.

For interactive prompts specifically:

- **survey/promptui** — older libraries, less maintained, more limited
  styling integration.
- **charmbracelet/huh** — modern, composable, integrates naturally with
  lipgloss styles. Same ecosystem means consistent look and feel.

## Decision

Adopt two Charmbracelet libraries:

- **charmbracelet/lipgloss** — terminal styling for `sol status` and
  `sol doctor` output. Section headers, colored status indicators,
  styled tables.
- **charmbracelet/huh** — interactive form prompts for `sol init`
  interactive mode (when run without flags and stdin is a TTY).

Design constraints:

- **`--json` bypasses all styling** — machine-readable output is never
  styled. JSON output uses `encoding/json` directly, same as today.
- **Graceful degradation** — if the terminal doesn't support color
  (redirected to file, CI environment), lipgloss automatically falls
  back to unstyled output. No special handling needed.
- **Scoped adoption** — lipgloss and huh are used in rendering and
  input layers only. Core logic, store operations, and session
  management remain dependency-free. The styling boundary is
  `internal/status/render.go` and `cmd/init.go`.
- **Future TUI** — this decision opens the door for a future
  interactive TUI (e.g., live `sol status` dashboard using
  charmbracelet/bubbletea). That is explicitly not in Arc 2 scope
  but the dependency foundation is in place.

## Consequences

**Benefits:**
- Polished operator experience for doctor, init, and status output
- Interactive prompts without manual stdin handling
- Consistent styling across all operator-facing commands
- Terminal capability detection handled by the library (no manual ANSI)
- Foundation for future interactive TUI if desired

**Tradeoffs:**
- Adds transitive dependencies (lipgloss pulls in several Charmbracelet
  sub-packages). Acceptable: all are pure Go, MIT-licensed, widely used.
- Binary size increases slightly. Acceptable for an operator-facing CLI.
- Styling code must be kept separate from data gathering to maintain
  testability and `--json` bypass.
```

---

## Task 2: Add Dependencies

Run `go get` to add both libraries:

```bash
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/huh@latest
```

Then run `go mod tidy` to clean up the module graph.

---

## Task 3: Verify

1. `go mod tidy` — no errors
2. `make build` — compiles cleanly
3. `make test` — all existing tests pass
4. Verify `go.mod` contains both new direct dependencies:
   ```bash
   grep charmbracelet go.mod
   ```
   Should show `github.com/charmbracelet/lipgloss` and
   `github.com/charmbracelet/huh` as direct (not indirect) dependencies.

---

## Guidelines

- The ADR follows the lightweight MADR format: Context → Decision →
  Consequences.
- Do NOT write any Go code in this prompt — only the ADR document and
  dependency management.
- The `go get` commands will update both `go.mod` and `go.sum`. Both
  files should be committed.
- Commit after verification passes with message:
  `docs: ADR-0012 Charmbracelet adoption + add lipgloss and huh dependencies`

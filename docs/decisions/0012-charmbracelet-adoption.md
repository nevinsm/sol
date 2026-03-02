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

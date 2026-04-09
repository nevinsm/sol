# ADR-0038: Envoy Memory via Claude Code Auto-Memory

Status: accepted
Date: 2026-04-09
Supersedes: [ADR-0013](0013-brief-system.md)

## Context

ADR-0013 introduced the **brief system** to give persistent agents (envoys,
governors, chancellors) durable context across sessions. Each agent maintained
a self-authored `.brief/memory.md` file inside its worktree, and a Claude Code
`SessionStart` hook injected the brief contents into every new session as a
system message. The agent was expected to update its own brief during a
debrief phase before exiting (`sol envoy debrief`).

Two things have changed since ADR-0013 landed:

1. **Claude Code gained native auto-memory.** The runtime now persists a
   `MEMORY.md` file at a configured path and surfaces it to the model
   automatically across sessions and compactions — no hook required, no
   self-authored discipline required, no size enforcement required.

2. **The runtime adapter abstraction (ADR-0031) made it pluggable.** With
   `RuntimeAdapter` separating runtime primitives from sol's orchestration
   model, sol can configure the runtime's auto-memory location per agent
   instead of layering its own injection mechanism on top.

The brief system also accumulated friction in practice:

- Debrief was an extra phase agents often forgot or skipped under context
  pressure.
- Briefs lived inside the worktree and were lost on worktree rebuild unless
  explicitly preserved.
- Size enforcement, hook injection, and the debrief command added meaningful
  surface area (`internal/brief`, `cmd/brief.go`, the SessionStart hook,
  `AutoMemoryBlockCommand`) for behavior the runtime now provides directly.
- Governors and chancellors — the other persistent agents that justified the
  brief system — were both removed (ADR-0035, ADR-0037). Only envoys remain.

## Decision

Retire the brief system. Envoys persist accumulated context via **Claude Code
auto-memory** at:

```
<envoyDir>/memory/MEMORY.md
```

where `<envoyDir>` is the envoy's persistent directory under
`$SOL_HOME/{world}/envoys/{name}/`. The memory file lives **outside the
worktree** so it survives worktree rebuilds automatically.

The Claude adapter (ADR-0031) writes the absolute memory path into each
envoy's `settings.local.json`, telling Claude Code where to read and persist
auto-memory. The runtime owns the file format, the loading semantics, and the
lifecycle; sol owns only the path.

The `sol migrate` framework ships a one-shot **`envoy-memory`** migration that
moves any existing `.brief/memory.md` content into the new auto-memory
location for envoys created before this change. The migration only references
`.brief/` because that is the legacy state it consumes.

The following are removed:

- `internal/brief/` package (brief file management, size enforcement)
- `cmd/brief.go` (brief injection hook entrypoint)
- `sol envoy brief` and `sol envoy debrief` subcommands
- The `SessionStart` brief inject hook
- `AutoMemoryBlockCommand` and the brief block in generated `CLAUDE.md`

## Consequences

**Benefits:**
- Memory survives worktree rebuilds without operator intervention — the file
  lives outside the worktree.
- No pre-stop debrief phase required; the runtime persists context as the
  agent works.
- Removes a meaningful slice of bespoke surface area (brief package, hook,
  CLI subcommands) in favor of a runtime feature.
- Aligns persistence with the runtime adapter contract instead of layering a
  parallel mechanism on top.

**Tradeoffs:**
- Memory quality now depends on the runtime's auto-memory heuristics rather
  than the agent's explicit debrief discipline. Early observation suggests the
  runtime captures decisions and context faithfully, but there are cases where
  an explicit save before stop produces better continuity. The
  "Restore pre-stop memory save prompt" writ reintroduces a lightweight save
  prompt for that quality reason — not because the brief system is needed.
- Memory format is now opaque to sol; tooling that inspected `.brief/memory.md`
  directly must read `MEMORY.md` instead and accept whatever shape the runtime
  uses.
- Non-Claude runtimes will need their own persistence story. The adapter
  contract makes this a per-runtime concern rather than a sol-level one.

**Breaking change:**

This is a breaking change tagged **0.2.0**. Existing envoys must run the
`envoy-memory` migration to carry over their brief contents; afterwards, the
brief files and the brief CLI surface no longer exist.

**Migration path:**

1. Upgrade to 0.2.0.
2. Run `sol migrate envoy-memory` to copy existing `.brief/memory.md` content
   into `<envoyDir>/memory/MEMORY.md` for each envoy.
3. The next envoy session loads memory via Claude Code auto-memory directly.

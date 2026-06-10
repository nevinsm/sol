# ADR-0041: Thin Runtime Contract

Status: Proposed
Date: 2026-06-10

## Context

ADR-0040 cleared the credential management layer — removing account/quota/budget machinery and simplifying sol's relationship with operator-managed credentials. That simplification creates an opportunity to revisit the runtime layer itself.

The current `RuntimeAdapter` interface (`internal/adapter/`) defines 14 methods across two implementations:

- Claude adapter (`internal/adapter/claude/`): ~449 lines
- Codex adapter (`internal/adapter/codex/`): ~797 lines

Sol's value lives above the runtime layer: tether durability, writ lifecycle,
caravan phase sequencing, workflow execution, forge merge pipeline, the
three-tier supervision triad, persona resolution, and envoy persistent memory.
The runtime layer should be as thin as the contract allows.

Emdash's evidence demonstrates that the abstraction shape can be much lighter:
a 676-line metadata table supports 30 providers via small bridge functions —
without a heavyweight interface per runtime. The insight is that most
per-runtime variation is *data* (command names, env vars, file paths,
supported hooks), not behavior. Only a small residual of genuine behavioral
variation remains.

### Current Over-Specification

The 14-method `RuntimeAdapter` interface conflates data-shaped variation
(which env var names a runtime uses, where its credential file lives) with
genuine behavioral variation (how it assembles a command line, how it installs
hooks). The data-shaped variation can be captured in a descriptor struct,
shared helpers can consume the descriptor, and only the genuinely behavioral
methods need to remain as interface methods.

## Decision

Restructure the runtime layer into three pieces: a data descriptor, a small
behavioral interface, and shared helpers that consume the descriptor.

### 1. `RuntimeDescriptor` Struct

Pure data per runtime — no methods:

```go
type RuntimeDescriptor struct {
    Name            string
    PersonaFile     string
    SkillsDir       string
    ConfigDirEnv    string
    CredentialFile  string
    GlobalCredsPath string
    DefaultModel    string
    CalloutCommand  string
    SupportedHooks  []string
    StaticEnv       map[string]string
}
```

### 2. `Runtime` Interface — 3 Behavioral Methods

```go
type Runtime interface {
    BuildCommand(ctx CommandContext) string
    InstallHooks(worktreeDir string, hooks HookSet) error
    ExtractTelemetry(eventName string, attrs map[string]string) *TelemetryRecord
}
```

These three methods represent genuine behavioral variation that cannot be
reduced to data:

- **`BuildCommand`**: Command-line assembly has real complexity — resume-vs-fresh,
  session-id-on-resume-only, dedupe-singleton-args. Keeping it as a method is
  honest about this.
- **`InstallHooks`**: Hook installation format varies per runtime (e.g., Claude's
  `settings.local.json` vs Codex's equivalent).
- **`ExtractTelemetry`**: Runtime-specific event parsing for the ledger telemetry
  contract (ADR-0033).

### 3. Sol-Side Shared Helpers in `internal/runtime/`

Helpers that take a `RuntimeDescriptor` (accessed via embedding) plus args,
and perform the work once for all runtimes:

```
WritePersona(desc RuntimeDescriptor, worktreeDir, content string) error
InstallSkills(desc RuntimeDescriptor, worktreeDir string, skills []Skill) error
InjectSystemPrompt(desc RuntimeDescriptor, worktreeDir, content string, replace bool) error
EnsureConfigDir(desc RuntimeDescriptor, agentDir string) (ConfigResult, error)
CleanupConfigDir(desc RuntimeDescriptor, agentDir string) error
BuildTelemetryEnv(desc RuntimeDescriptor, endpoint string) map[string]string
CredentialEnv(desc RuntimeDescriptor, agentDir string) map[string]string
MemoryDir(desc RuntimeDescriptor, agentDir string) string
```

Each runtime implementation is a struct that embeds `RuntimeDescriptor` and
implements the 3 behavioral methods.

### Package Layout

`internal/runtime/` replaces `internal/adapter/`. Runtime implementations
live at `internal/runtime/claude/` and `internal/runtime/codex/` — no
additional subdirectory nesting beyond the runtime name.

### Migration Strategy

Big-bang migration sequenced as a caravan:

1. **Foundation writ**: Build `internal/runtime/` package — define
   `RuntimeDescriptor`, `Runtime` interface, and all shared helpers. No
   callers migrated yet.
2. **Parallel port writs**: Port Claude adapter to `internal/runtime/claude/`
   and Codex adapter to `internal/runtime/codex/`. Each writ is independently
   completable against the foundation.
3. **Caller migration + deletion writ**: Migrate all callers from
   `internal/adapter/` to `internal/runtime/`, then delete `internal/adapter/`.

The big-bang approach (rather than incremental coexistence) is chosen because
a half-migrated state — two packages with overlapping responsibilities,
callers split between them — creates fragile seams and continuous churn during
the transition period.

## Consequences

### Positive

- 14-method interface collapses to 3-method interface + 10-field descriptor +
  8 shared helpers
- Per-runtime implementations drop from ~500–800 lines to ~150–200 lines
  (descriptor declaration + 3 method implementations)
- Shared helpers eliminate boilerplate duplication between adapters; a bug fix
  or improvement applies once for all runtimes
- Adding a new runtime becomes: declare a descriptor (~10 lines), implement 3
  behavioral methods (~130 lines)
- The descriptor documents per-runtime variation explicitly — it is
  machine-readable configuration, not tribal knowledge spread across a large
  adapter implementation

### Negative / Trade-offs

- The descriptor pattern assumes runtimes are CLI binaries with config-dir env
  vars. A runtime with a fundamentally different model (REST API, IPC socket)
  would require the descriptor to be extended. For the runtimes sol targets
  today, this fits well.
- `BuildCommand` is a real behavioral method, not reducible to data — emdash's
  evidence confirms that command-line assembly has genuine complexity
  (resume-vs-fresh, session-id-on-resume-only, dedupe-singleton-args).

### Supersession

- `internal/adapter/` package and the `RuntimeAdapter` interface are removed
  entirely upon completion of the migration caravan.
- ADR-0031 (Runtime Adapter Interface) is superseded by this ADR.

## Alternatives Considered

### Keep the 14-Method Interface

Rejected — over-specified for a 2-runtime system. The interface is hostile to
adding a third runtime: every method must be implemented, even those where a
new runtime's variation is purely data-shaped. The consequence is that adapter
implementations grow to hundreds of lines implementing methods that are just
data lookups.

### Function-Typed Fields on a Single Struct

Replace the interface with a struct containing function fields
(`BuildCommand func(...)`, etc.). Rejected — less Go-idiomatic (the zero value
is broken, interface satisfaction cannot be checked at compile time), harder to
test (mocking a function-field struct is awkward), and does not cleanly
separate data from behavior.

### Incremental Migration with Both Packages Coexisting

Port runtimes one at a time, with `internal/adapter/` and `internal/runtime/`
coexisting until the migration is complete. Rejected — the half-migrated state
is fragile and creates churn. Callers are split between two packages, duplicate
abstractions exist for the same concepts, and the migration never feels "done"
until the final deletion. Big-bang sequenced as a caravan avoids this.

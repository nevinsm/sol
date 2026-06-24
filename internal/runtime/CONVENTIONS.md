# Runtime Package Conventions (CC-9)

This file documents the conventions for implementing and extending `internal/runtime/`.

## Design Shape

Each runtime implementation follows the **descriptor + 5 interface methods + shared helpers** pattern:

```
internal/runtime/
    runtime.go          -- RuntimeDescriptor struct, Runtime interface (6 methods)
    types.go            -- shared value types (HookSet, Skill, TelemetryRecord, etc.)
    helpers.go          -- package-level helpers (WritePersonaFile, InstallSkills, EnsureConfigDir, ...)
    attrutil/           -- attribute parsing helpers shared by runtime implementations
    loader/             -- Get(name) and All() factory functions (avoids import cycles)
    claude/             -- ClaudeRuntime implements Runtime
    codex/              -- CodexRuntime implements Runtime
    CONVENTIONS.md      -- this file
```

## The Runtime Interface

`Runtime` has exactly six methods:

```go
type Runtime interface {
    Descriptor() RuntimeDescriptor   // pure data; no side effects
    BuildCommand(ctx CommandContext) string
    WritePersona(ctx SpawnContext, content []byte) error
    InstallHooks(ctx SpawnContext, hooks HookSet) error
    Seed(ctx SpawnContext) error
    ExtractTelemetry(eventName string, attrs map[string]string) *TelemetryRecord
}
```

Operations that are structurally identical across all runtimes live in `helpers.go` as
package-level functions, not interface methods. Adding a new helper to the interface
requires changing all implementations symmetrically — use package-level functions instead.

## SOL_SESSION_COMMAND Override

`BuildCommand` implementations **MUST** return `os.Getenv("SOL_SESSION_COMMAND")` verbatim
when that env var is non-empty. This override is the test isolation mechanism; skipping it
causes test flakiness or session exhaustion in CI.

```go
func (r *MyRuntime) BuildCommand(ctx CommandContext) string {
    if override := os.Getenv("SOL_SESSION_COMMAND"); override != "" {
        return override
    }
    // ... normal implementation
}
```

## Symmetric Implementation Across Runtimes

When adding a feature to one runtime, audit all other runtimes:

- If the feature is structurally identical: move it to `helpers.go` as a package-level function.
- If the feature is runtime-specific: add it only to the relevant implementation.
- Never add a method to the `Runtime` interface without implementing it in **all** runtimes.

## Atomic Writes for Persisted State

Any file written into a worktree or config dir **MUST** use `fileutil.AtomicWrite`.
This prevents half-written files from confusing the agent runtime after a crash.

## EnsureConfigDir Lifecycle

`EnsureConfigDir(d, worldDir, role, agent)` creates `<worldDir>/.<d.Name>-config/<role>/<agent>/`
and, if the descriptor has `CredentialFile` and `GlobalCredsPath`, creates a credential symlink.

- Idempotent: safe to call repeatedly.
- **Only outpost config dirs are ephemeral** — callers MUST NOT invoke `CleanupConfigDir`
  for envoy or forge agents (their config is durable across sessions).
- The corresponding `CleanupConfigDir` is called by `dispatch.cleanupOutpostConfigDir`
  during resolve (before `mgr.Stop`) to prevent credential leaks.

## Import Cycle Prevention

The `loader` package (`internal/runtime/loader/`) exists specifically to avoid an import
cycle: `internal/runtime` ← `claude`/`codex` means `Get`/`All` factory functions
cannot live in `internal/runtime/` itself.

Use `loader.Get(name)` and `loader.All()` at call sites that need runtime selection.
Do **not** add `Get`/`All` to `internal/runtime/runtime.go`.

## Adding a New Runtime

1. Create `internal/runtime/<name>/` with a struct implementing `runtime.Runtime`.
2. Register it in `internal/runtime/loader/loader.go`.
3. Add the new runtime to all tests that iterate `loader.All()`.
4. Verify `make build && make test` pass.

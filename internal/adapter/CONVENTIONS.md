# internal/adapter Conventions

Conventions for implementing and extending the `RuntimeAdapter` interface.
The goal is that all runtime adapters behave symmetrically so sol can add or
swap runtimes without auditing every call site.

## 1. Persisted state must be written atomically

Any adapter method that writes state to the filesystem (`InjectPersona`,
`InjectSystemPrompt`, `InstallHooks`, `InstallSkills`, `InstallCredential`,
`EnsureConfigDir`) must use atomic writes — write to a `.tmp` file, fsync,
then rename into place. Direct `os.WriteFile` calls on the final path are
not acceptable: a crash mid-write leaves a truncated file that is
indistinguishable from a valid one.

Use `internal/fileutil.AtomicWrite` (which handles the temp-file + fsync +
rename sequence) rather than rolling an ad-hoc replacement:

```go
if err := fileutil.AtomicWrite(destPath, content, 0o644); err != nil {
    return fmt.Errorf("failed to write persona for agent %q: %w", agent, err)
}
```

**See also:** `internal/tether/tether.go` (`Write`) and
`internal/fileutil/fileutil.go` for the reference implementation.

## 2. Symmetric implementation across runtimes

Every method in the `RuntimeAdapter` interface must be implemented by every
concrete adapter. When a runtime does not support a capability, the method
must return a meaningful zero value or a descriptive error — not a silent
no-op that hides misconfiguration.

If the Claude and Codex adapters diverge in a non-obvious way, the divergence
must be documented in a comment on the implementing method explaining *why*
the platforms differ.

Symmetric coverage checklist when adding a new adapter method:

- [ ] `internal/adapter/claude/` implementation added
- [ ] `internal/adapter/codex/` implementation added
- [ ] Both implementations tested (unit or integration)
- [ ] `RuntimeAdapter` interface godoc updated

## 3. `SOL_SESSION_COMMAND` override in `BuildCommand`

`BuildCommand` must honour the `SOL_SESSION_COMMAND` environment variable.
When it is set, return its value verbatim without modification. This allows
test harnesses to substitute a no-op command (`sleep 300`) without starting
a real AI session.

```go
func (a *Adapter) BuildCommand(ctx adapter.CommandContext) string {
    if override := os.Getenv("SOL_SESSION_COMMAND"); override != "" {
        return override
    }
    // ... normal command construction
}
```

**See also:** `internal/config/config.go` (`SessionCommand`), which wraps
this look-up; prefer calling `config.SessionCommand()` over reading the env
var directly so the default is consistent across adapters.

## 4. `CredentialEnv` must fail fast on unknown credential types

Return a descriptive error for unrecognised credential types rather than
returning an empty map. `Launch` calls `CredentialEnv` before creating the
tmux session so an unknown credential type aborts cleanly instead of
starting a session that immediately fails authentication.

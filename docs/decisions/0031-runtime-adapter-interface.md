# ADR-0031: Runtime Adapter Interface

Status: Accepted

## Context

Sol is Claude-native. Every agent session assumes Claude Code: its directory walk discovery, hook system, CLI flags, config directory layout. The session startup layer (`internal/startup`) contains twelve Claude-specific primitives woven directly into `startup.Launch`:

1. **Persona** — written to `CLAUDE.local.md` (Claude's local variant discovery)
2. **System prompt** — injected via `--system-prompt` flag, with replace vs append semantics
3. **Skills** — written to `.claude/skills/` (Claude's skill directory)
4. **Hooks** — written to `.claude/settings.local.json` (Claude's local settings)
5. **Config dir** — `CLAUDE_CONFIG_DIR` env var, per-agent isolation path
6. **Command building** — `claude --dangerously-skip-permissions --model ...` shape
7. **Credentials** — `ANTHROPIC_API_KEY` / OAuth token env vars
8. **Telemetry** — `CLAUDE_CODE_ENABLE_TELEMETRY`, `OTEL_EXPORTER_OTLP_ENDPOINT` etc.
9. **Resume** — `--resume` flag
10. **Model** — `--model` flag
11. **Brief** — `.brief/memory.md` injected via hook
12. **Project instructions** — `CLAUDE.md` / `CLAUDE.local.md` discovery walk

None of these primitives are intrinsic to sol's orchestration model. They are Claude Code implementation details. If sol ever supports a second runtime (Gemini CLI, a future Anthropic agent SDK, a stub for testing), every one of these must be re-implemented from scratch with no compile-time guidance on what is required.

V1 scope: Claude-only. One adapter, one runtime. The extraction forces clean seams and documents the contract. No second adapter is planned.

See `.brief/multi-runtime-adapter.md` for the full design exploration, Paperclip comparison, and six refinement passes that shaped this interface.

### Key Design Observation: Replace vs Append

Outpost and forge agents receive a **replacing** system prompt — their entire operating context is the writ. Envoy, governor, and chancellor receive an **appending** system prompt — they carry persistent state and the injected prompt extends their base persona. This distinction is load-bearing. The `InjectSystemPrompt(replace bool)` parameter captures it explicitly.

### Persona vs System Prompt

These are separate concerns. A persona defines *who* the agent is (name, role, disposition) and is written to a file that the runtime discovers via directory walk. A system prompt defines *what* the agent does right now and may be injected at launch. Conflating them forces awkward workarounds when a runtime uses different mechanisms for each. The interface keeps them separate.

### HookSet as Runtime-Agnostic Hook Description

`HookSet` describes hooks in terms of what they do (session-start, pre-compact, turn-boundary, guard patterns), not in terms of how a specific runtime represents them. The adapter translates a `HookSet` into whatever format the runtime requires (e.g., Claude's `settings.local.json` `hooks` array).

## Decision

Define a `RuntimeAdapter` interface in `internal/adapter/` with nine methods mapping to the startup lifecycle steps. Adapters are compiled in (no plugin system). Adapter selection is via `world.toml` under `agents.models.<role>.runtime`. The default adapter is `"claude"`.

Tmux stays as the universal container. The adapter is responsible for the agent runtime layer (how the process is configured and launched), not the session container.

The V1 implementation defines the interface, implements the full Claude adapter, wires the registry, and migrates `startup.Launch` to use the adapter throughout.

### Interface

```go
package adapter

type RuntimeAdapter interface {
    InjectPersona(worktreeDir string, content []byte) error
    InstallSkills(worktreeDir string, skills []Skill) error
    InjectSystemPrompt(worktreeDir, content string, replace bool) (string, error)
    InstallHooks(worktreeDir string, hooks HookSet) error
    EnsureConfigDir(worldDir, role, agent, worktreeDir string) (ConfigResult, error)
    BuildCommand(ctx CommandContext) string
    CredentialEnv(cred Credential) map[string]string
    TelemetryEnv(port int, agent, world, activeWrit string) map[string]string
    Name() string
}
```

### Supporting Types

```go
// CommandContext holds all arguments needed to build a session launch command.
type CommandContext struct {
    WorktreeDir      string
    Prompt           string
    Continue         bool
    Model            string
    SystemPromptFile string // relative path returned by InjectSystemPrompt (or "" if none)
    ReplacePrompt    bool   // true = --system-prompt-file, false = --append-system-prompt-file
}

// ConfigResult holds the output of EnsureConfigDir.
type ConfigResult struct {
    Dir    string            // absolute path to the runtime config directory
    EnvVar map[string]string // env vars to inject (e.g. {"CLAUDE_CONFIG_DIR": "..."})
}

// Guard is a pre-tool-use guard that blocks a matched tool call.
type Guard struct {
    Pattern string // PreToolUse matcher
    Command string // command to execute; should exit 2 to block
}
```

### Registry

```go
func Register(name string, a RuntimeAdapter)
func Get(name string) (RuntimeAdapter, bool)
func Default() RuntimeAdapter  // panics if claude adapter not registered
```

Adapters register themselves via `init()` in their package. Callers import the adapter package for its side effects. `Default()` panics with a clear message (`"adapter: claude adapter not registered (missing blank import)"`) rather than returning nil silently.

### Claude Adapter

`internal/adapter/claude/` contains a `*Adapter` that satisfies `RuntimeAdapter`. A compile-time check (`var _ adapter.RuntimeAdapter = (*Adapter)(nil)`) ensures the implementation stays current as the interface evolves.

## Consequences

**Positive**:
- Compile-time contract for every Claude-specific primitive. Adding a new primitive to the interface immediately breaks the Claude adapter skeleton, forcing explicit acknowledgment.
- Clean seam for future runtimes. A second adapter needs to implement exactly nine methods to be fully integrated.
- `replace bool` in `InjectSystemPrompt` documents the outpost/forge vs envoy/governor/chancellor distinction explicitly, preventing the Paperclip mistake of treating all system prompts identically.
- Registry pattern enables adapter selection from configuration without import-cycle problems.

**Negative / Trade-offs**:
- Nine methods is a moderate interface size. If primitives are added later (e.g., a `BriefDir` method, a `ResumeState` method), the interface grows and every adapter must implement the new methods.

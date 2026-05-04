# Persona Templates

Persona templates provide reusable behavioral postures for envoy agents. Instead of
writing a `persona.md` from scratch for each envoy, you can use `--persona=<name>` at
creation time to start from a template:

```bash
sol envoy create MyEnvoy --world=myworld --persona=engineer
```

See [`sol envoy create`](cli.md) in the CLI reference for flag details.

## Built-in templates

| Name       | Description                                                                                                    |
|------------|----------------------------------------------------------------------------------------------------------------|
| `planner`  | Design partner — shapes work, defines scope/criteria/sequencing, reviews landed work. Does not implement code. |
| `engineer` | Senior engineer pairing with the operator — hands-on-keyboard, makes implementation judgment calls.            |

The built-in defaults live in `internal/persona/defaults/` and are compiled
into the `sol` binary. The set of recognized names is derived at init time
from the embedded `defaults/*.md` files (see `internal/persona/defaults.go`),
so adding a `defaults/<name>.md` file automatically registers a new template.

## Three-tier resolution

When `--persona=<name>` is specified, the template is resolved via first-match-wins
lookup (see `internal/persona/resolve.go`):

1. **Project:** `{repo}/.sol/personas/{name}.md` — project-specific overrides
2. **User:** `$SOL_HOME/personas/{name}.md` — operator customizations
3. **Embedded:** built-in defaults compiled into the `sol` binary

This means a project-level template shadows a user-level template of the same
name, which in turn shadows the built-in default. The same three-tier pattern is
used for workflows (see ADR-0021).

## Creating custom templates

To create a custom persona template available across all worlds:

```bash
mkdir -p $SOL_HOME/personas
cat > $SOL_HOME/personas/my-persona.md << 'EOF'
# My Persona
Your custom behavioral posture here.
EOF

sol envoy create MyEnvoy --world=myworld --persona=my-persona
```

For project-specific templates, place them in the repo at
`.sol/personas/{name}.md`.

## How it works

When an envoy is created with `--persona=<name>`, the resolved template is
written to the envoy's `persona.md` file. On session start, the existing
startup mechanism reads `persona.md` and injects it into the agent's system
prompt. After creation, you can freely edit the `persona.md` to customize it
for world-specific concerns — the template is just the starting point.

When `--persona` is omitted, no persona file is created (bare envoy — write
your own `persona.md`).

## Distinguishing persona concepts

Sol uses the word "persona" at two tiers; keep them separate:

- **Persona template** (this document) — a reusable template resolved via the
  three-tier lookup above. Lives in `.sol/personas/`, `$SOL_HOME/personas/`,
  or embedded under `internal/persona/defaults/`. Selected at envoy creation.
- **Per-session persona file** — the concrete `persona.md` written into an
  envoy's state directory at creation time and injected on session start.
  See `docs/naming.md`.

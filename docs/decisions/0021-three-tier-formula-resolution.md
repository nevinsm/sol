# ADR-0021: Three-Tier Workflow Resolution

Status: accepted
Date: 2026-03-06

## Context

Workflows currently exist only as embedded defaults compiled into
the sol binary. `Resolve()` in `internal/workflow/defaults.go`
checks `$SOL_HOME/workflows/{name}/` on disk, and if absent, extracts the
workflow from `embed.FS` if it's in the `knownDefaults` map. This means
there are effectively two tiers today — a user-tier cache on disk and
embedded defaults — but the user-tier directory is only populated by
extraction, never by the autarch.

Three gaps result from this design:

1. **No project-specific workflows.** A team that wants a custom review or
   deploy workflow for their repository must modify the sol binary. The
   workflow cannot live alongside the source code it governs.

2. **No autarch customization layer.** The autarch running multiple
   worlds cannot define personal workflow variants (e.g., a stricter
   code-review workflow) without rebuilding the binary. There is no way
   to override embedded defaults without touching source.

3. **No development iteration.** Testing a workflow change requires
   recompiling sol. The autarch cannot prototype workflows by dropping files
   into a directory and running `sol workflow instantiate`.

Sol's configuration system already uses layered resolution — world config
loads defaults → `sol.toml` → `world.toml` (ADR-0008). The managed
repository pattern (ADR-0014) establishes `$SOL_HOME/{world}/repo/` as
the canonical source tree for each world. A workflow resolution hierarchy
that mirrors these patterns is a natural extension.

## Decision

### Three-tier resolution order

Workflow resolution follows project > user > embedded, with first match
winning at the whole-workflow level:

1. **Project tier**: `.sol/workflows/{name}/manifest.toml` in the world's
   managed repository (`$SOL_HOME/{world}/repo/.sol/workflows/{name}/`).
   Project workflows are version-controlled with the source repo — they
   travel with the code they describe.

2. **User tier**: `$SOL_HOME/workflows/{name}/manifest.toml`. Global to
   the sol instance, not scoped per-world. These are autarch
   customizations that apply across all worlds — personal workflow
   variants, organization-standard workflows, or overrides of embedded
   defaults.

3. **Embedded tier**: workflows compiled into the binary via `embed.FS`.
   The current `knownDefaults` map and extraction logic. This is the
   fallback that ensures sol works out of the box with no additional
   setup.

Resolution stops at the first tier that contains a directory with a
`manifest.toml` for the requested workflow name. There is no per-step
merging across tiers — a project workflow named `code-review` completely
replaces the embedded `code-review`, it does not inherit or extend it.

### EnsureFormula becomes Resolve

The current `Resolve(name, repo string)` signature gains a world
parameter and is renamed to reflect its new role:

```go
// Resolve finds a workflow by name using three-tier resolution:
// project (.sol/workflows/ in managed repo) > user ($SOL_HOME/workflows/)
// > embedded (binary defaults).
// Returns the absolute path to the workflow directory and the tier it
// was resolved from.
func Resolve(name, world string) (dir string, tier string, err error)
```

The `tier` return value is one of `"project"`, `"user"`, or `"embedded"`.
This enables `sol workflow instantiate` and `sol cast` to report which
tier a workflow resolved from, giving the autarch visibility into which
workflow is actually in effect.

For the embedded tier, the existing extraction behavior is preserved:
if the workflow is a known default and hasn't been extracted yet, it is
extracted to `$SOL_HOME/workflows/{name}/` before returning the path.
This keeps embedded workflows as a materialized cache in the user tier
directory, matching current behavior.

When `world` is empty (e.g., a workflow operation not tied to a specific
world), the project tier is skipped and resolution starts at the user
tier.

### Project workflow directory convention

Project workflows live at `.sol/workflows/{name}/manifest.toml` in the
source repository root. The `.sol/` directory is the project-level
namespace for sol configuration, parallel to how `.github/` or `.gitlab/`
work for their respective platforms.

```
myproject/
├── .sol/
│   └── workflows/
│       ├── deploy/
│       │   ├── manifest.toml
│       │   └── steps/
│       │       ├── 01-preflight.md
│       │       └── 02-release.md
│       └── review/
│           ├── manifest.toml
│           └── steps/
│               └── 01-review.md
├── src/
└── ...
```

Project workflows are read from the managed repository
(`config.RepoPath(world)`), not from individual agent worktrees. This
ensures all agents in a world see the same workflow definitions,
regardless of which branch their worktree is on.

### Override semantics: whole-workflow replacement

When a workflow name exists at multiple tiers, the highest-priority tier
wins entirely. There is no merging of steps, variables, or metadata
across tiers.

Rationale: per-field merging creates subtle bugs where a project workflow
inherits unexpected defaults from embedded workflows, and makes it
difficult to reason about what a workflow actually does. Whole-workflow
replacement is simple, predictable, and matches how configuration
layering works elsewhere in sol (world.toml completely specifies world
config, it doesn't partially merge with sol.toml field-by-field for the
same key).

A project that wants to customize an embedded workflow copies the embedded
workflow directory into `.sol/workflows/` and modifies it. This is
explicit and inspectable.

### User-tier workflows are global, not per-world

User-tier workflows at `$SOL_HOME/workflows/` apply to all worlds. They
are not scoped per-world.

Per-world customization is handled by the project tier — since each
world's managed repository can contain its own `.sol/workflows/`, worlds
naturally get distinct workflows when their source repos differ. Adding a
per-world user tier (`$SOL_HOME/{world}/workflows/`) would create a
confusing four-tier hierarchy and blur the distinction between "autarch
preference" (user tier) and "project requirement" (project tier).

### Resolution reporting

`sol workflow instantiate` and `sol cast` log which tier the workflow
resolved from:

```
workflow "code-review" resolved from project tier (.sol/workflows/code-review/)
```

`sol workflow status` includes the resolution tier in its output when a
workflow is active, so the autarch can verify which workflow variant is
running.

### Listing available workflows

`sol workflow list` (new subcommand) shows all available workflows across
all three tiers, indicating which tier each would resolve from and
whether any are shadowed:

```
$ sol workflow list --world=myproject
NAME           TIER       SOURCE
deploy         project    .sol/workflows/deploy/
code-review    project    .sol/workflows/code-review/  (shadows: embedded)
default-work   embedded   (built-in)
rule-of-five   embedded   (built-in)
thorough-work  user       $SOL_HOME/workflows/thorough-work/  (shadows: embedded)
```

### Git implications

Project workflows in `.sol/workflows/` are committed to the source
repository and version-controlled like any other project file. They are
not added to `.git/info/exclude` — they belong to the project, not to
sol's operational state.

The `.sol/` directory may also be used for future project-level sol
configuration (analogous to how `.github/` contains workflows, issue
templates, and configuration). This ADR establishes the convention but
only defines `.sol/workflows/` — other `.sol/` contents are future work.

## Consequences

- Projects can define custom workflows that travel with the code.
  Workflow authoring requires no sol binary modification and no special
  tooling — create a directory with a `manifest.toml` and step files.
- The autarch can customize workflows globally by placing them in
  `$SOL_HOME/workflows/`, enabling personal workflow variants without
  per-project configuration.
- Embedded workflows continue to work unchanged. Existing deployments
  see no behavior difference until project or user workflows are created.
- The `EnsureFormula` → `Resolve` rename is a breaking internal
  API change. All call sites (`Instantiate`, `Advance`, `ListSteps`,
  `Materialize`) must pass the world parameter. This is a
  straightforward mechanical update.
- Whole-workflow replacement means the autarch must copy-and-modify rather
  than selectively override individual steps. This trades convenience
  for predictability — the autarch always knows exactly what a workflow
  contains by reading a single directory.
- The `sol workflow list` command gives the autarch a clear view of
  workflow resolution, making shadowing visible rather than surprising.
- Project workflows read from the managed repo (not worktrees) means
  workflow changes require a push to the target branch to take effect
  for new dispatches. This is intentional — workflows are infrastructure,
  not in-flight work.

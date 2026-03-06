# ADR-0021: Three-Tier Workflow Formula Resolution

Status: accepted
Date: 2026-03-06

## Context

Workflow formulas currently exist only as embedded defaults compiled into
the sol binary. `EnsureFormula()` in `internal/workflow/defaults.go`
checks `$SOL_HOME/formulas/{name}/` on disk, and if absent, extracts the
formula from `embed.FS` if it's in the `knownDefaults` map. This means
there are effectively two tiers today — a user-tier cache on disk and
embedded defaults — but the user-tier directory is only populated by
extraction, never by the operator.

Three gaps result from this design:

1. **No project-specific formulas.** A team that wants a custom review or
   deploy workflow for their repository must modify the sol binary. The
   formula cannot live alongside the source code it governs.

2. **No operator customization layer.** An operator running multiple
   worlds cannot define personal formula variants (e.g., a stricter
   code-review formula) without rebuilding the binary. There is no way
   to override embedded defaults without touching source.

3. **No development iteration.** Testing a formula change requires
   recompiling sol. Operators cannot prototype formulas by dropping files
   into a directory and running `sol workflow instantiate`.

Sol's configuration system already uses layered resolution — world config
loads defaults → `sol.toml` → `world.toml` (ADR-0008). The managed
repository pattern (ADR-0014) establishes `$SOL_HOME/{world}/repo/` as
the canonical source tree for each world. A formula resolution hierarchy
that mirrors these patterns is a natural extension.

## Decision

### Three-tier resolution order

Formula resolution follows project > user > embedded, with first match
winning at the whole-formula level:

1. **Project tier**: `.sol/formulas/{name}/manifest.toml` in the world's
   managed repository (`$SOL_HOME/{world}/repo/.sol/formulas/{name}/`).
   Project formulas are version-controlled with the source repo — they
   travel with the code they describe.

2. **User tier**: `$SOL_HOME/formulas/{name}/manifest.toml`. Global to
   the sol instance, not scoped per-world. These are operator
   customizations that apply across all worlds — personal workflow
   variants, organization-standard formulas, or overrides of embedded
   defaults.

3. **Embedded tier**: formulas compiled into the binary via `embed.FS`.
   The current `knownDefaults` map and extraction logic. This is the
   fallback that ensures sol works out of the box with no additional
   setup.

Resolution stops at the first tier that contains a directory with a
`manifest.toml` for the requested formula name. There is no per-step
merging across tiers — a project formula named `code-review` completely
replaces the embedded `code-review`, it does not inherit or extend it.

### EnsureFormula becomes ResolveFormula

The current `EnsureFormula(formulaName string)` signature gains a world
parameter and is renamed to reflect its new role:

```go
// ResolveFormula finds a formula by name using three-tier resolution:
// project (.sol/formulas/ in managed repo) > user ($SOL_HOME/formulas/)
// > embedded (binary defaults).
// Returns the absolute path to the formula directory and the tier it
// was resolved from.
func ResolveFormula(formulaName, world string) (dir string, tier string, err error)
```

The `tier` return value is one of `"project"`, `"user"`, or `"embedded"`.
This enables `sol workflow instantiate` and `sol cast` to report which
tier a formula resolved from, giving operators visibility into which
formula is actually in effect.

For the embedded tier, the existing extraction behavior is preserved:
if the formula is a known default and hasn't been extracted yet, it is
extracted to `$SOL_HOME/formulas/{name}/` before returning the path.
This keeps embedded formulas as a materialized cache in the user tier
directory, matching current behavior.

When `world` is empty (e.g., a formula operation not tied to a specific
world), the project tier is skipped and resolution starts at the user
tier.

### Project formula directory convention

Project formulas live at `.sol/formulas/{name}/manifest.toml` in the
source repository root. The `.sol/` directory is the project-level
namespace for sol configuration, parallel to how `.github/` or `.gitlab/`
work for their respective platforms.

```
myproject/
├── .sol/
│   └── formulas/
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

Project formulas are read from the managed repository
(`config.RepoPath(world)`), not from individual agent worktrees. This
ensures all agents in a world see the same formula definitions,
regardless of which branch their worktree is on.

### Override semantics: whole-formula replacement

When a formula name exists at multiple tiers, the highest-priority tier
wins entirely. There is no merging of steps, variables, or metadata
across tiers.

Rationale: per-field merging creates subtle bugs where a project formula
inherits unexpected defaults from embedded formulas, and makes it
difficult to reason about what a formula actually does. Whole-formula
replacement is simple, predictable, and matches how configuration
layering works elsewhere in sol (world.toml completely specifies world
config, it doesn't partially merge with sol.toml field-by-field for the
same key).

A project that wants to customize an embedded formula copies the embedded
formula directory into `.sol/formulas/` and modifies it. This is
explicit and inspectable.

### User-tier formulas are global, not per-world

User-tier formulas at `$SOL_HOME/formulas/` apply to all worlds. They
are not scoped per-world.

Per-world customization is handled by the project tier — since each
world's managed repository can contain its own `.sol/formulas/`, worlds
naturally get distinct formulas when their source repos differ. Adding a
per-world user tier (`$SOL_HOME/{world}/formulas/`) would create a
confusing four-tier hierarchy and blur the distinction between "operator
preference" (user tier) and "project requirement" (project tier).

### Resolution reporting

`sol workflow instantiate` and `sol cast` log which tier the formula
resolved from:

```
formula "code-review" resolved from project tier (.sol/formulas/code-review/)
```

`sol workflow status` includes the resolution tier in its output when a
workflow is active, so the operator can verify which formula variant is
running.

### Listing available formulas

`sol workflow list` (new subcommand) shows all available formulas across
all three tiers, indicating which tier each would resolve from and
whether any are shadowed:

```
$ sol workflow list --world=myproject
NAME           TIER       SOURCE
deploy         project    .sol/formulas/deploy/
code-review    project    .sol/formulas/code-review/  (shadows: embedded)
default-work   embedded   (built-in)
rule-of-five   embedded   (built-in)
forge-patrol   user       $SOL_HOME/formulas/forge-patrol/  (shadows: embedded)
```

### Git implications

Project formulas in `.sol/formulas/` are committed to the source
repository and version-controlled like any other project file. They are
not added to `.git/info/exclude` — they belong to the project, not to
sol's operational state.

The `.sol/` directory may also be used for future project-level sol
configuration (analogous to how `.github/` contains workflows, issue
templates, and configuration). This ADR establishes the convention but
only defines `.sol/formulas/` — other `.sol/` contents are future work.

## Consequences

- Projects can define custom workflows that travel with the code.
  Formula authoring requires no sol binary modification and no special
  tooling — create a directory with a `manifest.toml` and step files.
- Operators can customize formulas globally by placing them in
  `$SOL_HOME/formulas/`, enabling personal workflow variants without
  per-project configuration.
- Embedded formulas continue to work unchanged. Existing deployments
  see no behavior difference until project or user formulas are created.
- The `EnsureFormula` → `ResolveFormula` rename is a breaking internal
  API change. All call sites (`Instantiate`, `Advance`, `ListSteps`,
  `ManifestFormula`) must pass the world parameter. This is a
  straightforward mechanical update.
- Whole-formula replacement means operators must copy-and-modify rather
  than selectively override individual steps. This trades convenience
  for predictability — the operator always knows exactly what a formula
  contains by reading a single directory.
- The `sol workflow list` command gives operators a clear view of
  formula resolution, making shadowing visible rather than surprising.
- Project formulas read from the managed repo (not worktrees) means
  formula changes require a push to the target branch to take effect
  for new dispatches. This is intentional — formulas are infrastructure,
  not in-flight work.

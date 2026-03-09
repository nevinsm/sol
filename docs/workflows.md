# Workflow Authoring Guide

Workflows are reusable work patterns that define how writs are created,
sequenced, and executed. They encode repeatable processes — code review,
iterative refinement, investigation pipelines — into declarative TOML
manifests that sol can instantiate, dispatch, and track.

## Table of Contents

- [What Workflows Are](#what-workflows-are)
- [The Three Types](#the-three-types)
- [TOML Schema Reference](#toml-schema-reference)
- [Variable Syntax](#variable-syntax)
- [Three-Tier Resolution](#three-tier-resolution)
- [Workflow Lifecycle](#workflow-lifecycle)
- [Embedded Workflow Catalog](#embedded-workflow-catalog)
- [CLI Reference](#cli-reference)

---

## What Workflows Are

A workflow is a directory containing a `manifest.toml` and optional
instruction files. The manifest declares:

- **Steps, templates, or legs** that define the work to be done.
- **Dependencies** between those units (a DAG).
- **Variables** that parameterize the workflow at instantiation time.

### Relationship to writs, caravans, and dispatch

| Concept | Role |
|---------|------|
| **Writ** | The unit of work. Workflows create or operate on writs. |
| **Caravan** | A batch of related writs with phase-based sequencing. Manifested workflows produce caravans. |
| **Cast** | Dispatch a writ to an agent. `sol cast` can instantiate a workflow for the target agent. |
| **Forge** | Merge pipeline. Code writs produced by workflows flow through forge like any other writ. |

### When to use workflows vs. manual writ creation

Use **workflows** when:
- The work follows a repeatable pattern (review, refine, investigate).
- Multiple agents should work on different dimensions of the same problem.
- Steps have dependencies that should be enforced automatically.
- You want fresh agent context per step (manifested workflows).

Use **manual writ creation** when:
- The work is a one-off task with no repeatable structure.
- You need full control over writ descriptions and dependencies.
- The work doesn't decompose into a standard pattern.

---

## The Three Types

### Workflow

Sequential steps with DAG ordering.

**Default mode (inline):** A single agent session receives all steps at
prime time. The agent sees the full checklist and works through it.
`sol workflow advance` checkpoints progress — if the session dies, it
restarts from the last advance.

**Manifested mode (`manifest = true`):** Each step becomes a child writ in
a caravan. Each child is cast to a separate outpost session and goes through
the full cast → resolve → forge pipeline independently. Branch continuity
is maintained — each child inherits the previous step's merged commits.

```toml
name = "my-workflow"
type = "workflow"
manifest = true  # optional; default false (inline)

[[steps]]
id = "design"
title = "Design the approach"
instructions = "steps/01-design.md"

[[steps]]
id = "implement"
title = "Implement the change"
instructions = "steps/02-implement.md"
needs = ["design"]
```

### Expansion

Template-based child writ generation against a target writ. Always
manifested — there is no inline expansion. Each template entry becomes a
child writ whose title and description are derived from the target writ
via variable substitution.

Use case: iterative refinement where each pass gets fresh context. The
rule-of-five pattern (draft → correctness → clarity → edge cases → polish)
is the canonical example.

```toml
name = "my-expansion"
type = "expansion"

[[template]]
id = "{target}.draft"
title = "Draft: {target.title}"
description = "Initial attempt at {target.title}."

[[template]]
id = "{target}.refine"
title = "Refine: {target.title}"
description = "Improve the draft of {target.title}."
needs = ["{target}.draft"]
```

### Convoy

Parallel fan-out with synthesis. Independent legs are dispatched
simultaneously to separate outpost sessions. A synthesis step runs after
all specified legs have merged, combining results into a consolidated
output.

Use case: multi-perspective analysis (code review, plan review, design
exploration) where different dimensions benefit from independent,
focused attention.

```toml
name = "my-convoy"
type = "convoy"

[[legs]]
id = "security"
title = "Security Analysis"
description = "Evaluate security implications."
focus = "Trust boundaries and attack surface."
kind = "analysis"

[[legs]]
id = "performance"
title = "Performance Analysis"
description = "Evaluate performance characteristics."
focus = "Latency, throughput, and resource usage."
kind = "analysis"

[synthesis]
title = "Consolidated Review"
description = "Merge findings from all legs."
kind = "analysis"
depends_on = ["security", "performance"]
```

### Which type should I use?

| Scenario | Type | Why |
|----------|------|-----|
| Linear work: load context → implement → verify | **workflow** (inline) | Single session, full visibility, simple. |
| Multi-step work needing fresh context per step | **workflow** (manifested) | Each step gets a clean agent perspective. |
| Iterative refinement: draft then N revision passes | **expansion** | Each pass builds on the previous, fresh context per pass. |
| Parallel analysis: review from multiple angles | **convoy** | Independent legs run simultaneously, synthesis combines. |
| Quality gates: design → implement → review → test | **workflow** (inline or manifested) | Sequential with dependencies. |
| Fan-out work with a merge step | **convoy** | Legs are independent; synthesis depends on all legs. |

---

## TOML Schema Reference

Every workflow is defined by a `manifest.toml` file in its directory.

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Workflow name. Must match `[a-zA-Z0-9][a-zA-Z0-9_-]*`. |
| `type` | string | yes | One of `"workflow"`, `"expansion"`, or `"convoy"`. |
| `description` | string | no | Human-readable description of the workflow's purpose. |
| `manifest` | bool | no | Default `false`. When `true`, steps become child writs (workflow type only). Expansion and convoy always manifest. |

### Variables: `[variables]` or `[vars]`

Both section names are supported. If both are present, `[vars]` entries
take precedence for keys that appear in both.

```toml
[variables]
issue = { required = true, description = "The writ ID to work on" }
base_branch = { default = "main", description = "Branch to base work from" }
```

| Field | Type | Description |
|-------|------|-------------|
| `required` | bool | If `true`, the variable must be provided at instantiation time (unless it has a default). |
| `default` | string | Default value used when the variable is not provided. |
| `description` | string | Human-readable description of the variable. |

### Steps: `[[steps]]` (workflow type)

Workflow-type manifests use `[[steps]]` to define sequential work units.
Each step references an instruction file containing markdown with variable
substitution.

```toml
[[steps]]
id = "implement"
title = "Implement the change"
instructions = "steps/02-implement.md"
needs = ["design"]
kind = "code"
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Unique identifier within the workflow. |
| `title` | string | yes | Human-readable step name. |
| `instructions` | string | yes | Relative path to the instruction markdown file. |
| `needs` | string[] | no | Step IDs this step depends on. Empty means no dependencies (runs first). |
| `kind` | string | no | `"code"` (default) or `"analysis"`. Propagated to child writs when manifested. |

### Templates: `[[template]]` (expansion type)

Expansion-type manifests use `[[template]]` to define child writ patterns
generated against a target writ.

```toml
[[template]]
id = "{target}.draft"
title = "Draft: {target.title}"
description = "Initial attempt at {target.title}."
needs = ["{target}.previous"]
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Unique identifier. Supports `{target}` substitution (replaced with the target writ ID). |
| `title` | string | yes | Human-readable name. Supports `{target.title}` substitution. |
| `description` | string | no | Detailed description. Supports `{target.title}`, `{target.description}`, `{target.id}` substitution. |
| `needs` | string[] | no | Template IDs this template depends on. Supports `{target}` in references. |

### Legs: `[[legs]]` (convoy type)

Convoy-type manifests use `[[legs]]` to define independent work dimensions
dispatched in parallel.

```toml
[[legs]]
id = "security"
title = "Security Analysis: {target.title}"
description = "Evaluate security implications."
focus = "Trust boundaries and attack surface."
kind = "analysis"
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Unique identifier within the convoy. |
| `title` | string | yes | Human-readable name. Supports `{target.title}` substitution when a target writ is provided. |
| `description` | string | no | Detailed description. Supports target substitutions. `focus` is appended to the description in the generated writ. |
| `focus` | string | no | Guidance for the leg's focus area. Appended to the writ description as "Focus: {value}". |
| `kind` | string | no | `"code"` (default) or `"analysis"`. Determines the resolve path for the leg's writ. |

### Synthesis: `[synthesis]` (convoy type)

A single synthesis section defines the follow-up step that runs after
all specified legs have completed and merged.

```toml
[synthesis]
title = "Consolidated Review: {target.title}"
description = "Read analysis findings from each leg. Produce a consolidated review."
kind = "analysis"
depends_on = ["security", "performance", "correctness"]
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | yes | Human-readable name. Supports target substitutions. |
| `description` | string | no | Detailed description. Supports target substitutions. Automatically enriched with leg writ references and output directory paths. |
| `depends_on` | string[] | yes | Leg IDs that must complete before synthesis runs. |
| `kind` | string | no | `"code"` (default) or `"analysis"`. |

---

## Variable Syntax

Workflows support two variable substitution mechanisms for different
contexts.

### Workflow variables: `{{variable}}`

Used in step instruction files (the `.md` files referenced by
`instructions`). Double-brace syntax is replaced with the resolved
variable value at instantiation time.

**Declaration:**

```toml
[variables]
world = { required = true }
gate_command = { default = "make build && make test" }
```

**Usage in instruction files:**

```markdown
# Run Quality Gates

Run the gate command for the {{world}} world:

    {{gate_command}}
```

**Providing values:**

```bash
sol workflow manifest default-work --world=myworld \
  --var world=myworld
```

### Target variables: `{target.*}`

Used in expansion templates and convoy legs/synthesis. Single-brace
syntax references the target writ's properties. These are substituted
when the workflow is manifested against a target writ.

| Variable | Substituted with |
|----------|-----------------|
| `{target}` | The target writ's ID (in template IDs and needs) |
| `{target.id}` | The target writ's ID |
| `{target.title}` | The target writ's title |
| `{target.description}` | The target writ's description |

**Example (expansion):**

```toml
[[template]]
id = "{target}.draft"
title = "Draft: {target.title}"
description = "Initial attempt at {target.title}. Full context: {target.description}"
```

**Example (convoy):**

```toml
[[legs]]
id = "requirements"
title = "Requirements Analysis: {target.title}"
description = "Review code changes for requirements completeness."

[synthesis]
title = "Consolidate Review: {target.title}"
description = "Produce a consolidated review of {target.title}."
```

### Variable resolution

Variables are resolved in this order:

1. **Provided values** — from `--var key=val` CLI flags.
2. **Defaults** — from `default = "value"` in the manifest.
3. **Required check** — error if a required variable has no value and no default.

Both `[variables]` and `[vars]` sections are supported. If both declare
the same key, the `[vars]` entry takes precedence.

---

## Three-Tier Resolution

Workflows are resolved using a three-tier lookup. The first match wins
at the whole-workflow level — there is no per-step merging across tiers.

### Tier 1: Project

**Path:** `{repo}/.sol/workflows/{name}/manifest.toml`

Project workflows live in the managed repository and are version-controlled
with the source code. They travel with the project they describe.

```
myproject/
├── .sol/
│   └── workflows/
│       └── deploy/
│           ├── manifest.toml
│           └── steps/
│               ├── 01-preflight.md
│               └── 02-release.md
├── src/
└── ...
```

Project workflows are read from the managed repository
(`$SOL_HOME/{world}/repo/`), not from individual agent worktrees. All
agents in a world see the same workflow definitions.

### Tier 2: User

**Path:** `$SOL_HOME/workflows/{name}/manifest.toml`

User workflows are global to the sol instance — not scoped per-world.
These are operator customizations: personal workflow variants,
organization-standard workflows, or overrides of embedded defaults.

### Tier 3: Embedded

Workflows compiled into the sol binary via `embed.FS`. These are the
built-in defaults that ensure sol works out of the box with no additional
setup.

On first use, embedded workflows are extracted to the user tier directory
(`$SOL_HOME/workflows/{name}/`). This materialized cache makes them
editable after extraction.

### Resolution order

```
Project (.sol/workflows/{name}/)  →  first match wins
User ($SOL_HOME/workflows/{name}/)  →  first match wins
Embedded (compiled into binary)  →  fallback
```

### Shadowing

When a workflow name exists at multiple tiers, the highest-priority tier
completely replaces lower tiers. A project workflow named `code-review`
completely replaces the embedded `code-review` — it does not inherit or
extend it.

To see all tiers including shadowed workflows:

```bash
sol workflow list --all
```

Output shows which workflows are active and which are shadowed:

```
NAME           TYPE        TIER       DESCRIPTION
code-review    convoy      project    Custom project review
code-review    convoy      embedded   Multi-perspective code review (shadowed)
default-work   workflow    embedded   Standard outpost work execution
```

---

## Workflow Lifecycle

### Creating a new workflow

```bash
# Create a workflow-type workflow in the user tier
sol workflow init my-workflow

# Create a convoy-type workflow in the user tier
sol workflow init my-review --type=convoy

# Create an expansion-type workflow in the project tier
sol workflow init my-expansion --type=expansion --project --world=myworld
```

This scaffolds a directory with a `manifest.toml` and (for workflow type)
a `steps/` directory with a placeholder step file. Edit `manifest.toml`
to define your workflow.

### Customizing an embedded workflow

```bash
# Eject to user tier for customization
sol workflow eject code-review

# Eject to project tier
sol workflow eject code-review --project --world=myworld

# Force re-eject (backs up existing to {name}.bak-{timestamp})
sol workflow eject code-review --force
```

Eject copies an embedded workflow so you can modify it. The ejected copy
takes precedence over the embedded version due to tier resolution.

### Previewing a workflow

```bash
# Show a workflow resolved by name
sol workflow show default-work

# Show a workflow from a specific directory
sol workflow show --path ./my-workflow/

# Show with JSON output
sol workflow show default-work --json
```

Output includes name, type, tier, path, variables, steps/templates/legs,
and validation status.

### Listing available workflows

```bash
# List active workflows (highest-priority tier only)
sol workflow list

# List all workflows including shadowed ones
sol workflow list --all

# List with JSON output
sol workflow list --json

# List with project-tier scanning for a specific world
sol workflow list --world=myworld
```

### Manifesting a workflow

Manifesting materializes a workflow into child writs grouped in a caravan.
This is required for expansion and convoy types, and optional (with
`manifest = true`) for workflow types.

```bash
# Manifest a workflow
sol workflow manifest thorough-work --world=myworld \
  --var issue=sol-a1b2c3d4

# Manifest an expansion against a target writ
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4

# Manifest a convoy against a target writ
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4

# Manifest with JSON output
sol workflow manifest thorough-work --world=myworld \
  --var issue=sol-a1b2c3d4 --json
```

### Using workflows with cast

When casting a writ to an agent, you can specify a workflow to instantiate:

```bash
sol cast sol-a1b2c3d4 --workflow=default-work --var base_branch=develop
```

The workflow is instantiated for the agent when the writ is cast. The
agent's session receives step instructions at prime time.

### Inline execution commands

Agents executing inline workflows use these commands to progress through
steps:

```bash
# Print the current step's instructions
sol workflow current

# Mark current step complete, advance to next
sol workflow advance

# Skip the current step (treated as completed for DAG purposes)
sol workflow skip

# Mark current step and workflow as failed (stops execution)
sol workflow fail

# Show workflow progress
sol workflow status

# Show workflow progress as JSON
sol workflow status --json
```

These commands use `SOL_WORLD` and `SOL_AGENT` environment variables
(set automatically in agent sessions) or accept `--world` and `--agent`
flags.

---

## Embedded Workflow Catalog

Sol ships with nine embedded workflows covering common work patterns.

### 1. default-work

**Type:** workflow (inline, 3 steps)
**Purpose:** Standard outpost work execution. Load context, implement,
verify. The default pattern for straightforward coding tasks.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `issue` | yes | — | The writ ID to work on |
| `base_branch` | no | `"main"` | Branch to base work from |

**Steps:**

1. `load-context` — Load work context
2. `implement` — Implement the change (needs: load-context)
3. `verify` — Verify the implementation (needs: implement)

**Example:**

```bash
sol cast sol-a1b2c3d4 --workflow=default-work
```

---

### 2. thorough-work

**Type:** workflow (inline, 5 steps)
**Purpose:** Quality-focused work execution with design and review gates.
Use when work quality matters more than speed — the extra design and
review steps catch issues before they reach forge.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `issue` | yes | — | The writ ID to work on |
| `base_branch` | no | `"main"` | Branch to base work from |

**Steps:**

1. `design` — Design the approach
2. `implement` — Implement the change (needs: design)
3. `review` — Review the implementation (needs: implement)
4. `test` — Test thoroughly (needs: review)
5. `submit` — Submit the work (needs: test)

**Example:**

```bash
sol cast sol-a1b2c3d4 --workflow=thorough-work
```

---

### 3. deep-scan

**Type:** workflow (inline, 5 steps)
**Purpose:** Investigation pipeline to root cause. Takes a bug or issue
through orientation, code survey, root cause isolation, documentation, and
fix planning. Produces a fix caravan rather than fixing the issue directly.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `issue` | yes | — | The writ ID to investigate |
| `base_branch` | no | `"main"` | Branch to base investigation from |

**Steps:**

1. `orient` — Orient on the symptom
2. `survey` — Survey the code area (needs: orient)
3. `isolate` — Isolate root cause (needs: survey)
4. `document` — Document findings and design fixes (needs: isolate)
5. `chart` — Chart the fix caravan (needs: document)

**Example:**

```bash
sol cast sol-a1b2c3d4 --workflow=deep-scan
```

---

### 4. idea-to-plan

**Type:** workflow (inline, 6 steps)
**Purpose:** Planning pipeline from concept to writs. Takes a vague idea
through requirements review, design exploration, and plan review to
produce concrete writs ready for dispatch.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `idea` | yes | — | The idea or concept to plan |
| `world` | yes | — | The world to create writs in |

**Steps:**

1. `understand-intent` — Understand the intent
2. `review-requirements` — Review requirements (needs: understand-intent)
3. `explore-design` — Explore design options (needs: review-requirements)
4. `review-plan` — Review the plan (needs: explore-design)
5. `create-writs` — Create writs (needs: review-plan)
6. `summarize` — Summarize (needs: create-writs)

**Example:**

```bash
sol cast sol-a1b2c3d4 --workflow=idea-to-plan \
  --var idea="Add real-time notifications" \
  --var world=myworld
```

---

### 5. rule-of-five

**Type:** expansion (5 templates)
**Purpose:** Five-pass iterative refinement. Each pass gets a fresh agent
session: draft for breadth, then four focused revision passes for
correctness, clarity, edge cases, and polish. Always manifested — each
template becomes a child writ.

**Variables:** None declared. Uses target writ substitution.

**Templates:**

1. `{target}.draft` — Draft: breadth over depth, get a working solution
2. `{target}.refine-correctness` — Fix errors, bugs, and logical mistakes (needs: draft)
3. `{target}.refine-clarity` — Improve readability, naming, and structure (needs: refine-correctness)
4. `{target}.refine-edge-cases` — Handle boundary conditions and error paths (needs: refine-clarity)
5. `{target}.refine-polish` — Final pass: tests, documentation, commit hygiene (needs: refine-edge-cases)

**Example:**

```bash
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 6. code-review

**Type:** convoy (2 legs + synthesis)
**Purpose:** Multi-perspective code review with parallel analysis and
synthesis. Two independent review dimensions run simultaneously, then a
synthesis step consolidates findings.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `target` | yes | — | Writ ID of the code to review |

**Legs:**

1. `requirements` (analysis) — Review code changes for requirements completeness. Focus: success criteria, edge cases, scope.
2. `feasibility` (analysis) — Evaluate technical feasibility and architectural fit. Focus: patterns, architectural concerns, maintainability.

**Synthesis:** `Consolidate Review` (analysis) — Read analysis findings from each leg. Produce a consolidated review with prioritized action items, risks, and recommendations. Depends on: requirements, feasibility.

**Example:**

```bash
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 7. plan-review

**Type:** convoy (5 legs + synthesis)
**Purpose:** Parallel plan analysis with five independent review dimensions
and a go/no-go synthesis. Useful for reviewing plans, PRDs, or proposals
from multiple angles simultaneously.

**Variables:** None declared. Uses target writ substitution.

**Legs:**

1. `completeness` (analysis) — Assess coverage of goals, deliverables, acceptance criteria, resources, timeline.
2. `sequencing` (analysis) — Evaluate step ordering, dependency relationships, parallelism opportunities.
3. `risk` (analysis) — Identify technical, operational, and integration risks.
4. `scope-creep` (analysis) — Compare stated objectives against proposed work, identify gold-plating.
5. `testability` (analysis) — Assess whether each deliverable can be verified.

**Synthesis:** `Go/No-Go Recommendation` (analysis) — Consolidate findings into a single go/no-go recommendation with concerns ranked by severity. Depends on: completeness, sequencing, risk, scope-creep, testability.

**Example:**

```bash
sol workflow manifest plan-review --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 8. guided-design

**Type:** convoy (6 legs + synthesis)
**Purpose:** Parallel design exploration across six dimensions with
synthesis into a coherent design recommendation. Useful for ADR authoring
and architecture writs.

**Variables:** None declared. Uses target writ substitution.

**Legs:**

1. `api-design` (analysis) — Explore API surface: endpoints, methods, request/response shapes, versioning, error conventions.
2. `data-model` (analysis) — Explore data model: entities, relationships, storage, migration, query patterns.
3. `ux-interaction` (analysis) — Explore user-facing interaction: CLI commands, flags, output, developer ergonomics.
4. `scalability` (analysis) — Explore scalability: concurrency, resource usage, growth limits, caching, performance.
5. `security` (analysis) — Explore security: authentication, authorization, input validation, secrets, attack surface.
6. `integration` (analysis) — Explore integration points: dependencies, extension hooks, backward compatibility, migration.

**Synthesis:** `Design Synthesis` (analysis) — Merge findings from all six legs into a coherent design recommendation. Identify tensions, propose trade-offs, produce a draft ADR or design document. Depends on: api-design, data-model, ux-interaction, scalability, security, integration.

**Example:**

```bash
sol workflow manifest guided-design --world=myworld \
  --target=sol-a1b2c3d4
```

---

## CLI Reference

All workflow commands are under `sol workflow`.

### `sol workflow init`

Scaffold a new workflow directory with a manifest template.

```
sol workflow init <name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--type` | string | `workflow` | Workflow type: `workflow`, `expansion`, or `convoy` |
| `--project` | bool | `false` | Create in project tier (`.sol/workflows/`). Requires `--world`. |
| `--world` | string | — | World name. Required with `--project`. |

**Examples:**

```bash
# Scaffold a workflow in the user tier
sol workflow init deploy-pipeline

# Scaffold a convoy in the project tier
sol workflow init team-review --type=convoy --project --world=myworld
```

---

### `sol workflow eject`

Copy an embedded workflow to the user or project tier for customization.

```
sol workflow eject <name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | bool | `false` | Eject to project tier. Requires `--world`. |
| `--world` | string | — | World name. Required with `--project`. |
| `--force` | bool | `false` | Overwrite existing. Backs up to `{name}.bak-{timestamp}`. |

**Examples:**

```bash
# Eject code-review to user tier
sol workflow eject code-review

# Eject to project tier
sol workflow eject code-review --project --world=myworld

# Re-eject, backing up existing customization
sol workflow eject code-review --force
```

---

### `sol workflow show`

Display workflow details and resolution source.

```
sol workflow show [workflow] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--path` | string | — | Load workflow from a directory path instead of by name. Mutually exclusive with the positional argument. |
| `--world` | string | — | World name for project-tier resolution. |
| `--json` | bool | `false` | Output as JSON. |

**Examples:**

```bash
# Show a workflow by name
sol workflow show default-work

# Show a workflow from a local directory
sol workflow show --path ./my-custom-workflow/

# Show with JSON output for scripting
sol workflow show code-review --json
```

---

### `sol workflow list`

List available workflows across all resolution tiers.

```
sol workflow list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | `false` | Show all tiers including shadowed workflows. |
| `--world` | string | — | World name for project-tier discovery. |
| `--json` | bool | `false` | Output as JSON. |

Output columns: NAME, TYPE, TIER, DESCRIPTION.

**Examples:**

```bash
# List active workflows
sol workflow list

# List all including shadowed
sol workflow list --all

# List with project-tier scanning
sol workflow list --world=myworld
```

---

### `sol workflow manifest`

Materialize a workflow into child writs and a caravan.

```
sol workflow manifest <workflow> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. Required. |
| `--var` | string[] | — | Variable assignment (`key=val`). Repeatable. |
| `--target` | string | — | Existing writ ID to manifest against. Required for expansion workflows. |
| `--json` | bool | `false` | Output as JSON. |

**Examples:**

```bash
# Manifest a workflow with variables
sol workflow manifest thorough-work --world=myworld \
  --var issue=sol-a1b2c3d4

# Manifest an expansion against a target writ
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4

# Manifest a convoy against a target writ
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4
```

---

### `sol workflow instantiate`

Instantiate a workflow for an agent (internal, used by cast).

```
sol workflow instantiate <workflow> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. |
| `--agent` | string | — | Agent name. Defaults to `SOL_AGENT` env. |
| `--item` | string | — | Writ ID. Required. |
| `--var` | string[] | — | Variable assignment (`key=val`). Repeatable. |

---

### `sol workflow current`

Print the current step's rendered instructions.

```
sol workflow current [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. Defaults to `SOL_WORLD` env. |
| `--agent` | string | — | Agent name. Defaults to `SOL_AGENT` env. |

Exits with code 1 if no active workflow step.

---

### `sol workflow advance`

Mark the current step as complete and advance to the next ready step.

```
sol workflow advance [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. Defaults to `SOL_WORLD` env. |
| `--agent` | string | — | Agent name. Defaults to `SOL_AGENT` env. |

Prints "Workflow complete." when all steps are done.

---

### `sol workflow skip`

Skip the current step and advance to the next. Skipped steps are treated
as completed for DAG purposes — they don't block dependent steps.

```
sol workflow skip [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. Defaults to `SOL_WORLD` env. |
| `--agent` | string | — | Agent name. Defaults to `SOL_AGENT` env. |

---

### `sol workflow fail`

Mark the current step and the entire workflow as failed. Execution stops —
no further steps are advanced.

```
sol workflow fail [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. Defaults to `SOL_WORLD` env. |
| `--agent` | string | — | Agent name. Defaults to `SOL_AGENT` env. |

---

### `sol workflow status`

Show workflow execution progress.

```
sol workflow status [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--world` | string | — | World name. Defaults to `SOL_WORLD` env. |
| `--agent` | string | — | Agent name. Defaults to `SOL_AGENT` env. |
| `--json` | bool | `false` | Output as JSON. |

**Human-readable output:**

```
Workflow: default-work (sol-a1b2c3d4)
Status: running
Progress: 1/3 steps complete

Steps:
  [x] load-context — Load work context
  [>] implement — Implement the change (current)
  [ ] verify — Verify the implementation
```

**Step status markers:**

| Marker | Status |
|--------|--------|
| `[x]` | Complete |
| `[>]` | Executing (current) |
| `[s]` | Skipped |
| `[!]` | Failed |
| `[ ]` | Pending |

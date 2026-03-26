# Workflow Authoring Guide

Workflows are reusable work patterns that define how writs are created,
sequenced, and executed. They encode repeatable processes — code review,
iterative refinement, investigation pipelines — into declarative TOML
manifests that sol can instantiate, dispatch, and track.

## Table of Contents

- [What Workflows Are](#what-workflows-are)
- [Two Modes: Inline and Manifest](#two-modes-inline-and-manifest)
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

- **Steps** that define the work to be done.
- **Dependencies** between those steps (a DAG).
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
- You want fresh agent context per step (manifest mode).

Use **manual writ creation** when:
- The work is a one-off task with no repeatable structure.
- You need full control over writ descriptions and dependencies.
- The work doesn't decompose into a standard pattern.

---

## Two Modes: Inline and Manifest

Every workflow operates in one of two modes, controlled by the `mode`
field in `manifest.toml`.

### Inline mode (default)

A single agent session receives all steps at prime time. The agent sees
the full checklist and works through it. `sol workflow advance`
checkpoints progress — if the session dies, it restarts from the last
advance.

```toml
name = "my-workflow"

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

**When to use inline mode:**
- Linear work: load context, implement, verify.
- Quality gates: design, implement, review, test.
- Single session with full visibility — simple and fast.

### Manifest mode

Each step becomes a child writ in a caravan. Each child is cast to a
separate outpost session and goes through the full cast, resolve, forge
pipeline independently. Branch continuity is maintained — each child
inherits the previous step's merged commits.

Steps without dependencies on each other run in parallel (separate
phases). Steps with `needs` wait for their dependencies to complete
before being dispatched.

```toml
name = "my-review"
mode = "manifest"

[[steps]]
id = "requirements"
title = "Requirements Analysis"
kind = "analysis"
description = "Review code changes for requirements completeness."

[[steps]]
id = "feasibility"
title = "Feasibility Assessment"
kind = "analysis"
description = "Evaluate technical feasibility and architectural fit."

[[steps]]
id = "synthesis"
title = "Consolidated Review"
kind = "analysis"
description = "Consolidate findings from all analysis dimensions."
needs = ["requirements", "feasibility"]
```

**When to use manifest mode:**
- Multi-step work needing fresh context per step.
- Parallel analysis: review from multiple angles simultaneously.
- Iterative refinement: draft then N revision passes.
- Fan-out work with a merge/synthesis step.

### When to use inline vs manifest mode

| Scenario | Mode | Why |
|----------|------|-----|
| Linear work: load context, implement, verify | **inline** | Single session, full visibility, simple. |
| Multi-step work needing fresh context per step | **manifest** | Each step gets a clean agent perspective. |
| Iterative refinement: draft then N revision passes | **manifest** | Each pass builds on the previous, fresh context per pass. |
| Parallel analysis: review from multiple angles | **manifest** | Independent steps run simultaneously, synthesis combines. |
| Quality gates: design, implement, review, test | **inline** or **manifest** | Sequential with dependencies. |
| Fan-out work with a merge step | **manifest** | Independent steps run in parallel; final step depends on all. |

---

## TOML Schema Reference

Every workflow is defined by a `manifest.toml` file in its directory.

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Workflow name. Must match `[a-zA-Z0-9][a-zA-Z0-9_-]*`. |
| `type` | string | no | Defaults to `"workflow"`. |
| `mode` | string | no | `"inline"` (default) or `"manifest"`. Controls execution mode. |
| `description` | string | no | Human-readable description of the workflow's purpose. |

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

### Steps: `[[steps]]`

All workflows use `[[steps]]` to define work units. In inline mode,
steps are presented as a checklist. In manifest mode, each step becomes
a child writ.

```toml
[[steps]]
id = "implement"
title = "Implement the change"
description = "Make the code changes described in the writ."
instructions = "steps/02-implement.md"
needs = ["design"]
kind = "code"
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | yes | Unique identifier within the workflow. |
| `title` | string | yes | Human-readable step name. |
| `description` | string | no | Inline content for the step. When both `description` and `instructions` are set, `instructions` wins (file content replaces description). |
| `instructions` | string | no | Relative path to an instruction markdown file. Variable substitution is applied to file contents. |
| `needs` | string[] | no | Step IDs this step depends on. Empty means no dependencies (runs first). In manifest mode, steps without mutual dependencies run in parallel. |
| `kind` | string | no | `"code"` (default) or `"analysis"`. Propagated to child writs when manifested. Determines the resolve path — code writs flow through forge, analysis writs close directly. |

---

## Variable Syntax

Workflows use `{{variable}}` double-brace syntax for all variable
substitution. Variables are replaced with their resolved values at
instantiation time.

### Declaring variables

```toml
[variables]
world = { required = true }
gate_command = { default = "make build && make test" }
```

### Using variables in instruction files

```markdown
# Run Quality Gates

Run the gate command for the {{world}} world:

    {{gate_command}}
```

### Providing values

```bash
sol workflow manifest default-work --world=myworld \
  --var world=myworld
```

### Target variables

When `--target=<writ-id>` is provided to `sol workflow manifest`, the
target writ is loaded and the following variables are auto-populated:

| Variable | Substituted with |
|----------|-----------------|
| `{{target.id}}` | The target writ's ID |
| `{{target.title}}` | The target writ's title |
| `{{target.description}}` | The target writ's description |

These participate in the standard `{{variable}}` substitution — no
separate mechanism. Use them in step titles, descriptions, and
instruction files:

```toml
[[steps]]
id = "draft"
title = "Draft: {{target.title}}"
description = "Initial attempt at {{target.title}}. Full context: {{target.description}}"
```

### Variable resolution

Variables are resolved in this order:

1. **Target auto-population** — from `--target` writ properties (`{{target.title}}`, etc.).
2. **Provided values** — from `--var key=val` CLI flags.
3. **Defaults** — from `default = "value"` in the manifest.
4. **Required check** — error if a required variable has no value and no default.

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
These are autarch customizations: personal workflow variants,
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
code-review    workflow    project    Custom project review
code-review    workflow    embedded   Multi-perspective code review (shadowed)
default-work   workflow    embedded   Standard outpost work execution
```

---

## Workflow Lifecycle

### Creating a new workflow

```bash
# Create a workflow in the user tier
sol workflow init my-workflow

# Create a workflow in the project tier
sol workflow init my-review --project --world=myworld
```

This scaffolds a directory with a `manifest.toml` and a `steps/`
directory with a placeholder step file. Edit `manifest.toml` to define
your workflow.

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

Output includes name, type, tier, path, variables, steps, and
validation status.

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

Manifesting materializes a workflow into child writs grouped in a
caravan. This applies to workflows with `mode = "manifest"`.

```bash
# Manifest a workflow with variables
sol workflow manifest thorough-work --world=myworld \
  --var issue=sol-a1b2c3d4

# Manifest a workflow against a target writ
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4

# Manifest a review workflow against a target writ
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

Sol ships with eight embedded workflows covering common work patterns.

### 1. default-work

**Mode:** inline (3 steps)
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

**Mode:** inline (5 steps)
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

**Mode:** inline (5 steps)
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

**Mode:** inline (6 steps)
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

**Mode:** manifest (5 steps)
**Purpose:** Five-pass iterative refinement. Each pass gets a fresh agent
session: draft for breadth, then four focused revision passes for
correctness, clarity, edge cases, and polish. Each step becomes a child
writ with branch continuity from the previous step.

**Variables:** None declared. Uses `--target` for `{{target.title}}` substitution.

**Steps:**

1. `draft` — Draft: breadth over depth, get a working solution
2. `refine-correctness` — Fix errors, bugs, and logical mistakes (needs: draft)
3. `refine-clarity` — Improve readability, naming, and structure (needs: refine-correctness)
4. `refine-edge-cases` — Handle boundary conditions and error paths (needs: refine-clarity)
5. `refine-polish` — Final pass: tests, documentation, commit hygiene (needs: refine-edge-cases)

**Example:**

```bash
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 6. code-review

**Mode:** manifest (3 steps)
**Purpose:** Multi-perspective code review with parallel analysis and
synthesis. Two independent review dimensions run simultaneously, then a
synthesis step consolidates findings.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `target` | yes | — | Writ ID of the code to review |

**Steps:**

1. `requirements` (analysis) — Requirements Analysis: review code changes for requirements completeness. Focus: success criteria, edge cases, scope.
2. `feasibility` (analysis) — Feasibility Assessment: evaluate technical feasibility and architectural fit. Focus: patterns, architectural concerns, maintainability.
3. `synthesis` (analysis) — Consolidate Review: read analysis findings from each step's output directory. Produce a consolidated review with prioritized action items, risks, and recommendations. (needs: requirements, feasibility)

**Example:**

```bash
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 7. plan-review

**Mode:** manifest (6 steps)
**Purpose:** Parallel plan analysis with five independent review dimensions
and a go/no-go synthesis. Useful for reviewing plans, PRDs, or proposals
from multiple angles simultaneously.

**Variables:** None declared. Uses `--target` for target substitution.

**Steps:**

1. `completeness` (analysis) — Assess coverage of goals, deliverables, acceptance criteria, resources, timeline.
2. `sequencing` (analysis) — Evaluate step ordering, dependency relationships, parallelism opportunities.
3. `risk` (analysis) — Identify technical, operational, and integration risks.
4. `scope-creep` (analysis) — Compare stated objectives against proposed work, identify gold-plating.
5. `testability` (analysis) — Assess whether each deliverable can be verified.
6. `synthesis` (analysis) — Go/No-Go Recommendation: consolidate findings into a single go/no-go recommendation with concerns ranked by severity. (needs: completeness, sequencing, risk, scope-creep, testability)

**Example:**

```bash
sol workflow manifest plan-review --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 8. guided-design

**Mode:** manifest (7 steps)
**Purpose:** Parallel design exploration across six dimensions with
synthesis into a coherent design recommendation. Useful for ADR authoring
and architecture writs.

**Variables:** None declared. Uses `--target` for target substitution.

**Steps:**

1. `api-design` (analysis) — Explore API surface: endpoints, methods, request/response shapes, versioning, error conventions.
2. `data-model` (analysis) — Explore data model: entities, relationships, storage, migration, query patterns.
3. `ux-interaction` (analysis) — Explore user-facing interaction: CLI commands, flags, output, developer ergonomics.
4. `scalability` (analysis) — Explore scalability: concurrency, resource usage, growth limits, caching, performance.
5. `security` (analysis) — Explore security: authentication, authorization, input validation, secrets, attack surface.
6. `integration` (analysis) — Explore integration points: dependencies, extension hooks, backward compatibility, migration.
7. `synthesis` (analysis) — Design Synthesis: merge findings from all six dimensions into a coherent design recommendation. Identify tensions, propose trade-offs, produce a draft ADR or design document. (needs: api-design, data-model, ux-interaction, scalability, security, integration)

**Example:**

```bash
sol workflow manifest guided-design --world=myworld \
  --target=sol-a1b2c3d4
```

---

## Project-Tier Example Workflows

The following workflows ship with the sol source repository itself
(`.sol/workflows/`) as project-tier examples. They are **not** compiled
into the sol binary — users without the sol source repo will not see them
in `sol workflow list`.

### codebase-scan (project-tier)

**Mode:** manifest (12 steps)
**Purpose:** Comprehensive project-tier codebase review. Parallel analysis
across all code, tests, documentation, and configuration dimensions, then
synthesis into a consolidated findings report. Useful for full project
health checks and generating a prioritized fix caravan.

**Variables:** None declared. No target substitution.

**Steps:**

1. `core-infra` (analysis) — Review core infrastructure: store, config, setup, fileutil, processutil, logutil, envfile, git, namepool.
2. `session-lifecycle` (analysis) — Review session lifecycle: startup, dispatch, session, tether, adapter, handoff.
3. `agent-roles` (analysis) — Review agent roles: envoy, governor, brief, skills.
4. `services` (analysis) — Review service components: forge, sentinel, consul, prefect, service, heartbeat.
5. `protocol` (analysis) — Review the protocol layer.
6. `support-systems` (analysis) — Review support systems: ledger, broker, nudge, chronicle, events, quota, doctor, escalation, inbox, account, trace.
7. `cli` (analysis) — Review CLI commands.
8. `orchestration` (analysis) — Review orchestration and presentation: workflow, worldexport, worldsync, status, dash, style, docgen.
9. `integration-tests` (analysis) — Review integration tests.
10. `documentation` (analysis) — Review documentation.
11. `build-and-agent-env` (analysis) — Review build system and agent environment: Makefile, go.mod, embedded workflows, skill files, prompts, config defaults.
12. `synthesis` (analysis) — Synthesize findings into fix caravan: read all step findings and synthesize into a consolidated review with prioritized action items. (needs: all 11 analysis steps)

**Example:**

```bash
sol workflow manifest codebase-scan --world=myworld
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
| `--type` | string | `workflow` | Workflow type. |
| `--project` | bool | `false` | Create in project tier (`.sol/workflows/`). Requires `--world`. |
| `--world` | string | — | World name. Required with `--project`. |

**Examples:**

```bash
# Scaffold a workflow in the user tier
sol workflow init deploy-pipeline

# Scaffold a workflow in the project tier
sol workflow init team-review --project --world=myworld
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
| `--target` | string | — | Existing writ ID to manifest against. Auto-populates `{{target.title}}`, `{{target.description}}`, `{{target.id}}` variables. |
| `--json` | bool | `false` | Output as JSON. |

**Examples:**

```bash
# Manifest a workflow with variables
sol workflow manifest thorough-work --world=myworld \
  --var issue=sol-a1b2c3d4

# Manifest against a target writ
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4

# Manifest a review workflow against a target writ
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

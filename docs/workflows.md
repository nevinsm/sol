# Workflow Authoring Guide

> **Note:** For outpost execution guidance (how an agent works on a single writ),
> see [Guidelines](#guidelines) below. Workflows are used exclusively for
> **manifesting** — decomposing work into multiple writs via `sol workflow manifest`.

Workflows are reusable work patterns that define how writs are created,
sequenced, and executed. They encode repeatable processes — code review,
iterative refinement, investigation pipelines — into declarative TOML
manifests that sol can instantiate, dispatch, and track.

## Guidelines

Guidelines are markdown documents that provide execution guidance to outpost
agents. They replace the inline step-driven workflow approach with simpler,
stateless documents injected at prime time.

### How guidelines work

1. At **cast time**, a guidelines template is resolved and rendered with variable
   substitution, then written to `.guidelines.md` in the agent's worktree.
2. At **prime time**, the content of `.guidelines.md` is injected into the
   agent's initial context.
3. On **context compaction**, `.guidelines.md` is re-injected to maintain the
   agent's awareness of its execution approach.

### Template selection

Templates are auto-selected by writ kind:
- `code` (or empty) → `default` template
- Any other kind → `analysis` template

Override with `--guidelines=<name>` on `sol cast` or configure per-world mappings
in `world.toml`:

```toml
[guidelines]
code = "default"
analysis = "analysis"
research = "deep-investigation"
```

### Three-tier resolution

Guidelines templates resolve via the same three-tier lookup as workflows:
1. **Project:** `{repo}/.sol/guidelines/{name}.md`
2. **User:** `$SOL_HOME/guidelines/{name}.md`
3. **Embedded:** built-in defaults (extracted to user tier on first use)

### Embedded templates

| Name | Purpose |
|------|---------|
| `default` | Code writs: understand, design, implement, review, verify, resolve |
| `analysis` | Analysis writs: understand, investigate, document, resolve |
| `investigation` | Debugging: orient, survey, isolate, document, chart, resolve |

### Variable substitution

Templates support `{{variable}}` syntax. The `issue` variable is auto-set to
the writ ID. Additional variables can be passed with `--var key=val` on `sol cast`.

## Table of Contents

- [What Workflows Are](#what-workflows-are)
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
- You want fresh agent context per step.

Use **manual writ creation** when:
- The work is a one-off task with no repeatable structure.
- You need full control over writ descriptions and dependencies.
- The work doesn't decompose into a standard pattern.

### How manifesting works

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

**Use cases:**
- Multi-step work needing fresh context per step.
- Parallel analysis: review from multiple angles simultaneously.
- Iterative refinement: draft then N revision passes.
- Fan-out work with a merge/synthesis step.

---

## TOML Schema Reference

Every workflow is defined by a `manifest.toml` file in its directory.

### Top-Level Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Workflow name. Must match `[a-zA-Z0-9][a-zA-Z0-9_-]*`. |
| `type` | string | no | Defaults to `"workflow"`. |
| `mode` | string | no | `"manifest"`. Controls execution mode. |
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

All workflows use `[[steps]]` to define work units. Each step becomes
a child writ when manifested.

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
| `needs` | string[] | no | Step IDs this step depends on. Empty means no dependencies (runs first). Steps without mutual dependencies run in parallel. |
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
sol workflow manifest code-review --world=myworld \
  --var target=sol-a1b2c3d4
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
rule-of-five   workflow    embedded   Five-pass iterative refinement
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

> **Note:** `sol workflow eject` is a hidden/internal command — it does not appear
> in `sol workflow --help` output. It remains callable for advanced use cases.

```bash
# Eject to user tier for customization
sol workflow eject code-review

# Eject to project tier
sol workflow eject code-review --project --world=myworld

# Re-eject, confirming overwrite (backs up existing to {name}.bak-{timestamp})
sol workflow eject code-review --confirm
```

Without `--confirm`, `eject` previews what would be copied and exits without making changes (dry-run pattern).
Eject copies an embedded workflow so you can modify it. The ejected copy
takes precedence over the embedded version due to tier resolution.

### Previewing a workflow

```bash
# Show a workflow resolved by name
sol workflow show code-review

# Show a workflow from a specific directory
sol workflow show --path ./my-workflow/

# Show with JSON output
sol workflow show code-review --json
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
# Manifest a workflow against a target writ
sol workflow manifest rule-of-five --world=myworld \
  --target=sol-a1b2c3d4

# Manifest a review workflow against a target writ
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4

# Manifest with JSON output
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4 --json
```

## Embedded Workflow Catalog

Sol ships with six embedded workflows covering common work patterns.

### 1. rule-of-five

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

<!-- generated from internal/workflow/defaults/code-review/manifest.toml -->
### 2. code-review

**Mode:** manifest (11 steps)
**Purpose:** Comprehensive code review via parallel specialized reviewers
with synthesis. Ten independent analysis legs run simultaneously, then a
synthesis step consolidates findings into a prioritized review.

**Variables:**

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `target` | yes | — | Writ ID of the code to review |

**Steps:**

Phase 0 — review legs (10 parallel, no dependencies):

1. `correctness` (analysis) — Correctness review of the target writ.
2. `performance` (analysis) — Performance review.
3. `security` (analysis) — Security review.
4. `elegance` (analysis) — Elegance / readability review.
5. `resilience` (analysis) — Resilience and error-handling review.
6. `style` (analysis) — Style review.
7. `smells` (analysis) — Code smells review.
8. `wiring` (analysis) — Wiring review (integration between layers).
9. `commit-discipline` (analysis) — Commit discipline review.
10. `test-quality` (analysis) — Test quality review.

Phase 1 — synthesis (1 step):

11. `synthesis` (analysis) — Consolidate review: read findings from each
    leg's output directory and produce a prioritized consolidated review
    (needs: all 10 phase-0 legs).

**Example:**

```bash
sol workflow manifest code-review --world=myworld \
  --target=sol-a1b2c3d4
```

---

### 3. plan-review

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

### 4. guided-design

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

### 5. prd-review

**Mode:** manifest (7 steps)
**Purpose:** Parallel PRD review across six dimensions with synthesis into
prioritized questions. Surfaces missing requirements, ambiguities,
feasibility risks, and scope issues before design or implementation begins.

**Variables:**

| Variable  | Required | Default | Description                                                              |
|-----------|----------|---------|--------------------------------------------------------------------------|
| `problem` | yes      | —       | The idea, feature, or problem statement to review                        |
| `context` | no       | —       | Additional context: existing code, constraints, prior decisions          |

**Steps:**

1. `requirements` (analysis) — Requirements Completeness: assess coverage of goals, deliverables, success criteria.
2. `gaps` (analysis) — Missing Requirements: surface unstated requirements and silent assumptions.
3. `ambiguity` (analysis) — Ambiguity Analysis: flag wording that admits multiple reasonable readings.
4. `feasibility` (analysis) — Technical Feasibility: evaluate technical risk against the existing codebase and constraints.
5. `scope` (analysis) — Scope Analysis: identify gold-plating and out-of-scope creep.
6. `stakeholders` (analysis) — Stakeholder Analysis: identify impacted users, operators, and dependencies.
7. `synthesis` (analysis) — PRD Review Synthesis: consolidate findings into prioritized questions for the author. (needs: requirements, gaps, ambiguity, feasibility, scope, stakeholders)

**Example:**

```bash
sol workflow manifest prd-review --world=myworld \
  --problem="add multi-tenant quota tracking"
```

---

### 6. security-audit

**Mode:** manifest (6 steps)
**Purpose:** Parallel security review across five dimensions with prioritized
findings synthesis. On-demand audit for a codebase area.

**Variables:** None declared. Uses `--target` for target substitution.

**Steps:**

1. `dependency-audit` (analysis) — Dependency Audit: review third-party dependencies for known vulnerabilities and license risk.
2. `secrets-scan` (analysis) — Secrets Scan: search for embedded credentials, tokens, and key material.
3. `owasp-surface` (analysis) — OWASP Top 10 Review: evaluate the change against the OWASP Top 10 attack categories.
4. `auth-authz` (analysis) — Authentication & Authorization Review: verify identity, role checks, and privilege boundaries.
5. `input-validation` (analysis) — Input Validation Review: check parsing, sanitization, and trust boundaries on external inputs.
6. `synthesis` (analysis) — Security Audit Synthesis: consolidate findings with severity-ranked recommendations. (needs: dependency-audit, secrets-scan, owasp-surface, auth-authz, input-validation)

**Example:**

```bash
sol workflow manifest security-audit --world=myworld \
  --target=sol-a1b2c3d4
```

---

## Project-Tier Example Workflows

The following workflows ship with the sol source repository itself
(`.sol/workflows/`) as project-tier examples. They are **not** compiled
into the sol binary — users without the sol source repo will not see them
in `sol workflow list`.

<!-- generated from .sol/workflows/codebase-scan/manifest.toml -->
### codebase-scan (project-tier)

**Mode:** manifest (23 steps)
**Purpose:** Comprehensive project-tier codebase review. Parallel domain
analysis followed by batch verification, adversarial triage, cross-domain
review, and commission into a fix caravan. Useful for full project health
checks and generating a prioritized fix caravan.

**Variables:**

- `prior_caravan` (optional) — Caravan ID from a prior scan run. When
  provided, the commission step cross-references to avoid re-creating writs
  for already-fixed issues.

**Phases and steps:**

The 23 steps in `.sol/workflows/codebase-scan/manifest.toml` are organized
into five phases. Within a phase, steps run in parallel; later phases wait
for the steps they declare in `needs`.

**Phase 0 — domain analysis (15 parallel steps, no dependencies):**

1. `store` — Review store layer (`internal/store/`).
2. `config-and-setup` — Review config, setup, and utilities (`internal/config/`, `setup/`, `fileutil/`, `processutil/`, `logutil/`, `envfile/`, `namepool/`).
3. `session-lifecycle` — Review session lifecycle (`internal/startup/`, `dispatch/`, `session/`, `tether/`, `adapter/`, `handoff/`, `budget/`, `guidelines/`).
4. `agent-roles` — Review agent roles (`internal/envoy/`).
5. `protocol-and-skills` — Review protocol layer and skills (`internal/protocol/`, `persona/`).
6. `forge` — Review forge (`internal/forge/`).
7. `supervision` — Review supervision layer (`internal/sentinel/`, `consul/`, `prefect/`, `service/`, `heartbeat/`).
8. `messaging` — Review messaging systems (`internal/broker/`, `nudge/`, `inbox/`, `escalation/`).
9. `observability` — Review observability systems (`internal/ledger/`, `chronicle/`, `events/`, `trace/`).
10. `operational` — Review operational utilities (`internal/quota/`, `doctor/`, `account/`, `git/`).
11. `cli` — Review CLI commands (`cmd/`).
12. `orchestration` — Review orchestration and presentation (`internal/workflow/`, `worldexport/`, `worldsync/`, `status/`, `dash/`, `style/`, `docgen/`).
13. `integration-tests` — Review integration tests (`test/integration/`).
14. `documentation` — Review documentation (`docs/`).
15. `build-and-agent-env` — Review build system and agent environment (Makefile, `go.mod`, embedded workflows, skill files, prompts, config defaults).

**Phase 1 — batch verification (5 parallel steps):**

16. `batch-verify-1` — Verify findings: data layer, config, and dispatch (needs: `store`, `config-and-setup`, `session-lifecycle`).
17. `batch-verify-2` — Verify findings: agent infrastructure and merge pipeline (needs: `agent-roles`, `protocol-and-skills`, `forge`).
18. `batch-verify-3` — Verify findings: monitoring, messaging, and telemetry (needs: `supervision`, `messaging`, `observability`).
19. `batch-verify-4` — Verify findings: CLI, quota, workflow, and status (needs: `operational`, `cli`, `orchestration`).
20. `batch-verify-5` — Verify findings: tests, docs, and build system (needs: `integration-tests`, `documentation`, `build-and-agent-env`).

**Phase 2 — adversarial triage (1 step):**

21. `adversarial-triage` — Adversarial triage of verified findings (needs: all five `batch-verify-*` steps).

**Phase 3 — cross-domain review (1 step):**

22. `cross-domain` — Cross-domain review (needs: `adversarial-triage`).

**Phase 4 — commission (1 step):**

23. `commission` — Commission fix caravan: synthesize a prioritized writ
    list from the cross-domain review (needs: `cross-domain`). When
    `prior_caravan` is set, cross-references it to avoid re-creating writs
    for already-fixed issues.

**Example:**

```bash
sol workflow manifest codebase-scan --world=myworld
sol workflow manifest codebase-scan --world=myworld --var prior_caravan=car-…
```

---

### security-scan (project-tier)

**Mode:** manifest (7 steps)
**Purpose:** Static security analysis — parallel SAST (`gosec`), dependency
vulnerability scanning (`govulncheck`), and secrets detection, then triage
against a baseline and commission a fix caravan.

**Variables:** None declared. No target substitution.

**Steps:**

1. `gosec-run` (analysis) — Run `gosec` and save raw output for downstream analysis.
2. `gosec-input-handling` (analysis) — SAST: review injection and file-operation risks. (needs: gosec-run)
3. `gosec-code-quality` (analysis) — SAST: review error handling, concurrency, and cryptography. (needs: gosec-run)
4. `secrets-scan` (analysis) — Secrets detection scan across the worktree.
5. `dep-audit` (analysis) — Dependency vulnerability audit (`govulncheck`).
6. `triage` (analysis) — Triage and validate security findings against the baseline. (needs: gosec-input-handling, gosec-code-quality, secrets-scan, dep-audit)
7. `commission` (analysis) — Commission a security fix caravan from triaged findings. (needs: triage)

**Example:**

```bash
sol workflow manifest security-scan --world=myworld
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

> **Hidden command.** This command has `Hidden: true` in the CLI — it does not
> appear in `sol workflow --help`. It is intended for advanced/internal use.

Copy an embedded workflow to the user or project tier for customization.

```
sol workflow eject <name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | bool | `false` | Eject to project tier. Requires `--world`. |
| `--world` | string | — | World name. Required with `--project`. |
| `--confirm` | bool | `false` | Confirm overwrite of existing ejected workflow. Backs up to `{name}.bak-{timestamp}`. Without `--confirm`, the command previews what would be copied and exits without writing (dry-run pattern). |

**Examples:**

```bash
# Eject code-review to user tier
sol workflow eject code-review

# Eject to project tier
sol workflow eject code-review --project --world=myworld

# Re-eject, backing up existing customization
sol workflow eject code-review --confirm
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


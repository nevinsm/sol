# ADR-0015: Workflow Manifest and Workflow Types

Status: accepted
Date: 2026-03-04

## Context

Sol's workflow system supports a single execution model: sequential steps
within a single outpost session. The agent polls `sol workflow current`,
executes, runs `sol workflow advance`, and repeats until done. This works
for simple linear work but falls short in several ways:

1. **No full-picture visibility.** The agent sees one step at a time via
   `sol workflow current`. It cannot make informed decisions about the current
   step without understanding what comes next. Gastown's inline display shows
   all steps at prime time — agents work faster and make better choices.

2. **No multi-session workflows.** Some workflows benefit from fresh context
   per step — iterative refinement (draft → correctness → clarity → edge
   cases → polish) produces better results when each pass gets a clean
   perspective. Currently a single session handles all steps, accumulating
   context and biases.

3. **No parallel fan-out.** Code review, PRD analysis, and other work that
   decomposes into independent dimensions cannot be parallelized across
   multiple outposts. Each dimension would benefit from focused attention
   by a separate agent, with a synthesis step combining results.

4. **Only one workflow type.** The manifest schema supports `type = "workflow"`
   but no other types. The `[[steps]]` structure with `needs` dependencies
   already implements a DAG, but the execution engine treats it as strictly
   sequential (one current step at a time).

## Decision

### Three execution modes

Workflows support three execution modes controlled by workflow type and a
`manifest` flag:

**Inline (default for workflow type)**
All steps displayed at prime time. Agent sees the full checklist and works
through it in a single session. `sol workflow advance` is optional
checkpointing — if the agent dies, it restarts from the last advance. This
is the current model, enhanced with full-workflow visibility.

**Manifested (workflow or expansion with `manifest = true`)**
Each step materializes as a child writ in the store, with dependencies
mirroring the step DAG. Each child is cast to a separate outpost and goes
through the full cast → resolve → forge pipeline independently. A caravan
groups all children for tracking. The branch carries forward — each outpost
inherits the previous step's merged commits.

**Convoy (new workflow type)**
Each leg dispatches to a separate outpost in parallel. A synthesis step
triggers when all legs have merged. Legs work independently on different
dimensions of the same problem.

### Three workflow types

**`type = "workflow"`** — Sequential steps with dependency DAG.
- Default execution: inline (all steps shown, single session).
- With `manifest = true`: each step becomes a child writ.
- Steps use `needs` for ordering. DAG validated at parse time.
- Schema: `[[steps]]` with `id`, `title`, `instructions`, `needs`.

**`type = "expansion"`** — Template-based generation of related writs.
- Always manifested (expansion implies manifest).
- `[[template]]` entries define child writs generated against a
  target writ. Variable substitution via `{target.title}`,
  `{target.description}`, `{target.id}`.
- Templates use `needs` for ordering between generated items.
- Use case: iterative refinement (rule-of-five pattern), multi-pass
  review cycles.
- Schema: `[[template]]` with `id`, `title`, `description`, `needs`.

**`type = "convoy"`** — Parallel fan-out with synthesis.
- `[[legs]]` define independent work dimensions, each cast to a
  separate outpost simultaneously.
- `[synthesis]` defines a follow-up step that runs after all legs merge.
  `depends_on` lists which legs must complete.
- Each leg gets its own writ, branch, and outpost session.
- Synthesis receives all leg outputs and produces a consolidated result.
- Schema: `[[legs]]` with `id`, `title`, `description`, `focus`.
  `[synthesis]` with `title`, `description`, `depends_on`.

### Manifest mechanics

When a workflow is materialized (explicitly or by type):

1. A parent writ is created (or an existing target item is used).
2. Child writs are created for each step/template/leg.
3. Dependencies between children mirror the workflow's DAG (`needs`).
4. Children are grouped in a caravan, with phases derived from
   dependency depth (roots = phase 0, their dependents = phase 1, etc.).
5. Dispatch follows normal caravan rules — phase 0 items cast first,
   subsequent phases gate on previous phase completion.

For sequential manifested workflows, branch continuity is maintained:
each child's outpost branch is based on the merge commit of its
predecessor. The forge merge of step N updates the target branch;
step N+1's outpost branches from that new HEAD.

### CLI surface

- `sol workflow manifest <workflow> --world=W --var key=val` — materialize
  a workflow into writs + caravan.
- Inline display requires no new commands — `primeWithWorkflow()` is
  updated to show all steps.
- Convoy dispatch integrates with `sol cast --workflow=<convoy>`.

### Workflow schema additions

```toml
# Workflow with manifest
type = "workflow"
manifest = true           # new field, default false

# Expansion (manifest implied)
type = "expansion"
[[template]]
id = "..."
title = "..."
description = "..."
needs = ["..."]

# Convoy
type = "convoy"
[[legs]]
id = "..."
title = "..."
description = "..."
focus = "..."

[synthesis]
title = "..."
description = "..."
depends_on = ["..."]
```

## Consequences

- Inline display gives agents full context at prime time, improving
  decision quality and reducing command round-trips.
- Manifested workflows enable multi-session execution where each step
  gets fresh context — better for iterative refinement patterns.
- Convoy workflows enable parallel fan-out for review and analysis work.
- All three modes reuse existing infrastructure: writs, dependencies,
  caravans, cast, forge. No new state management or execution engine needed.
- Expansion workflows always manifest — there is no inline expansion.
- The `manifest` flag on workflow types is opt-in. Existing workflows
  continue to run inline by default.
- Branch continuity for manifested sequential workflows depends on forge
  merging each step before the next is cast. This serializes through the
  forge pipeline, which is acceptable since the steps are inherently
  sequential.
- Convoy synthesis depends on all legs merging. Governor or consul watches
  for caravan phase completion and triggers the synthesis cast.

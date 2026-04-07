# ADR-0032: Workflow Type Unification

Status: Accepted (supersedes relevant parts of ADR-0015)

## Context

Sol has three workflow types — `workflow`, `convoy`, and `expansion` — but convoy and expansion are constrained manifested workflows with extra features bolted on:

- **Convoy**: parallel legs + synthesis step. Functionally equivalent to a workflow with `mode = "manifest"` where independent steps run in parallel and a final step depends on all of them.
- **Expansion**: template-based child writ creation against a target. Functionally equivalent to a manifested workflow with target variable substitution.

Each type had its own:
- Struct fields (`Steps`, `Legs`/`Synth`, `Templates`)
- Validation logic
- Materialization code path
- Variable substitution mechanism (`{target.*}` single-brace for convoy/expansion, `{{variable}}` double-brace for workflows)

This created unnecessary complexity. The `Manifest bool` field on workflows was a separate concept from the convoy/expansion types, yet they all produced the same outcome: child writs in a caravan.

## Decision

Unify into a single workflow type with two modes:

### Mode field replaces Manifest bool

- `mode = "inline"` (default): steps are executed sequentially by one agent (step-driven loop)
- `mode = "manifest"`: each step becomes a child writ dispatched to separate agents

The `Manifest bool` field is removed. `ShouldManifest()` checks `mode == "manifest"` (plus convoy/expansion types during transition).

### Type defaults to "workflow"

When `type` is absent from a manifest, it defaults to `"workflow"`. Convoy and expansion types continue to work during the transition phase but will be removed in a subsequent change.

### Description field on StepDef

Steps can now have a `description` field that provides inline content without requiring an external `.md` file. When both `description` and `instructions` are set, `instructions` wins (file content replaces description). This matches the existing Leg behavior where description is the inline content and instructions loads from a file.

### Unified target variable substitution

The separate `{target.*}` single-brace substitution mechanism is replaced with standard `{{target.*}}` double-brace variables. When `--target=<writ-id>` is provided, the target writ is loaded and `{{target.title}}`, `{{target.description}}`, `{{target.id}}` are auto-populated as resolved variables. These participate in the standard `{{variable}}` substitution — no separate mechanism.

### DAG enrichment for manifested workflows

When a manifested workflow step has `needs`, dependency writ information is injected into its description during writ creation. For each dependency:
- The dependency's writ ID and title are appended
- Analysis-kind dependencies include output directory paths
- Code-kind dependencies include branch information

This generalizes what convoy synthesis enrichment did for leg references.

## Consequences

### Positive

- **Simpler mental model**: one workflow type, two modes. No need to learn convoy vs expansion vs workflow distinctions.
- **One substitution syntax**: `{{variable}}` everywhere. Target fields are just variables.
- **Feature promotion**: DAG enrichment, description field, and target substitution are available to all workflows, not just convoy/expansion.
- **Cleaner code**: removes `renderTemplateField` and separate substitution paths.

### Negative

- **Breaking change**: `manifest = true` is no longer accepted. Existing manifests must use `mode = "manifest"`.
- **Breaking change**: `{target.*}` single-brace syntax is removed. Manifests must use `{{target.*}}`.
- **Transition period**: convoy and expansion types still exist during the transition. Embedded manifests that use `{target.*}` will not have target substitution until converted to `{{target.*}}` in the next phase.

### Migration path

1. This ADR: core engine changes (mode field, target unification, DAG enrichment)
2. Next phase: convert embedded manifests from `{target.*}` to `{{target.*}}`, convert convoy/expansion types to `workflow` with `mode = "manifest"`, remove convoy/expansion code paths

# ADR-0024: Writ Kind — Code vs Analysis Resolve Paths

Status: accepted
Date: 2026-03-08

## Context

The code-review workflow (Gitea #2) exposed a fundamental assumption
in the resolve path: every writ produces code. Manifested workflow steps that
performed analysis — reviewing code, writing reports, assessing quality —
produced findings but no meaningful git diff. When these steps resolved, the
system created merge requests for empty branches. Gitea's squash-merge
rejected them (empty commits), blocking the entire caravan.

The root cause is structural: `sol resolve` always pushes a branch and
creates a merge request. Analysis writs have no code to push. The resolve
path needs to branch based on what kind of work the writ represents.

### Why a column, not metadata

Writ kind determines how the system processes the writ at multiple points:

- **Resolve path**: code writs push branches and create MRs; non-code writs
  close directly.
- **Persona generation**: code writs get build/test quality gates; non-code
  writs get output directory instructions.
- **Forge involvement**: code writs flow through the forge pipeline;
  non-code writs bypass it entirely.
- **Session resilience**: code writs emphasize git commits; non-code writs
  emphasize output directory persistence.

Kind is not an annotation or tag — it controls system behavior. That makes
it a schema-level concern. Metadata is for things the system does not need
to understand (labels, descriptions, autarch notes). Kind is for things it
does.

## Decision

### Kind column on writs table

Add a `kind` column to the writs table:

```sql
kind TEXT NOT NULL DEFAULT 'code'
```

The default ensures backward compatibility — all existing writs are
implicitly code writs. The column is `NOT NULL` because every writ must
have a defined resolve path.

Known values:
- `code` — produces code changes, resolves through forge (branch → MR → merge)
- `analysis` — produces findings/reports, resolves by closing directly

The column is a free-form TEXT field, not an enum. New kinds can be added
without schema migration. The system branches on `kind == "code"` vs
`kind != "code"` — any non-code kind follows the direct-close path.

### Dual resolve path

`sol resolve` checks the writ's kind and branches:

**Code writs** (`kind == "code"` or empty):
1. Push branch to remote
2. Create merge request
3. Nudge forge and governor
4. Clear tether, set agent idle, log event

**Non-code writs** (`kind != "code"`):
1. Close the writ directly (status → closed)
2. Clear tether, set agent idle, log event
3. No branch push, no MR, no forge involvement

The output directory at `$SOL_HOME/{world}/writ-outputs/{writ-id}/` serves
as the delivery surface for non-code writs. Agents write findings, reports,
and structured data there. The directory survives worktree cleanup and is
GLASS-inspectable.

### Kind propagation

Kind flows through the full lifecycle:

1. **Creation**: `sol writ create --kind=analysis` (defaults to `code`)
2. **Workflow manifest**: manifest steps carry kind from workflow
   definitions to child writs
3. **Cast / persona**: `sol cast` reads kind and passes it to persona
   generation, which customizes instructions based on writ type
4. **Resolve**: kind determines the resolve path as described above

### Kind-aware persona generation

The agent persona (CLAUDE.local.md) adapts based on kind:

| Aspect | Code writ | Non-code writ |
|--------|-----------|---------------|
| Output directory | "auxiliary output (test reports, benchmarks)" | "primary output surface — all findings go here" |
| Quality gates | `make build && make test` | Review output directory |
| Resolve description | "pushes branch, creates MR" | "closes writ directly" |
| Session resilience | Emphasizes git commits | Emphasizes output directory |
| Completion checklist | Build + test gates | Output review |

### Direct dependency visibility

When a writ has upstream dependencies, the persona includes a "Direct
Dependencies" section listing each dependency's writ ID, title, kind, and
output directory path. This allows agents to read upstream analysis output
before starting their own work.

## Consequences

- **Non-code writs bypass forge entirely.** No empty branches, no failed
  squash-merges, no blocked caravans. The code-review workflow that
  triggered this work now completes cleanly.
- **"All Code through Forge" principle is preserved.** It was always scoped
  to code — code writs still flow through forge with quality gates. The
  principle's documentation is updated to make this scope explicit.
- **Output directories are the delivery surface for non-code writs.**
  GLASS-inspectable: `ls $SOL_HOME/{world}/writ-outputs/{writ-id}/`.
  The autarch can review analysis output with standard tools.
- **Sentinel reaps agents on closed writs.** When a non-code writ resolves
  (closes directly), any other agent that was somehow tethered to it gets
  reaped on the next sentinel patrol. This prevents orphaned sessions.
- **Backward compatible.** The `DEFAULT 'code'` ensures existing writs
  behave identically. No migration needed for running systems beyond the
  schema addition.
- **Extensible to new kinds.** The system branches on `code` vs not-code,
  so adding a new kind (e.g., `review`, `planning`) requires no code
  changes — it automatically follows the non-code resolve path.
- **Workflows can mix kinds.** A manifested workflow can have code steps and
  analysis steps. Each step resolves according to its own kind. Phase gating
  works correctly — analysis writs closing counts as completion for phase
  advancement.

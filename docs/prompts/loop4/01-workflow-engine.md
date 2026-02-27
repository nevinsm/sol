# Prompt 01: Loop 4 — Workflow Engine

You are extending the `sol` orchestration system with a directory-based
workflow engine. Workflows provide multi-step structured execution for
agents — a formula (template) is instantiated into a workflow instance
that tracks step progress and survives session crashes.

**Working directory:** `~/sol-src/`
**Prerequisite:** Loop 3 is complete.

Read all existing code first. Understand the dispatch package
(`internal/dispatch/dispatch.go` — especially `Cast()`, `Prime()`, and
`Done()`), the protocol package (`internal/protocol/` — CLAUDE.md
generation), the tether package (`internal/tether/`), and the store package
(`internal/store/`).

Read `docs/target-architecture.md` Section 3.5 (Workflow Engine) for
design context.

---

## Task 1: Workflow Package

Create `internal/workflow/` package with the core workflow engine.

### Formula (Template) Format

Formulas live in `$SOL_HOME/formulas/<name>/`. Each formula is a directory:

```
$SOL_HOME/formulas/default-work/
├── manifest.toml
└── steps/
    ├── 01-load-context.md
    ├── 02-implement.md
    └── 03-verify.md
```

### Manifest Format (TOML)

```toml
name = "default-work"
type = "workflow"
description = "Standard outpost work execution"

[variables]
issue = { required = true }
base_branch = { default = "main" }

[[steps]]
id = "load-context"
title = "Load work context"
instructions = "steps/01-load-context.md"

[[steps]]
id = "implement"
title = "Implement the change"
instructions = "steps/02-implement.md"
needs = ["load-context"]

[[steps]]
id = "verify"
title = "Verify the implementation"
instructions = "steps/03-verify.md"
needs = ["implement"]
```

- `name`: formula identifier (matches directory name)
- `type`: always `"workflow"` for Loop 4
- `description`: human-readable description
- `variables`: variable declarations — each key has `required` (bool) and
  `default` (string, optional)
- `steps`: ordered list of step definitions
  - `id`: unique within formula
  - `title`: human-readable step name
  - `instructions`: path to markdown file (relative to formula dir)
  - `needs`: list of step IDs that must complete before this step is
    ready (optional — if empty, step is immediately ready)

### Types

```go
// internal/workflow/workflow.go
package workflow

import "time"

// Manifest represents a formula's manifest.toml.
type Manifest struct {
    Name        string
    Type        string
    Description string
    Variables   map[string]VariableDecl
    Steps       []StepDef
}

// VariableDecl declares a workflow variable.
type VariableDecl struct {
    Required bool   `toml:"required"`
    Default  string `toml:"default"`
}

// StepDef defines a step in the formula.
type StepDef struct {
    ID           string   `toml:"id"`
    Title        string   `toml:"title"`
    Instructions string   `toml:"instructions"` // relative path to .md file
    Needs        []string `toml:"needs"`         // step IDs this depends on
}

// Instance holds metadata about an instantiated workflow.
type Instance struct {
    Formula       string            `json:"formula"`
    WorkItemID    string            `json:"work_item_id"`
    Variables     map[string]string `json:"variables"`
    InstantiatedAt time.Time        `json:"instantiated_at"`
}

// State tracks workflow execution progress.
type State struct {
    CurrentStep string   `json:"current_step"` // "" when complete
    Completed   []string `json:"completed"`
    Status      string   `json:"status"`       // "running", "done", "failed"
    StartedAt   time.Time `json:"started_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Step represents a single step instance within a running workflow.
type Step struct {
    ID           string     `json:"id"`
    Title        string     `json:"title"`
    Status       string     `json:"status"` // "pending", "ready", "executing", "complete"
    StartedAt    *time.Time `json:"started_at,omitempty"`
    CompletedAt  *time.Time `json:"completed_at,omitempty"`
    Instructions string     `json:"instructions"` // rendered markdown
}
```

### Core Functions

```go
// LoadManifest reads and parses a formula's manifest.toml.
// formulaDir is the absolute path to the formula directory.
func LoadManifest(formulaDir string) (*Manifest, error)

// Validate checks that a manifest is well-formed:
// - All step IDs are unique
// - All "needs" references point to existing step IDs
// - No dependency cycles (DAG validation via topological sort)
// Returns an error describing the first problem found.
func Validate(m *Manifest) error

// ResolveVariables merges provided variables with defaults, checks required.
// Returns error if a required variable is not provided and has no default.
func ResolveVariables(m *Manifest, provided map[string]string) (map[string]string, error)

// RenderStepInstructions reads a step's instruction file and performs
// variable substitution. Variables use {{variable}} syntax.
// Returns the rendered markdown string.
func RenderStepInstructions(formulaDir string, step StepDef, vars map[string]string) (string, error)
```

### Variable Substitution

Step instruction files use `{{variable}}` syntax for variable references.
Substitution is simple string replacement — no conditionals or loops.

Example step file (`steps/01-load-context.md`):
```markdown
# Load Context

Read work item {{issue}} and understand the requirements.
Checkout branch from {{base_branch}}.
```

With variables `{"issue": "sol-abc12345", "base_branch": "main"}`, the
rendered output is:
```markdown
# Load Context

Read work item sol-abc12345 and understand the requirements.
Checkout branch from main.
```

Unresolved variables (no matching key) are left as-is — this is not an
error.

### Instantiation

```go
// WorkflowDir returns the path to an agent's workflow instance.
// $SOL_HOME/{world}/outposts/{agentName}/.workflow/
func WorkflowDir(world, agentName string) string

// FormulaDir returns the path to a formula.
// $SOL_HOME/formulas/{formulaName}/
func FormulaDir(formulaName string) string

// Instantiate creates a workflow instance for an agent's assignment.
// 1. Loads and validates the manifest
// 2. Resolves variables (merge provided with defaults, check required)
// 3. Creates the .workflow/ directory in the agent's outpost dir
// 4. Writes manifest.json (Instance struct)
// 5. Renders each step's instructions with variable substitution
// 6. Writes step files as JSON (Step structs)
// 7. Finds the first ready step (no unmet dependencies)
// 8. Writes state.json with current_step set to the first ready step,
//    status="running"
//
// Returns the Instance and initial State, or error.
func Instantiate(world, agentName, formulaName string,
    vars map[string]string) (*Instance, *State, error)
```

The instance directory structure:

```
$SOL_HOME/{world}/outposts/{agentName}/.workflow/
├── manifest.json          # Instance metadata
├── state.json             # Execution progress
└── steps/
    ├── load-context.json  # Step instance (keyed by step ID)
    ├── implement.json
    └── verify.json
```

### Reading State

```go
// ReadState reads the current workflow state for an agent.
// Returns nil, nil if no workflow exists (no .workflow/ directory).
func ReadState(world, agentName string) (*State, error)

// ReadCurrentStep reads the current step's full details.
// Returns nil, nil if workflow is complete or doesn't exist.
func ReadCurrentStep(world, agentName string) (*Step, error)

// ReadInstance reads the workflow instance metadata.
func ReadInstance(world, agentName string) (*Instance, error)

// ListSteps reads all step files and returns them in manifest order.
func ListSteps(world, agentName string) ([]Step, error)
```

### Advancing

```go
// Advance marks the current step as complete and finds the next ready step.
// 1. Read state.json
// 2. Verify current_step is valid and status is "running"
// 3. Read the current step file, set status="complete", completedAt=now
// 4. Write the updated step file
// 5. Add current step ID to completed list
// 6. Find next ready steps (all "needs" satisfied by completed list)
//    Use topological sort — pick the first ready step by manifest order
// 7. If no more ready steps and all steps complete: status="done",
//    current_step="", completedAt=now
// 8. If next step found: update current_step, set next step
//    status="executing", startedAt=now
// 9. Write state.json
//
// Returns the next step (nil if done), whether workflow is complete, and error.
func Advance(world, agentName string) (nextStep *Step, done bool, err error)
```

### Topological Sort

```go
// NextReadySteps returns step IDs whose dependencies are all in the
// completed set and that are not themselves completed.
// Steps are returned in manifest order (stable ordering).
func NextReadySteps(steps []StepDef, completed []string) []string
```

Implementation: for each step, check if all entries in its `needs` list
appear in the `completed` set and the step itself is not in `completed`.
Return matching step IDs in the order they appear in the manifest.

### Cleanup

```go
// Remove deletes a workflow instance directory.
// Used for ephemeral workflows and cleanup.
func Remove(world, agentName string) error
```

---

## Task 2: CLI Commands

Create `cmd/workflow.go` with the `sol workflow` command group.

### sol workflow instantiate

```
sol workflow instantiate <formula> --item=<id> --world=<world> --agent=<name> [--var=key=val ...]
```

- `<formula>`: formula name (looked up in `$SOL_HOME/formulas/<formula>/`)
- `--item` (required): work item ID to associate
- `--world` (required): world name
- `--agent` (required): agent name
- `--var`: variable assignment (repeatable)

**Behavior:**
1. Call `workflow.Instantiate(world, agent, formula, vars)`
2. Print: `Workflow instantiated: <formula> for <item> (step: <current_step>)`
3. Exit 0

**Errors:** formula not found, required variable missing, DAG cycle →
print error, exit 1.

### sol workflow current

```
sol workflow current --world=<world> --agent=<name>
```

- `--world` (required): world name
- `--agent` (required): agent name

**Behavior:**
1. Call `workflow.ReadCurrentStep(world, agent)`
2. If no workflow or workflow complete:
   print `No active workflow step.` to stderr, exit 1
3. Print the step's rendered instructions to stdout
4. Exit 0

The output is the raw step markdown — this is what gets injected into
the agent's context.

### sol workflow advance

```
sol workflow advance --world=<world> --agent=<name>
```

- `--world` (required): world name
- `--agent` (required): agent name

**Behavior:**
1. Call `workflow.Advance(world, agent)`
2. If done: print `Workflow complete.`, exit 0
3. If advanced: print `Advanced to step: <title>`, exit 0
4. If error: print error, exit 1

### sol workflow status

```
sol workflow status --world=<world> --agent=<name> [--json]
```

- `--world` (required): world name
- `--agent` (required): agent name
- `--json`: output as JSON

**Human output:**
```
Workflow: default-work (sol-abc12345)
Status: running
Progress: 1/3 steps complete

Steps:
  [x] load-context — Load work context
  [>] implement — Implement the change (current)
  [ ] verify — Verify the implementation
```

**JSON output:** `{"formula":"default-work","work_item_id":"sol-abc12345",
"status":"running","current_step":"implement","completed":["load-context"],
"total_steps":3,"completed_count":1}`

---

## Task 3: Default Formula

Create a default `default-work` formula that ships with sol. This is
installed to `$SOL_HOME/formulas/default-work/` on first use (if the
directory doesn't exist, `Instantiate` creates it from embedded defaults).

### Embedded Defaults

Embed the default formula in the Go binary using `//go:embed`:

```go
// internal/workflow/defaults.go
package workflow

import "embed"

//go:embed defaults/default-work/manifest.toml
//go:embed defaults/default-work/steps/01-load-context.md
//go:embed defaults/default-work/steps/02-implement.md
//go:embed defaults/default-work/steps/03-verify.md
var defaultFormulas embed.FS
```

Place the actual files at:
```
internal/workflow/defaults/default-work/manifest.toml
internal/workflow/defaults/default-work/steps/01-load-context.md
internal/workflow/defaults/default-work/steps/02-implement.md
internal/workflow/defaults/default-work/steps/03-verify.md
```

### manifest.toml

```toml
name = "default-work"
type = "workflow"
description = "Standard outpost work execution"

[variables]
issue = { required = true }
base_branch = { default = "main" }

[[steps]]
id = "load-context"
title = "Load work context"
instructions = "steps/01-load-context.md"

[[steps]]
id = "implement"
title = "Implement the change"
instructions = "steps/02-implement.md"
needs = ["load-context"]

[[steps]]
id = "verify"
title = "Verify the implementation"
instructions = "steps/03-verify.md"
needs = ["implement"]
```

### steps/01-load-context.md

```markdown
# Load Context

Read work item {{issue}} and understand the requirements fully.

1. Review the work item title and description
2. Explore the relevant areas of the codebase
3. Identify the files that need to change
4. Note any dependencies or related work

When you have a clear understanding of what needs to be done, advance
to the next step: `sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
```

### steps/02-implement.md

```markdown
# Implement

Implement the changes for {{issue}}.

1. Make the necessary code changes
2. Write or update tests as appropriate
3. Ensure the code compiles and tests pass
4. Commit your changes with a clear message

When implementation is complete and tests pass, advance:
`sol workflow advance --world=$SOL_WORLD --agent=$SOL_AGENT`
```

### steps/03-verify.md

```markdown
# Verify

Final verification for {{issue}}.

1. Run the full test suite
2. Review your changes for correctness and style
3. Ensure no unintended side effects
4. Verify the commit history is clean

When satisfied, signal completion: `sol resolve`
```

### EnsureFormula

```go
// EnsureFormula checks if a formula exists on disk. If not and it's a
// known default formula, extract it from the embedded defaults.
// Returns the absolute path to the formula directory.
func EnsureFormula(formulaName string) (string, error)
```

This is called by `Instantiate` before loading the manifest.

---

## Task 4: Tests

### Workflow Engine Tests

Create `internal/workflow/workflow_test.go`:

```go
func TestLoadManifest(t *testing.T)
    // Create a temp formula directory with valid manifest.toml
    // Load it, verify all fields parsed correctly
    // Verify steps, variables, needs

func TestLoadManifestMissing(t *testing.T)
    // Non-existent directory → error

func TestValidateValid(t *testing.T)
    // Valid DAG with dependencies → no error

func TestValidateDuplicateStepID(t *testing.T)
    // Two steps with same ID → error

func TestValidateMissingNeed(t *testing.T)
    // Step references non-existent need → error

func TestValidateCycle(t *testing.T)
    // A needs B, B needs A → error

func TestResolveVariables(t *testing.T)
    // Required variable provided → resolved
    // Variable with default, not provided → default used
    // Required variable missing → error

func TestRenderStepInstructions(t *testing.T)
    // Template with {{variable}} → substituted
    // Unknown {{variable}} → left as-is

func TestInstantiate(t *testing.T)
    // Create formula dir, instantiate
    // Verify: manifest.json, state.json, step files created
    // Verify: state.current_step is first ready step
    // Verify: step instructions rendered with variables

func TestInstantiateRequiredVariableMissing(t *testing.T)
    // Missing required variable → error, no directory created

func TestReadState(t *testing.T)
    // Instantiate, then ReadState → valid state
    // Non-existent workflow → nil, nil

func TestReadCurrentStep(t *testing.T)
    // Instantiate → returns first step with rendered instructions

func TestAdvance(t *testing.T)
    // Instantiate (3 steps, linear deps)
    // Advance → moves to step 2, step 1 marked complete
    // Advance → moves to step 3
    // Advance → workflow done, no next step

func TestAdvanceDAG(t *testing.T)
    // Formula with branching DAG:
    //   A (no deps)
    //   B needs A
    //   C needs A
    //   D needs B, C
    // After A: both B and C ready, pick first by manifest order
    // After B: C still ready (or D if C done)
    // Continue until done

func TestNextReadySteps(t *testing.T)
    // Various completed sets → correct ready steps

func TestRemove(t *testing.T)
    // Instantiate, then Remove → directory gone

func TestEnsureFormula(t *testing.T)
    // Known formula not on disk → extracted from defaults
    // Already exists → no-op
    // Unknown formula not on disk → error
```

### CLI Smoke Tests

Add to `test/integration/cli_loop4_test.go`:

```go
func TestCLIWorkflowInstantiateHelp(t *testing.T)
func TestCLIWorkflowCurrentHelp(t *testing.T)
func TestCLIWorkflowAdvanceHelp(t *testing.T)
func TestCLIWorkflowStatusHelp(t *testing.T)
```

---

## Task 5: Verify

1. `make test` — all existing and new tests pass
2. `make build` — succeeds
3. Manual smoke test:
   ```bash
   export SOL_HOME=/tmp/sol-test
   mkdir -p /tmp/sol-test/myworld/outposts/Toast

   # Instantiate the default formula
   bin/sol workflow instantiate default-work \
     --item=sol-abc12345 --world=myworld --agent=Toast \
     --var=issue=sol-abc12345

   # Check status
   bin/sol workflow status --world=myworld --agent=Toast

   # Read current step
   bin/sol workflow current --world=myworld --agent=Toast

   # Advance through steps
   bin/sol workflow advance --world=myworld --agent=Toast
   bin/sol workflow advance --world=myworld --agent=Toast
   bin/sol workflow advance --world=myworld --agent=Toast

   # Verify done
   bin/sol workflow status --world=myworld --agent=Toast
   ```
4. Clean up `/tmp/sol-test` after verification.

---

## Guidelines

- The workflow engine is a **directory-based state machine** — all state
  is on disk, inspectable with `ls`, `cat`, `jq` (GLASS principle).
- Crash recovery is automatic: `state.json` is the source of truth. If
  a session crashes, `sol prime` (extended in prompt 03) re-reads
  `state.json` and injects the current step.
- Step advancement is idempotent — re-running a completed step is safe.
- Variable substitution is simple `{{variable}}` → value string
  replacement. No template logic.
- Default formulas are embedded in the binary but extracted to
  `$SOL_HOME/formulas/` on first use. Operator can modify the extracted
  copy.
- The `needs` field creates a DAG, not just a linear sequence. The
  engine must handle branching dependencies (multiple steps ready
  simultaneously — pick first by manifest order).
- All existing tests must continue to pass.
- Commit after tests pass with message:
  `feat(workflow): add directory-based workflow engine with formula templates`

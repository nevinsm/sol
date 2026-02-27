package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/config"
)

// Manifest represents a formula's manifest.toml.
type Manifest struct {
	Name        string                  `toml:"name"`
	Type        string                  `toml:"type"`
	Description string                  `toml:"description"`
	Variables   map[string]VariableDecl `toml:"variables"`
	Steps       []StepDef              `toml:"steps"`
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
	Formula        string            `json:"formula"`
	WorkItemID     string            `json:"work_item_id"`
	Variables      map[string]string `json:"variables"`
	InstantiatedAt time.Time         `json:"instantiated_at"`
}

// State tracks workflow execution progress.
type State struct {
	CurrentStep string     `json:"current_step"` // "" when complete
	Completed   []string   `json:"completed"`
	Status      string     `json:"status"` // "running", "done", "failed"
	StartedAt   time.Time  `json:"started_at"`
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

// WorkflowDir returns the path to an agent's workflow instance.
// $SOL_HOME/{world}/outposts/{agentName}/.workflow/
func WorkflowDir(world, agentName string) string {
	return filepath.Join(config.Home(), world, "outposts", agentName, ".workflow")
}

// FormulaDir returns the path to a formula.
// $SOL_HOME/formulas/{formulaName}/
func FormulaDir(formulaName string) string {
	return filepath.Join(config.Home(), "formulas", formulaName)
}

// LoadManifest reads and parses a formula's manifest.toml.
// formulaDir is the absolute path to the formula directory.
func LoadManifest(formulaDir string) (*Manifest, error) {
	path := filepath.Join(formulaDir, "manifest.toml")
	var m Manifest
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil, fmt.Errorf("failed to load manifest %q: %w", path, err)
	}
	return &m, nil
}

// Validate checks that a manifest is well-formed:
// - All step IDs are unique
// - All "needs" references point to existing step IDs
// - No dependency cycles (DAG validation)
// Returns an error describing the first problem found.
func Validate(m *Manifest) error {
	ids := make(map[string]bool, len(m.Steps))
	for _, s := range m.Steps {
		if ids[s.ID] {
			return fmt.Errorf("duplicate step ID %q", s.ID)
		}
		ids[s.ID] = true
	}

	for _, s := range m.Steps {
		for _, need := range s.Needs {
			if !ids[need] {
				return fmt.Errorf("step %q references unknown dependency %q", s.ID, need)
			}
		}
	}

	// Cycle detection via topological sort (Kahn's algorithm).
	inDegree := make(map[string]int, len(m.Steps))
	dependents := make(map[string][]string, len(m.Steps))
	for _, s := range m.Steps {
		if _, ok := inDegree[s.ID]; !ok {
			inDegree[s.ID] = 0
		}
		for _, need := range s.Needs {
			inDegree[s.ID]++
			dependents[need] = append(dependents[need], s.ID)
		}
	}

	queue := make([]string, 0, len(m.Steps))
	for _, s := range m.Steps {
		if inDegree[s.ID] == 0 {
			queue = append(queue, s.ID)
		}
	}

	visited := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		visited++
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if visited != len(m.Steps) {
		return fmt.Errorf("dependency cycle detected in workflow steps")
	}

	return nil
}

// ResolveVariables merges provided variables with defaults, checks required.
// Returns error if a required variable is not provided and has no default.
func ResolveVariables(m *Manifest, provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string)

	// Start with provided values.
	for k, v := range provided {
		resolved[k] = v
	}

	// Apply defaults and check required.
	for name, decl := range m.Variables {
		if _, ok := resolved[name]; ok {
			continue
		}
		if decl.Default != "" {
			resolved[name] = decl.Default
		} else if decl.Required {
			return nil, fmt.Errorf("required variable %q not provided", name)
		}
	}

	return resolved, nil
}

// RenderStepInstructions reads a step's instruction file and performs
// variable substitution. Variables use {{variable}} syntax.
// Returns the rendered markdown string.
func RenderStepInstructions(formulaDir string, step StepDef, vars map[string]string) (string, error) {
	path := filepath.Join(formulaDir, step.Instructions)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read step instructions %q: %w", path, err)
	}

	content := string(data)
	for k, v := range vars {
		content = strings.ReplaceAll(content, "{{"+k+"}}", v)
	}

	return content, nil
}

// NextReadySteps returns step IDs whose dependencies are all in the
// completed set and that are not themselves completed.
// Steps are returned in manifest order (stable ordering).
func NextReadySteps(steps []StepDef, completed []string) []string {
	done := make(map[string]bool, len(completed))
	for _, id := range completed {
		done[id] = true
	}

	var ready []string
	for _, s := range steps {
		if done[s.ID] {
			continue
		}
		allMet := true
		for _, need := range s.Needs {
			if !done[need] {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, s.ID)
		}
	}
	return ready
}

// Instantiate creates a workflow instance for an agent's assignment.
func Instantiate(world, agentName, formulaName string,
	vars map[string]string) (*Instance, *State, error) {

	// Ensure formula exists (extract from embedded defaults if needed).
	formulaPath, err := EnsureFormula(formulaName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to ensure formula %q: %w", formulaName, err)
	}

	// Load and validate manifest.
	m, err := LoadManifest(formulaPath)
	if err != nil {
		return nil, nil, err
	}
	if err := Validate(m); err != nil {
		return nil, nil, fmt.Errorf("invalid formula %q: %w", formulaName, err)
	}

	// Resolve variables.
	resolved, err := ResolveVariables(m, vars)
	if err != nil {
		return nil, nil, err
	}

	// Create .workflow/ directory.
	wfDir := WorkflowDir(world, agentName)
	stepsDir := filepath.Join(wfDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create workflow directory: %w", err)
	}

	// Rollback on error after directory creation.
	rollback := func() {
		os.RemoveAll(wfDir)
	}

	// Build instance.
	inst := &Instance{
		Formula:        formulaName,
		WorkItemID:     resolved["issue"],
		Variables:      resolved,
		InstantiatedAt: time.Now().UTC(),
	}

	// Write manifest.json.
	if err := writeJSON(filepath.Join(wfDir, "manifest.json"), inst); err != nil {
		rollback()
		return nil, nil, fmt.Errorf("failed to write manifest.json: %w", err)
	}

	// Render and write each step file.
	for _, sd := range m.Steps {
		rendered, err := RenderStepInstructions(formulaPath, sd, resolved)
		if err != nil {
			rollback()
			return nil, nil, err
		}

		step := &Step{
			ID:           sd.ID,
			Title:        sd.Title,
			Status:       "pending",
			Instructions: rendered,
		}

		if err := writeJSON(filepath.Join(stepsDir, sd.ID+".json"), step); err != nil {
			rollback()
			return nil, nil, fmt.Errorf("failed to write step %q: %w", sd.ID, err)
		}
	}

	// Find first ready step.
	ready := NextReadySteps(m.Steps, nil)
	var currentStep string
	if len(ready) > 0 {
		currentStep = ready[0]
		// Mark it as executing.
		stepPath := filepath.Join(stepsDir, currentStep+".json")
		step, err := readStepFile(stepPath)
		if err != nil {
			rollback()
			return nil, nil, err
		}
		now := time.Now().UTC()
		step.Status = "executing"
		step.StartedAt = &now
		if err := writeJSON(stepPath, step); err != nil {
			rollback()
			return nil, nil, err
		}
	}

	// Write state.json.
	now := time.Now().UTC()
	state := &State{
		CurrentStep: currentStep,
		Completed:   []string{},
		Status:      "running",
		StartedAt:   now,
	}
	if err := writeJSON(filepath.Join(wfDir, "state.json"), state); err != nil {
		rollback()
		return nil, nil, fmt.Errorf("failed to write state.json: %w", err)
	}

	return inst, state, nil
}

// ReadState reads the current workflow state for an agent.
// Returns nil, nil if no workflow exists (no .workflow/ directory).
func ReadState(world, agentName string) (*State, error) {
	path := filepath.Join(WorkflowDir(world, agentName), "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read workflow state: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse workflow state: %w", err)
	}
	return &s, nil
}

// ReadCurrentStep reads the current step's full details.
// Returns nil, nil if workflow is complete or doesn't exist.
func ReadCurrentStep(world, agentName string) (*Step, error) {
	state, err := ReadState(world, agentName)
	if err != nil {
		return nil, err
	}
	if state == nil || state.CurrentStep == "" {
		return nil, nil
	}

	stepPath := filepath.Join(WorkflowDir(world, agentName), "steps", state.CurrentStep+".json")
	return readStepFile(stepPath)
}

// ReadInstance reads the workflow instance metadata.
func ReadInstance(world, agentName string) (*Instance, error) {
	path := filepath.Join(WorkflowDir(world, agentName), "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read workflow instance: %w", err)
	}

	var inst Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("failed to parse workflow instance: %w", err)
	}
	return &inst, nil
}

// ListSteps reads all step files and returns them in manifest order.
func ListSteps(world, agentName string) ([]Step, error) {
	wfDir := WorkflowDir(world, agentName)

	// Read the instance to get the formula and load the manifest for step order.
	inst, err := ReadInstance(world, agentName)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, nil
	}

	formulaPath, err := EnsureFormula(inst.Formula)
	if err != nil {
		return nil, err
	}

	m, err := LoadManifest(formulaPath)
	if err != nil {
		return nil, err
	}

	steps := make([]Step, 0, len(m.Steps))
	for _, sd := range m.Steps {
		stepPath := filepath.Join(wfDir, "steps", sd.ID+".json")
		step, err := readStepFile(stepPath)
		if err != nil {
			return nil, err
		}
		steps = append(steps, *step)
	}

	return steps, nil
}

// Advance marks the current step as complete and finds the next ready step.
func Advance(world, agentName string) (nextStep *Step, done bool, err error) {
	wfDir := WorkflowDir(world, agentName)

	// Read state.
	state, err := ReadState(world, agentName)
	if err != nil {
		return nil, false, err
	}
	if state == nil {
		return nil, false, fmt.Errorf("no workflow found for agent %q in world %q", agentName, world)
	}
	if state.Status != "running" {
		return nil, false, fmt.Errorf("workflow status is %q, expected \"running\"", state.Status)
	}
	if state.CurrentStep == "" {
		return nil, false, fmt.Errorf("no current step to advance from")
	}

	// Mark current step as complete.
	stepPath := filepath.Join(wfDir, "steps", state.CurrentStep+".json")
	currentStep, err := readStepFile(stepPath)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	currentStep.Status = "complete"
	currentStep.CompletedAt = &now
	if err := writeJSON(stepPath, currentStep); err != nil {
		return nil, false, fmt.Errorf("failed to update step %q: %w", state.CurrentStep, err)
	}

	// Update completed list.
	state.Completed = append(state.Completed, state.CurrentStep)

	// Load manifest to determine next ready steps.
	inst, err := ReadInstance(world, agentName)
	if err != nil {
		return nil, false, err
	}
	formulaPath, err := EnsureFormula(inst.Formula)
	if err != nil {
		return nil, false, err
	}
	m, err := LoadManifest(formulaPath)
	if err != nil {
		return nil, false, err
	}

	// Find next ready steps.
	ready := NextReadySteps(m.Steps, state.Completed)

	if len(ready) == 0 {
		// All steps complete — workflow is done.
		state.CurrentStep = ""
		state.Status = "done"
		state.CompletedAt = &now
		if err := writeJSON(filepath.Join(wfDir, "state.json"), state); err != nil {
			return nil, false, fmt.Errorf("failed to write state.json: %w", err)
		}
		return nil, true, nil
	}

	// Pick first ready step (manifest order).
	nextID := ready[0]
	state.CurrentStep = nextID

	// Mark next step as executing.
	nextStepPath := filepath.Join(wfDir, "steps", nextID+".json")
	ns, err := readStepFile(nextStepPath)
	if err != nil {
		return nil, false, err
	}
	ns.Status = "executing"
	ns.StartedAt = &now
	if err := writeJSON(nextStepPath, ns); err != nil {
		return nil, false, fmt.Errorf("failed to update step %q: %w", nextID, err)
	}

	// Write state.
	if err := writeJSON(filepath.Join(wfDir, "state.json"), state); err != nil {
		return nil, false, fmt.Errorf("failed to write state.json: %w", err)
	}

	return ns, false, nil
}

// Remove deletes a workflow instance directory.
func Remove(world, agentName string) error {
	wfDir := WorkflowDir(world, agentName)
	if err := os.RemoveAll(wfDir); err != nil {
		return fmt.Errorf("failed to remove workflow directory: %w", err)
	}
	return nil
}

// writeJSON marshals v to JSON and writes it to path.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// readStepFile reads and parses a step JSON file.
func readStepFile(path string) (*Step, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read step file %q: %w", path, err)
	}
	var s Step
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse step file %q: %w", path, err)
	}
	return &s, nil
}

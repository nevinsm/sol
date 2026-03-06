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
	"github.com/nevinsm/sol/internal/store"
)

// Manifest represents a formula's manifest.toml.
type Manifest struct {
	Name        string                  `toml:"name"`
	Type        string                  `toml:"type"`
	Description string                  `toml:"description"`
	Manifest    bool                    `toml:"manifest"` // default false; when true, steps become child work items
	Variables   map[string]VariableDecl `toml:"variables"`
	Steps       []StepDef              `toml:"steps"`
	Templates   []Template             `toml:"template"`
	Legs        []Leg                  `toml:"legs"`
	Synth       *Synthesis             `toml:"synthesis"`
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

// Template defines a child work item template in an expansion formula.
type Template struct {
	ID          string   `toml:"id"`
	Title       string   `toml:"title"`
	Description string   `toml:"description"`
	Needs       []string `toml:"needs"`
}

// Leg defines an independent work dimension in a convoy formula.
type Leg struct {
	ID          string `toml:"id"`
	Title       string `toml:"title"`
	Description string `toml:"description"`
	Focus       string `toml:"focus"`
}

// Synthesis defines the follow-up step in a convoy formula that runs
// after all specified legs have completed.
type Synthesis struct {
	Title       string   `toml:"title"`
	Description string   `toml:"description"`
	DependsOn   []string `toml:"depends_on"`
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
// Uses role-aware directory: outposts/{name}/ for agents, envoys/{name}/ for envoys, etc.
func WorkflowDir(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".workflow")
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
// - Type is "workflow", "expansion", or "convoy"
// - Workflow type has steps (not templates/legs); expansion has templates (not steps/legs)
// - Convoy type has legs and synthesis (not steps/templates)
// - All IDs are unique
// - All "needs"/"depends_on" references point to existing IDs
// - No dependency cycles (DAG validation)
// Returns an error describing the first problem found.
func Validate(m *Manifest) error {
	if m.Type == "expansion" {
		if len(m.Steps) > 0 {
			return fmt.Errorf("expansion formula must not contain [[steps]] entries")
		}
		if len(m.Templates) == 0 {
			return fmt.Errorf("expansion formula requires at least one [[template]] entry")
		}
		// Convert templates to dagNodes for validation.
		nodes := make([]dagNode, len(m.Templates))
		for i, t := range m.Templates {
			nodes[i] = dagNode{ID: t.ID, Needs: t.Needs}
		}
		return validateDAG(nodes, "template")
	}

	if m.Type == "convoy" {
		if len(m.Steps) > 0 {
			return fmt.Errorf("convoy formula must not contain [[steps]] entries")
		}
		if len(m.Templates) > 0 {
			return fmt.Errorf("convoy formula must not contain [[template]] entries")
		}
		if len(m.Legs) == 0 {
			return fmt.Errorf("convoy formula requires at least one [[legs]] entry")
		}
		if m.Synth == nil {
			return fmt.Errorf("convoy formula requires a [synthesis] section")
		}
		// Validate unique leg IDs.
		legIDs := make(map[string]bool, len(m.Legs))
		for _, leg := range m.Legs {
			if legIDs[leg.ID] {
				return fmt.Errorf("duplicate leg ID %q", leg.ID)
			}
			legIDs[leg.ID] = true
		}
		// Validate synthesis depends_on references valid legs.
		for _, dep := range m.Synth.DependsOn {
			if !legIDs[dep] {
				return fmt.Errorf("synthesis depends_on references unknown leg %q", dep)
			}
		}
		return nil
	}

	// All other types (workflow, agent, etc.) validate steps.
	if len(m.Templates) > 0 && m.Type != "" {
		return fmt.Errorf("%s formula must not contain [[template]] entries", m.Type)
	}
	return validateDAG(m.Steps, "step")
}

// dagNode is a common interface for DAG validation across steps and templates.
type dagNode struct {
	ID    string
	Needs []string
}

// validateDAG checks unique IDs, valid needs references, and no cycles.
// label is used in error messages ("step" or "template").
func validateDAG[T interface{ dagID() string; dagNeeds() []string }](items []T, label string) error {
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		id := item.dagID()
		if ids[id] {
			return fmt.Errorf("duplicate %s ID %q", label, id)
		}
		ids[id] = true
	}

	for _, item := range items {
		for _, need := range item.dagNeeds() {
			if !ids[need] {
				return fmt.Errorf("%s %q references unknown dependency %q", label, item.dagID(), need)
			}
		}
	}

	// Cycle detection via topological sort (Kahn's algorithm).
	inDegree := make(map[string]int, len(items))
	dependents := make(map[string][]string, len(items))
	for _, item := range items {
		id := item.dagID()
		if _, ok := inDegree[id]; !ok {
			inDegree[id] = 0
		}
		for _, need := range item.dagNeeds() {
			inDegree[id]++
			dependents[need] = append(dependents[need], id)
		}
	}

	queue := make([]string, 0, len(items))
	for _, item := range items {
		if inDegree[item.dagID()] == 0 {
			queue = append(queue, item.dagID())
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

	if visited != len(items) {
		return fmt.Errorf("dependency cycle detected in %ss", label)
	}

	return nil
}

func (s StepDef) dagID() string      { return s.ID }
func (s StepDef) dagNeeds() []string { return s.Needs }
func (t dagNode) dagID() string      { return t.ID }
func (t dagNode) dagNeeds() []string { return t.Needs }

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
func Instantiate(world, agentName, role, formulaName string,
	vars map[string]string) (*Instance, *State, error) {

	// Ensure formula exists (extract from embedded defaults if needed).
	res, err := EnsureFormula(formulaName, config.RepoPath(world))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to ensure formula %q: %w", formulaName, err)
	}

	// Load and validate manifest.
	m, err := LoadManifest(res.Path)
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
	wfDir := WorkflowDir(world, agentName, role)
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
		rendered, err := RenderStepInstructions(res.Path, sd, resolved)
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
func ReadState(world, agentName, role string) (*State, error) {
	path := filepath.Join(WorkflowDir(world, agentName, role), "state.json")
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
func ReadCurrentStep(world, agentName, role string) (*Step, error) {
	state, err := ReadState(world, agentName, role)
	if err != nil {
		return nil, err
	}
	if state == nil || state.CurrentStep == "" {
		return nil, nil
	}

	stepPath := filepath.Join(WorkflowDir(world, agentName, role), "steps", state.CurrentStep+".json")
	return readStepFile(stepPath)
}

// ReadInstance reads the workflow instance metadata.
func ReadInstance(world, agentName, role string) (*Instance, error) {
	path := filepath.Join(WorkflowDir(world, agentName, role), "manifest.json")
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
func ListSteps(world, agentName, role string) ([]Step, error) {
	wfDir := WorkflowDir(world, agentName, role)

	// Read the instance to get the formula and load the manifest for step order.
	inst, err := ReadInstance(world, agentName, role)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, nil
	}

	res, err := EnsureFormula(inst.Formula, config.RepoPath(world))
	if err != nil {
		return nil, err
	}

	m, err := LoadManifest(res.Path)
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
func Advance(world, agentName, role string) (nextStep *Step, done bool, err error) {
	wfDir := WorkflowDir(world, agentName, role)

	// Read state.
	state, err := ReadState(world, agentName, role)
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

	// Mark current step as complete (idempotent for crash recovery).
	stepPath := filepath.Join(wfDir, "steps", state.CurrentStep+".json")
	currentStep, err := readStepFile(stepPath)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	// If the step is already complete (e.g., from a crash recovery), skip to finding the next step.
	if currentStep.Status == "complete" {
		// Fall through to the next-step logic below.
	} else {
		currentStep.Status = "complete"
		currentStep.CompletedAt = &now
		if err := writeJSON(stepPath, currentStep); err != nil {
			return nil, false, fmt.Errorf("failed to write step %q: %w", state.CurrentStep, err)
		}
		// Only append to Completed list when freshly completing.
		state.Completed = append(state.Completed, state.CurrentStep)
	}

	// Load manifest to determine next ready steps.
	inst, err := ReadInstance(world, agentName, role)
	if err != nil {
		return nil, false, err
	}
	res, err := EnsureFormula(inst.Formula, config.RepoPath(world))
	if err != nil {
		return nil, false, err
	}
	m, err := LoadManifest(res.Path)
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
func Remove(world, agentName, role string) error {
	wfDir := WorkflowDir(world, agentName, role)
	if err := os.RemoveAll(wfDir); err != nil {
		return fmt.Errorf("failed to remove workflow directory: %w", err)
	}
	return nil
}

// writeJSON marshals v to JSON and writes it to path atomically.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit %s: %w", filepath.Base(path), err)
	}
	return nil
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

// ManifestResult holds the output of manifesting a formula into work items.
type ManifestResult struct {
	CaravanID string            `json:"caravan_id"`
	ParentID  string            `json:"parent_id"`
	ChildIDs  map[string]string `json:"child_ids"` // step/template ID → work item ID
	Phases    map[string]int    `json:"phases"`     // step/template ID → phase number
}

// ManifestOpts holds parameters for ManifestFormula.
type ManifestOpts struct {
	FormulaName string
	World       string
	ParentID    string // if empty, a parent work item is created
	Variables   map[string]string
	CreatedBy   string
}

// ShouldManifest returns true if the formula should be manifested.
// Expansion and convoy formulas always manifest. Workflow formulas
// manifest when the manifest flag is set.
func ShouldManifest(m *Manifest) bool {
	return m.Type == "expansion" || m.Type == "convoy" || m.Manifest
}

// ComputePhases returns the phase (dependency depth) for each item in a DAG.
// Items with no dependencies are phase 0. Items whose dependencies are all
// phase N or lower are phase max(N)+1.
func ComputePhases[T interface {
	dagID() string
	dagNeeds() []string
}](items []T) map[string]int {
	phases := make(map[string]int, len(items))

	// Seed phase 0 for items with no dependencies.
	for _, item := range items {
		if len(item.dagNeeds()) == 0 {
			phases[item.dagID()] = 0
		}
	}

	// Iterate until all items have phases assigned.
	for range len(items) {
		for _, item := range items {
			if _, ok := phases[item.dagID()]; ok {
				continue
			}
			maxPhase := -1
			allResolved := true
			for _, need := range item.dagNeeds() {
				p, ok := phases[need]
				if !ok {
					allResolved = false
					break
				}
				if p > maxPhase {
					maxPhase = p
				}
			}
			if allResolved {
				phases[item.dagID()] = maxPhase + 1
			}
		}
	}

	return phases
}

// phaseable adapts formula items for ComputePhases.
type phaseable struct {
	id    string
	needs []string
}

func (p phaseable) dagID() string      { return p.id }
func (p phaseable) dagNeeds() []string { return p.needs }

// renderTemplateField substitutes {target.title}, {target.description},
// and {target.id} in a template string.
func renderTemplateField(s string, target *store.WorkItem) string {
	s = strings.ReplaceAll(s, "{target.title}", target.Title)
	s = strings.ReplaceAll(s, "{target.description}", target.Description)
	s = strings.ReplaceAll(s, "{target.id}", target.ID)
	return s
}

// ManifestFormula materializes a formula into child work items with a caravan.
// Each step (workflow) or template (expansion) becomes a child work item.
// Dependencies between children mirror the formula's DAG. Children are
// grouped in a caravan with phases derived from dependency depth.
func ManifestFormula(worldStore, sphereStore *store.Store, opts ManifestOpts) (*ManifestResult, error) {
	// Load formula.
	res, err := EnsureFormula(opts.FormulaName, config.RepoPath(opts.World))
	if err != nil {
		return nil, fmt.Errorf("failed to ensure formula %q: %w", opts.FormulaName, err)
	}

	m, err := LoadManifest(res.Path)
	if err != nil {
		return nil, err
	}
	if err := Validate(m); err != nil {
		return nil, fmt.Errorf("invalid formula %q: %w", opts.FormulaName, err)
	}

	if !ShouldManifest(m) {
		return nil, fmt.Errorf("formula %q is not configured for manifestation (set manifest = true or use expansion type)", opts.FormulaName)
	}

	// Resolve variables (for workflow type step rendering).
	resolved, err := ResolveVariables(m, opts.Variables)
	if err != nil {
		return nil, err
	}

	parentID := opts.ParentID

	// For expansion formulas, the parent must exist (it's the target).
	// For workflow and convoy formulas, create a parent if not provided.
	var target *store.WorkItem
	if m.Type == "expansion" {
		if parentID == "" {
			return nil, fmt.Errorf("expansion formula requires a parent work item (target)")
		}
		target, err = worldStore.GetWorkItem(parentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get target work item %q: %w", parentID, err)
		}
	} else if parentID == "" {
		parentID, err = worldStore.CreateWorkItem(
			m.Name+": "+resolved["issue"],
			m.Description,
			opts.CreatedBy,
			0,
			[]string{"manifest"},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create parent work item: %w", err)
		}
	}

	// Build child items from steps or templates.
	type childDef struct {
		formulaID   string
		title       string
		description string
		needs       []string
		labels      []string // additional labels beyond "manifest-child"
	}

	var children []childDef

	if m.Type == "expansion" {
		for _, tmpl := range m.Templates {
			children = append(children, childDef{
				formulaID:   tmpl.ID,
				title:       renderTemplateField(tmpl.Title, target),
				description: renderTemplateField(tmpl.Description, target),
				needs:       tmpl.Needs,
			})
		}
	} else if m.Type == "convoy" {
		// Convoy: legs are independent (phase 0), synthesis depends on legs (phase 1).
		for _, leg := range m.Legs {
			desc := leg.Description
			if leg.Focus != "" {
				desc += "\n\nFocus: " + leg.Focus
			}
			children = append(children, childDef{
				formulaID:   leg.ID,
				title:       leg.Title,
				description: desc,
				labels:      []string{"convoy-leg"},
			})
		}
		// Synthesis description is enriched with leg references after leg items are created.
		children = append(children, childDef{
			formulaID:   "synthesis",
			title:       m.Synth.Title,
			description: m.Synth.Description,
			needs:       m.Synth.DependsOn,
			labels:      []string{"convoy-synthesis"},
		})
	} else {
		// Workflow type — render step instructions as descriptions.
		for _, step := range m.Steps {
			rendered, err := RenderStepInstructions(res.Path, step, resolved)
			if err != nil {
				return nil, fmt.Errorf("failed to render step %q instructions: %w", step.ID, err)
			}
			children = append(children, childDef{
				formulaID:   step.ID,
				title:       step.Title,
				description: rendered,
				needs:       step.Needs,
			})
		}
	}

	// Compute phases from the DAG.
	phaseItems := make([]phaseable, len(children))
	for i, c := range children {
		phaseItems[i] = phaseable{id: c.formulaID, needs: c.needs}
	}
	phases := ComputePhases(phaseItems)

	// Create child work items.
	childIDs := make(map[string]string, len(children))
	for i, c := range children {
		labels := append([]string{"manifest-child"}, c.labels...)

		desc := c.description

		// For convoy synthesis: enrich description with leg references
		// (legs were created before synthesis in the children slice).
		if m.Type == "convoy" && c.formulaID == "synthesis" {
			var legRefs strings.Builder
			legRefs.WriteString("\n\n## Leg Work Items\n")
			for _, leg := range m.Legs {
				legItemID := childIDs[leg.ID]
				legRefs.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", leg.Title, legItemID, leg.Description))
			}
			legRefs.WriteString("\nAll leg branches have been merged to the target branch. Their changes are available in your worktree.")
			desc += legRefs.String()
			children[i].description = desc
		}

		id, err := worldStore.CreateWorkItemWithOpts(store.CreateWorkItemOpts{
			Title:       c.title,
			Description: desc,
			CreatedBy:   opts.CreatedBy,
			ParentID:    parentID,
			Labels:      labels,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create child work item for %q: %w", c.formulaID, err)
		}
		childIDs[c.formulaID] = id
	}

	// Add dependencies between children mirroring the DAG.
	for _, c := range children {
		childID := childIDs[c.formulaID]
		for _, need := range c.needs {
			depID, ok := childIDs[need]
			if !ok {
				return nil, fmt.Errorf("child %q references unknown dependency %q", c.formulaID, need)
			}
			if err := worldStore.AddDependency(childID, depID); err != nil {
				return nil, fmt.Errorf("failed to add dependency %q → %q: %w", c.formulaID, need, err)
			}
		}
	}

	// Create caravan and add children.
	caravanName := opts.FormulaName
	if opts.ParentID != "" {
		caravanName += ":" + opts.ParentID
	} else {
		caravanName += ":" + parentID
	}
	caravanID, err := sphereStore.CreateCaravan(caravanName, opts.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to create caravan: %w", err)
	}

	for formulaID, workItemID := range childIDs {
		phase := phases[formulaID]
		if err := sphereStore.CreateCaravanItem(caravanID, workItemID, opts.World, phase); err != nil {
			return nil, fmt.Errorf("failed to add item %q to caravan: %w", formulaID, err)
		}
	}

	// Mark caravan as ready for dispatch.
	if err := sphereStore.UpdateCaravanStatus(caravanID, "ready"); err != nil {
		return nil, fmt.Errorf("failed to update caravan status: %w", err)
	}

	result := &ManifestResult{
		CaravanID: caravanID,
		ParentID:  parentID,
		ChildIDs:  childIDs,
		Phases:    phases,
	}

	return result, nil
}

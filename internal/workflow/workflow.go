package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
	"github.com/nevinsm/sol/internal/store"
)

// Manifest represents a workflow's manifest.toml.
type Manifest struct {
	Name        string                  `toml:"name"`
	Type        string                  `toml:"type"`
	Description string                  `toml:"description"`
	Mode        string                  `toml:"mode"` // "inline" (default) or "manifest"
	Variables   map[string]VariableDecl `toml:"variables"`
	Vars        map[string]VariableDecl `toml:"vars"`
	Steps       []StepDef              `toml:"steps"`
}

// VariableDecl declares a workflow variable.
type VariableDecl struct {
	Required    bool   `toml:"required"`
	Default     string `toml:"default"`
	Description string `toml:"description"`
}

// StepDef defines a step in the workflow.
type StepDef struct {
	ID           string   `toml:"id"`
	Title        string   `toml:"title"`
	Description  string   `toml:"description"`   // inline description; instructions overrides when set
	Instructions string   `toml:"instructions"`   // relative path to .md file
	Needs        []string `toml:"needs"`           // step IDs this depends on
	Kind         string   `toml:"kind"`            // "code" (default) or "analysis"
}

// Instance holds metadata about an instantiated workflow.
type Instance struct {
	Workflow       string            `json:"workflow"`
	WritID     string            `json:"writ_id"`
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

// InstanceDir returns the path to an agent's workflow instance.
// Uses role-aware directory: outposts/{name}/ for agents, envoys/{name}/ for envoys, etc.
func InstanceDir(world, agentName, role string) string {
	return filepath.Join(config.AgentDir(world, agentName, role), ".workflow")
}

// Dir returns the path to a workflow.
// $SOL_HOME/workflows/{workflowName}/
func Dir(workflowName string) string {
	return filepath.Join(config.Home(), "workflows", workflowName)
}

// LoadManifest reads and parses a workflow's manifest.toml.
// workflowDir is the absolute path to the workflow directory.
func LoadManifest(workflowDir string) (*Manifest, error) {
	return loadManifestFile(filepath.Join(workflowDir, "manifest.toml"))
}

// loadManifestFile reads and parses a manifest TOML file at the given path.
func loadManifestFile(path string) (*Manifest, error) {
	var m Manifest
	if _, err := toml.DecodeFile(path, &m); err != nil {
		return nil, fmt.Errorf("failed to load manifest %q: %w", path, err)
	}
	// Default type to "workflow" when absent.
	if m.Type == "" {
		m.Type = "workflow"
	}
	return &m, nil
}

// loadManifestSnapshot loads the manifest from the workflow instance snapshot,
// falling back to re-resolving from disk for backward compatibility with
// instances created before snapshot support.
func loadManifestSnapshot(wfDir, workflowName, world string) (*Manifest, error) {
	snapshotPath := filepath.Join(wfDir, "source-manifest.toml")
	if _, err := os.Stat(snapshotPath); err == nil {
		return loadManifestFile(snapshotPath)
	}
	// Fallback: re-resolve from disk.
	res, err := Resolve(workflowName, config.RepoPath(world))
	if err != nil {
		return nil, err
	}
	return LoadManifest(res.Path)
}

// Validate checks that a manifest is well-formed:
// - Type is "workflow" (or empty, which defaults to "workflow")
// - All step IDs are unique
// - All "needs" references point to existing step IDs
// - No dependency cycles (DAG validation)
// - When workflowDir is provided, instructions files exist on disk
// Returns an error describing the first problem found.
// The optional workflowDir parameter enables file-existence checks for
// instruction paths. When omitted, instruction paths are not validated.
func Validate(m *Manifest, workflowDir ...string) error {
	switch m.Type {
	case "", "workflow": // valid types
	case "convoy":
		return fmt.Errorf("workflow type %q is no longer supported; convert to unified workflow with [[steps]] and mode = \"manifest\"", m.Type)
	case "expansion":
		return fmt.Errorf("workflow type %q is no longer supported; convert to unified workflow with [[steps]] and mode = \"manifest\"", m.Type)
	default:
		return fmt.Errorf("unknown workflow type %q: must be workflow", m.Type)
	}

	// Validate instructions files exist when workflow directory is known.
	if len(workflowDir) > 0 && workflowDir[0] != "" {
		dir := workflowDir[0]
		for _, step := range m.Steps {
			if step.Instructions != "" {
				path := filepath.Join(dir, step.Instructions)
				if _, err := os.Stat(path); err != nil {
					return fmt.Errorf("step %q instructions file %q not found", step.ID, step.Instructions)
				}
			}
		}
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
// Supports both [variables] and [vars] sections in the manifest.
func ResolveVariables(m *Manifest, provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string)

	// Start with provided values.
	for k, v := range provided {
		resolved[k] = v
	}

	// Merge [variables] and [vars] declarations. [vars] entries take
	// precedence if both sections declare the same key.
	merged := make(map[string]VariableDecl)
	for name, decl := range m.Variables {
		merged[name] = decl
	}
	for name, decl := range m.Vars {
		merged[name] = decl
	}

	// Apply defaults and check required.
	for name, decl := range merged {
		if _, ok := resolved[name]; ok {
			continue
		}
		if decl.Required {
			return nil, fmt.Errorf("required variable %q not provided", name)
		}
		resolved[name] = decl.Default
	}

	return resolved, nil
}

// unresolvedVarRe matches any remaining {{...}} tokens after substitution.
var unresolvedVarRe = regexp.MustCompile(`\{\{[^}]+\}\}`)

// RenderStepInstructions reads a step's instruction file and performs
// variable substitution. Variables use {{variable}} syntax.
// Returns the rendered markdown string, or an error if any {{variable}}
// tokens remain unresolved after substitution.
func RenderStepInstructions(workflowDir string, step StepDef, vars map[string]string) (string, error) {
	path := filepath.Join(workflowDir, step.Instructions)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read step instructions %q: %w", path, err)
	}

	content := string(data)
	for k, v := range vars {
		content = strings.ReplaceAll(content, "{{"+k+"}}", v)
	}

	if unresolved := unresolvedVarRe.FindAllString(content, -1); len(unresolved) > 0 {
		return "", fmt.Errorf("step %q has unresolved variables: %s", step.ID, strings.Join(unresolved, ", "))
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
func Instantiate(world, agentName, role, workflowName string,
	vars map[string]string) (*Instance, *State, error) {

	// Ensure workflow exists (extract from embedded defaults if needed).
	res, err := Resolve(workflowName, config.RepoPath(world))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve workflow %q: %w", workflowName, err)
	}

	// Load and validate manifest.
	m, err := LoadManifest(res.Path)
	if err != nil {
		return nil, nil, err
	}
	if err := Validate(m, res.Path); err != nil {
		return nil, nil, fmt.Errorf("invalid workflow %q: %w", workflowName, err)
	}

	// Resolve variables.
	resolved, err := ResolveVariables(m, vars)
	if err != nil {
		return nil, nil, err
	}

	// Create .workflow/ directory.
	wfDir := InstanceDir(world, agentName, role)
	stepsDir := filepath.Join(wfDir, "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("failed to create workflow directory: %w", err)
	}

	// Rollback on error after directory creation.
	rollback := func() {
		os.RemoveAll(wfDir)
	}

	// Snapshot the manifest to the workflow directory so that Advance uses
	// the version the workflow was started with, not whatever is on disk later.
	srcManifestPath := filepath.Join(res.Path, "manifest.toml")
	srcManifestData, err := os.ReadFile(srcManifestPath)
	if err != nil {
		rollback()
		return nil, nil, fmt.Errorf("failed to read manifest for snapshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, "source-manifest.toml"), srcManifestData, 0o644); err != nil {
		rollback()
		return nil, nil, fmt.Errorf("failed to snapshot manifest: %w", err)
	}

	// Build instance.
	inst := &Instance{
		Workflow:       workflowName,
		WritID:     resolved["issue"],
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
	path := filepath.Join(InstanceDir(world, agentName, role), "state.json")
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

	stepPath := filepath.Join(InstanceDir(world, agentName, role), "steps", state.CurrentStep+".json")
	return readStepFile(stepPath)
}

// ReadInstance reads the workflow instance metadata.
func ReadInstance(world, agentName, role string) (*Instance, error) {
	path := filepath.Join(InstanceDir(world, agentName, role), "manifest.json")
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
	wfDir := InstanceDir(world, agentName, role)

	// Read the instance to get the workflow and load the manifest for step order.
	inst, err := ReadInstance(world, agentName, role)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, nil
	}

	m, err := loadManifestSnapshot(wfDir, inst.Workflow, world)
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
	wfDir := InstanceDir(world, agentName, role)

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
	// If the step is already complete (e.g., from a crash recovery), ensure it is recorded in
	// state.Completed so NextReadySteps does not return it again (crash-recovery idempotency).
	if currentStep.Status == "complete" {
		if !slices.Contains(state.Completed, state.CurrentStep) {
			state.Completed = append(state.Completed, state.CurrentStep)
		}
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

	// Load manifest from snapshot to determine next ready steps.
	inst, err := ReadInstance(world, agentName, role)
	if err != nil {
		return nil, false, err
	}
	m, err := loadManifestSnapshot(wfDir, inst.Workflow, world)
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

// Skip marks the current step as skipped and finds the next ready step.
// Skipped steps are treated as completed for DAG purposes — they don't block dependents.
func Skip(world, agentName, role string) (nextStep *Step, done bool, err error) {
	wfDir := InstanceDir(world, agentName, role)

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
		return nil, false, fmt.Errorf("no current step to skip")
	}

	// Mark current step as skipped.
	stepPath := filepath.Join(wfDir, "steps", state.CurrentStep+".json")
	currentStep, err := readStepFile(stepPath)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	currentStep.Status = "skipped"
	currentStep.CompletedAt = &now
	if err := writeJSON(stepPath, currentStep); err != nil {
		return nil, false, fmt.Errorf("failed to write step %q: %w", state.CurrentStep, err)
	}

	// Add to completed list (skipped steps don't block dependents).
	state.Completed = append(state.Completed, state.CurrentStep)

	// Load manifest from snapshot to determine next ready steps.
	inst, err := ReadInstance(world, agentName, role)
	if err != nil {
		return nil, false, err
	}
	m, err := loadManifestSnapshot(wfDir, inst.Workflow, world)
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

// Fail marks the current step as failed and the workflow as failed.
// Does not advance to the next step — execution stops.
func Fail(world, agentName, role string) (failedStep *Step, err error) {
	wfDir := InstanceDir(world, agentName, role)

	// Read state.
	state, err := ReadState(world, agentName, role)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("no workflow found for agent %q in world %q", agentName, world)
	}
	if state.Status != "running" {
		return nil, fmt.Errorf("workflow status is %q, expected \"running\"", state.Status)
	}
	if state.CurrentStep == "" {
		return nil, fmt.Errorf("no current step to fail")
	}

	// Mark current step as failed.
	stepPath := filepath.Join(wfDir, "steps", state.CurrentStep+".json")
	currentStep, err := readStepFile(stepPath)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	currentStep.Status = "failed"
	currentStep.CompletedAt = &now
	if err := writeJSON(stepPath, currentStep); err != nil {
		return nil, fmt.Errorf("failed to write step %q: %w", state.CurrentStep, err)
	}

	// Mark workflow as failed. Do NOT add step to Completed or advance.
	state.Status = "failed"
	state.CompletedAt = &now
	if err := writeJSON(filepath.Join(wfDir, "state.json"), state); err != nil {
		return nil, fmt.Errorf("failed to write state.json: %w", err)
	}

	return currentStep, nil
}

// Remove deletes a workflow instance directory.
func Remove(world, agentName, role string) error {
	wfDir := InstanceDir(world, agentName, role)
	if err := os.RemoveAll(wfDir); err != nil {
		return fmt.Errorf("failed to remove workflow directory: %w", err)
	}
	return nil
}

// writeJSON marshals v to JSON and writes it to path atomically.
func writeJSON(path string, v any) error {
	return fileutil.AtomicWriteJSON(path, v, 0o644)
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

// ManifestResult holds the output of manifesting a workflow into writs.
type ManifestResult struct {
	CaravanID string            `json:"caravan_id"`
	ParentID  string            `json:"parent_id"`
	ChildIDs  map[string]string `json:"child_ids"` // step/template ID → writ ID
	Phases    map[string]int    `json:"phases"`     // step/template ID → phase number
}

// ManifestOpts holds parameters for Manifest.
type ManifestOpts struct {
	Name  string
	World       string
	ParentID    string // if empty, a parent writ is created
	Variables   map[string]string
	CreatedBy   string
}

// ShouldManifest returns true if the workflow should be manifested.
// Workflows manifest when mode is set to "manifest".
func ShouldManifest(m *Manifest) bool {
	return m.Mode == "manifest"
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

// phaseable adapts workflow items for ComputePhases.
type phaseable struct {
	id    string
	needs []string
}

func (p phaseable) dagID() string      { return p.id }
func (p phaseable) dagNeeds() []string { return p.needs }

// Manifest materializes a workflow into child writs with a caravan.
// Each step becomes a child writ. Dependencies between children mirror
// the workflow's DAG. Children are grouped in a caravan with phases
// derived from dependency depth.
func Materialize(worldStore *store.WorldStore, sphereStore *store.SphereStore, opts ManifestOpts) (*ManifestResult, error) {
	// Load workflow.
	res, err := Resolve(opts.Name, config.RepoPath(opts.World))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workflow %q: %w", opts.Name, err)
	}

	m, err := LoadManifest(res.Path)
	if err != nil {
		return nil, err
	}
	if err := Validate(m, res.Path); err != nil {
		return nil, fmt.Errorf("invalid workflow %q: %w", opts.Name, err)
	}

	if !ShouldManifest(m) {
		return nil, fmt.Errorf("workflow %q is not configured for manifestation (set mode = \"manifest\")", opts.Name)
	}

	vars := opts.Variables
	if vars == nil {
		vars = make(map[string]string)
	}

	parentID := opts.ParentID

	// Load target writ and auto-populate target variables.
	// When a ParentID is provided and the writ exists, populate
	// {{target.title}}, {{target.description}}, and {{target.id}}.
	if parentID != "" {
		// Inject ParentID as the "target" variable so that
		// [vars] target = { required = true } declarations are satisfied.
		if _, ok := vars["target"]; !ok {
			vars["target"] = parentID
		}

		target, err := worldStore.GetWrit(parentID)
		if err != nil {
			return nil, fmt.Errorf("failed to get target writ %q: %w", parentID, err)
		}
		// Auto-populate target variables for standard {{variable}} substitution.
		vars["target.title"] = target.Title
		vars["target.description"] = target.Description
		vars["target.id"] = target.ID
	}

	// Resolve variables (merges with defaults, checks required).
	resolved, err := ResolveVariables(m, vars)
	if err != nil {
		return nil, err
	}

	// Create parent writ if not provided.
	if parentID == "" {
		parentID, err = worldStore.CreateWrit(
			m.Name+": "+resolved["issue"],
			m.Description,
			opts.CreatedBy,
			0,
			[]string{"manifest"},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create parent writ: %w", err)
		}
	}

	// Build child items from steps.
	type childDef struct {
		itemID      string
		title       string
		description string
		needs       []string
		labels      []string // additional labels beyond "manifest-child"
		kind        string   // "code" (default) or "analysis"
	}

	var children []childDef

	for _, step := range m.Steps {
		title := step.Title
		for k, v := range resolved {
			title = strings.ReplaceAll(title, "{{"+k+"}}", v)
		}
		var desc string
		if step.Instructions != "" {
			rendered, err := RenderStepInstructions(res.Path, step, resolved)
			if err != nil {
				return nil, fmt.Errorf("failed to render step %q instructions: %w", step.ID, err)
			}
			desc = rendered
		} else {
			// Use description field with variable substitution.
			desc = step.Description
			for k, v := range resolved {
				desc = strings.ReplaceAll(desc, "{{"+k+"}}", v)
			}
		}
		children = append(children, childDef{
			itemID:      step.ID,
			title:       title,
			description: desc,
			needs:       step.Needs,
			kind:        step.Kind,
		})
	}

	// Compute phases from the DAG.
	phaseItems := make([]phaseable, len(children))
	for i, c := range children {
		phaseItems[i] = phaseable{id: c.itemID, needs: c.needs}
	}
	phases := ComputePhases(phaseItems)

	// Sort children in topological order (by phase, preserving declaration order
	// within the same phase) so that dependency writs are created before their
	// dependents. This ensures childIDs[need] is populated when enriching
	// step descriptions with dependency writ IDs.
	slices.SortStableFunc(children, func(a, b childDef) int {
		return phases[a.itemID] - phases[b.itemID]
	})

	// Create child writs.
	childIDs := make(map[string]string, len(children))
	for i, c := range children {
		labels := append([]string{"manifest-child"}, c.labels...)

		desc := c.description

		// DAG enrichment: for manifested steps with dependencies, inject
		// dependency writ information into the description.
		if len(c.needs) > 0 {
			var depRefs strings.Builder
			depRefs.WriteString("\n\n## Dependency Writs\n")
			for _, need := range c.needs {
				depWritID := childIDs[need]
				// Find the child def for this dependency to get its kind.
				var depKind string
				for _, dc := range children {
					if dc.itemID == need {
						depKind = dc.kind
						break
					}
				}
				if depKind == "" {
					depKind = "code"
				}
				// Find the title of the dependency.
				var depTitle string
				for _, dc := range children {
					if dc.itemID == need {
						depTitle = dc.title
						break
					}
				}
				depRefs.WriteString(fmt.Sprintf("- **%s** (%s)", depTitle, depWritID))
				if depKind == "analysis" {
					depRefs.WriteString(fmt.Sprintf(": output at `%s`", config.WritOutputDir(opts.World, depWritID)))
				} else {
					depRefs.WriteString(": branch merged to target")
				}
				depRefs.WriteString("\n")
			}
			desc += depRefs.String()
			children[i].description = desc
		}

		id, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
			Title:       c.title,
			Description: desc,
			CreatedBy:   opts.CreatedBy,
			ParentID:    parentID,
			Labels:      labels,
			Kind:        c.kind,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create child writ for %q: %w", c.itemID, err)
		}
		childIDs[c.itemID] = id
	}

	// Add dependencies between children mirroring the DAG.
	for _, c := range children {
		childID := childIDs[c.itemID]
		for _, need := range c.needs {
			depID, ok := childIDs[need]
			if !ok {
				return nil, fmt.Errorf("child %q references unknown dependency %q", c.itemID, need)
			}
			if err := worldStore.AddDependency(childID, depID); err != nil {
				return nil, fmt.Errorf("failed to add dependency %q → %q: %w", c.itemID, need, err)
			}
		}
	}

	// Create caravan and add children.
	caravanName := opts.Name
	if opts.ParentID != "" {
		caravanName += ":" + opts.ParentID
	} else {
		caravanName += ":" + parentID
	}
	caravanID, err := sphereStore.CreateCaravan(caravanName, opts.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to create caravan: %w", err)
	}

	for itemID, writID := range childIDs {
		phase := phases[itemID]
		if err := sphereStore.CreateCaravanItem(caravanID, writID, opts.World, phase); err != nil {
			return nil, fmt.Errorf("failed to add item %q to caravan: %w", itemID, err)
		}
	}

	result := &ManifestResult{
		CaravanID: caravanID,
		ParentID:  parentID,
		ChildIDs:  childIDs,
		Phases:    phases,
	}

	return result, nil
}

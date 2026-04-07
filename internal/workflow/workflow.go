package workflow

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// MaterializeWorldStore is the subset of *store.WorldStore that Materialize
// requires. Defining it as an interface lets tests inject faults without
// having to spin up a real SQLite-backed store.
type MaterializeWorldStore interface {
	GetWrit(id string) (*store.Writ, error)
	CreateWritWithOpts(opts store.CreateWritOpts) (string, error)
	AddDependency(fromID, toID string) error
	CloseWrit(id string, closeReason ...string) ([]string, error)
}

// MaterializeSphereStore is the subset of *store.SphereStore that Materialize
// requires.
type MaterializeSphereStore interface {
	CreateCaravan(name, owner string) (string, error)
	CreateCaravanItem(caravanID, writID, world string, phase int) error
	DeleteCaravan(id string) error
}

// Manifest represents a workflow's manifest.toml.
type Manifest struct {
	Name        string                  `toml:"name"`
	Type        string                  `toml:"type"`
	Description string                  `toml:"description"`
	Mode        string                  `toml:"mode"` // "manifest" (only supported mode)
	Variables   map[string]VariableDecl `toml:"variables"`
	Vars        map[string]VariableDecl `toml:"vars"` // Deprecated alias; kept for backward compatibility with existing manifests.
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

	switch m.Mode {
	case "", "manifest": // valid modes ("" defaults to manifest)
	default:
		return fmt.Errorf("unknown workflow mode %q: must be manifest", m.Mode)
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

	// Merge [variables] and [vars] declarations. Both section names are
	// accepted for backward compatibility; [variables] is the canonical name.
	// [vars] entries take precedence if both sections declare the same key.
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

// unresolvedVarRe matches remaining workflow variable tokens (e.g. {{issue}},
// {{target.title}}) after substitution. Only matches the specific workflow
// variable format (identifier with optional dot-separated segments), so Go
// template syntax like {{.Name}} or {{range .Items}} is not flagged.
var unresolvedVarRe = regexp.MustCompile(`\{\{[a-zA-Z_]\w*(?:\.\w+)*\}\}`)

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
	ParentID    string // if empty, children have no parent; if set, used as parent for all children
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
func Materialize(worldStore MaterializeWorldStore, sphereStore MaterializeSphereStore, opts ManifestOpts) (result *ManifestResult, err error) {
	// Track created entities so we can roll them back on partial failure.
	// Any error returned from this function (including from the deferred
	// rollback chain itself) leaves the system in a clean state: zero
	// orphan writs and zero orphan caravan rows.
	var createdWritIDs []string
	var createdCaravanID string
	defer func() {
		if err == nil {
			return
		}
		// Best-effort rollback. Errors are logged but do not override the
		// original failure: the caller's view of "what went wrong" should
		// be the original error, not a rollback artifact.
		for _, id := range createdWritIDs {
			if _, rbErr := worldStore.CloseWrit(id, "materialize-failed"); rbErr != nil {
				slog.Error("materialize rollback: failed to close writ",
					"writ_id", id, "err", rbErr)
			}
		}
		if createdCaravanID != "" {
			if rbErr := sphereStore.DeleteCaravan(createdCaravanID); rbErr != nil {
				slog.Error("materialize rollback: failed to delete caravan",
					"caravan_id", createdCaravanID, "err", rbErr)
			}
		}
	}()

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
		createdWritIDs = append(createdWritIDs, id)
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
	if parentID != "" {
		caravanName += ":" + parentID
	} else {
		caravanName += ":" + opts.World + ":" + fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	caravanID, err := sphereStore.CreateCaravan(caravanName, opts.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("failed to create caravan: %w", err)
	}
	createdCaravanID = caravanID

	for itemID, writID := range childIDs {
		phase := phases[itemID]
		if err := sphereStore.CreateCaravanItem(caravanID, writID, opts.World, phase); err != nil {
			return nil, fmt.Errorf("failed to add item %q to caravan: %w", itemID, err)
		}
	}

	result = &ManifestResult{
		CaravanID: caravanID,
		ParentID:  parentID,
		ChildIDs:  childIDs,
		Phases:    phases,
	}

	return result, nil
}

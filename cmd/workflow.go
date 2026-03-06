package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/workflow"
	"github.com/spf13/cobra"
)

var wfVars []string

var workflowCmd = &cobra.Command{
	Use:     "workflow",
	Short:   "Manage workflow instances",
	GroupID: groupWorkItems,
}

var workflowInstantiateCmd = &cobra.Command{
	Use:          "instantiate <formula>",
	Short:        "Instantiate a workflow from a formula",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agent, _ := cmd.Flags().GetString("agent")
		item, _ := cmd.Flags().GetString("item")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		formula := args[0]

		// Parse --var flags into map.
		vars, err := parseVarFlags(wfVars)
		if err != nil {
			return err
		}

		if item != "" {
			vars["issue"] = item
		}

		inst, state, err := workflow.Instantiate(world, agent, "agent", formula, vars)
		if err != nil {
			return err
		}

		fmt.Printf("Workflow instantiated: %s for %s (step: %s)\n",
			inst.Formula, item, state.CurrentStep)
		return nil
	},
}

var workflowCurrentCmd = &cobra.Command{
	Use:          "current",
	Short:        "Print the current step's instructions",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agent, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		step, err := workflow.ReadCurrentStep(world, agent, "agent")
		if err != nil {
			return err
		}
		if step == nil {
			fmt.Fprintln(os.Stderr, "No active workflow step.")
			return &exitError{code: 1}
		}

		fmt.Print(step.Instructions)
		return nil
	},
}

var workflowAdvanceCmd = &cobra.Command{
	Use:          "advance",
	Short:        "Advance to the next workflow step",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agent, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		// Read work item ID for event payload before advancing.
		inst, _ := workflow.ReadInstance(world, agent, "agent")
		workItemID := ""
		if inst != nil {
			workItemID = inst.WorkItemID
		}

		nextStep, done, err := workflow.Advance(world, agent, "agent")
		if err != nil {
			return err
		}

		logger := events.NewLogger(config.Home())
		if done {
			logger.Emit(events.EventWorkflowComplete, "sol", agent, "both", map[string]string{
				"work_item_id": workItemID,
				"agent":        agent,
				"world":        world,
			})
			fmt.Println("Workflow complete.")
			return nil
		}

		logger.Emit(events.EventWorkflowAdvance, "sol", agent, "both", map[string]string{
			"work_item_id": workItemID,
			"step":         nextStep.Title,
			"step_id":      nextStep.ID,
			"agent":        agent,
			"world":        world,
		})
		fmt.Printf("Advanced to step: %s\n", nextStep.Title)
		return nil
	},
}

var workflowStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show workflow status",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agent, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		inst, err := workflow.ReadInstance(world, agent, "agent")
		if err != nil {
			return err
		}
		if inst == nil {
			return fmt.Errorf("no workflow found for agent %q in world %q", agent, world)
		}

		state, err := workflow.ReadState(world, agent, "agent")
		if err != nil {
			return err
		}

		steps, err := workflow.ListSteps(world, agent, "agent")
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			out := struct {
				Formula        string   `json:"formula"`
				WorkItemID     string   `json:"work_item_id"`
				Status         string   `json:"status"`
				CurrentStep    string   `json:"current_step"`
				Completed      []string `json:"completed"`
				TotalSteps     int      `json:"total_steps"`
				CompletedCount int      `json:"completed_count"`
			}{
				Formula:        inst.Formula,
				WorkItemID:     inst.WorkItemID,
				Status:         state.Status,
				CurrentStep:    state.CurrentStep,
				Completed:      state.Completed,
				TotalSteps:     len(steps),
				CompletedCount: len(state.Completed),
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(out)
		}

		// Human-readable output.
		fmt.Printf("Workflow: %s (%s)\n", inst.Formula, inst.WorkItemID)
		fmt.Printf("Status: %s\n", state.Status)
		fmt.Printf("Progress: %d/%d steps complete\n", len(state.Completed), len(steps))
		fmt.Println()
		fmt.Println("Steps:")
		for _, s := range steps {
			var marker string
			switch s.Status {
			case "complete":
				marker = "[x]"
			case "executing":
				marker = "[>]"
			default:
				marker = "[ ]"
			}
			line := fmt.Sprintf("  %s %s — %s", marker, s.ID, s.Title)
			if s.Status == "executing" {
				line += " (current)"
			}
			fmt.Println(line)
		}

		return nil
	},
}

var workflowShowCmd = &cobra.Command{
	Use:          "show <formula>",
	Short:        "Display formula details and resolution source",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		formulaName := args[0]

		// Resolve world for project-tier lookup (optional).
		var repoPath string
		worldFlag, _ := cmd.Flags().GetString("world")
		if worldFlag != "" || os.Getenv("SOL_WORLD") != "" {
			world, err := config.ResolveWorld(worldFlag)
			if err != nil {
				return err
			}
			repoPath = config.RepoPath(world)
		}

		res, err := workflow.EnsureFormula(formulaName, repoPath)
		if err != nil {
			return err
		}

		m, err := workflow.LoadManifest(res.Path)
		if err != nil {
			return err
		}

		validationErr := workflow.Validate(m)

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printShowJSON(m, res, validationErr)
		}

		return printShowHuman(m, res, validationErr)
	},
}

func printShowJSON(m *workflow.Manifest, res *workflow.FormulaResolution, validationErr error) error {
	type varJSON struct {
		Required bool   `json:"required"`
		Default  string `json:"default,omitempty"`
	}
	type stepJSON struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Instructions string   `json:"instructions"`
		Needs        []string `json:"needs,omitempty"`
	}
	type templateJSON struct {
		ID          string   `json:"id"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Needs       []string `json:"needs,omitempty"`
	}
	type legJSON struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Focus       string `json:"focus,omitempty"`
	}
	type synthesisJSON struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		DependsOn   []string `json:"depends_on"`
	}
	type output struct {
		Name        string                `json:"name"`
		Type        string                `json:"type"`
		Description string                `json:"description,omitempty"`
		Manifest    bool                  `json:"manifest"`
		Tier        workflow.FormulaTier   `json:"tier"`
		Path        string                `json:"path"`
		Valid       bool                  `json:"valid"`
		Error       string                `json:"error,omitempty"`
		Variables   map[string]varJSON    `json:"variables,omitempty"`
		Steps       []stepJSON            `json:"steps,omitempty"`
		Templates   []templateJSON        `json:"templates,omitempty"`
		Legs        []legJSON             `json:"legs,omitempty"`
		Synthesis   *synthesisJSON        `json:"synthesis,omitempty"`
	}

	out := output{
		Name:        m.Name,
		Type:        m.Type,
		Description: m.Description,
		Manifest:    m.Manifest,
		Tier:        res.Tier,
		Path:        res.Path,
		Valid:       validationErr == nil,
	}
	if validationErr != nil {
		out.Error = validationErr.Error()
	}
	if len(m.Variables) > 0 {
		out.Variables = make(map[string]varJSON, len(m.Variables))
		for k, v := range m.Variables {
			out.Variables[k] = varJSON{Required: v.Required, Default: v.Default}
		}
	}
	for _, s := range m.Steps {
		out.Steps = append(out.Steps, stepJSON{ID: s.ID, Title: s.Title, Instructions: s.Instructions, Needs: s.Needs})
	}
	for _, t := range m.Templates {
		out.Templates = append(out.Templates, templateJSON{ID: t.ID, Title: t.Title, Description: t.Description, Needs: t.Needs})
	}
	for _, l := range m.Legs {
		out.Legs = append(out.Legs, legJSON{ID: l.ID, Title: l.Title, Description: l.Description, Focus: l.Focus})
	}
	if m.Synth != nil {
		out.Synthesis = &synthesisJSON{Title: m.Synth.Title, Description: m.Synth.Description, DependsOn: m.Synth.DependsOn}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printShowHuman(m *workflow.Manifest, res *workflow.FormulaResolution, validationErr error) error {
	formulaType := m.Type
	if formulaType == "" {
		formulaType = "workflow"
	}

	fmt.Printf("Name:        %s\n", m.Name)
	fmt.Printf("Type:        %s\n", formulaType)
	fmt.Printf("Tier:        %s\n", res.Tier)
	fmt.Printf("Path:        %s\n", res.Path)
	if m.Description != "" {
		fmt.Printf("Description: %s\n", m.Description)
	}
	if m.Manifest {
		fmt.Printf("Manifest:    true\n")
	}
	if validationErr != nil {
		fmt.Printf("Validation:  INVALID — %s\n", validationErr)
	}

	// Variables.
	if len(m.Variables) > 0 {
		fmt.Println()
		fmt.Println("Variables:")
		// Sort for stable output.
		varNames := make([]string, 0, len(m.Variables))
		for k := range m.Variables {
			varNames = append(varNames, k)
		}
		sort.Strings(varNames)
		for _, name := range varNames {
			decl := m.Variables[name]
			var attrs []string
			if decl.Required {
				attrs = append(attrs, "required")
			}
			if decl.Default != "" {
				attrs = append(attrs, fmt.Sprintf("default=%q", decl.Default))
			}
			if len(attrs) > 0 {
				fmt.Printf("  %s (%s)\n", name, strings.Join(attrs, ", "))
			} else {
				fmt.Printf("  %s\n", name)
			}
		}
	}

	// Steps (workflow type).
	if len(m.Steps) > 0 {
		fmt.Println()
		fmt.Println("Steps:")
		for i, s := range m.Steps {
			line := fmt.Sprintf("  %d. %s — %s", i+1, s.ID, s.Title)
			if len(s.Needs) > 0 {
				line += fmt.Sprintf(" (needs: %s)", strings.Join(s.Needs, ", "))
			}
			fmt.Println(line)
		}
	}

	// Templates (expansion type).
	if len(m.Templates) > 0 {
		fmt.Println()
		fmt.Println("Templates:")
		for i, t := range m.Templates {
			line := fmt.Sprintf("  %d. %s — %s", i+1, t.ID, t.Title)
			if len(t.Needs) > 0 {
				line += fmt.Sprintf(" (needs: %s)", strings.Join(t.Needs, ", "))
			}
			fmt.Println(line)
		}
	}

	// Legs (convoy type).
	if len(m.Legs) > 0 {
		fmt.Println()
		fmt.Println("Legs:")
		for i, l := range m.Legs {
			fmt.Printf("  %d. %s — %s\n", i+1, l.ID, l.Title)
		}
		if m.Synth != nil {
			fmt.Printf("\nSynthesis: %s\n", m.Synth.Title)
			if len(m.Synth.DependsOn) > 0 {
				fmt.Printf("  depends on: %s\n", strings.Join(m.Synth.DependsOn, ", "))
			}
		}
	}

	return nil
}

var workflowManifestCmd = &cobra.Command{
	Use:          "manifest <formula>",
	Short:        "Manifest a formula into work items and a caravan",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		formula := args[0]

		vars, err := parseVarFlags(wfVars)
		if err != nil {
			return err
		}

		target, _ := cmd.Flags().GetString("target")

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		result, err := workflow.ManifestFormula(worldStore, sphereStore, workflow.ManifestOpts{
			FormulaName: formula,
			World:       world,
			ParentID:    target,
			Variables:   vars,
			CreatedBy:   "operator",
		})
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		fmt.Printf("Caravan: %s\n", result.CaravanID)
		fmt.Printf("Parent:  %s\n", result.ParentID)
		fmt.Printf("Items:   %d\n", len(result.ChildIDs))

		// Sort by phase for readable output.
		type entry struct {
			stepID     string
			workItemID string
			phase      int
		}
		entries := make([]entry, 0, len(result.ChildIDs))
		for stepID, itemID := range result.ChildIDs {
			entries = append(entries, entry{stepID, itemID, result.Phases[stepID]})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].phase != entries[j].phase {
				return entries[i].phase < entries[j].phase
			}
			return entries[i].stepID < entries[j].stepID
		})

		fmt.Println()
		for _, e := range entries {
			fmt.Printf("  phase %d: %s → %s\n", e.phase, e.stepID, e.workItemID)
		}

		return nil
	},
}

var workflowListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List available workflow formulas",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		showAll, _ := cmd.Flags().GetBool("all")
		jsonOut, _ := cmd.Flags().GetBool("json")

		var repoPath string
		if worldFlag != "" {
			// Explicit world requested — fail if invalid.
			world, err := config.ResolveWorld(worldFlag)
			if err != nil {
				return err
			}
			repoPath = config.RepoPath(world)
		} else {
			// Try to infer world for project-tier scanning; skip if unavailable.
			if world, err := config.ResolveWorld(""); err == nil {
				repoPath = config.RepoPath(world)
			}
		}

		entries, err := workflow.ListFormulas(repoPath)
		if err != nil {
			return err
		}

		// Filter shadowed entries unless --all.
		if !showAll {
			filtered := []workflow.FormulaEntry{}
			for _, e := range entries {
				if !e.Shadowed {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(entries)
		}

		if len(entries) == 0 {
			fmt.Println("No formulas found.")
			return nil
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tTYPE\tTIER\tDESCRIPTION\n")
		for _, e := range entries {
			name := e.Name
			if e.Shadowed {
				name += " (shadowed)"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, e.Type, e.Tier, e.Description)
		}
		tw.Flush()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(workflowCmd)
	workflowCmd.AddCommand(workflowInstantiateCmd)
	workflowCmd.AddCommand(workflowCurrentCmd)
	workflowCmd.AddCommand(workflowAdvanceCmd)
	workflowCmd.AddCommand(workflowStatusCmd)
	workflowCmd.AddCommand(workflowManifestCmd)
	workflowCmd.AddCommand(workflowShowCmd)
	workflowCmd.AddCommand(workflowListCmd)

	// show flags
	workflowShowCmd.Flags().String("world", "", "world name (for project-tier resolution)")
	workflowShowCmd.Flags().Bool("json", false, "output as JSON")

	// instantiate flags
	workflowInstantiateCmd.Flags().String("item", "", "work item ID")
	workflowInstantiateCmd.Flags().String("world", "", "world name (optional with SOL_WORLD or inside a world directory)")
	workflowInstantiateCmd.Flags().String("agent", "", "agent name")
	workflowInstantiateCmd.Flags().StringSliceVar(&wfVars, "var", nil, "variable assignment (key=val)")
	workflowInstantiateCmd.MarkFlagRequired("item")
	workflowInstantiateCmd.MarkFlagRequired("agent")

	// current flags
	workflowCurrentCmd.Flags().String("world", "", "world name (optional with SOL_WORLD or inside a world directory)")
	workflowCurrentCmd.Flags().String("agent", "", "agent name")
	workflowCurrentCmd.MarkFlagRequired("agent")

	// advance flags
	workflowAdvanceCmd.Flags().String("world", "", "world name (optional with SOL_WORLD or inside a world directory)")
	workflowAdvanceCmd.Flags().String("agent", "", "agent name")
	workflowAdvanceCmd.MarkFlagRequired("agent")

	// status flags
	workflowStatusCmd.Flags().String("world", "", "world name (optional with SOL_WORLD or inside a world directory)")
	workflowStatusCmd.Flags().String("agent", "", "agent name")
	workflowStatusCmd.Flags().Bool("json", false, "output as JSON")
	workflowStatusCmd.MarkFlagRequired("agent")

	// manifest flags
	workflowManifestCmd.Flags().String("world", "", "world name (optional with SOL_WORLD or inside a world directory)")
	workflowManifestCmd.Flags().StringSliceVar(&wfVars, "var", nil, "variable assignment (key=val)")
	workflowManifestCmd.Flags().String("target", "", "existing work item ID to manifest against (required for expansion formulas)")
	workflowManifestCmd.Flags().Bool("json", false, "output as JSON")

	// list flags
	workflowListCmd.Flags().String("world", "", "world name (for project-tier discovery)")
	workflowListCmd.Flags().Bool("all", false, "show all tiers including shadowed formulas")
	workflowListCmd.Flags().Bool("json", false, "output as JSON")
}

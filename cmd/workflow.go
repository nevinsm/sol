package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

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

func init() {
	rootCmd.AddCommand(workflowCmd)
	workflowCmd.AddCommand(workflowInstantiateCmd)
	workflowCmd.AddCommand(workflowCurrentCmd)
	workflowCmd.AddCommand(workflowAdvanceCmd)
	workflowCmd.AddCommand(workflowStatusCmd)
	workflowCmd.AddCommand(workflowManifestCmd)

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
}

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nevinsm/gt/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	wfRig   string
	wfAgent string
	wfItem  string
	wfVars  []string
	wfJSON  bool
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Manage workflow instances",
}

var workflowInstantiateCmd = &cobra.Command{
	Use:   "instantiate <formula>",
	Short: "Instantiate a workflow from a formula",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		formula := args[0]

		// Parse --var flags into map.
		vars := make(map[string]string)
		for _, v := range wfVars {
			for i := range v {
				if v[i] == '=' {
					vars[v[:i]] = v[i+1:]
					break
				}
			}
		}

		inst, state, err := workflow.Instantiate(wfRig, wfAgent, formula, vars)
		if err != nil {
			return err
		}

		fmt.Printf("Workflow instantiated: %s for %s (step: %s)\n",
			inst.Formula, wfItem, state.CurrentStep)
		return nil
	},
}

var workflowCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Print the current step's instructions",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		step, err := workflow.ReadCurrentStep(wfRig, wfAgent)
		if err != nil {
			return err
		}
		if step == nil {
			fmt.Fprintln(os.Stderr, "No active workflow step.")
			os.Exit(1)
		}

		fmt.Print(step.Instructions)
		return nil
	},
}

var workflowAdvanceCmd = &cobra.Command{
	Use:   "advance",
	Short: "Advance to the next workflow step",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		nextStep, done, err := workflow.Advance(wfRig, wfAgent)
		if err != nil {
			return err
		}
		if done {
			fmt.Println("Workflow complete.")
			return nil
		}
		fmt.Printf("Advanced to step: %s\n", nextStep.Title)
		return nil
	},
}

var workflowStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workflow status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		inst, err := workflow.ReadInstance(wfRig, wfAgent)
		if err != nil {
			return err
		}
		if inst == nil {
			return fmt.Errorf("no workflow found for agent %q in rig %q", wfAgent, wfRig)
		}

		state, err := workflow.ReadState(wfRig, wfAgent)
		if err != nil {
			return err
		}

		steps, err := workflow.ListSteps(wfRig, wfAgent)
		if err != nil {
			return err
		}

		if wfJSON {
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

func init() {
	rootCmd.AddCommand(workflowCmd)
	workflowCmd.AddCommand(workflowInstantiateCmd)
	workflowCmd.AddCommand(workflowCurrentCmd)
	workflowCmd.AddCommand(workflowAdvanceCmd)
	workflowCmd.AddCommand(workflowStatusCmd)

	// instantiate flags
	workflowInstantiateCmd.Flags().StringVar(&wfItem, "item", "", "work item ID")
	workflowInstantiateCmd.Flags().StringVar(&wfRig, "rig", "", "rig name")
	workflowInstantiateCmd.Flags().StringVar(&wfAgent, "agent", "", "agent name")
	workflowInstantiateCmd.Flags().StringSliceVar(&wfVars, "var", nil, "variable assignment (key=val)")
	workflowInstantiateCmd.MarkFlagRequired("item")
	workflowInstantiateCmd.MarkFlagRequired("rig")
	workflowInstantiateCmd.MarkFlagRequired("agent")

	// current flags
	workflowCurrentCmd.Flags().StringVar(&wfRig, "rig", "", "rig name")
	workflowCurrentCmd.Flags().StringVar(&wfAgent, "agent", "", "agent name")
	workflowCurrentCmd.MarkFlagRequired("rig")
	workflowCurrentCmd.MarkFlagRequired("agent")

	// advance flags
	workflowAdvanceCmd.Flags().StringVar(&wfRig, "rig", "", "rig name")
	workflowAdvanceCmd.Flags().StringVar(&wfAgent, "agent", "", "agent name")
	workflowAdvanceCmd.MarkFlagRequired("rig")
	workflowAdvanceCmd.MarkFlagRequired("agent")

	// status flags
	workflowStatusCmd.Flags().StringVar(&wfRig, "rig", "", "rig name")
	workflowStatusCmd.Flags().StringVar(&wfAgent, "agent", "", "agent name")
	workflowStatusCmd.Flags().BoolVar(&wfJSON, "json", false, "output as JSON")
	workflowStatusCmd.MarkFlagRequired("rig")
	workflowStatusCmd.MarkFlagRequired("agent")
}

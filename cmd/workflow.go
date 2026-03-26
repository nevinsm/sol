package cmd

import (
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
	GroupID: groupWrits,
}

var workflowInstantiateCmd = &cobra.Command{
	Use:          "instantiate <workflow>",
	Short:        "Instantiate a workflow",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agentFlag, _ := cmd.Flags().GetString("agent")
		item, _ := cmd.Flags().GetString("item")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(agentFlag)
		if err != nil {
			return err
		}

		workflowName := args[0]

		// Parse --var flags into map.
		vars, err := parseVarFlags(wfVars)
		if err != nil {
			return err
		}

		if item != "" {
			vars["issue"] = item
		}

		inst, state, err := workflow.Instantiate(world, agent, "outpost", workflowName, vars)
		if err != nil {
			return err
		}

		fmt.Printf("Workflow instantiated: %s for %s (step: %s)\n",
			inst.Workflow, item, state.CurrentStep)
		return nil
	},
}

var workflowCurrentCmd = &cobra.Command{
	Use:    "current",
	Short:  "Print the current step's instructions",
	Hidden: true,
	Long: `Print the current step's instructions from the active workflow.

Used by agents to read their next step in a step-driven workflow loop.

Exit codes:
  0 - Active workflow step found
  1 - No active workflow step`,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agentFlag, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(agentFlag)
		if err != nil {
			return err
		}

		step, err := workflow.ReadCurrentStep(world, agent, "outpost")
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
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agentFlag, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(agentFlag)
		if err != nil {
			return err
		}

		// Read writ ID for event payload before advancing.
		inst, _ := workflow.ReadInstance(world, agent, "outpost")
		writID := ""
		if inst != nil {
			writID = inst.WritID
		}

		nextStep, done, err := workflow.Advance(world, agent, "outpost")
		if err != nil {
			return err
		}

		logger := events.NewLogger(config.Home())
		if done {
			logger.Emit(events.EventWorkflowComplete, "sol", agent, "both", map[string]string{
				"writ_id": writID,
				"agent":        agent,
				"world":        world,
			})
			fmt.Println("Workflow complete.")
			return nil
		}

		logger.Emit(events.EventWorkflowAdvance, "sol", agent, "both", map[string]string{
			"writ_id": writID,
			"step":         nextStep.Title,
			"step_id":      nextStep.ID,
			"agent":        agent,
			"world":        world,
		})
		fmt.Printf("Advanced to step: %s\n", nextStep.Title)
		return nil
	},
}

var workflowSkipCmd = &cobra.Command{
	Use:          "skip",
	Short:        "Skip the current workflow step and advance to the next",
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agentFlag, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(agentFlag)
		if err != nil {
			return err
		}

		// Read writ ID for event payload before skipping.
		inst, _ := workflow.ReadInstance(world, agent, "outpost")
		writID := ""
		if inst != nil {
			writID = inst.WritID
		}

		// Read current step ID before skip (it changes after).
		state, _ := workflow.ReadState(world, agent, "outpost")
		skippedStepID := ""
		if state != nil {
			skippedStepID = state.CurrentStep
		}

		nextStep, done, err := workflow.Skip(world, agent, "outpost")
		if err != nil {
			return err
		}

		logger := events.NewLogger(config.Home())
		if done {
			logger.Emit(events.EventWorkflowAdvance, "sol", agent, "both", map[string]string{
				"writ_id": writID,
				"step_id": skippedStepID,
				"skipped": "true",
				"agent":   agent,
				"world":   world,
			})
			logger.Emit(events.EventWorkflowComplete, "sol", agent, "both", map[string]string{
				"writ_id": writID,
				"agent":   agent,
				"world":   world,
			})
			fmt.Println("Step skipped. Workflow complete.")
			return nil
		}

		logger.Emit(events.EventWorkflowAdvance, "sol", agent, "both", map[string]string{
			"writ_id": writID,
			"step":    nextStep.Title,
			"step_id": nextStep.ID,
			"skipped": "true",
			"agent":   agent,
			"world":   world,
		})
		fmt.Printf("Step skipped. Advanced to step: %s\n", nextStep.Title)
		return nil
	},
}

var workflowFailCmd = &cobra.Command{
	Use:          "fail",
	Short:        "Mark the current workflow step and workflow as failed",
	Hidden:       true,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		agentFlag, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(agentFlag)
		if err != nil {
			return err
		}

		// Read writ ID for event payload.
		inst, _ := workflow.ReadInstance(world, agent, "outpost")
		writID := ""
		if inst != nil {
			writID = inst.WritID
		}

		failedStep, err := workflow.Fail(world, agent, "outpost")
		if err != nil {
			return err
		}

		logger := events.NewLogger(config.Home())
		logger.Emit(events.EventWorkflowFail, "sol", agent, "both", map[string]string{
			"writ_id": writID,
			"step":    failedStep.Title,
			"step_id": failedStep.ID,
			"agent":   agent,
			"world":   world,
		})
		fmt.Printf("Step failed: %s. Workflow marked as failed.\n", failedStep.Title)
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
		agentFlag, _ := cmd.Flags().GetString("agent")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent, err := config.ResolveAgent(agentFlag)
		if err != nil {
			return err
		}

		inst, err := workflow.ReadInstance(world, agent, "outpost")
		if err != nil {
			return err
		}
		if inst == nil {
			return fmt.Errorf("no workflow found for agent %q in world %q", agent, world)
		}

		state, err := workflow.ReadState(world, agent, "outpost")
		if err != nil {
			return err
		}

		steps, err := workflow.ListSteps(world, agent, "outpost")
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			type stepStatus struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			}
			stepStatuses := make([]stepStatus, len(steps))
			for i, s := range steps {
				stepStatuses[i] = stepStatus{ID: s.ID, Title: s.Title, Status: s.Status}
			}
			out := struct {
				Workflow       string       `json:"workflow"`
				WritID         string       `json:"writ_id"`
				Status         string       `json:"status"`
				CurrentStep    string       `json:"current_step"`
				Completed      []string     `json:"completed"`
				TotalSteps     int          `json:"total_steps"`
				CompletedCount int          `json:"completed_count"`
				Steps          []stepStatus `json:"steps"`
			}{
				Workflow:       inst.Workflow,
				WritID:         inst.WritID,
				Status:         state.Status,
				CurrentStep:    state.CurrentStep,
				Completed:      state.Completed,
				TotalSteps:     len(steps),
				CompletedCount: len(state.Completed),
				Steps:          stepStatuses,
			}
			return printJSON(out)
		}

		// Human-readable output.
		fmt.Printf("Workflow: %s (%s)\n", inst.Workflow, inst.WritID)
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
			case "skipped":
				marker = "[s]"
			case "failed":
				marker = "[!]"
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
	Use:          "show [workflow]",
	Short:        "Display workflow details and resolution source",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		pathFlag, _ := cmd.Flags().GetString("path")

		// Validate mutual exclusivity: --path or positional arg, not both.
		if pathFlag != "" && len(args) > 0 {
			return fmt.Errorf("--path and positional <workflow> argument are mutually exclusive")
		}
		if pathFlag == "" && len(args) == 0 {
			return fmt.Errorf("either a <workflow> name or --path must be provided")
		}

		var res *workflow.Resolution

		if pathFlag != "" {
			// Load from arbitrary directory path.
			res = &workflow.Resolution{
				Path: pathFlag,
				Tier: workflow.TierLocal,
			}
		} else {
			workflowName := args[0]

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

			var err error
			res, err = workflow.Resolve(workflowName, repoPath)
			if err != nil {
				return err
			}
		}

		m, err := workflow.LoadManifest(res.Path)
		if err != nil {
			return err
		}

		validationErr := workflow.Validate(m, res.Path)

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printShowJSON(m, res, validationErr)
		}

		return printShowHuman(m, res, validationErr)
	},
}

var workflowInitCmd = &cobra.Command{
	Use:          "init <name>",
	Short:        "Scaffold a new workflow",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		typeFlag, _ := cmd.Flags().GetString("type")
		projectFlag, _ := cmd.Flags().GetBool("project")
		worldFlag, _ := cmd.Flags().GetString("world")

		// Validate --project requires --world.
		var repoPath string
		if projectFlag {
			if worldFlag == "" {
				return fmt.Errorf("--project requires --world")
			}
			world, err := config.ResolveWorld(worldFlag)
			if err != nil {
				return err
			}
			repoPath = config.RepoPath(world)
		}

		dir, err := workflow.Init(name, typeFlag, repoPath, projectFlag)
		if err != nil {
			return err
		}

		fmt.Printf("Created workflow at %s. Edit manifest.toml to define your workflow, then preview with `sol workflow show %s`.\n", dir, name)
		return nil
	},
}

func printShowJSON(m *workflow.Manifest, res *workflow.Resolution, validationErr error) error {
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
	type output struct {
		Name        string                `json:"name"`
		Type        string                `json:"type"`
		Description string                `json:"description,omitempty"`
		Manifest    bool                  `json:"manifest"`
		Tier        workflow.Tier           `json:"tier"`
		Path        string                `json:"path"`
		Valid       bool                  `json:"valid"`
		Error       string                `json:"error,omitempty"`
		Variables   map[string]varJSON    `json:"variables,omitempty"`
		Steps       []stepJSON            `json:"steps,omitempty"`
	}

	out := output{
		Name:        m.Name,
		Type:        m.Type,
		Description: m.Description,
		Manifest:    m.Mode == "manifest",
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

	return printJSON(out)
}

func printShowHuman(m *workflow.Manifest, res *workflow.Resolution, validationErr error) error {
	wfType := m.Type
	if wfType == "" {
		wfType = "workflow"
	}

	fmt.Printf("Name:        %s\n", m.Name)
	fmt.Printf("Type:        %s\n", wfType)
	fmt.Printf("Tier:        %s\n", res.Tier)
	fmt.Printf("Path:        %s\n", res.Path)
	if m.Description != "" {
		fmt.Printf("Description: %s\n", m.Description)
	}
	if m.Mode == "manifest" {
		fmt.Printf("Mode:        manifest\n")
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

	return nil
}

var workflowManifestCmd = &cobra.Command{
	Use:          "manifest <workflow>",
	Short:        "Manifest a workflow into writs and a caravan",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}

		workflowName := args[0]

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

		result, err := workflow.Materialize(worldStore, sphereStore, workflow.ManifestOpts{
			Name:  workflowName,
			World:       world,
			ParentID:    target,
			Variables:   vars,
			CreatedBy:   config.Autarch,
		})
		if err != nil {
			return err
		}

		jsonOut, _ := cmd.Flags().GetBool("json")
		if jsonOut {
			return printJSON(result)
		}

		fmt.Printf("Caravan: %s\n", result.CaravanID)
		if result.ParentID != "" {
			fmt.Printf("Parent:  %s\n", result.ParentID)
		}
		fmt.Printf("Items:   %d\n", len(result.ChildIDs))

		// Sort by phase for readable output.
		type entry struct {
			stepID     string
			writID string
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
			fmt.Printf("  phase %d: %s → %s\n", e.phase, e.stepID, e.writID)
		}

		return nil
	},
}

var workflowEjectCmd = &cobra.Command{
	Use:          "eject <name>",
	Short:        "Eject an embedded workflow for customization",
	Hidden:       true,
	Long:         "Copies an embedded workflow to the user or project tier so it can be customized. Use --force to refresh from embedded defaults (backs up existing).",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		projectFlag, _ := cmd.Flags().GetBool("project")
		worldFlag, _ := cmd.Flags().GetString("world")
		forceFlag, _ := cmd.Flags().GetBool("force")

		var repoPath string
		if projectFlag {
			if worldFlag == "" {
				return fmt.Errorf("--project requires --world")
			}
			world, err := config.ResolveWorld(worldFlag)
			if err != nil {
				return err
			}
			repoPath = config.RepoPath(world)
		}

		targetDir, err := workflow.Eject(name, repoPath, forceFlag)
		if err != nil {
			return err
		}

		fmt.Printf("Ejected workflow %s to %s. Edit manifest.toml to customize.\n", name, targetDir)
		return nil
	},
}

var workflowListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List available workflows",
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

		entries, err := workflow.List(repoPath)
		if err != nil {
			return err
		}

		// Filter shadowed entries unless --all.
		if !showAll {
			filtered := []workflow.Entry{}
			for _, e := range entries {
				if !e.Shadowed {
					filtered = append(filtered, e)
				}
			}
			entries = filtered
		}

		if jsonOut {
			return printJSON(entries)
		}

		if len(entries) == 0 {
			fmt.Println("No workflows found.")
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
	workflowCmd.AddCommand(workflowSkipCmd)
	workflowCmd.AddCommand(workflowFailCmd)
	workflowCmd.AddCommand(workflowStatusCmd)
	workflowCmd.AddCommand(workflowManifestCmd)
	workflowCmd.AddCommand(workflowShowCmd)
	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowEjectCmd)
	workflowCmd.AddCommand(workflowInitCmd)

	// eject flags
	workflowEjectCmd.Flags().Bool("project", false, "eject to project tier instead of user tier (requires --world)")
	workflowEjectCmd.Flags().String("world", "", "world name")
	workflowEjectCmd.Flags().Bool("force", false, "overwrite existing workflow (backs up to {name}.bak-{timestamp})")

	// show flags
	workflowShowCmd.Flags().String("world", "", "world name")
	workflowShowCmd.Flags().Bool("json", false, "output as JSON")
	workflowShowCmd.Flags().String("path", "", "load workflow from directory path instead of by name")

	// init flags
	workflowInitCmd.Flags().String("type", "workflow", "workflow type")
	workflowInitCmd.Flags().Bool("project", false, "create in project tier (.sol/workflows/)")
	workflowInitCmd.Flags().String("world", "", "world name")

	// instantiate flags
	workflowInstantiateCmd.Flags().String("item", "", "writ ID")
	workflowInstantiateCmd.Flags().String("world", "", "world name")
	workflowInstantiateCmd.Flags().String("agent", "", "agent name (defaults to SOL_AGENT env)")
	workflowInstantiateCmd.Flags().StringSliceVar(&wfVars, "var", nil, "variable assignment (key=val)")
	workflowInstantiateCmd.MarkFlagRequired("item")

	// current flags
	workflowCurrentCmd.Flags().String("world", "", "world name")
	workflowCurrentCmd.Flags().String("agent", "", "agent name (defaults to SOL_AGENT env)")

	// advance flags
	workflowAdvanceCmd.Flags().String("world", "", "world name")
	workflowAdvanceCmd.Flags().String("agent", "", "agent name (defaults to SOL_AGENT env)")

	// skip flags
	workflowSkipCmd.Flags().String("world", "", "world name")
	workflowSkipCmd.Flags().String("agent", "", "agent name (defaults to SOL_AGENT env)")

	// fail flags
	workflowFailCmd.Flags().String("world", "", "world name")
	workflowFailCmd.Flags().String("agent", "", "agent name (defaults to SOL_AGENT env)")

	// status flags
	workflowStatusCmd.Flags().String("world", "", "world name")
	workflowStatusCmd.Flags().String("agent", "", "agent name (defaults to SOL_AGENT env)")
	workflowStatusCmd.Flags().Bool("json", false, "output as JSON")

	// manifest flags
	workflowManifestCmd.Flags().String("world", "", "world name")
	workflowManifestCmd.Flags().StringSliceVar(&wfVars, "var", nil, "variable assignment (key=val)")
	workflowManifestCmd.Flags().String("target", "", "existing writ ID to manifest against")
	workflowManifestCmd.Flags().Bool("json", false, "output as JSON")

	// list flags
	workflowListCmd.Flags().String("world", "", "world name")
	workflowListCmd.Flags().Bool("all", false, "show all tiers including shadowed workflows")
	workflowListCmd.Flags().Bool("json", false, "output as JSON")
}

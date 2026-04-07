package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/workflow"
	"github.com/spf13/cobra"
)

var wfVars []string

var workflowCmd = &cobra.Command{
	Use:     "workflow",
	Short:   "Manage workflows",
	GroupID: groupWrits,
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
	// Merge Variables and Vars (matching ResolveVariables logic).
	allVars := make(map[string]workflow.VariableDecl, len(m.Variables)+len(m.Vars))
	for k, v := range m.Variables {
		allVars[k] = v
	}
	for k, v := range m.Vars {
		allVars[k] = v
	}
	if len(allVars) > 0 {
		out.Variables = make(map[string]varJSON, len(allVars))
		for k, v := range allVars {
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

	// Variables — merge Variables and Vars (matching ResolveVariables logic).
	allVars := make(map[string]workflow.VariableDecl, len(m.Variables)+len(m.Vars))
	for k, v := range m.Variables {
		allVars[k] = v
	}
	for k, v := range m.Vars {
		allVars[k] = v
	}
	if len(allVars) > 0 {
		fmt.Println()
		fmt.Println("Variables:")
		// Sort for stable output.
		varNames := make([]string, 0, len(allVars))
		for k := range allVars {
			varNames = append(varNames, k)
		}
		sort.Strings(varNames)
		for _, name := range varNames {
			decl := allVars[name]
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
	Use:    "eject <name>",
	Short:  "Eject an embedded workflow for customization",
	Hidden: true,
	Long: `Copies an embedded workflow to the user or project tier so it can be customized.

If the target directory already exists, this is a destructive operation
(the existing directory — including any hand edits — is renamed to
{name}.bak-{timestamp} before the fresh copy is extracted). In that case
--confirm is required; without it the command previews what would be
overwritten and exits 1.

Exit codes:
  0 - Workflow ejected (or would be ejected, if target did not yet exist)
  1 - Preview only (target exists but --confirm not provided), or error`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		projectFlag, _ := cmd.Flags().GetBool("project")
		worldFlag, _ := cmd.Flags().GetString("world")
		confirmFlag, _ := cmd.Flags().GetBool("confirm")

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

		// Compute target directory so we can detect the destructive overwrite case.
		var targetDir string
		if repoPath != "" {
			targetDir = workflow.ProjectDir(repoPath, name)
		} else {
			targetDir = workflow.Dir(name)
		}

		// Destructive path: target already exists.
		if info, err := os.Stat(targetDir); err == nil && info.IsDir() {
			if !confirmFlag {
				fmt.Printf("Workflow %q is already ejected at %s.\n", name, targetDir)
				fmt.Println("Re-ejecting would overwrite the existing directory, backing it up to:")
				fmt.Printf("  %s.bak-<timestamp>\n", targetDir)
				fmt.Println()
				fmt.Println("Run with --confirm to proceed.")
				return &exitError{code: 1}
			}
			// With --confirm, pass force=true to the underlying Eject to
			// perform the backup-and-overwrite.
			if _, err := workflow.Eject(name, repoPath, true); err != nil {
				return err
			}
			fmt.Printf("Re-ejected workflow %s to %s (previous copy backed up). Edit manifest.toml to customize.\n", name, targetDir)
			return nil
		}

		// Fresh-ejection path: target does not exist yet, not destructive.
		dir, err := workflow.Eject(name, repoPath, false)
		if err != nil {
			return err
		}
		fmt.Printf("Ejected workflow %s to %s. Edit manifest.toml to customize.\n", name, dir)
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
	workflowCmd.AddCommand(workflowManifestCmd)
	workflowCmd.AddCommand(workflowShowCmd)
	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowEjectCmd)
	workflowCmd.AddCommand(workflowInitCmd)

	// eject flags
	workflowEjectCmd.Flags().Bool("project", false, "eject to project tier instead of user tier (requires --world)")
	workflowEjectCmd.Flags().String("world", "", "world name")
	workflowEjectCmd.Flags().Bool("confirm", false, "confirm destructive overwrite of an already-ejected workflow (backs up existing to {name}.bak-{timestamp})")

	// show flags
	workflowShowCmd.Flags().String("world", "", "world name")
	workflowShowCmd.Flags().Bool("json", false, "output as JSON")
	workflowShowCmd.Flags().String("path", "", "load workflow from directory path instead of by name")

	// init flags
	workflowInitCmd.Flags().String("type", "workflow", "workflow type")
	workflowInitCmd.Flags().Bool("project", false, "create in project tier (.sol/workflows/)")
	workflowInitCmd.Flags().String("world", "", "world name")

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

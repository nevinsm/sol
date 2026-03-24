package cmd

import (
	"context"
	"fmt"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/sitrep"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var sitrepCmd = &cobra.Command{
	Use:           "sitrep",
	Short:         "AI-generated situation report",
	GroupID:       groupDispatch,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	SilenceUsage:  true,
	RunE:          runSitrep,
}

var sitrepEjectCmd = &cobra.Command{
	Use:          "eject",
	Short:        "Write default prompt template to SOL_HOME",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		dest, err := sitrep.Eject(force)
		if err != nil {
			return err
		}
		fmt.Printf("Ejected sitrep prompt to %s\n", dest)
		return nil
	},
}

func runSitrep(cmd *cobra.Command, args []string) error {
	sphereFlag, _ := cmd.Flags().GetBool("sphere")
	worldFlag, _ := cmd.Flags().GetString("world")
	jsonFlag, _ := cmd.Flags().GetBool("json")

	// Determine scope.
	var scope sitrep.Scope

	if sphereFlag {
		scope = sitrep.Scope{Sphere: true}
	} else {
		// Try to resolve world from flag, env, or cwd.
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			// No world resolved — default to sphere scope.
			scope = sitrep.Scope{Sphere: true}
		} else {
			scope = sitrep.Scope{World: world}
		}
	}

	// Open sphere store.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return err
	}
	defer sphereStore.Close()

	// Collect data (with spinner for interactive use).
	spin := startSpinner("Collecting data...")
	data, err := sitrep.Collect(sphereStore, gatedWorldOpener, scope)
	if err != nil {
		spin.stop()
		return err
	}

	// JSON mode: dump collected data and exit (no spinner output).
	if jsonFlag {
		spin.stop()
		return printJSON(data)
	}

	// Load config for AI invocation.
	var cfg config.SitrepSection
	if !scope.Sphere {
		worldCfg, err := config.LoadWorldConfig(scope.World)
		if err != nil {
			// Fall back to global config.
			globalCfg, gErr := config.LoadGlobalConfig()
			if gErr != nil {
				cfg = config.DefaultWorldConfig().Sitrep
			} else {
				cfg = globalCfg.Sitrep
			}
		} else {
			cfg = worldCfg.Sitrep
		}
	} else {
		globalCfg, err := config.LoadGlobalConfig()
		if err != nil {
			cfg = config.DefaultWorldConfig().Sitrep
		} else {
			cfg = globalCfg.Sitrep
		}
	}

	// Build prompt.
	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		spin.stop()
		return err
	}

	// Raw mode: print the formatted prompt and exit (no AI call).
	rawFlag, _ := cmd.Flags().GetBool("raw")
	if rawFlag {
		spin.stop()
		fmt.Println(prompt)
		return nil
	}

	// Run AI assessment.
	spin.setLabel("Generating report...")
	ctx := context.Background()
	report, err := sitrep.Run(ctx, cfg, prompt)
	spin.stop()
	if err != nil {
		return err
	}

	fmt.Println(report)

	return nil
}

func init() {
	rootCmd.AddCommand(sitrepCmd)
	sitrepCmd.AddCommand(sitrepEjectCmd)

	sitrepCmd.Flags().Bool("sphere", false, "force sphere scope")
	sitrepCmd.Flags().String("world", "", "target specific world")
	sitrepCmd.Flags().Bool("json", false, "dump collected data as JSON (no AI call)")
	sitrepCmd.Flags().Bool("raw", false, "print the formatted prompt and exit (no AI call)")

	sitrepEjectCmd.Flags().Bool("force", false, "overwrite existing prompt")
}

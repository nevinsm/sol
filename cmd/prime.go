package cmd

import (
	"fmt"

	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/store"
	"github.com/spf13/cobra"
)

var (
	primeRig   string
	primeAgent string
)

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Assemble and print execution context for an agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if primeRig == "" {
			return fmt.Errorf("--rig is required")
		}
		if primeAgent == "" {
			return fmt.Errorf("--agent is required")
		}

		rigStore, err := store.OpenRig(primeRig)
		if err != nil {
			return err
		}
		defer rigStore.Close()

		result, err := dispatch.Prime(primeRig, primeAgent, rigStore)
		if err != nil {
			return err
		}

		fmt.Println(result.Output)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
	primeCmd.Flags().StringVar(&primeRig, "rig", "", "rig name")
	primeCmd.Flags().StringVar(&primeAgent, "agent", "", "agent name")
}

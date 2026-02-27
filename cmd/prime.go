package cmd

import (
	"fmt"

	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	primeWorld string
	primeAgent string
)

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Assemble and print execution context for an agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		if primeWorld == "" {
			return fmt.Errorf("--world is required")
		}
		if primeAgent == "" {
			return fmt.Errorf("--agent is required")
		}

		worldStore, err := store.OpenWorld(primeWorld)
		if err != nil {
			return err
		}
		defer worldStore.Close()

		result, err := dispatch.Prime(primeWorld, primeAgent, worldStore)
		if err != nil {
			return err
		}

		fmt.Println(result.Output)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
	primeCmd.Flags().StringVar(&primeWorld, "world", "", "world name")
	primeCmd.Flags().StringVar(&primeAgent, "agent", "", "agent name")
}

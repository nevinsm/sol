package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	forgetAgent string
	forgetAll   bool
)

var forgetCmd = &cobra.Command{
	Use:   "forget [key]",
	Short: "Delete a memory for the current agent",
	Long: `Delete a memory by key, or all memories with --all.

  sol forget "key"     — delete a single memory
  sol forget --all     — delete all memories for this agent`,
	GroupID:      groupAgents,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent := forgetAgent
		if agent == "" {
			agent = os.Getenv("SOL_AGENT")
		}
		if agent == "" {
			return fmt.Errorf("--agent is required (or set SOL_AGENT env var)")
		}

		if !forgetAll && len(args) == 0 {
			return fmt.Errorf("key argument required (or use --all)")
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		if forgetAll {
			n, err := s.DeleteAllAgentMemories(agent)
			if err != nil {
				return err
			}
			fmt.Printf("forgot %d memories\n", n)
			return nil
		}

		key := args[0]
		if err := s.DeleteAgentMemory(agent, key); err != nil {
			return err
		}
		fmt.Printf("forgot: %s\n", key)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(forgetCmd)
	forgetCmd.Flags().String("world", "", "world name")
	forgetCmd.Flags().StringVar(&forgetAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
	forgetCmd.Flags().BoolVar(&forgetAll, "all", false, "delete all memories for this agent")
}

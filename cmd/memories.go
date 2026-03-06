package cmd

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var memoriesAgent string

var memoriesCmd = &cobra.Command{
	Use:          "memories",
	Short:        "List all memories for the current agent",
	GroupID:      groupAgents,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent := memoriesAgent
		if agent == "" {
			agent = os.Getenv("SOL_AGENT")
		}
		if agent == "" {
			return fmt.Errorf("--agent is required (or set SOL_AGENT env var)")
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		memories, err := s.ListAgentMemories(agent)
		if err != nil {
			return err
		}

		if len(memories) == 0 {
			fmt.Println("No memories.")
			return nil
		}

		for _, m := range memories {
			fmt.Printf("- %s: %s\n", m.Key, m.Value)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(memoriesCmd)
	memoriesCmd.Flags().String("world", "", "world name")
	memoriesCmd.Flags().StringVar(&memoriesAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
}

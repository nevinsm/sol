package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var rememberAgent string

var rememberCmd = &cobra.Command{
	Use:   "remember [key] <value>",
	Short: "Persist a memory for the current agent",
	Long: `Persist a key-value memory that survives across sessions.

With two arguments: sol remember "key" "value"
With one argument:  sol remember "value"  (key auto-generated from hash)`,
	GroupID:      groupAgents,
	Args:         cobra.RangeArgs(1, 2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		worldFlag, _ := cmd.Flags().GetString("world")
		world, err := config.ResolveWorld(worldFlag)
		if err != nil {
			return err
		}
		agent := rememberAgent
		if agent == "" {
			agent = os.Getenv("SOL_AGENT")
		}
		if agent == "" {
			return fmt.Errorf("--agent is required (or set SOL_AGENT env var)")
		}

		var key, value string
		if len(args) == 2 {
			key = args[0]
			value = args[1]
		} else {
			value = args[0]
			h := sha256.Sum256([]byte(value))
			key = hex.EncodeToString(h[:4])
		}

		s, err := store.OpenWorld(world)
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.SetAgentMemory(agent, key, value); err != nil {
			return err
		}

		fmt.Printf("remembered: %s\n", key)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rememberCmd)
	rememberCmd.Flags().String("world", "", "world name")
	rememberCmd.Flags().StringVar(&rememberAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")
}

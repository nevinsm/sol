package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nevinsm/sol/internal/channelserve"
	"github.com/spf13/cobra"
)

var (
	channelWorld string
	channelAgent string
)

var channelCmd = &cobra.Command{
	Use:     "channel",
	Short:   "Claude Code channel bridge operations",
	Hidden:  true,
	GroupID: groupCommunication,
}

var channelServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the sol channel bridge (stdio MCP server)",
	Long: `Run the sol channel bridge: a stdio JSON-RPC MCP server that Claude Code
launches as a plugin subprocess and speaks to over stdin/stdout.

This is not meant to be run by hand — it is the command sol's channel
plugin's .mcp.json points at (see internal/channelplugin), started
automatically by Claude Code for any agent whose world has
agents.channels_enabled = true and whose command line includes --channels
(see ClaudeRuntime.BuildCommand). See docs/decisions/0044-claude-channels-plugin.md.

It is a thin, stateless bridge over the agent's own nudge queue
(internal/nudge): it watches the queue and pushes pending messages into the
live session as notifications/claude/channel events, in place of the pane
doorbell. No cursor or durability of its own — the nudge queue is the single
source of truth, and at-least-once redelivery on restart is correct behavior.

World and agent are resolved the same way as every other sol command
(--world/--agent flags, falling back to SOL_WORLD/SOL_AGENT env vars) since
the plugin's .mcp.json is shared across every agent and cannot template
per-agent arguments — this process instead relies on the SOL_WORLD/SOL_AGENT
env vars already present in its parent claude session's environment
(internal/startup.Launch sets both before starting the session).

Runs until stdin closes (the parent claude process exited) or it receives
SIGTERM/SIGINT.`,
	Args:         cobra.NoArgs,
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		session, err := resolveNudgeSession(channelWorld, channelAgent)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		go func() { <-sigCh; cancel() }()

		return channelserve.Serve(ctx, channelserve.Options{
			Session: session,
			In:      os.Stdin,
			Out:     os.Stdout,
			Log:     os.Stderr,
		})
	},
}

func init() {
	rootCmd.AddCommand(channelCmd)

	channelCmd.PersistentFlags().StringVar(&channelWorld, "world", "", "world name (defaults to SOL_WORLD env)")
	channelCmd.PersistentFlags().StringVar(&channelAgent, "agent", "", "agent name (defaults to SOL_AGENT env)")

	channelCmd.AddCommand(channelServeCmd)
}

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/worldsync"
	"github.com/spf13/cobra"
)

var envoyCmd = &cobra.Command{
	Use:     "envoy",
	Short:   "Manage persistent envoy agents",
	GroupID: groupAgents,
}

// --- sol envoy create ---

var envoyCreateWorld string

var envoyCreateCmd = &cobra.Command{
	Use:          "create <name>",
	Short:        "Create an envoy agent",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyCreateWorld)
		if err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return err
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		if err := envoy.Create(envoy.CreateOpts{
			World:      world,
			Name:       name,
			SourceRepo: sourceRepo,
		}, sphereStore); err != nil {
			return err
		}

		fmt.Printf("Created envoy %q in world %q\n", name, world)
		return nil
	},
}

// --- sol envoy start ---

var envoyStartWorld string

var envoyStartCmd = &cobra.Command{
	Use:          "start <name>",
	Short:        "Start an envoy session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyStartWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()

		// Read optional persona file.
		var personaContent string
		personaPath := envoy.PersonaPath(world, name)
		if data, err := os.ReadFile(personaPath); err == nil {
			personaContent = string(data)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read persona file %q: %w", personaPath, err)
		}

		// Install envoy CLAUDE.md before starting session.
		worktree := envoy.WorktreePath(world, name)
		if err := protocol.InstallEnvoyClaudeMD(worktree, protocol.EnvoyClaudeMDContext{
			AgentName:      name,
			World:          world,
			SolBinary:      "sol",
			PersonaContent: personaContent,
		}); err != nil {
			return fmt.Errorf("failed to install envoy CLAUDE.md: %w", err)
		}

		if err := envoy.Start(envoy.StartOpts{
			World: world,
			Name:  name,
		}, sphereStore, mgr); err != nil {
			return err
		}

		fmt.Printf("Started envoy %q in world %q\n", name, world)
		return nil
	},
}

// --- sol envoy stop ---

var envoyStopWorld string

var envoyStopCmd = &cobra.Command{
	Use:          "stop <name>",
	Short:        "Stop an envoy session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyStopWorld)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()

		if err := envoy.Stop(world, name, sphereStore, mgr); err != nil {
			return err
		}

		fmt.Printf("Stopped envoy %q in world %q\n", name, world)
		return nil
	},
}

// --- sol envoy attach ---

var envoyAttachWorld string

var envoyAttachCmd = &cobra.Command{
	Use:          "attach <name>",
	Short:        "Attach to an envoy's tmux session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyAttachWorld)
		if err != nil {
			return err
		}

		sessName := envoy.SessionName(world, name)
		mgr := session.New()

		if !mgr.Exists(sessName) {
			return fmt.Errorf("no envoy session for %q in world %q (run 'sol envoy start %s --world=%s' first)",
				name, world, name, world)
		}

		return mgr.Attach(sessName)
	},
}

// --- sol envoy list ---

var (
	envoyListWorld string
	envoyListJSON  bool
)

var envoyListCmd = &cobra.Command{
	Use:          "list",
	Short:        "List envoy agents",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		envoys, err := envoy.List(envoyListWorld, sphereStore)
		if err != nil {
			return err
		}

		if envoyListJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(envoys)
		}

		if len(envoys) == 0 {
			fmt.Println("No envoys found.")
			return nil
		}

		mgr := session.New()
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tWORLD\tSTATE\tSESSION\n")
		for _, e := range envoys {
			sessName := envoy.SessionName(e.World, e.Name)
			sessStatus := "stopped"
			if mgr.Exists(sessName) {
				sessStatus = "running"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Name, e.World, e.State, sessStatus)
		}
		tw.Flush()
		return nil
	},
}

// --- sol envoy brief ---

var envoyBriefWorld string

var envoyBriefCmd = &cobra.Command{
	Use:          "brief <name>",
	Short:        "Display an envoy's brief",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyBriefWorld)
		if err != nil {
			return err
		}

		briefPath := envoy.BriefPath(world, name)
		data, err := os.ReadFile(briefPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("No brief found for envoy %q\n", name)
				return nil
			}
			return fmt.Errorf("failed to read brief: %w", err)
		}

		fmt.Print(string(data))
		return nil
	},
}

// --- sol envoy debrief ---

var envoyDebriefWorld string

var envoyDebriefCmd = &cobra.Command{
	Use:          "debrief <name>",
	Short:        "Archive the envoy's brief and reset for fresh engagement",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoyDebriefWorld)
		if err != nil {
			return err
		}

		briefPath := envoy.BriefPath(world, name)
		if _, err := os.Stat(briefPath); err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("No brief found for envoy %q\n", name)
				return nil
			}
			return fmt.Errorf("failed to check brief: %w", err)
		}

		// Create archive directory.
		briefDir := envoy.BriefDir(world, name)
		archiveDir := filepath.Join(briefDir, "archive")
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return fmt.Errorf("failed to create archive directory: %w", err)
		}

		// Generate archive filename with RFC3339 timestamp, colons replaced by dashes.
		ts := time.Now().UTC().Format(time.RFC3339)
		safeTS := strings.ReplaceAll(ts, ":", "-")
		archiveFile := safeTS + ".md"
		archivePath := filepath.Join(archiveDir, archiveFile)

		// Move current brief to archive.
		if err := os.Rename(briefPath, archivePath); err != nil {
			return fmt.Errorf("failed to archive brief: %w", err)
		}

		fmt.Printf("Archived brief to .brief/archive/%s\n", archiveFile)
		fmt.Printf("Envoy %q ready for fresh engagement\n", name)
		return nil
	},
}

// --- sol envoy delete ---

var (
	envoyDeleteWorld string
	envoyDeleteForce bool
)

var envoyDeleteCmd = &cobra.Command{
	Use:          "delete <name>",
	Short:        "Delete an envoy agent and all associated resources",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if envoyDeleteWorld == "" {
			return fmt.Errorf("--world is required")
		}
		if err := config.RequireWorld(envoyDeleteWorld); err != nil {
			return err
		}

		worldCfg, err := config.LoadWorldConfig(envoyDeleteWorld)
		if err != nil {
			return err
		}
		sourceRepo, err := dispatch.ResolveSourceRepo(envoyDeleteWorld, worldCfg)
		if err != nil {
			return err
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return err
		}
		defer sphereStore.Close()

		mgr := session.New()

		if err := envoy.Delete(envoy.DeleteOpts{
			World:      envoyDeleteWorld,
			Name:       name,
			SourceRepo: sourceRepo,
			Force:      envoyDeleteForce,
		}, sphereStore, mgr); err != nil {
			return err
		}

		fmt.Printf("Deleted envoy %q from world %q\n", name, envoyDeleteWorld)
		return nil
	},
}

// --- sol envoy sync ---

var envoySyncWorld string

var envoySyncCmd = &cobra.Command{
	Use:          "sync <name>",
	Short:        "Sync managed repo and notify a running envoy session",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		world, err := config.ResolveWorld(envoySyncWorld)
		if err != nil {
			return err
		}

		// Sync managed repo first.
		if err := worldsync.SyncRepo(world); err != nil {
			return fmt.Errorf("failed to sync managed repo: %w", err)
		}

		// Notify envoy session if running.
		mgr := session.New()
		if err := worldsync.SyncEnvoy(world, name, mgr); err != nil {
			return err
		}

		fmt.Printf("Synced for envoy %q in world %q\n", name, world)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(envoyCmd)
	envoyCmd.AddCommand(envoyCreateCmd, envoyStartCmd, envoyStopCmd,
		envoyAttachCmd, envoyListCmd, envoyBriefCmd, envoyDebriefCmd, envoySyncCmd, envoyDeleteCmd)

	// envoy create flags
	envoyCreateCmd.Flags().StringVar(&envoyCreateWorld, "world", "", "world name")

	// envoy start flags
	envoyStartCmd.Flags().StringVar(&envoyStartWorld, "world", "", "world name")

	// envoy stop flags
	envoyStopCmd.Flags().StringVar(&envoyStopWorld, "world", "", "world name")

	// envoy attach flags
	envoyAttachCmd.Flags().StringVar(&envoyAttachWorld, "world", "", "world name")

	// envoy list flags
	envoyListCmd.Flags().StringVar(&envoyListWorld, "world", "", "world name (optional, lists all if omitted)")
	envoyListCmd.Flags().BoolVar(&envoyListJSON, "json", false, "output as JSON")

	// envoy brief flags
	envoyBriefCmd.Flags().StringVar(&envoyBriefWorld, "world", "", "world name")

	// envoy debrief flags
	envoyDebriefCmd.Flags().StringVar(&envoyDebriefWorld, "world", "", "world name")

	// envoy delete flags
	envoyDeleteCmd.Flags().StringVar(&envoyDeleteWorld, "world", "", "world name")
	envoyDeleteCmd.Flags().BoolVar(&envoyDeleteForce, "force", false, "force delete even if session is active or tethered")

	// envoy sync flags
	envoySyncCmd.Flags().StringVar(&envoySyncWorld, "world", "", "world name")
}

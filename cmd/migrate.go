package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

var (
	migrateListJSON    bool
	migrateHistoryJSON bool
	migrateRunConfirm  bool
	migrateRunForce    bool
	migrateRunWorld    string
)

var migrateCmd = &cobra.Command{
	Use:     "migrate",
	Short:   "Manage sol migrations",
	GroupID: groupSetup,
	Long: `Manage sol's built-in migration framework.

Sol ships with a registry of migrations — upgrade steps that shift an
existing installation from one state to another. Pending migrations are
surfaced automatically via 'sol doctor' and the banner printed by 'sol up'
so operators see them the moment they matter.

Subcommands:
  list     — show all registered migrations with their status
  show     — print the full description of a single migration
  run      — execute a migration (requires --confirm)
  history  — show previously applied migrations (newest first)`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

var migrateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered migrations with status",
	Long: `List all registered migrations with their current status.

STATUS values:
  applied      — recorded in migrations_applied; nothing to do
  pending      — Detect reports the migration is applicable
  not-needed   — Detect reports the migration is not applicable
  error        — Detect returned an error; see the REASON column

Exit code 0 unless IO fails.`,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE:         runMigrateList,
}

var migrateShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Print the full description of a migration",
	Long: `Print the markdown description of a registered migration to stdout.

Exit code 1 if the named migration is not registered.`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runMigrateShow,
}

var migrateRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Execute a registered migration",
	Long: `Execute a registered migration.

By default, 'sol migrate run' is a dry run: it calls the migration's Detect
function and prints what it would do, then exits 1. Use --confirm to
actually execute the migration.

On success, the result is recorded in the sphere's migrations_applied
table. On failure, nothing is recorded — migrations must be idempotent, so
re-running after fixing the underlying issue is safe.

Flags:
  --confirm       actually execute (otherwise dry-run only)
  --force         bypass the "already applied" guard (does not bypass Detect)
  --world=<name>  scope to a single world (ignored by sphere-wide migrations)

Exit codes:
  0  success or dry-run with an applicable detection
  1  dry-run (printed what would run), migration not registered, or failure`,
	SilenceUsage: true,
	Args:         cobra.ExactArgs(1),
	RunE:         runMigrateRun,
}

var migrateHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show previously applied migrations, newest first",
	Long: `Show the migrations_applied table, newest first.

Columns: NAME, VERSION, APPLIED AT, SUMMARY.

Exit code 0 unless IO fails.`,
	SilenceUsage: true,
	Args:         cobra.NoArgs,
	RunE:         runMigrateHistory,
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.AddCommand(migrateListCmd)
	migrateCmd.AddCommand(migrateShowCmd)
	migrateCmd.AddCommand(migrateRunCmd)
	migrateCmd.AddCommand(migrateHistoryCmd)

	migrateListCmd.Flags().BoolVar(&migrateListJSON, "json", false, "output as JSON")
	migrateHistoryCmd.Flags().BoolVar(&migrateHistoryJSON, "json", false, "output as JSON")
	migrateRunCmd.Flags().BoolVar(&migrateRunConfirm, "confirm", false, "actually execute (default: dry-run)")
	migrateRunCmd.Flags().BoolVar(&migrateRunForce, "force", false, "bypass already-applied guard")
	migrateRunCmd.Flags().StringVar(&migrateRunWorld, "world", "", "scope to a single world")
}

// openMigrateContext opens the sphere store and returns a migrate.Context.
// The returned closer must be called to release the sphere store.
func openMigrateContext() (migrate.Context, func(), error) {
	ss, err := store.OpenSphere()
	if err != nil {
		return migrate.Context{}, func() {}, fmt.Errorf("failed to open sphere store: %w", err)
	}
	ctx := migrate.Context{
		SolHome:     config.Home(),
		SphereStore: ss,
	}
	return ctx, func() { ss.Close() }, nil
}

func runMigrateList(cmd *cobra.Command, _ []string) error {
	ctx, closer, err := openMigrateContext()
	if err != nil {
		return err
	}
	defer closer()

	statuses, err := migrate.List(ctx)
	if err != nil {
		return err
	}

	if migrateListJSON {
		return printJSON(statuses)
	}

	if len(statuses) == 0 {
		fmt.Println("No migrations registered.")
		return nil
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tSTATUS\tDESCRIPTION")
	for _, s := range statuses {
		status := statusLabel(s)
		desc := s.Migration.Title
		if desc == "" {
			desc = s.Reason
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Migration.Name, s.Migration.Version, status, desc)
	}
	return tw.Flush()
}

func statusLabel(s migrate.Status) string {
	switch {
	case s.Applied:
		return "applied"
	case strings.HasPrefix(s.Reason, "detect error:"):
		return "error"
	case s.Needed:
		return "pending"
	default:
		return "not-needed"
	}
}

func runMigrateShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	m, ok := migrate.Get(name)
	if !ok {
		return fmt.Errorf("migration %q is not registered", name)
	}
	if m.Description == "" {
		fmt.Printf("# %s (%s)\n\n%s\n", m.Name, m.Version, m.Title)
		return nil
	}
	fmt.Println(m.Description)
	return nil
}

func runMigrateRun(cmd *cobra.Command, args []string) error {
	name := args[0]
	if _, ok := migrate.Get(name); !ok {
		return fmt.Errorf("migration %q is not registered", name)
	}

	ctx, closer, err := openMigrateContext()
	if err != nil {
		return err
	}
	defer closer()

	opts := migrate.RunOpts{
		Confirm: migrateRunConfirm,
		Force:   migrateRunForce,
		World:   migrateRunWorld,
	}

	res, err := migrate.Run(ctx, name, opts)
	if err != nil {
		return err
	}

	if !opts.Confirm {
		fmt.Fprintln(os.Stdout, res.Summary)
		fmt.Fprintln(os.Stderr, "(dry-run; re-run with --confirm to execute)")
		return &exitError{code: 1}
	}

	fmt.Println(res.Summary)
	if len(res.Details) > 0 {
		for k, v := range res.Details {
			fmt.Printf("  %s: %v\n", k, v)
		}
	}
	return nil
}

func runMigrateHistory(cmd *cobra.Command, _ []string) error {
	ss, err := store.OpenSphere()
	if err != nil {
		return fmt.Errorf("failed to open sphere store: %w", err)
	}
	defer ss.Close()

	rows, err := ss.ListAppliedMigrations()
	if err != nil {
		return err
	}

	if migrateHistoryJSON {
		return printJSON(rows)
	}

	if len(rows) == 0 {
		fmt.Println("No migrations applied.")
		return nil
	}

	return writeHistoryTable(os.Stdout, rows)
}

func writeHistoryTable(w io.Writer, rows []store.AppliedMigration) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tAPPLIED AT\tSUMMARY")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.Name, r.Version, r.AppliedAt.Format(time.RFC3339), r.Summary)
	}
	return tw.Flush()
}

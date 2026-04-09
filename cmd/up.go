package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

// pendingMigrationsCheck is the function used by sol up to count pending
// migrations. It is overridable in tests.
var pendingMigrationsCheck = defaultPendingMigrationsCheck

// migrationBannerTimeout bounds how long sol up will wait for the
// pending-migrations check before giving up. Detect functions should be
// fast; if any of them hangs, we do not want to block startup.
const migrationBannerTimeout = 2 * time.Second

func defaultPendingMigrationsCheck() (pending int, detectErrors int, err error) {
	ss, oerr := store.OpenSphere()
	if oerr != nil {
		return 0, 0, oerr
	}
	defer ss.Close()
	ctx := migrate.Context{SolHome: config.Home(), SphereStore: ss}
	p, de := migrate.PendingCount(ctx)
	return p, de, nil
}

// printPendingMigrationsBanner runs the pending-migrations check with a
// short timeout and, if any are pending or unknown, prints a yellow
// advisory banner to stderr. It never fails sol up — operators may be in
// a state where they cannot immediately run the migration (active
// sessions, merge in progress, etc.).
func printPendingMigrationsBanner() {
	type result struct {
		pending, detectErrors int
		err                   error
	}
	ch := make(chan result, 1)
	go func() {
		p, de, err := pendingMigrationsCheck()
		ch <- result{p, de, err}
	}()

	var r result
	select {
	case r = <-ch:
	case <-time.After(migrationBannerTimeout):
		fmt.Fprintln(os.Stderr, upWarn.Render("⚠ unable to check migrations (timed out)"))
		return
	}

	if r.err != nil {
		// Sphere store open failure shouldn't fail sol up — it is
		// already reported by other paths. Log a generic notice.
		fmt.Fprintln(os.Stderr, upWarn.Render("⚠ unable to check migrations"))
		return
	}
	if r.pending == 0 && r.detectErrors == 0 {
		return
	}
	msg := fmt.Sprintf("⚠ %d pending migration(s). Run `sol migrate list` to see them.", r.pending)
	if r.pending == 0 {
		msg = fmt.Sprintf("⚠ %d migration(s) could not be checked. Run `sol migrate list` to investigate.", r.detectErrors)
	} else if r.detectErrors > 0 {
		msg = fmt.Sprintf("⚠ %d pending, %d unknown migration(s). Run `sol migrate list` to see them.", r.pending, r.detectErrors)
	}
	fmt.Fprintln(os.Stderr, upWarn.Render(msg))
}

// sphereDaemonLifecycles is the canonical list of sphere-level daemons managed
// by sol up/down. Each entry is a daemon.Lifecycle defined in the daemon's own
// cmd file (e.g. prefectLifecycle in cmd/prefect.go). The order here also
// determines start order (prefect first so it is alive before consul etc).
var sphereDaemonLifecycles = []daemon.Lifecycle{
	prefectLifecycle,
	consulLifecycle,
	chronicleLifecycle,
	ledgerLifecycle,
	brokerLifecycle,
}

// worldServices are the per-world services started/stopped by sol up/down.
// Envoy is not auto-started (human-managed session).
var worldServices = []string{"sentinel", "forge"}

var (
	upWorldFlag   string
	upWorldsFlag  []string
	downWorldFlag string
	downAllFlag   bool
)

var upCmd = &cobra.Command{
	Use:          "up",
	Short:        "Start sphere daemons and world services",
	GroupID:      groupProcesses,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runUp,
}

var downCmd = &cobra.Command{
	Use:          "down",
	Short:        "Stop sphere daemons and world services",
	GroupID:      groupProcesses,
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runDown,
}

func init() {
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)

	upCmd.Flags().StringVar(&upWorldFlag, "world", "", "start only world services (optionally for a specific world)")
	upCmd.Flags().Lookup("world").NoOptDefVal = ""
	upCmd.Flags().StringSliceVar(&upWorldsFlag, "worlds", nil, "comma-separated list of worlds to supervise and start services for")

	downCmd.Flags().StringVar(&downWorldFlag, "world", "", "stop only world services (optionally for a specific world)")
	downCmd.Flags().Lookup("world").NoOptDefVal = ""
	downCmd.Flags().BoolVar(&downAllFlag, "all", false, "also stop envoy sessions")
}

// --- PID helpers ---
//
// daemonPIDPath and daemonLogPath are small utility closures for the sphere
// daemons (ledger/broker/chronicle) whose lifecycle vars compose with them.
// The flock-authoritative read/write/clear logic lives in the internal/daemon
// package now — these helpers exist only to name the on-disk files.

func daemonPIDPath(name string) string {
	return filepath.Join(config.RuntimeDir(), name+".pid")
}

func daemonLogPath(name string) string {
	return filepath.Join(config.RuntimeDir(), name+".log")
}

// checkSystemdUnits returns names of sphere daemons managed by systemd.
func checkSystemdUnits() []string {
	var managed []string
	for _, lc := range sphereDaemonLifecycles {
		unit := "sol-" + lc.Name + ".service"
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			managed = append(managed, lc.Name)
		}
	}
	return managed
}

// --- Styles ---

var (
	upOK   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	upErr  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	upDim  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	upWarn = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// --- World helpers ---

// activeWorlds returns non-sleeping worlds. If specificWorld is non-empty,
// validates and returns it alone (errors if sleeping).
func activeWorlds(specificWorld string) ([]string, error) {
	if specificWorld != "" {
		if err := config.RequireWorld(specificWorld); err != nil {
			return nil, err
		}
		sleeping, err := config.IsSleeping(specificWorld)
		if err != nil {
			return nil, fmt.Errorf("failed to check sleep status for world %q: %w", specificWorld, err)
		}
		if sleeping {
			return nil, fmt.Errorf("world %q is sleeping (wake it with 'sol world wake %s')", specificWorld, specificWorld)
		}
		return []string{specificWorld}, nil
	}

	return listNonSleepingWorlds()
}

// activeWorldsList returns the non-sleeping worlds from a given list.
// Sleeping worlds are silently skipped.
func activeWorldsList(names []string) ([]string, error) {
	var active []string
	for _, name := range names {
		if err := config.RequireWorld(name); err != nil {
			return nil, err
		}
		sleeping, err := config.IsSleeping(name)
		if err != nil {
			return nil, fmt.Errorf("failed to check sleep status for world %q: %w", name, err)
		}
		if !sleeping {
			active = append(active, name)
		}
	}
	return active, nil
}

// listNonSleepingWorlds returns all worlds that are not sleeping.
func listNonSleepingWorlds() ([]string, error) {
	worlds, err := listAllWorlds()
	if err != nil {
		return nil, err
	}

	var active []string
	for _, name := range worlds {
		sleeping, err := config.IsSleeping(name)
		if err != nil {
			return nil, fmt.Errorf("failed to check sleep status for world %q: %w", name, err)
		}
		if !sleeping {
			active = append(active, name)
		}
	}
	return active, nil
}

// listAllWorlds returns all world names from the sphere store.
// Returns nil (no error) if the store cannot be opened.
func listAllWorlds() ([]string, error) {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return nil, nil
	}
	defer sphereStore.Close()

	worlds, err := sphereStore.ListWorlds()
	if err != nil {
		return nil, fmt.Errorf("failed to list worlds: %w", err)
	}

	var names []string
	for _, w := range worlds {
		names = append(names, w.Name)
	}
	return names, nil
}

// resolveWorldsForDown returns the worlds to stop services for.
// Unlike activeWorlds, does not filter sleeping worlds (we stop everything).
func resolveWorldsForDown(specificWorld string) ([]string, error) {
	if specificWorld != "" {
		if err := config.RequireWorld(specificWorld); err != nil {
			return nil, err
		}
		return []string{specificWorld}, nil
	}
	return listAllWorlds()
}

// --- sol up ---

func runUp(cmd *cobra.Command, _ []string) error {
	worldOnly := cmd.Flags().Changed("world")

	solBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find sol binary: %w", err)
	}

	var hadFailure bool

	// Sphere daemons (skipped with --world).
	if !worldOnly {
		failed, err := startSphereDaemons(upWorldsFlag)
		if err != nil {
			return err
		}
		if failed {
			hadFailure = true
		}
	}

	// World services — --worlds takes precedence over --world for service scope.
	var worlds []string
	if len(upWorldsFlag) > 0 {
		worlds, err = activeWorldsList(upWorldsFlag)
	} else {
		worlds, err = activeWorlds(upWorldFlag)
	}
	if err != nil {
		return err
	}

	if len(worlds) > 0 {
		if failed := startWorldServicesBatch(solBin, worlds); failed {
			hadFailure = true
		}
	}

	if hadFailure {
		return fmt.Errorf("some services failed to start")
	}

	// After successful startup, surface any pending migrations so the
	// operator notices a breaking upgrade that needs manual action. This
	// is advisory only and never fails sol up.
	printPendingMigrationsBanner()
	return nil
}

// startSphereDaemons starts sphere-level daemons via the daemon package.
// Returns true if any failed. If worlds is non-empty, the --worlds flag is
// passed to the prefect. Returns an error if sphere daemons are managed by
// systemd (dual management).
func startSphereDaemons(worlds []string) (bool, error) {
	if managed := checkSystemdUnits(); len(managed) > 0 {
		return false, fmt.Errorf("sphere daemons managed by systemd (%s).\n"+
			"Use 'systemctl --user start/stop/restart' to manage them,\n"+
			"or 'sol service uninstall' to switch back to sol up",
			strings.Join(managed, ", "))
	}

	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		return false, fmt.Errorf("failed to create runtime directory: %w", err)
	}

	baseEnv := append(os.Environ(), "SOL_HOME="+config.Home())

	type result struct {
		name, status string
		pid          int
		err          error
	}
	var results []result

	for _, lc := range sphereDaemonLifecycles {
		lc.Env = baseEnv
		// Prefect under `sol up` gets --consul and optional --worlds=....
		if lc.Name == "prefect" {
			args := []string{"prefect", "run", "--consul"}
			if len(worlds) > 0 {
				args = append(args, "--worlds="+strings.Join(worlds, ","))
			}
			lc.RunArgs = args
		}

		res, err := daemon.Start(lc)
		r := result{name: lc.Name}
		if err != nil {
			r.status = "failed"
			r.err = err
		} else {
			r.status = res.Status
			r.pid = res.PID
		}
		results = append(results, r)
	}

	// Print status table.
	fmt.Println()
	hadFailure := false
	for _, r := range results {
		var indicator, detail string
		switch r.status {
		case "started":
			indicator = upOK.Render("✓")
			detail = upOK.Render("started")
			if r.pid > 0 {
				detail += upDim.Render(fmt.Sprintf("  pid %d", r.pid))
			}
		case "running":
			indicator = upOK.Render("✓")
			detail = upDim.Render("already running")
			if r.pid > 0 {
				detail += upDim.Render(fmt.Sprintf("  pid %d", r.pid))
			}
		case "failed":
			indicator = upErr.Render("✗")
			detail = upErr.Render("failed")
			if r.err != nil {
				detail += upDim.Render("  " + r.err.Error())
			}
			hadFailure = true
		}
		fmt.Printf("  %s %-12s %s\n", indicator, r.name, detail)
	}
	fmt.Println()

	return hadFailure, nil
}

// startWorldServicesBatch starts world services for the given worlds.
// Returns true if any failed.
func startWorldServicesBatch(solBin string, worlds []string) bool {
	type result struct {
		world, service, status string
		err                    error
	}

	mgr := session.New()
	var results []result

	for _, world := range worlds {
		for _, svc := range worldServices {
			r := result{world: world, service: svc}

			// Check if already running: sentinel and forge use PID files, others use tmux session.
			alreadyRunning := false
			if svc == "sentinel" {
				pid := sentinel.ReadPID(world)
				alreadyRunning = pid > 0 && sentinel.IsRunning(pid)
			} else if svc == "forge" {
				pid := forge.ReadPID(world)
				alreadyRunning = pid > 0 && forge.IsRunning(pid)
			} else {
				sessName := config.SessionName(world, svc)
				alreadyRunning = mgr.Exists(sessName)
			}

			if alreadyRunning {
				r.status = "running"
				results = append(results, r)
				continue
			}

			out, err := exec.Command(solBin, svc, "start", "--world="+world).CombinedOutput()
			if err != nil {
				r.status = "failed"
				r.err = fmt.Errorf("%s", strings.TrimSpace(string(out)))
			} else {
				r.status = "started"
			}
			results = append(results, r)
		}
	}

	// Print grouped by world.
	hadFailure := false
	currentWorld := ""
	for _, r := range results {
		if r.world != currentWorld {
			currentWorld = r.world
			fmt.Printf("  %s\n", upDim.Render(r.world))
		}

		var indicator, detail string
		switch r.status {
		case "started":
			indicator = upOK.Render("✓")
			detail = upOK.Render("started")
		case "running":
			indicator = upOK.Render("✓")
			detail = upDim.Render("already running")
		case "failed":
			indicator = upErr.Render("✗")
			detail = upErr.Render("failed")
			if r.err != nil {
				detail += upDim.Render("  " + r.err.Error())
			}
			hadFailure = true
		}
		fmt.Printf("    %s %-12s %s\n", indicator, r.service, detail)
	}
	if len(results) > 0 {
		fmt.Println()
	}

	return hadFailure
}

// --- sol down ---

func runDown(cmd *cobra.Command, _ []string) error {
	worldOnly := cmd.Flags().Changed("world")

	var hadFailure bool

	// Sphere daemons (skipped with --world).
	if !worldOnly {
		if stopSphereDaemons() {
			hadFailure = true
		}
	}

	// World services.
	worlds, err := resolveWorldsForDown(downWorldFlag)
	if err != nil {
		return err
	}

	if len(worlds) > 0 {
		if stopWorldServicesBatch(worlds) {
			hadFailure = true
		}
	}

	// With --all, also stop envoys.
	if downAllFlag {
		if stopManagedSessions(worlds) {
			hadFailure = true
		}
	}

	if hadFailure {
		return fmt.Errorf("some components failed to stop (see errors above)")
	}
	return nil
}

// stopSphereDaemons stops sphere-level daemons via the daemon package and
// prints results. Returns true if any daemon failed to stop.
//
// Prefect is killed first — it is the supervisor that respawns other daemons,
// and must be fully dead before consul/ledger/broker/chronicle are stopped,
// otherwise its heartbeat loop can respawn them between their kill and
// prefect's own kill.
func stopSphereDaemons() bool {
	type result struct {
		name, status string
		err          error
	}
	var results []result

	stopOne := func(lc daemon.Lifecycle) result {
		r := result{name: lc.Name}
		if err := daemon.Stop(lc); err != nil {
			r.status = "failed"
			r.err = err
			return r
		}
		r.status = "stopped"
		return r
	}

	// Kill prefect first.
	var prefectLC daemon.Lifecycle
	for _, lc := range sphereDaemonLifecycles {
		if lc.Name == "prefect" {
			prefectLC = lc
			break
		}
	}
	results = append(results, stopOne(prefectLC))

	// Remaining daemons in reverse order.
	for i := len(sphereDaemonLifecycles) - 1; i >= 0; i-- {
		lc := sphereDaemonLifecycles[i]
		if lc.Name == "prefect" {
			continue
		}
		results = append(results, stopOne(lc))
	}

	fmt.Println()
	hadFailure := false
	for _, r := range results {
		var indicator, detail string
		switch r.status {
		case "stopped":
			indicator = upOK.Render("✓")
			detail = "stopped"
		case "failed":
			indicator = upErr.Render("✗")
			detail = upErr.Render("error")
			if r.err != nil {
				detail += upDim.Render("  " + r.err.Error())
			}
			hadFailure = true
		}
		fmt.Printf("  %s %-12s %s\n", indicator, r.name, detail)
	}
	fmt.Println()
	return hadFailure
}

// stopWorldServicesBatch stops world services for the given worlds.
// Returns true if any service failed to stop.
func stopWorldServicesBatch(worlds []string) bool {
	type result struct {
		world, service, status string
		err                    error
	}

	mgr := session.New()
	var results []result

	for _, world := range worlds {
		for _, svc := range worldServices {
			r := result{world: world, service: svc}

			if svc == "sentinel" {
				// Sentinel is a direct process — stop via PID.
				pid := sentinel.ReadPID(world)
				if pid <= 0 || !sentinel.IsRunning(pid) {
					r.status = "not running"
					results = append(results, r)
					continue
				}
				if proc, err := os.FindProcess(pid); err == nil {
					if err := proc.Signal(syscall.SIGTERM); err != nil {
						r.status = "failed"
						r.err = err
					} else {
						r.status = "stopped"
					}
				} else {
					r.status = "failed"
					r.err = err
				}
			} else if svc == "forge" {
				// Forge is a direct process — stop via PID.
				pid := forge.ReadPID(world)
				if pid <= 0 || !forge.IsRunning(pid) {
					r.status = "not running"
					results = append(results, r)
					continue
				}
				if err := forge.StopProcess(world, 10*time.Second); err != nil {
					r.status = "failed"
					r.err = err
				} else {
					r.status = "stopped"
				}
			} else {
				// Session-based service — stop via tmux.
				sessName := config.SessionName(world, svc)
				if !mgr.Exists(sessName) {
					r.status = "not running"
					results = append(results, r)
					continue
				}
				if err := mgr.Stop(sessName, false); err != nil {
					r.status = "failed"
					r.err = err
				} else {
					r.status = "stopped"
				}
			}

			// Clean up stale forge agent record to prevent sentinel
			// from seeing "working + dead session" on next sol up.
			if svc == "forge" {
				if ss, err := store.OpenSphere(); err == nil {
					agentID := world + "/forge"
					ss.DeleteAgent(agentID) // best-effort
					ss.Close()
				}
			}

			results = append(results, r)
		}
	}

	// Print grouped by world.
	hadFailure := false
	currentWorld := ""
	for _, r := range results {
		if r.world != currentWorld {
			currentWorld = r.world
			fmt.Printf("  %s\n", upDim.Render(r.world))
		}

		var indicator, detail string
		switch r.status {
		case "stopped":
			indicator = upOK.Render("✓")
			detail = "stopped"
		case "not running":
			indicator = upDim.Render("—")
			detail = upDim.Render("not running")
		case "failed":
			indicator = upErr.Render("✗")
			detail = upErr.Render("error")
			if r.err != nil {
				detail += upDim.Render("  " + r.err.Error())
			}
			hadFailure = true
		}
		fmt.Printf("    %s %-12s %s\n", indicator, r.service, detail)
	}
	if len(results) > 0 {
		fmt.Println()
	}
	return hadFailure
}

// stopManagedSessions stops envoy sessions.
// Called by sol down --all.
// Returns true if any session failed to stop.
func stopManagedSessions(worlds []string) bool {
	mgr := session.New()

	type result struct {
		role, name, status string
		err                error
	}
	var results []result

	// Query sphere store for envoys.
	sphereStore, err := store.OpenSphere()
	if err == nil {
		agents, err := sphereStore.ListAgents("", "")
		if err == nil {
			for _, a := range agents {
				if a.Role != "envoy" {
					continue
				}
				r := result{role: a.Role, name: config.SessionName(a.World, a.Name)}
				if !mgr.Exists(r.name) {
					r.status = "not running"
					results = append(results, r)
					continue
				}
				if err := mgr.Stop(r.name, false); err != nil {
					r.status = "failed"
					r.err = err
				} else {
					r.status = "stopped"
				}
				results = append(results, r)
			}
		}
		sphereStore.Close()
	}

	// Print results.
	hadFailure := false
	if len(results) > 0 {
		fmt.Printf("  %s\n", upDim.Render("managed sessions"))
		for _, r := range results {
			var indicator, detail string
			label := fmt.Sprintf("%s (%s)", r.name, r.role)
			switch r.status {
			case "stopped":
				indicator = upOK.Render("✓")
				detail = "stopped"
			case "not running":
				indicator = upDim.Render("—")
				detail = upDim.Render("not running")
			case "failed":
				indicator = upErr.Render("✗")
				detail = upErr.Render("error")
				if r.err != nil {
					detail += upDim.Render("  " + r.err.Error())
				}
				hadFailure = true
			}
			fmt.Printf("    %s %-32s %s\n", indicator, label, detail)
		}
		fmt.Println()
	}
	return hadFailure
}

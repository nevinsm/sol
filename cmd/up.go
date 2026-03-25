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
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
)

// chancellorSession is the tmux session name for the chancellor
// (matches the constant in internal/chancellor).
const chancellorSession = "sol-chancellor"

// sphereDaemon describes a sphere-level daemon managed by sol up/down.
type sphereDaemon struct {
	name    string
	session string // tmux session name to check (if managed via tmux)
}

var sphereDaemons = []sphereDaemon{
	{name: "prefect"},
	{name: "consul"},
	{name: "chronicle"},
	{name: "ledger"},
	{name: "broker"},
}

// worldServices are the per-world services started/stopped by sol up/down.
// Governor is not auto-started (human-managed session).
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
	downCmd.Flags().BoolVar(&downAllFlag, "all", false, "also stop envoy, governor, and chancellor sessions")
}

// --- PID helpers ---

func daemonPIDPath(name string) string {
	return filepath.Join(config.RuntimeDir(), name+".pid")
}

func daemonLogPath(name string) string {
	return filepath.Join(config.RuntimeDir(), name+".log")
}

func readDaemonPID(name string) int {
	pid, _ := processutil.ReadPID(daemonPIDPath(name))
	return pid
}

func writeDaemonPID(name string, pid int) error {
	return processutil.WritePID(daemonPIDPath(name), pid)
}

func clearDaemonPID(name string) {
	_ = processutil.ClearPID(daemonPIDPath(name))
}

// isDaemonRunning checks PID file and tmux session.
func isDaemonRunning(d sphereDaemon) (pid int, running bool) {
	if p := readDaemonPID(d.name); p > 0 && prefect.IsRunning(p) {
		return p, true
	}
	if d.session != "" && session.New().Exists(d.session) {
		return 0, true
	}
	return 0, false
}

// checkSystemdUnits returns names of daemons managed by systemd.
func checkSystemdUnits() []string {
	var managed []string
	for _, d := range sphereDaemons {
		unit := "sol-" + d.name + ".service"
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			managed = append(managed, d.name)
		}
	}
	return managed
}

// --- Styles ---

var (
	upOK  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	upErr = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	upDim = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// --- World helpers ---

// activeWorlds returns non-sleeping worlds. If specificWorld is non-empty,
// validates and returns it alone (errors if sleeping).
func activeWorlds(specificWorld string) ([]string, error) {
	if specificWorld != "" {
		if err := config.RequireWorld(specificWorld); err != nil {
			return nil, err
		}
		if config.IsSleeping(specificWorld) {
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
		if !config.IsSleeping(name) {
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
		if !config.IsSleeping(name) {
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
		failed, err := startSphereDaemons(solBin, upWorldsFlag)
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
	return nil
}

// startSphereDaemons starts sphere-level daemons. Returns true if any failed.
// If worlds is non-empty, the --worlds flag is passed to the prefect.
// Returns an error if sphere daemons are managed by systemd (dual management).
func startSphereDaemons(solBin string, worlds []string) (bool, error) {
	if managed := checkSystemdUnits(); len(managed) > 0 {
		return false, fmt.Errorf("sphere daemons managed by systemd (%s).\n"+
			"Use 'systemctl --user start/stop/restart' to manage them,\n"+
			"or 'sol service uninstall' to switch back to sol up",
			strings.Join(managed, ", "))
	}

	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		return false, fmt.Errorf("failed to create runtime directory: %w", err)
	}

	type result struct {
		name, status string
		pid          int
		err          error
	}
	var results []result

	for _, d := range sphereDaemons {
		r := result{name: d.name}

		// Idempotent: skip if already running.
		if pid, running := isDaemonRunning(d); running {
			r.status = "running"
			r.pid = pid
			results = append(results, r)
			continue
		}

		// Clear stale PID file.
		clearDaemonPID(d.name)

		// Open log file for stdout/stderr.
		logPath := daemonLogPath(d.name)
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			r.status = "failed"
			r.err = fmt.Errorf("log file: %w", err)
			results = append(results, r)
			continue
		}

		// Start: sol {component} run
		args := []string{d.name, "run"}
		if d.name == "prefect" {
			args = append(args, "--consul")
			if len(worlds) > 0 {
				args = append(args, "--worlds="+strings.Join(worlds, ","))
			}
		}
		proc := exec.Command(solBin, args...)
		proc.Stdout = logFile
		proc.Stderr = logFile
		proc.Env = append(os.Environ(), "SOL_HOME="+config.Home())
		proc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		if err := proc.Start(); err != nil {
			logFile.Close()
			r.status = "failed"
			r.err = err
			results = append(results, r)
			continue
		}

		pid := proc.Process.Pid
		logFile.Close()

		// Prefect writes its own PID in Run(); write PID for others.
		if d.name != "prefect" {
			_ = writeDaemonPID(d.name, pid)
		}

		// Reap the child in the background so it does not become a zombie if it
		// exits before sol up does. We must not call Release() — that would
		// prevent Go's runtime from reaping the child, leaving a defunct process
		// that IsRunning() would incorrectly report as alive.
		go func() { _ = proc.Wait() }()

		// Wait briefly and confirm alive.
		time.Sleep(time.Second)

		if prefect.IsRunning(pid) {
			r.status = "started"
			r.pid = pid
		} else {
			r.status = "failed"
			r.err = fmt.Errorf("exited immediately (check %s)", logPath)
			clearDaemonPID(d.name)
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

	// With --all, also stop envoys, governors, and chancellor.
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

// stopSphereDaemons stops sphere-level daemons and prints results.
// Returns true if any daemon failed to stop.
func stopSphereDaemons() bool {
	mgr := session.New()

	type result struct {
		name, status string
		pid          int
		err          error
	}
	var results []result

	// Stop daemons in reverse order — non-prefect first, prefect last so its
	// shutdown cascade doesn't race with individual stops.
	for i := len(sphereDaemons) - 1; i >= 0; i-- {
		d := sphereDaemons[i]
		r := result{name: d.name}
		stopped := false

		// PID-based stop.
		if pid := readDaemonPID(d.name); pid > 0 {
			if processutil.IsRunning(pid) {
				if err := processutil.GracefulKill(pid, 5*time.Second); err != nil {
					r.err = fmt.Errorf("stop pid %d: %w", pid, err)
				} else {
					r.pid = pid
					stopped = true
				}
			}
			clearDaemonPID(d.name)
		}

		// Tmux session stop.
		if d.session != "" && mgr.Exists(d.session) {
			if err := mgr.Stop(d.session, false); err != nil {
				if r.err == nil {
					r.err = fmt.Errorf("session %s: %w", d.session, err)
				}
			} else {
				stopped = true
			}
		}

		if r.err != nil {
			r.status = "failed"
		} else if stopped {
			r.status = "stopped"
		} else {
			r.status = "not running"
		}

		results = append(results, r)
	}

	// Print results.
	fmt.Println()
	hadFailure := false
	for _, r := range results {
		var indicator, detail string
		switch r.status {
		case "stopped":
			indicator = upOK.Render("✓")
			detail = "stopped"
			if r.pid > 0 {
				detail += upDim.Render(fmt.Sprintf("  pid %d", r.pid))
			}
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

// stopManagedSessions stops envoy, governor, and chancellor sessions.
// Called by sol down --all.
// Returns true if any session failed to stop.
func stopManagedSessions(worlds []string) bool {
	mgr := session.New()

	type result struct {
		role, name, status string
		err                error
	}
	var results []result

	// Query sphere store for envoys and governors.
	sphereStore, err := store.OpenSphere()
	if err == nil {
		agents, err := sphereStore.ListAgents("", "")
		if err == nil {
			for _, a := range agents {
				if a.Role != "envoy" && a.Role != "governor" {
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

	// Chancellor session.
	r := result{role: "chancellor", name: chancellorSession}
	if !mgr.Exists(chancellorSession) {
		r.status = "not running"
	} else if err := mgr.Stop(chancellorSession, false); err != nil {
		r.status = "failed"
		r.err = err
	} else {
		r.status = "stopped"
	}
	results = append(results, r)

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

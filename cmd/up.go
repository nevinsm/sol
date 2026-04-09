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

// writeDaemonPID writes pid to the named daemon's pidfile without acquiring a
// flock (pid != self uses the unlocked os.WriteFile branch). This is retained
// only for the ledger/broker `start` subcommands which record the child pid
// before the child's own WritePID has completed. Sphere daemon startup in
// startSphereDaemons no longer uses this: see the comment there and the writ
// sol-a0d18aac092e8ab4 for why parent-side writes are racy.
func writeDaemonPID(name string, pid int) error {
	return processutil.WritePID(daemonPIDPath(name), pid)
}

func clearDaemonPID(name string) {
	_ = processutil.ClearPID(daemonPIDPath(name))
}

// classifyDaemonStartup determines what happened after a parent spawned a
// sphere daemon child and waited briefly for it to take ownership of the
// pidfile. filePID is the pid currently recorded in the file (0 if empty or
// unreadable); childPid is the pid of the process we just spawned.
//
// Returns one of:
//   - "running": the pidfile records a live pid that is NOT our child. Our
//     child correctly detected another instance via its own WritePID flock
//     failing and exited cleanly — this is a success. The caller must NOT
//     clear the pidfile; the other instance owns it. ownerPID is filePID.
//   - "started": the pidfile records our child's pid and our child is alive.
//     The normal successful-start path. ownerPID is childPid.
//   - "failed": the pidfile is empty, stale, or our child is dead and it
//     owned the pidfile. The caller should use the defensive clearer
//     (clearDaemonPIDIfMine) to avoid clobbering another instance's file.
//     ownerPID is 0.
//
// See writ sol-a0d18aac092e8ab4 for the race this fixes: prefect's heartbeat
// loop can spawn consul directly, and that spawn can be in-flight while `sol
// up`'s iteration reaches consul — the `sol up` child then sees another
// running instance and exits nil, which the old single-IsRunning check
// misinterpreted as a crash and which caused clearDaemonPID to truncate the
// pidfile of the still-live original consul.
func classifyDaemonStartup(filePID, childPid int) (status string, ownerPID int) {
	if filePID > 0 && filePID != childPid && prefect.IsRunning(filePID) {
		return "running", filePID
	}
	if filePID == childPid && prefect.IsRunning(childPid) {
		return "started", childPid
	}
	return "failed", 0
}

// clearDaemonPIDIfMine is the defensive variant of clearDaemonPID. It only
// truncates the pidfile when the current contents are absent, stale (dead pid),
// or exactly expectedPid — never when the file contains a different live pid.
// Use this after a parent spawns a child that may have legitimately exited via
// the "already running" early-return path: in that case the pidfile still
// belongs to the other instance, and clobbering it would destroy a live
// daemon's only record of its own pid.
func clearDaemonPIDIfMine(name string, expectedPid int) {
	_ = processutil.ClearPIDIfMatches(daemonPIDPath(name), expectedPid)
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

		// Every sphere daemon writes its own PID via processutil.WritePID
		// (with flock) from inside its run command. The parent must NOT write
		// the child's PID here: the unlocked os.WriteFile branch would race
		// against the child's own WritePID and, worse, could clobber the
		// pidfile of an OTHER running instance when prefect's heartbeat loop
		// has already spawned the daemon. See docs for ClearPIDIfMatches.

		// Reap the child in the background so it does not become a zombie if it
		// exits before sol up does. We must not call Release() — that would
		// prevent Go's runtime from reaping the child, leaving a defunct process
		// that IsRunning() would incorrectly report as alive.
		go func() { _ = proc.Wait() }()

		// Wait briefly and determine the daemon's state from the pidfile.
		time.Sleep(time.Second)

		status, ownerPID := classifyDaemonStartup(readDaemonPID(d.name), pid)
		switch status {
		case "running":
			r.status = "running"
			r.pid = ownerPID
		case "started":
			r.status = "started"
			r.pid = pid
		default:
			r.status = "failed"
			r.err = fmt.Errorf("exited immediately (check %s)", logPath)
			clearDaemonPIDIfMine(d.name, pid)
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

	// stopOne kills a single daemon by PID and/or tmux session.
	stopOne := func(d sphereDaemon) result {
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
		return r
	}

	// Kill prefect first — it's the supervisor that restarts other daemons.
	// Must be fully dead before we stop anything else, otherwise its heartbeat
	// loop can respawn daemons (especially consul) between their kill and
	// prefect's own kill, leaving orphaned processes.
	var prefectDaemon sphereDaemon
	for _, d := range sphereDaemons {
		if d.name == "prefect" {
			prefectDaemon = d
			break
		}
	}
	results = append(results, stopOne(prefectDaemon))

	// Now kill remaining daemons in reverse order — with prefect dead, nothing
	// will respawn them.
	for i := len(sphereDaemons) - 1; i >= 0; i-- {
		d := sphereDaemons[i]
		if d.name == "prefect" {
			continue // already handled
		}
		results = append(results, stopOne(d))
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

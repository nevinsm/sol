package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/session"
	"github.com/spf13/cobra"
)

// consulTmuxSession is the tmux session name used when consul is managed
// by the prefect (matches the constant in internal/prefect).
const consulTmuxSession = "sol-sphere-consul"

// sphereDaemon describes a sphere-level daemon managed by sol up/down.
type sphereDaemon struct {
	name    string
	session string // tmux session name to check (if managed via tmux)
}

var sphereDaemons = []sphereDaemon{
	{name: "prefect"},
	{name: "consul", session: consulTmuxSession},
	{name: "chronicle", session: chronicleSessionName},
}

var upCmd = &cobra.Command{
	Use:          "up",
	Short:        "Start sphere-level daemons (prefect, consul, chronicle)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runUp,
}

var downCmd = &cobra.Command{
	Use:          "down",
	Short:        "Stop sphere-level daemons (prefect, consul, chronicle)",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runDown,
}

func init() {
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)
}

// --- PID helpers ---

func daemonPIDPath(name string) string {
	return filepath.Join(config.RuntimeDir(), name+".pid")
}

func daemonLogPath(name string) string {
	return filepath.Join(config.RuntimeDir(), name+".log")
}

func readDaemonPID(name string) int {
	if name == "prefect" {
		pid, _ := prefect.ReadPID()
		return pid
	}
	data, err := os.ReadFile(daemonPIDPath(name))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

func writeDaemonPID(name string, pid int) error {
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(daemonPIDPath(name), []byte(strconv.Itoa(pid)), 0o644)
}

func clearDaemonPID(name string) {
	if name == "prefect" {
		_ = prefect.ClearPID()
		return
	}
	_ = os.Remove(daemonPIDPath(name))
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

// --- sol up ---

func runUp(_ *cobra.Command, _ []string) error {
	if managed := checkSystemdUnits(); len(managed) > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		fmt.Fprintf(os.Stderr, "%s %s managed by systemd — use systemctl instead\n",
			warnStyle.Render("Warning:"), strings.Join(managed, ", "))
	}

	solBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find sol binary: %w", err)
	}

	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
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
		proc := exec.Command(solBin, d.name, "run")
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

		// Detach so daemon survives sol up exit.
		_ = proc.Process.Release()

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
		}
		fmt.Printf("  %s %-12s %s\n", indicator, r.name, detail)
	}
	fmt.Println()

	for _, r := range results {
		if r.status == "failed" {
			return fmt.Errorf("some daemons failed to start")
		}
	}
	return nil
}

// --- sol down ---

func runDown(_ *cobra.Command, _ []string) error {
	mgr := session.New()

	type result struct {
		name, status string
		pid          int
		err          error
	}
	var results []result

	for _, d := range sphereDaemons {
		r := result{name: d.name}
		stopped := false

		// PID-based stop.
		if pid := readDaemonPID(d.name); pid > 0 {
			if prefect.IsRunning(pid) {
				if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
					r.err = fmt.Errorf("SIGTERM pid %d: %w", pid, err)
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
		}
		fmt.Printf("  %s %-12s %s\n", indicator, r.name, detail)
	}
	fmt.Println()

	return nil
}

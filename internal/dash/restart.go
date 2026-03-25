package dash

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/prefect"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
)

// --- Sphere process restart (from sphere view) ---

// restartDoneMsg carries the result of a process restart.
type restartDoneMsg struct {
	processName string
	err         error
}

// sphereProcessInfo describes a sphere process for restart purposes.
type sphereProcessInfo struct {
	cliName     string // CLI subcommand name (e.g., "prefect", "consul")
	sessionName string // tmux session name (empty for PID-only processes)
	startCmd    string // CLI subcommand for starting (e.g., "run", "start")
	pidBased    bool   // true for processes that use PID files
	tmuxManaged bool   // true for processes that use tmux sessions
}

// sphereProcessMap maps display names to process info for restart.
var sphereProcessMap = map[string]sphereProcessInfo{
	"Prefect":   {cliName: "prefect", startCmd: "run", pidBased: true},
	"Consul":    {cliName: "consul", startCmd: "run", pidBased: true},
	"Chronicle": {cliName: "chronicle", startCmd: "run", pidBased: true},
	"Ledger":    {cliName: "ledger", startCmd: "run", pidBased: true},
	"Broker":    {cliName: "broker", startCmd: "run", pidBased: true},
	"Chancellor": {cliName: "chancellor", sessionName: "sol-chancellor", startCmd: "start", tmuxManaged: true},
}

// checkSystemdManaged checks if a sphere process is managed by systemd.
func checkSystemdManaged(cliName string) bool {
	unit := "sol-" + cliName + ".service"
	return exec.Command("systemctl", "--user", "is-active", "--quiet", unit).Run() == nil
}

// restartSphereProcess stops and re-launches a sphere process.
// It follows the patterns from cmd/up.go.
func restartSphereProcess(solBin, name string) error {
	info, ok := sphereProcessMap[name]
	if !ok {
		return fmt.Errorf("unknown sphere process: %s", name)
	}

	// Systemd guard.
	if checkSystemdManaged(info.cliName) {
		return fmt.Errorf("managed by systemd — use systemctl --user restart sol-%s", info.cliName)
	}

	mgr := session.New()

	// --- Stop phase ---

	// PID-based stop.
	if info.pidBased {
		pid := readProcessPID(info.cliName)
		if pid > 0 && prefect.IsRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				return fmt.Errorf("failed to SIGTERM %s (pid %d): %w", name, pid, err)
			}
			// Wait for process to exit.
			for i := 0; i < 10; i++ {
				time.Sleep(500 * time.Millisecond)
				if !prefect.IsRunning(pid) {
					break
				}
			}
			if prefect.IsRunning(pid) {
				return fmt.Errorf("%s (pid %d) did not exit after SIGTERM", name, pid)
			}
		}
		clearProcessPID(info.cliName)
	}

	// Tmux session stop.
	if info.tmuxManaged && info.sessionName != "" {
		if mgr.Exists(info.sessionName) {
			if err := mgr.Stop(info.sessionName, false); err != nil {
				return fmt.Errorf("failed to stop session %s: %w", info.sessionName, err)
			}
		}
	}

	// --- Start phase ---

	logPath := processLogPath(info.cliName)
	pid, err := processutil.StartDaemon(logPath, append(os.Environ(), "SOL_HOME="+config.Home()), solBin, info.cliName, info.startCmd)
	if err != nil {
		return fmt.Errorf("failed to start %s: %w", name, err)
	}

	// Write PID file (prefect writes its own).
	if info.cliName != "prefect" && info.pidBased {
		_ = writeProcessPID(info.cliName, pid)
	}

	// Verify alive after 1 second.
	time.Sleep(time.Second)
	if !prefect.IsRunning(pid) {
		clearProcessPID(info.cliName)
		return fmt.Errorf("%s exited immediately (check %s)", name, logPath)
	}

	return nil
}

// readProcessPID reads the PID from the runtime PID file.
func readProcessPID(cliName string) int {
	if cliName == "prefect" {
		pid, _ := prefect.ReadPID()
		return pid
	}
	pidPath := processFilePath(cliName, ".pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		return 0
	}
	return pid
}

// clearProcessPID removes the PID file.
func clearProcessPID(cliName string) {
	if cliName == "prefect" {
		_ = prefect.ClearPID()
		return
	}
	_ = os.Remove(processFilePath(cliName, ".pid"))
}

// writeProcessPID writes the PID to the runtime PID file.
func writeProcessPID(cliName string, pid int) error {
	if err := os.MkdirAll(config.RuntimeDir(), 0o755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	if err := os.WriteFile(processFilePath(cliName, ".pid"), []byte(fmt.Sprintf("%d", pid)), 0o644); err != nil {
		return fmt.Errorf("failed to write PID file for %s: %w", cliName, err)
	}
	return nil
}

// processFilePath returns the path for a process runtime file.
func processFilePath(cliName, suffix string) string {
	return config.RuntimeDir() + "/" + cliName + suffix
}

// processLogPath returns the log file path for a process.
func processLogPath(cliName string) string {
	return processFilePath(cliName, ".log")
}

// sphereRestartCmd returns a tea.Cmd that restarts a sphere process.
func sphereRestartCmd(processName string) tea.Cmd {
	return func() tea.Msg {
		solBin, err := os.Executable()
		if err != nil {
			return restartDoneMsg{processName: processName, err: fmt.Errorf("failed to find sol binary: %w", err)}
		}

		err = restartSphereProcess(solBin, processName)
		return restartDoneMsg{processName: processName, err: err}
	}
}

// --- World-level restart (agents and services from world view) ---

// restartTarget describes an item to restart from the world view.
type restartTarget struct {
	name          string // display name (agent name or service name)
	role          string // "outpost", "envoy", "forge", "sentinel", "governor"
	world         string
	sessionName   string // tmux session name
	confirmTitle  string // e.g. "Restart Toast?"
	confirmDetail string // e.g. "Kill session and re-cast tethered writ"
}

// requestRestartMsg is emitted by the world view when R is pressed on a restartable item.
type requestRestartMsg struct {
	target restartTarget
}

// worldRestartDoneMsg is emitted when a world-level restart operation completes.
type worldRestartDoneMsg struct {
	name string
	err  error
}

// clearRestartFeedbackMsg triggers clearing the inline feedback message.
type clearRestartFeedbackMsg struct{}

// worldRestartCmd returns a tea.Cmd that executes a world-level restart in a goroutine.
func worldRestartCmd(target restartTarget) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch target.role {
		case "outpost", "envoy":
			err = restartAgent(target.world, target.name, target.role, target.sessionName)
		case "forge", "sentinel", "governor":
			err = restartService(target.world, target.role)
		default:
			err = fmt.Errorf("unknown restart target role %q", target.role)
		}
		return worldRestartDoneMsg{name: target.name, err: err}
	}
}

// restartAgent stops a tmux session and respawns using startup.Respawn.
func restartAgent(world, name, role, sessionName string) error {
	mgr := session.New()
	// Force-stop the session (ignore error — session may already be dead).
	_ = mgr.Stop(sessionName, true)

	// Respawn via startup — it opens its own sphere store when opts.Sphere is nil.
	_, err := startup.Respawn(role, world, name, startup.LaunchOpts{})
	if err != nil {
		return fmt.Errorf("failed to respawn agent %s: %w", name, err)
	}
	return nil
}

// restartService shells out to `sol <service> stop` then `sol <service> start`.
func restartService(world, service string) error {
	solBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find sol binary: %w", err)
	}

	// Stop (ignore error — service may not be running).
	stopCmd := exec.Command(solBin, service, "stop", "--world="+world)
	_ = stopCmd.Run()

	// Start.
	startCmd := exec.Command(solBin, service, "start", "--world="+world)
	out, startErr := startCmd.CombinedOutput()
	if startErr != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// scheduleClearFeedback returns a Cmd that fires clearRestartFeedbackMsg after 3 seconds.
func scheduleClearFeedback() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return clearRestartFeedbackMsg{}
	})
}

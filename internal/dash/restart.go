package dash

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/daemon"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
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
}

// systemctlIsActive reports whether the given systemd --user unit is active.
// Package-level indirection so dash's tests aren't coupled to whether sol
// happens to be installed as a systemd service (via `sol service install`)
// on the machine running the test suite — see checkSystemdManaged.
var systemctlIsActive = func(unit string) bool {
	return exec.Command("systemctl", "--user", "is-active", "--quiet", unit).Run() == nil
}

// checkSystemdManaged checks if a sphere process is managed by systemd.
func checkSystemdManaged(cliName string) bool {
	unit := "sol-" + cliName + ".service"
	return systemctlIsActive(unit)
}

// systemdManaged is the injectable systemd-managed probe. Production code
// defaults to the real host check (checkSystemdManaged); tests override it
// to pin the guard to a known value, keeping restart-confirmation tests
// hermetic regardless of whether the host actually runs sol as systemd
// user services.
var systemdManaged = checkSystemdManaged

// restartSphereProcess stops and re-launches a sphere process.
//
// The stop/start sequence itself — SIGTERM + poll + escalate to SIGKILL on
// the way down, spawn + probe-delay + pidfile classification on the way up —
// is not reimplemented here; it's the same daemon.Stop/daemon.Start pair
// `sol up`/`sol down` use (see cmd/up.go's stopSphereDaemons/
// startSphereDaemons). solBin is accepted for backward compatibility with
// existing callers/tests but is otherwise unused: daemon.Start resolves the
// running sol binary itself.
func restartSphereProcess(_, name string) error {
	info, ok := sphereProcessMap[name]
	if !ok {
		return fmt.Errorf("unknown sphere process: %s", name)
	}

	// Systemd guard — runs before the shared stop/start path so a
	// systemd-managed daemon is never touched by dash.
	if systemdManaged(info.cliName) {
		return fmt.Errorf("managed by systemd — use systemctl --user restart sol-%s", info.cliName)
	}

	// Tmux-managed sphere processes (none currently) stop via the session
	// manager instead of the PID-based daemon path below.
	if info.tmuxManaged && info.sessionName != "" {
		mgr := session.New()
		if mgr.Exists(info.sessionName) {
			if err := mgr.Stop(info.sessionName, false); err != nil {
				return fmt.Errorf("failed to stop session %s: %w", info.sessionName, err)
			}
		}
	}

	if !info.pidBased {
		return nil
	}

	lc, ok := daemon.SphereLifecycle(info.cliName)
	if !ok {
		return fmt.Errorf("no lifecycle registered for sphere process: %s", info.cliName)
	}
	lc.Env = append(os.Environ(), "SOL_HOME="+config.Home())

	if err := daemon.Stop(lc); err != nil {
		return fmt.Errorf("failed to stop %s: %w", name, err)
	}

	if _, err := daemon.Start(lc); err != nil {
		return fmt.Errorf("failed to start %s: %w", name, err)
	}

	return nil
}

// sphereRestartCmd returns a tea.Cmd that restarts a sphere process.
func sphereRestartCmd(processName string) tea.Cmd {
	return func() tea.Msg {
		// The sol binary path is no longer resolved here: daemon.Start (via
		// restartSphereProcess) resolves it itself, the same way
		// cmd/up.go's startSphereDaemons does.
		err := restartSphereProcess("", processName)
		return restartDoneMsg{processName: processName, err: err}
	}
}

// --- World-level restart (agents and services from world view) ---

// restartTarget describes an item to restart from the world view.
type restartTarget struct {
	name          string // display name (agent name or service name)
	role          string // "outpost", "envoy", "forge", "sentinel"
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
		case "forge", "sentinel":
			err = restartService(target.world, target.role)
		default:
			err = fmt.Errorf("unknown restart target role %q", target.role)
		}
		return worldRestartDoneMsg{name: target.name, err: err}
	}
}

// restartAgent stops a tmux session and respawns using startup.Respawn.
func restartAgent(world, name, role, sessionName string) error {
	// Hold the agent lock to prevent concurrent operator commands (start/stop/restart/delete)
	// from racing on agent state.
	agentID := world + "/" + name
	agentLock, err := flock.AcquireAgentLock(agentID)
	if err != nil {
		return fmt.Errorf("failed to acquire agent lock for restart: %w", err)
	}
	defer agentLock.Release()

	mgr := session.New()
	// Force-stop the session (ignore error — session may already be dead).
	_ = mgr.Stop(sessionName, true)

	// Respawn via startup — it opens its own sphere store when opts.Sphere is nil.
	writExists := func(id string) bool {
		if id == "" {
			return true
		}
		ws, err := store.OpenWorld(world)
		if err != nil {
			return true // transient: treat as exists
		}
		defer ws.Close()
		_, err = ws.GetWrit(id)
		if errors.Is(err, store.ErrNotFound) {
			return false
		}
		return true
	}
	_, err = startup.Respawn(role, world, name, startup.LaunchOpts{
		WritExists: writExists,
	})
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

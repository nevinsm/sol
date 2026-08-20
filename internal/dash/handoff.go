package dash

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/envoy"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/flock"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
)

// --- Graceful handoff (from world view, agent/envoy rows) ---
//
// Handoff (ADR-0023) cycles a session before context exhaustion, preserving
// committed code and writ binding, so the successor session resumes with
// full git history. 'R' restart is the blunt alternative (kill + re-cast);
// 'H' handoff is the graceful one — it prompts the agent to save state
// (internal/sessionsave, via internal/handoff.Exec) before cycling.
//
// This file wires dash's 'H' key to the same internal/handoff.Exec entry
// point cmd/handoff.go (`sol handoff`) drives — no handoff semantics are
// reimplemented here, only the dash-side request/confirm/dispatch plumbing.

// handoffTarget describes an item to hand off from the world view.
type handoffTarget struct {
	name          string
	role          string // "outpost" or "envoy"
	world         string
	sessionName   string
	confirmTitle  string
	confirmDetail string
}

// requestHandoffMsg is emitted by the world view when H is pressed on a
// handoff-capable item, requesting a confirmation.
type requestHandoffMsg struct {
	target handoffTarget
}

// worldHandoffDoneMsg carries the result of a world-level handoff operation.
type worldHandoffDoneMsg struct {
	name string
	err  error
}

// worldHandoffCmd returns a tea.Cmd that executes a graceful handoff in a
// goroutine, mirroring worldRestartCmd's async shape — handoff involves
// prompting the agent to save state and can take time.
func worldHandoffCmd(target handoffTarget) tea.Cmd {
	return func() tea.Msg {
		sphereStore, err := store.OpenSphere()
		if err != nil {
			return worldHandoffDoneMsg{name: target.name, err: fmt.Errorf("failed to open sphere store: %w", err)}
		}
		defer sphereStore.Close()

		mgr := session.New()
		err = handoffAgent(target.world, target.name, target.role, mgr, sphereStore)
		return worldHandoffDoneMsg{name: target.name, err: err}
	}
}

// handoffAgent triggers a graceful handoff for a live agent session via
// internal/handoff.Exec — the same function cmd/handoff.go calls — with the
// session manager and sphere store injected so this is unit-testable with
// fakes. Mirrors restartAgent's locking pattern (internal/dash/restart.go):
// handoff.Exec does not itself acquire the agent lock, so we hold it here to
// prevent concurrent operator commands (start/stop/restart/delete) from
// racing on agent state during the cycle.
func handoffAgent(world, name, role string, mgr handoff.SessionManager, sphereStore handoff.SphereStore) error {
	agentID := world + "/" + name
	agentLock, err := flock.AcquireAgentLock(agentID)
	if err != nil {
		return fmt.Errorf("failed to acquire agent lock for handoff: %w", err)
	}
	defer agentLock.Release()

	logger := events.NewLogger(config.Home())

	return handoff.Exec(handoff.ExecOpts{
		World:       world,
		AgentName:   name,
		Role:        role,
		WorktreeDir: handoffWorktreeDir(world, name, role),
		Reason:      "manual",
		SelfInvoked: false, // dash is an operator acting on the session, never the session itself
	}, mgr, sphereStore, logger)
}

// handoffWorktreeDir resolves an agent's worktree path by role — mirrors
// cmd/handoff.go's worktreeDirForRole for the two roles dash's handoff
// action supports (outpost, envoy). Forge is out of scope: dash only
// triggers handoff from outpost/envoy rows.
func handoffWorktreeDir(world, name, role string) string {
	if role == "envoy" {
		return envoy.WorktreePath(world, name)
	}
	return config.WorktreePath(world, name)
}

// handleHandoff builds a handoff confirmation request for the currently
// focused outpost or envoy row. Dead sessions (no live tmux session) and
// idle sessions (no work in progress) get an inline message instead of a
// confirmation — handoff exists to gracefully cycle a working session, not
// to revive an idle or dead one.
func (wm worldModel) handleHandoff(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if data == nil || !wm.hasFocus {
		return wm, nil
	}

	var target handoffTarget
	target.world = data.World

	switch wm.focusedSection {
	case sectionOutposts:
		if wm.outpostCursor >= len(data.Agents) {
			return wm, nil
		}
		a := data.Agents[wm.outpostCursor]
		if msg := handoffGateMessage(a.Name, a.State, a.SessionAlive); msg != "" {
			return wm, func() tea.Msg { return noSessionMsg{message: msg} }
		}
		target.name = a.Name
		target.role = "outpost"
		target.sessionName = config.SessionName(data.World, a.Name)
		target.confirmTitle = fmt.Sprintf("Handoff %s?", a.Name)
		target.confirmDetail = "Gracefully cycle the session (state saved, tether preserved) — see R for a hard restart"

	case sectionEnvoys:
		if wm.envoyCursor >= len(data.Envoys) {
			return wm, nil
		}
		e := data.Envoys[wm.envoyCursor]
		if msg := handoffGateMessage(e.Name, e.State, e.SessionAlive); msg != "" {
			return wm, func() tea.Msg { return noSessionMsg{message: msg} }
		}
		target.name = e.Name
		target.role = "envoy"
		target.sessionName = config.SessionName(data.World, e.Name)
		target.confirmTitle = fmt.Sprintf("Handoff %s?", e.Name)
		target.confirmDetail = "Gracefully cycle the session (state saved, tether preserved) — see R for a hard restart"

	default:
		return wm, nil
	}

	return wm, func() tea.Msg { return requestHandoffMsg{target: target} }
}

// handoffGateMessage returns a descriptive "can't hand off" message for a
// dead or idle agent/envoy, or "" when the row is a live, working session
// eligible for handoff.
func handoffGateMessage(name, state string, sessionAlive bool) string {
	if !sessionAlive {
		return fmt.Sprintf("%s has no active session to hand off", name)
	}
	if state != "working" {
		return fmt.Sprintf("%s is idle — nothing to hand off", name)
	}
	return ""
}

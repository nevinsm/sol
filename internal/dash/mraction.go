package dash

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// mrAction identifies which merge-queue action is being requested for the
// focused failed MR.
type mrAction int

const (
	mrActionRequeue mrAction = iota
	mrActionSupersede
)

// requestMRActionMsg is emitted once the guard-check query (writ status +
// tether check) for a requeue/supersede request on the focused failed MR
// completes. confirmDetail already carries the guard findings so the
// operator sees them before confirming; blocked means the confirm overlay
// should be informational only (no action on 'y').
type requestMRActionMsg struct {
	world         string
	mrID          string
	action        mrAction
	confirmTitle  string
	confirmDetail string
	blocked       bool
}

// mrActionDoneMsg carries the result of executing a requeue/supersede action.
type mrActionDoneMsg struct {
	action mrAction
	mrID   string
	err    error
}

// mrGuardCmd queries the current writ status and tether state for mrID's
// writ and returns a requestMRActionMsg describing what the confirm overlay
// should show. This is the "operator guard checks that are currently manual
// habit" the merge-queue actions exist to surface:
//   - a closed writ blocks requeue outright (resurrecting a merge attempt
//     for closed work would be wrong — supersede instead).
//   - a writ currently tethered to an outpost means the work was already
//     re-dispatched; the failed MR's branch is likely stale, so requeue is
//     still offered but with a warning steering the operator to supersede.
//
// Supersede has no block condition: marking a failed MR terminal is always
// safe regardless of writ state (it's exactly what CloseWrit does
// automatically for a writ's failed MRs on close).
func mrGuardCmd(world, mrID, writID string, action mrAction) tea.Cmd {
	return func() tea.Msg {
		msg := requestMRActionMsg{world: world, mrID: mrID, action: action}

		worldStore, err := store.OpenWorld(world)
		writStatus := "(unable to verify)"
		if err == nil {
			defer worldStore.Close()
			if w, gErr := worldStore.GetWrit(writID); gErr == nil {
				writStatus = w.Status
			}
		}

		var tetheredBy string
		if sphereStore, sErr := store.OpenSphere(); sErr == nil {
			defer sphereStore.Close()
			if agents, lErr := sphereStore.ListAgents(world, ""); lErr == nil {
				for _, a := range agents {
					if a.Role == "outpost" && a.ActiveWrit == writID {
						tetheredBy = a.Name
						break
					}
				}
			}
		}

		switch action {
		case mrActionRequeue:
			msg.confirmTitle = fmt.Sprintf("Requeue %s?", mrID)
			if writStatus == "closed" {
				msg.blocked = true
				msg.confirmDetail = fmt.Sprintf(
					"Writ %s is closed — a closed writ's MR must not be requeued. Supersede it instead.",
					writID)
				return msg
			}
			var lines []string
			lines = append(lines, fmt.Sprintf("Writ %s status: %s.", writID, writStatus))
			if tetheredBy != "" {
				lines = append(lines, fmt.Sprintf(
					"Warning: writ is currently tethered to %s — this MR's branch is likely stale from before re-dispatch. Consider superseding instead.",
					tetheredBy))
			}
			lines = append(lines, "Resets attempts and moves the MR back to ready for forge to reclaim.")
			msg.confirmDetail = strings.Join(lines, " ")

		case mrActionSupersede:
			msg.confirmTitle = fmt.Sprintf("Supersede %s?", mrID)
			var lines []string
			lines = append(lines, fmt.Sprintf("Writ %s status: %s.", writID, writStatus))
			if tetheredBy != "" {
				lines = append(lines, fmt.Sprintf("Writ is currently tethered to %s.", tetheredBy))
			}
			lines = append(lines, "Marks the MR as superseded (terminal) — a new MR will be needed to merge this work.")
			msg.confirmDetail = strings.Join(lines, " ")
		}

		return msg
	}
}

// mrRequeueCmd resets mrID from failed back to ready via
// ResetMergeRequestForRetry — the same store path resolve.go's conflict
// resolution flow uses to put a parent MR back in play — then mirrors `sol
// mr create`'s post-ready housekeeping (cmd/mr.go): emit EventMergeQueued
// and nudge forge so the ready MR is picked up promptly.
func mrRequeueCmd(world, mrID string) tea.Cmd {
	return func() tea.Msg {
		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return mrActionDoneMsg{action: mrActionRequeue, mrID: mrID, err: err}
		}
		defer worldStore.Close()

		if err := worldStore.ResetMergeRequestForRetry(mrID); err != nil {
			return mrActionDoneMsg{action: mrActionRequeue, mrID: mrID, err: err}
		}

		// Auto-resolve the escalation forge raised when this MR failed
		// (best-effort) — mirrors the escalation cleanup cmd/writ.go's
		// `close` and forge's markMergedImpl perform for superseded MRs.
		resolveMREscalations(mrID)

		// Nudge forge that a ready MR is waiting (best-effort — forge still
		// polls, but this speeds pickup, same as resolve.go/mr.go).
		mr, mrErr := worldStore.GetMergeRequest(mrID)
		branch, title := "", ""
		writID := ""
		if mrErr == nil {
			branch = mr.Branch
			writID = mr.WritID
			if item, wErr := worldStore.GetWrit(mr.WritID); wErr == nil {
				title = item.Title
			}
		}

		eventLog := events.NewLogger(config.Home())
		eventLog.Emit(events.EventMergeQueued, "sol", config.ResolveActorIdentity(""), "both", map[string]string{
			"merge_request_id": mrID,
			"writ_id":          writID,
			"branch":           branch,
			"world":            world,
			"via":              "dash_requeue",
		})

		forgeSession := config.SessionName(world, "forge")
		_ = nudge.Deliver(forgeSession, nudge.Message{
			Sender:   config.Autarch,
			Type:     "MR_READY",
			Subject:  fmt.Sprintf("MR %s requeued for merge", mrID),
			Body:     fmt.Sprintf(`{"writ_id":%q,"merge_request_id":%q,"branch":%q,"title":%q}`, writID, mrID, branch, title),
			Priority: "normal",
		})

		return mrActionDoneMsg{action: mrActionRequeue, mrID: mrID}
	}
}

// mrSupersedeCmd marks mrID as superseded via the same UpdateMergeRequestPhase
// transition path CloseWrit and forge's Release/MarkFailed use — failed →
// superseded is a legal transition in the store's transition table
// (validMRTransition), so no new transition is introduced here.
func mrSupersedeCmd(world, mrID string) tea.Cmd {
	return func() tea.Msg {
		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return mrActionDoneMsg{action: mrActionSupersede, mrID: mrID, err: err}
		}
		defer worldStore.Close()

		if err := worldStore.UpdateMergeRequestPhase(mrID, "superseded"); err != nil {
			return mrActionDoneMsg{action: mrActionSupersede, mrID: mrID, err: err}
		}

		// Auto-resolve the escalation forge raised when this MR failed
		// (best-effort) — mirrors cmd/writ.go's `close` command, which does
		// the same for MRs superseded by writ closure.
		resolveMREscalations(mrID)

		return mrActionDoneMsg{action: mrActionSupersede, mrID: mrID}
	}
}

// resolveMREscalations auto-resolves any open escalations linked to mrID
// (best-effort — matches the "mr:"+id sourceRef convention used by
// cmd/writ.go's `close` command and internal/forge/toolbox.go's
// markMergedImpl).
func resolveMREscalations(mrID string) {
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return
	}
	defer sphereStore.Close()
	escalations, err := sphereStore.ListEscalationsBySourceRef("mr:" + mrID)
	if err != nil {
		return
	}
	for _, esc := range escalations {
		_ = sphereStore.ResolveEscalation(esc.ID)
	}
}

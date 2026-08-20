package dash

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/status"
)

// mrFixture creates a world store with one writ and one merge request in
// "failed" phase (claimed then failed, mirroring how forge's MarkFailed
// actually gets an MR into this state) and returns their IDs. Callers set
// SOL_HOME via t.Setenv before calling.
func mrFixture(t *testing.T, world string) (writID, mrID string) {
	t.Helper()
	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	defer ws.Close()

	writID, err = ws.CreateWrit("fix things", "desc", "tester", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	mrID, err = ws.CreateMergeRequest(writID, "agent/branch-1", 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}
	if _, err := ws.ClaimMergeRequest("claimer", 0); err != nil {
		t.Fatalf("ClaimMergeRequest: %v", err)
	}
	if err := ws.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
		t.Fatalf("UpdateMergeRequestPhase(failed): %v", err)
	}
	return writID, mrID
}

func TestMRGuardCmdBlocksRequeueOnClosedWrit(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	writID, mrID := mrFixture(t, world)

	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	// Close the writ directly via UpdateWrit (not CloseWrit) so the failed
	// MR is NOT auto-superseded — CloseWrit would supersede it as a side
	// effect, making it impossible to exercise the "closed writ, still-failed
	// MR" guard case this test targets.
	if err := ws.UpdateWrit(writID, store.WritUpdates{Status: "closed"}); err != nil {
		t.Fatalf("UpdateWrit: %v", err)
	}
	ws.Close()

	msg := mrGuardCmd(world, mrID, writID, mrActionRequeue)().(requestMRActionMsg)
	if !msg.blocked {
		t.Fatalf("expected requeue on a closed writ to be blocked, got %+v", msg)
	}
	if !strings.Contains(msg.confirmDetail, "closed") {
		t.Errorf("expected confirmDetail to explain the closed-writ block, got %q", msg.confirmDetail)
	}
}

func TestMRGuardCmdWarnsRequeueOnTetheredWrit(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	writID, mrID := mrFixture(t, world)

	// The writ was reopened (as forge's MarkFailed does) and re-dispatched
	// to a fresh outpost — simulate that by tethering an agent to it.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("OpenSphere: %v", err)
	}
	if err := ss.EnsureAgent("Nova", world, "outpost"); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	if err := ss.UpdateAgentState(world+"/Nova", "working", writID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	ss.Close()

	msg := mrGuardCmd(world, mrID, writID, mrActionRequeue)().(requestMRActionMsg)
	if msg.blocked {
		t.Fatalf("tethered-but-open writ should warn, not block: %+v", msg)
	}
	if !strings.Contains(msg.confirmDetail, "Nova") {
		t.Errorf("expected confirmDetail to name the tethering agent, got %q", msg.confirmDetail)
	}
	if !strings.Contains(strings.ToLower(msg.confirmDetail), "supersed") {
		t.Errorf("expected confirmDetail to steer the operator toward supersede, got %q", msg.confirmDetail)
	}
}

func TestMRGuardCmdAllowsRequeueNormalCase(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	writID, mrID := mrFixture(t, world)

	msg := mrGuardCmd(world, mrID, writID, mrActionRequeue)().(requestMRActionMsg)
	if msg.blocked {
		t.Fatalf("expected requeue to be allowed for an open, untethered writ, got %+v", msg)
	}
	if strings.Contains(msg.confirmDetail, "Warning") {
		t.Errorf("did not expect a tether warning, got %q", msg.confirmDetail)
	}
}

func TestMRGuardCmdSupersedeNeverBlocked(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	writID, mrID := mrFixture(t, world)

	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	if err := ws.UpdateWrit(writID, store.WritUpdates{Status: "closed"}); err != nil {
		t.Fatalf("UpdateWrit: %v", err)
	}
	ws.Close()

	msg := mrGuardCmd(world, mrID, writID, mrActionSupersede)().(requestMRActionMsg)
	if msg.blocked {
		t.Fatalf("supersede should never be blocked, got %+v", msg)
	}
}

func TestMRRequeueCmdTransitionsToReady(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	_, mrID := mrFixture(t, world)

	msg := mrRequeueCmd(world, mrID)().(mrActionDoneMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}

	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	defer ws.Close()
	mr, err := ws.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != "ready" {
		t.Errorf("expected phase=ready after requeue, got %q", mr.Phase)
	}
	if mr.Attempts != 0 {
		t.Errorf("expected attempts reset to 0, got %d", mr.Attempts)
	}
	if mr.ClaimedBy != "" {
		t.Errorf("expected claimed_by cleared, got %q", mr.ClaimedBy)
	}
}

// TestMRRequeueCmdRejectsSupersededMR verifies the store's own transition
// table (ResetMergeRequestForRetry's WHERE clause only accepts source
// phases ready/claimed/failed) is what gates requeue, not any dash-level
// check — a superseded MR (terminal) must reject it exactly like any other
// caller of ResetMergeRequestForRetry would see.
func TestMRRequeueCmdRejectsSupersededMR(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	_, mrID := mrFixture(t, world)

	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	if err := ws.UpdateMergeRequestPhase(mrID, "superseded"); err != nil {
		t.Fatalf("UpdateMergeRequestPhase(superseded): %v", err)
	}
	ws.Close()

	msg := mrRequeueCmd(world, mrID)().(mrActionDoneMsg)
	if msg.err == nil {
		t.Fatal("expected requeue of a superseded (terminal) MR to fail")
	}
}

func TestMRSupersedeCmdTransitionsFromFailed(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	_, mrID := mrFixture(t, world)

	msg := mrSupersedeCmd(world, mrID)().(mrActionDoneMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}

	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	defer ws.Close()
	mr, err := ws.GetMergeRequest(mrID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	if mr.Phase != "superseded" {
		t.Errorf("expected phase=superseded, got %q", mr.Phase)
	}
}

// TestMRSupersedeCmdRejectsFromReady checks fidelity to the store's
// transition table: supersede is only legal from "failed" (validMRTransition
// case "failed": to == "superseded"); a ready MR must not be superseded
// through this path since UpdateMergeRequestPhase's WHERE clause for target
// "superseded" only accepts from=failed/superseded.
func TestMRSupersedeCmdRejectsFromReady(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	writID, err := ws.CreateWrit("fix things", "desc", "tester", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	mrID, err := ws.CreateMergeRequest(writID, "agent/branch-1", 2)
	if err != nil {
		t.Fatalf("CreateMergeRequest: %v", err)
	}
	ws.Close() // MR is left in "ready" phase.

	msg := mrSupersedeCmd(world, mrID)().(mrActionDoneMsg)
	if msg.err == nil {
		t.Fatal("expected supersede of a ready MR to fail (only failed -> superseded is legal)")
	}
}

func TestMRRequeueCmdResolvesLinkedEscalation(t *testing.T) {
	t.Setenv("SOL_HOME", t.TempDir())
	world := "testworld"
	_, mrID := mrFixture(t, world)

	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("OpenSphere: %v", err)
	}
	escID, err := ss.CreateEscalation("high", "forge", "merge failed", "mr:"+mrID)
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}
	ss.Close()

	if msg := mrRequeueCmd(world, mrID)().(mrActionDoneMsg); msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}

	ss, err = store.OpenSphere()
	if err != nil {
		t.Fatalf("OpenSphere: %v", err)
	}
	defer ss.Close()
	esc, err := ss.GetEscalation(escID)
	if err != nil {
		t.Fatalf("GetEscalation: %v", err)
	}
	if esc.Status != "resolved" {
		t.Errorf("expected escalation to be auto-resolved, got status=%q", esc.Status)
	}
}

// --- Key-handling wiring ---

func TestMRRequeueKeyOnFailedMR(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World: "testworld",
		MergeQueue: status.MergeQueueInfo{Total: 1, Failed: 1},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-abc123", WritID: "sol-aaa", Phase: "failed", Title: "fix things"},
		},
	}
	wm.updateData(data)
	wm.hasFocus = true
	wm.focusedSection = sectionMergeQueue
	wm.mqCursor = 0

	_, cmd := wm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}, data)
	if cmd == nil {
		t.Fatal("pressing u on a failed MR should return a command")
	}
	msg, ok := cmd().(requestMRActionMsg)
	if !ok {
		t.Fatalf("expected requestMRActionMsg, got %T", msg)
	}
	if msg.mrID != "mr-abc123" || msg.action != mrActionRequeue {
		t.Errorf("unexpected msg: %+v", msg)
	}
}

func TestMRActionKeysNoOpOnNonFailedMR(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World: "testworld",
		MergeQueue: status.MergeQueueInfo{Total: 1, Ready: 1},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-abc123", WritID: "sol-aaa", Phase: "ready", Title: "fix things"},
		},
	}
	wm.updateData(data)
	wm.hasFocus = true
	wm.focusedSection = sectionMergeQueue
	wm.mqCursor = 0

	for _, key := range []rune{'u', 's'} {
		_, cmd := wm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}}, data)
		if cmd != nil {
			t.Errorf("key %q on a ready MR should be a no-op", string(key))
		}
	}
}

func TestMRActionKeysNoOpWrongSection(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World: "testworld",
		MergeQueue: status.MergeQueueInfo{Total: 1, Failed: 1},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-abc123", WritID: "sol-aaa", Phase: "failed", Title: "fix things"},
		},
		Agents: []status.AgentStatus{{Name: "Toast", State: "idle"}},
	}
	wm.updateData(data)
	wm.hasFocus = true
	wm.focusedSection = sectionOutposts

	for _, key := range []rune{'u', 's'} {
		_, cmd := wm.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}}, data)
		if cmd != nil {
			t.Errorf("key %q outside the merge queue section should be a no-op", string(key))
		}
	}
}

// --- Confirm-overlay wiring at the Model level ---

func TestRequestMRActionMsgBlockedShowsInfoOnly(t *testing.T) {
	m := NewModel(Config{SOLHome: t.TempDir()})
	m.ready = true
	m.width = 120
	m.height = 40

	result, _ := m.Update(requestMRActionMsg{
		world:         "testworld",
		mrID:          "mr-abc123",
		action:        mrActionRequeue,
		confirmTitle:  "Requeue mr-abc123?",
		confirmDetail: "Writ sol-aaa is closed — cannot requeue.",
		blocked:       true,
	})
	updated := result.(Model)

	if !updated.confirm.active {
		t.Fatal("blocked requestMRActionMsg should still show the confirm overlay")
	}
	if updated.confirm.onYes != nil {
		t.Error("blocked request should not wire an action to 'y'")
	}
}

func TestRequestMRActionMsgActionableWiresOnYes(t *testing.T) {
	m := NewModel(Config{SOLHome: t.TempDir()})
	m.ready = true
	m.width = 120
	m.height = 40

	result, _ := m.Update(requestMRActionMsg{
		world:         "testworld",
		mrID:          "mr-abc123",
		action:        mrActionSupersede,
		confirmTitle:  "Supersede mr-abc123?",
		confirmDetail: "Marks the MR as superseded.",
		blocked:       false,
	})
	updated := result.(Model)

	if !updated.confirm.active {
		t.Fatal("requestMRActionMsg should show the confirm overlay")
	}
	if updated.confirm.onYes == nil {
		t.Error("actionable request should wire a command to 'y'")
	}
}

func TestMRActionDoneMsgFeedback(t *testing.T) {
	cases := []struct {
		name       string
		msg        mrActionDoneMsg
		wantSubstr string
		wantErr    bool
	}{
		{"requeued", mrActionDoneMsg{action: mrActionRequeue, mrID: "mr-1", err: nil}, "mr-1 requeued", false},
		{"superseded", mrActionDoneMsg{action: mrActionSupersede, mrID: "mr-2", err: nil}, "mr-2 superseded", false},
		{"requeue error", mrActionDoneMsg{action: mrActionRequeue, mrID: "mr-3", err: fmt.Errorf("boom")}, "requeued failed", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(Config{SOLHome: t.TempDir()})
			m.ready = true
			m.width = 120
			m.height = 40
			m.worldView = newWorldModel()

			result, _ := m.Update(tc.msg)
			updated := result.(Model)

			if !strings.Contains(updated.worldView.restartFeedback, tc.wantSubstr) {
				t.Errorf("expected feedback to contain %q, got %q", tc.wantSubstr, updated.worldView.restartFeedback)
			}
			if updated.worldView.restartFeedbackErr != tc.wantErr {
				t.Errorf("expected restartFeedbackErr=%v, got %v", tc.wantErr, updated.worldView.restartFeedbackErr)
			}
		})
	}
}

func TestWorldFooterMentionsMRActions(t *testing.T) {
	wm := newWorldModel()
	wm.width = 160
	wm.height = 40

	footer := wm.renderFooter(time.Now())
	if !strings.Contains(footer, "u requeue MR") {
		t.Errorf("footer should document the u key, got %q", footer)
	}
	if !strings.Contains(footer, "s supersede MR") {
		t.Errorf("footer should document the s key, got %q", footer)
	}
}

func TestMergeQueueHighlightsFocusedRow(t *testing.T) {
	wm := newWorldModel()
	wm.width = 120
	wm.height = 40

	data := &status.WorldStatus{
		World: "testworld",
		MergeQueue: status.MergeQueueInfo{Total: 2, Ready: 1, Failed: 1},
		MergeRequests: []status.MergeRequestInfo{
			{ID: "mr-abc123", WritID: "sol-aaa", Phase: "ready", Title: "fix things"},
			{ID: "mr-def456", WritID: "sol-bbb", Phase: "failed", Title: "add feature"},
		},
	}
	wm.updateData(data)
	wm.hasFocus = true
	wm.focusedSection = sectionMergeQueue
	wm.mqCursor = 1

	output := wm.view(data, time.Now(), 0, nil, false)
	lines := strings.Split(output, "\n")
	var focusedLine, otherLine string
	for _, l := range lines {
		if strings.Contains(l, "mr-def456") {
			focusedLine = l
		}
		if strings.Contains(l, "mr-abc123") {
			otherLine = l
		}
	}
	if focusedLine == "" || otherLine == "" {
		t.Fatalf("expected both MR rows in output, got:\n%s", output)
	}
	// The focused row should carry more styling escape bytes than the
	// unfocused row (selectStyle wraps the whole padded line).
	if len(focusedLine) <= len(otherLine) {
		t.Errorf("expected focused row to carry select-style escapes, focused=%q other=%q", focusedLine, otherLine)
	}
}

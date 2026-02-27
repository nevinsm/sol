package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/gt/internal/deacon"
	"github.com/nevinsm/gt/internal/dispatch"
	"github.com/nevinsm/gt/internal/escalation"
	"github.com/nevinsm/gt/internal/events"
	"github.com/nevinsm/gt/internal/handoff"
	"github.com/nevinsm/gt/internal/hook"
	"github.com/nevinsm/gt/internal/session"
	"github.com/nevinsm/gt/internal/store"
	"github.com/nevinsm/gt/internal/supervisor"
	"github.com/nevinsm/gt/internal/workflow"
)

// ========================================================================
// Escalation Integration Tests
// ========================================================================

func TestEscalationCreateAndRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// Set up test webhook server.
	var webhookMu sync.Mutex
	var webhookReceived bool
	var webhookBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookMu.Lock()
		defer webhookMu.Unlock()
		webhookReceived = true
		json.NewDecoder(r.Body).Decode(&webhookBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	logger := events.NewLogger(gtHome)
	router := escalation.DefaultRouter(logger, townStore, ts.URL)

	// Create high-severity escalation.
	id, err := townStore.CreateEscalation("high", "myrig/Toast", "Agent stuck in merge loop")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}

	esc, err := townStore.GetEscalation(id)
	if err != nil {
		t.Fatalf("GetEscalation: %v", err)
	}

	// Route it.
	if err := router.Route(context.Background(), *esc); err != nil {
		t.Fatalf("Route: %v", err)
	}

	// Verify escalation in DB.
	dbEsc, err := townStore.GetEscalation(id)
	if err != nil {
		t.Fatalf("GetEscalation after route: %v", err)
	}
	if dbEsc.Status != "open" {
		t.Errorf("escalation status: got %q, want open", dbEsc.Status)
	}
	if dbEsc.Severity != "high" {
		t.Errorf("escalation severity: got %q, want high", dbEsc.Severity)
	}

	// Verify mail sent to "operator".
	msgs, err := townStore.Inbox("operator")
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	foundMail := false
	for _, m := range msgs {
		if strings.Contains(m.Subject, "ESCALATION") || strings.Contains(m.Body, id) {
			foundMail = true
			break
		}
	}
	if !foundMail {
		t.Error("expected escalation mail sent to operator")
	}

	// Verify webhook received POST.
	webhookMu.Lock()
	if !webhookReceived {
		t.Error("expected webhook POST")
	}
	webhookMu.Unlock()
}

func TestEscalationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// 1. Create escalation.
	id, err := townStore.CreateEscalation("medium", "operator", "Test escalation lifecycle")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}

	// 2. List → appears as open.
	escs, err := townStore.ListEscalations("open")
	if err != nil {
		t.Fatalf("ListEscalations: %v", err)
	}
	found := false
	for _, e := range escs {
		if e.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("escalation should appear in open list")
	}

	// 3. Ack → acknowledged.
	if err := townStore.AckEscalation(id); err != nil {
		t.Fatalf("AckEscalation: %v", err)
	}

	// 4. List → appears as acknowledged.
	escs, err = townStore.ListEscalations("acknowledged")
	if err != nil {
		t.Fatalf("ListEscalations acknowledged: %v", err)
	}
	found = false
	for _, e := range escs {
		if e.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Error("escalation should appear in acknowledged list")
	}

	// 5. Resolve → resolved.
	if err := townStore.ResolveEscalation(id); err != nil {
		t.Fatalf("ResolveEscalation: %v", err)
	}

	// 6. CountOpen → 0.
	count, err := townStore.CountOpen()
	if err != nil {
		t.Fatalf("CountOpen: %v", err)
	}
	if count != 0 {
		t.Errorf("open count: got %d, want 0", count)
	}
}

func TestEscalationFromAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	logger := events.NewLogger(gtHome)
	router := escalation.DefaultRouter(logger, townStore, "")

	// Create escalation from an agent.
	id, err := townStore.CreateEscalation("medium", "myrig/Toast", "Cannot compile code")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}

	esc, err := townStore.GetEscalation(id)
	if err != nil {
		t.Fatalf("GetEscalation: %v", err)
	}

	// Verify source.
	if esc.Source != "myrig/Toast" {
		t.Errorf("source: got %q, want myrig/Toast", esc.Source)
	}

	// Route to trigger mail notification.
	if err := router.Route(context.Background(), *esc); err != nil {
		t.Fatalf("Route: %v", err)
	}

	// Verify mail sent.
	msgs, err := townStore.Inbox("operator")
	if err != nil {
		t.Fatalf("Inbox: %v", err)
	}
	if len(msgs) == 0 {
		t.Error("expected escalation mail sent to operator")
	}
}

// ========================================================================
// Handoff Integration Tests
// ========================================================================

func TestHandoffCaptureAndRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create agent and work item.
	townStore.CreateAgent("HandBot", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("Handoff task", "Test handoff", "operator", 2, nil)

	// Sling the work item.
	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "HandBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)

	// 1. Capture state.
	state, err := handoff.Capture(handoff.CaptureOpts{
		Rig:       "testrig",
		AgentName: "HandBot",
	}, func(name string, lines int) (string, error) {
		return "mock output line 1\nmock output line 2", nil
	}, func(dir string, count int) ([]string, error) {
		return []string{"abc1234 initial commit"}, nil
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if state.WorkItemID != itemID {
		t.Errorf("work item ID: got %q, want %q", state.WorkItemID, itemID)
	}

	// 2. Write handoff file.
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// 3. Verify file on disk.
	handoffPath := handoff.HandoffPath("testrig", "HandBot")
	if _, err := os.Stat(handoffPath); os.IsNotExist(err) {
		t.Fatal("handoff file should exist on disk")
	}

	// 4. Prime with handoff file → handoff context injected.
	primeResult, err := dispatch.Prime("testrig", "HandBot", rigStore)
	if err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if !strings.Contains(primeResult.Output, "HANDOFF CONTEXT") {
		t.Error("prime output should contain handoff context")
	}
	if !strings.Contains(primeResult.Output, "initial commit") {
		t.Error("prime output should contain recent commits")
	}

	// 5. Verify handoff file deleted after prime.
	if _, err := os.Stat(handoffPath); !os.IsNotExist(err) {
		t.Error("handoff file should be deleted after prime")
	}

	_ = gtHome // suppress unused
}

func TestHandoffPreservesHook(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create agent and work item.
	townStore.CreateAgent("HookBot", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("Hook task", "Test hook preservation", "operator", 2, nil)

	// Sling the work item.
	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "HookBot",
		SourceRepo: sourceRepo,
	}, rigStore, townStore, mgr, nil)

	// Write handoff file.
	state := &handoff.State{
		WorkItemID:  itemID,
		AgentName:   "HookBot",
		Rig:         "testrig",
		Summary:     "Test summary",
		HandedOffAt: time.Now().UTC(),
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify hook file still exists.
	hookContent, err := hook.Read("testrig", "HookBot")
	if err != nil {
		t.Fatalf("hook.Read: %v", err)
	}
	if hookContent != itemID {
		t.Errorf("hook content: got %q, want %q", hookContent, itemID)
	}

	// Verify work item status unchanged (still hooked).
	item, err := rigStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("GetWorkItem: %v", err)
	}
	if item.Status != "hooked" {
		t.Errorf("work item status: got %q, want hooked", item.Status)
	}
}

func TestHandoffWithWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(gtHome, "formulas", "handoff-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "handoff-formula"
type = "polecat"
description = "Handoff test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "step1"
title = "First Step"
instructions = "steps/01.md"

[[steps]]
id = "step2"
title = "Second Step"
instructions = "steps/02.md"
needs = ["step1"]
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Step 1 instructions.\n"), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "02.md"), []byte("Step 2 instructions.\n"), 0o644)

	// Create agent and work item.
	townStore.CreateAgent("WFHandBot", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("WF Handoff task", "Workflow handoff test", "operator", 2, nil)

	// Sling with formula.
	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "WFHandBot",
		SourceRepo: sourceRepo,
		Formula:    "handoff-formula",
	}, rigStore, townStore, mgr, nil)

	// Advance to step 2.
	workflow.Advance("testrig", "WFHandBot")

	// Capture → state includes workflow step and progress.
	state, err := handoff.Capture(handoff.CaptureOpts{
		Rig:       "testrig",
		AgentName: "WFHandBot",
	}, nil, nil)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if state.WorkflowStep != "step2" {
		t.Errorf("workflow step: got %q, want step2", state.WorkflowStep)
	}
	if state.WorkflowProgress == "" {
		t.Error("workflow progress should not be empty")
	}

	// Write handoff file.
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Prime with handoff → output references workflow step.
	result, err := dispatch.Prime("testrig", "WFHandBot", rigStore)
	if err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if !strings.Contains(result.Output, "HANDOFF") {
		t.Error("prime output should contain handoff context")
	}
	if !strings.Contains(result.Output, "step2") {
		t.Error("prime output should reference workflow step")
	}

	// After handoff consumed: subsequent Prime → normal workflow prime.
	result, err = dispatch.Prime("testrig", "WFHandBot", rigStore)
	if err != nil {
		t.Fatalf("Prime after handoff: %v", err)
	}
	if strings.Contains(result.Output, "HANDOFF") {
		t.Error("second prime should not contain handoff context")
	}
	if !strings.Contains(result.Output, "Step 2") {
		t.Error("second prime should contain workflow step instructions")
	}
}

func TestHandoffPrimeOverridesWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnv(t)
	rigStore, townStore := openStores(t, "testrig")
	mgr := newMockSessionChecker()

	// Create formula.
	formulaDir := filepath.Join(gtHome, "formulas", "override-formula")
	stepsDir := filepath.Join(formulaDir, "steps")
	os.MkdirAll(stepsDir, 0o755)

	manifest := `name = "override-formula"
type = "polecat"
description = "Override test"

[variables]
[variables.issue]
required = true

[[steps]]
id = "only"
title = "Only Step"
instructions = "steps/01.md"
`
	os.WriteFile(filepath.Join(formulaDir, "manifest.toml"), []byte(manifest), 0o644)
	os.WriteFile(filepath.Join(stepsDir, "01.md"), []byte("Only step.\n"), 0o644)

	// Create agent and work item.
	townStore.CreateAgent("OverBot", "testrig", "polecat")
	itemID, _ := rigStore.CreateWorkItem("Override task", "Override test", "operator", 2, nil)

	// Sling with formula.
	dispatch.Sling(dispatch.SlingOpts{
		WorkItemID: itemID,
		Rig:        "testrig",
		AgentName:  "OverBot",
		SourceRepo: sourceRepo,
		Formula:    "override-formula",
	}, rigStore, townStore, mgr, nil)

	// Write handoff file manually (simulating a handoff while workflow is active).
	state := &handoff.State{
		WorkItemID:  itemID,
		AgentName:   "OverBot",
		Rig:         "testrig",
		Summary:     "Handed off mid-workflow",
		HandedOffAt: time.Now().UTC(),
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Prime → returns handoff context (not workflow).
	result, err := dispatch.Prime("testrig", "OverBot", rigStore)
	if err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if !strings.Contains(result.Output, "HANDOFF") {
		t.Error("prime should return handoff context, not workflow")
	}
}

// ========================================================================
// Deacon Integration Tests
// ========================================================================

func TestDeaconStaleHookRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	rigStore, err := store.OpenRig("myrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}
	defer rigStore.Close()

	// Create work item in "hooked" status.
	itemID, _ := rigStore.CreateWorkItem("Stale task", "Stale hook test", "operator", 2, nil)
	rigStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "hooked", Assignee: "myrig/StaleBot"})

	// Create agent in "working" state.
	townStore.CreateAgent("StaleBot", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/StaleBot", "working", itemID)

	// Set updated_at to 2 hours ago.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	townStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", twoHoursAgo, "myrig/StaleBot")

	// Write hook file.
	hook.Write("myrig", "StaleBot", itemID)

	// Mock session checker — no session alive.
	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	// Run one patrol.
	d.Patrol()

	// Verify: work item status back to "open".
	item, _ := rigStore.GetWorkItem(itemID)
	if item.Status != "open" {
		t.Errorf("work item status: got %q, want open", item.Status)
	}

	// Verify: agent state is "idle".
	agent, _ := townStore.GetAgent("myrig/StaleBot")
	if agent.State != "idle" {
		t.Errorf("agent state: got %q, want idle", agent.State)
	}

	// Verify: hook file cleared.
	hookContent, err := hook.Read("myrig", "StaleBot")
	if err != nil {
		t.Fatalf("hook.Read: %v", err)
	}
	if hookContent != "" {
		t.Errorf("hook should be cleared, got %q", hookContent)
	}
}

func TestDeaconStaleHookIgnoresRecent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	rigStore, err := store.OpenRig("myrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}
	defer rigStore.Close()

	itemID, _ := rigStore.CreateWorkItem("Recent task", "Recent hook test", "operator", 2, nil)
	rigStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "hooked", Assignee: "myrig/RecentBot"})

	townStore.CreateAgent("RecentBot", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/RecentBot", "working", itemID)

	// updated_at is now (5 minutes ago is within timeout)
	fiveMinAgo := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	townStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", fiveMinAgo, "myrig/RecentBot")

	hook.Write("myrig", "RecentBot", itemID)

	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	d.Patrol()

	// Work item should still be hooked (not recovered — too recent).
	item, _ := rigStore.GetWorkItem(itemID)
	if item.Status != "hooked" {
		t.Errorf("work item status: got %q, want hooked (too recent)", item.Status)
	}
}

func TestDeaconStaleHookIgnoresAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	rigStore, err := store.OpenRig("myrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}
	defer rigStore.Close()

	itemID, _ := rigStore.CreateWorkItem("Alive task", "Alive hook test", "operator", 2, nil)
	rigStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "hooked", Assignee: "myrig/AliveBot"})

	townStore.CreateAgent("AliveBot", "myrig", "polecat")
	townStore.UpdateAgentState("myrig/AliveBot", "working", itemID)

	// Set updated_at to 2 hours ago.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	townStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", twoHoursAgo, "myrig/AliveBot")

	hook.Write("myrig", "AliveBot", itemID)

	// Session IS alive.
	sessions := newMockSessionChecker()
	sessions.alive["gt-myrig-AliveBot"] = true

	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	d.Patrol()

	// Work item should still be hooked (session is alive).
	item, _ := rigStore.GetWorkItem(itemID)
	if item.Status != "hooked" {
		t.Errorf("work item status: got %q, want hooked (session alive)", item.Status)
	}
}

func TestDeaconConvoyFeeding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	rigStore, err := store.OpenRig("testrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}

	// Create work items: A (no deps), B→A.
	idA, _ := rigStore.CreateWorkItem("Task A", "First task", "operator", 2, nil)
	idB, _ := rigStore.CreateWorkItem("Task B", "Depends on A", "operator", 2, nil)
	rigStore.AddDependency(idB, idA)
	rigStore.Close()

	// Create convoy with both items.
	convoyID, _ := townStore.CreateConvoy("feed-convoy", "operator")
	townStore.AddConvoyItem(convoyID, idA, "testrig")
	townStore.AddConvoyItem(convoyID, idB, "testrig")

	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	// Run patrol → should detect A is ready.
	d.Patrol()

	// Verify CONVOY_NEEDS_FEEDING message sent.
	msgs, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(msgs) == 0 {
		t.Fatal("expected CONVOY_NEEDS_FEEDING message")
	}
	if !strings.Contains(msgs[0].Body, convoyID) {
		t.Error("message body should contain convoy ID")
	}

	// Ack the message.
	townStore.AckMessage(msgs[0].ID)

	// Mark A as done.
	rs, _ := store.OpenRig("testrig")
	rs.UpdateWorkItem(idA, store.WorkItemUpdates{Status: "done"})
	rs.Close()

	// Run another patrol → B is now ready.
	d.Patrol()

	// Verify new CONVOY_NEEDS_FEEDING for B.
	msgs, _ = townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(msgs) == 0 {
		t.Fatal("expected new CONVOY_NEEDS_FEEDING message after A done")
	}
}

func TestDeaconConvoyFeedingNoDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	rigStore, err := store.OpenRig("testrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}
	idA, _ := rigStore.CreateWorkItem("Dup task", "No dup test", "operator", 2, nil)
	rigStore.Close()

	convoyID, _ := townStore.CreateConvoy("nodup-convoy", "operator")
	townStore.AddConvoyItem(convoyID, idA, "testrig")

	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	// Run patrol → message sent.
	d.Patrol()
	msgs1, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(msgs1) == 0 {
		t.Fatal("expected CONVOY_NEEDS_FEEDING message")
	}

	// Run patrol again → no duplicate message.
	d.Patrol()
	msgs2, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(msgs2) != len(msgs1) {
		t.Errorf("expected no duplicate messages: had %d, now have %d", len(msgs1), len(msgs2))
	}
}

func TestDeaconHeartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	// Run one patrol.
	d.Patrol()

	// Read heartbeat file.
	hb, err := deacon.ReadHeartbeat(gtHome)
	if err != nil {
		t.Fatalf("ReadHeartbeat: %v", err)
	}
	if hb == nil {
		t.Fatal("heartbeat should exist after patrol")
	}
	if hb.PatrolCount != 1 {
		t.Errorf("patrol count: got %d, want 1", hb.PatrolCount)
	}
	if time.Since(hb.Timestamp) > 5*time.Second {
		t.Error("heartbeat timestamp should be recent")
	}

	// Run another patrol.
	d.Patrol()
	hb, _ = deacon.ReadHeartbeat(gtHome)
	if hb.PatrolCount != 2 {
		t.Errorf("patrol count: got %d, want 2", hb.PatrolCount)
	}
}

func TestDeaconLifecycleShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// Register deacon.
	townStore.CreateAgent("deacon", "town", "deacon")
	townStore.UpdateAgentState("town/deacon", "working", "")

	// Send SHUTDOWN protocol message to "town/deacon".
	townStore.SendProtocolMessage("operator", "town/deacon", "SHUTDOWN", nil)

	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	// Patrol should process shutdown.
	err = d.Patrol()
	if err == nil || !strings.Contains(err.Error(), "shutdown") {
		t.Errorf("expected shutdown error, got: %v", err)
	}

	// Message should be acknowledged.
	pending, _ := townStore.PendingProtocol("town/deacon", "")
	for _, msg := range pending {
		if msg.Subject == "SHUTDOWN" {
			t.Error("SHUTDOWN message should be acknowledged")
		}
	}
}

// ========================================================================
// Supervisor + Deacon Integration Tests
// ========================================================================

// mockSupervisorSessions implements supervisor.SessionManager for testing.
type mockSupervisorSessions struct {
	mu      sync.Mutex
	alive   map[string]bool
	started []string
	stopped []string
}

func newMockSupervisorSessions() *mockSupervisorSessions {
	return &mockSupervisorSessions{alive: make(map[string]bool)}
}

func (m *mockSupervisorSessions) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockSupervisorSessions) Start(name, workdir, cmd string, env map[string]string, role, rig string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
	return nil
}

func (m *mockSupervisorSessions) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockSupervisorSessions) List() ([]session.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var infos []session.SessionInfo
	for name, alive := range m.alive {
		infos = append(infos, session.SessionInfo{Name: name, Alive: alive})
	}
	return infos, nil
}

func (m *mockSupervisorSessions) getStarted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.started))
	copy(result, m.started)
	return result
}

func TestSupervisorDeaconStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	mock := newMockSupervisorSessions()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := supervisor.DefaultConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.DeaconEnabled = true

	sup := supervisor.New(cfg, townStore, mock, logger)

	// Run with short-lived context (just enough for one heartbeat).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	// Verify: deacon session started.
	started := mock.getStarted()
	found := false
	for _, s := range started {
		if s == "gt-town-deacon" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected deacon session started, got: %v", started)
	}
}

func TestSupervisorDeaconRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// Write stale heartbeat (20 minutes old).
	deacon.WriteHeartbeat(gtHome, &deacon.Heartbeat{
		Timestamp:   time.Now().UTC().Add(-20 * time.Minute),
		PatrolCount: 10,
		Status:      "running",
	})

	mock := newMockSupervisorSessions()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := supervisor.DefaultConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.DeaconEnabled = true
	cfg.DeaconHeartbeatMax = 15 * time.Minute

	sup := supervisor.New(cfg, townStore, mock, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	// Verify: deacon session started (restarted).
	started := mock.getStarted()
	found := false
	for _, s := range started {
		if s == "gt-town-deacon" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected deacon session restarted, got: %v", started)
	}
}

func TestSupervisorDeaconHealthy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	// Write fresh heartbeat (1 minute old).
	deacon.WriteHeartbeat(gtHome, &deacon.Heartbeat{
		Timestamp:   time.Now().UTC().Add(-1 * time.Minute),
		PatrolCount: 5,
		Status:      "running",
	})

	mock := newMockSupervisorSessions()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := supervisor.DefaultConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.DeaconEnabled = true
	cfg.DeaconHeartbeatMax = 15 * time.Minute

	sup := supervisor.New(cfg, townStore, mock, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	// Verify: no restart attempted.
	started := mock.getStarted()
	for _, s := range started {
		if s == "gt-town-deacon" {
			t.Error("deacon should not be restarted when heartbeat is fresh")
		}
	}
}

// ========================================================================
// End-to-End Test
// ========================================================================

func TestFullOrchestrationCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("GT_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755)

	townStore, err := store.OpenTown()
	if err != nil {
		t.Fatalf("open town store: %v", err)
	}
	defer townStore.Close()

	rigStore, err := store.OpenRig("testrig")
	if err != nil {
		t.Fatalf("open rig store: %v", err)
	}

	// 1. Create rig with work items and dependencies.
	idA, _ := rigStore.CreateWorkItem("Task A", "No deps", "operator", 2, nil)
	idB, _ := rigStore.CreateWorkItem("Task B", "Depends on A", "operator", 2, nil)
	rigStore.AddDependency(idB, idA)
	rigStore.Close()

	// 2. Create convoy spanning the items.
	convoyID, _ := townStore.CreateConvoy("e2e-convoy", "operator")
	townStore.AddConvoyItem(convoyID, idA, "testrig")
	townStore.AddConvoyItem(convoyID, idB, "testrig")

	sessions := newMockSessionChecker()
	logger := events.NewLogger(gtHome)

	cfg := deacon.Config{
		PatrolInterval:   5 * time.Minute,
		StaleHookTimeout: 1 * time.Hour,
		GTHome:           gtHome,
	}
	d := deacon.New(cfg, townStore, sessions, escalation.NewRouter(), logger)
	d.SetRigOpener(func(rig string) (*store.Store, error) {
		return store.OpenRig(rig)
	})

	// 3. Run deacon patrol → detects stranded convoy.
	d.Patrol()

	// 4. Verify CONVOY_NEEDS_FEEDING message sent.
	msgs, _ := townStore.PendingProtocol("operator", store.ProtoConvoyNeedsFeeding)
	if len(msgs) == 0 {
		t.Fatal("expected CONVOY_NEEDS_FEEDING message")
	}
	// Ack to clean up.
	townStore.AckMessage(msgs[0].ID)

	// 5. Create escalation (simulating stuck agent).
	escID, err := townStore.CreateEscalation("high", "testrig/StuckBot", "Agent stuck in loop")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}
	esc, _ := townStore.GetEscalation(escID)
	escalation.NewRouter().Route(context.Background(), *esc)

	// 6. Verify escalation stored correctly.
	dbEsc, _ := townStore.GetEscalation(escID)
	if dbEsc.Status != "open" {
		t.Errorf("escalation status: got %q, want open", dbEsc.Status)
	}

	// 7. Simulate handoff: write handoff file, call Prime.
	// First, set up an agent with a hook.
	rigStore2, _ := store.OpenRig("testrig")
	defer rigStore2.Close()

	townStore.CreateAgent("E2EBot", "testrig", "polecat")
	townStore.UpdateAgentState("testrig/E2EBot", "working", idA)
	rigStore2.UpdateWorkItem(idA, store.WorkItemUpdates{Status: "hooked", Assignee: "testrig/E2EBot"})
	hook.Write("testrig", "E2EBot", idA)

	handoffState := &handoff.State{
		WorkItemID:  idA,
		AgentName:   "E2EBot",
		Rig:         "testrig",
		Summary:     "E2E handoff test",
		HandedOffAt: time.Now().UTC(),
	}
	handoff.Write(handoffState)

	// 8. Verify handoff context injected.
	primeResult, err := dispatch.Prime("testrig", "E2EBot", rigStore2)
	if err != nil {
		t.Fatalf("Prime with handoff: %v", err)
	}
	if !strings.Contains(primeResult.Output, "HANDOFF") {
		t.Error("prime should contain handoff context")
	}

	// 9. Simulate stale hook: mark agent working but session is dead.
	townStore.UpdateAgentState("testrig/E2EBot", "working", idA)
	// Set updated_at to 2 hours ago.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	townStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", twoHoursAgo, "testrig/E2EBot")
	hook.Write("testrig", "E2EBot", idA)
	rigStore2.UpdateWorkItem(idA, store.WorkItemUpdates{Status: "hooked", Assignee: "testrig/E2EBot"})

	// 10. Run deacon patrol → recovers stale hook.
	d.Patrol()

	// 11. Verify work item returned to open.
	item, _ := rigStore2.GetWorkItem(idA)
	if item.Status != "open" {
		t.Errorf("work item status after recovery: got %q, want open", item.Status)
	}

	// Verify events emitted.
	assertEventEmitted(t, gtHome, events.EventDeaconPatrol)
	assertEventEmitted(t, gtHome, events.EventDeaconConvoyFeed)
	assertEventEmitted(t, gtHome, events.EventDeaconStaleHook)
}

// Ensure unused imports don't cause issues.
var _ = fmt.Sprintf

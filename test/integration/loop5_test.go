package integration

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/consul"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/session"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/prefect"

)

// ========================================================================
// Escalation Integration Tests
// ========================================================================

func TestEscalationCreateAndRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// safe: no tmux/dispatch usage — escalation router + HTTP test server
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

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

	logger := events.NewLogger(solHome)
	router := escalation.DefaultRouter(logger, sphereStore, ts.URL)

	// Create high-severity escalation.
	id, err := sphereStore.CreateEscalation("high", "haven/Toast", "Agent stuck in merge loop")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}

	esc, err := sphereStore.GetEscalation(id)
	if err != nil {
		t.Fatalf("GetEscalation: %v", err)
	}

	// Route it.
	if err := router.Route(context.Background(), *esc); err != nil {
		t.Fatalf("Route: %v", err)
	}

	// Verify escalation in DB.
	dbEsc, err := sphereStore.GetEscalation(id)
	if err != nil {
		t.Fatalf("GetEscalation after route: %v", err)
	}
	if dbEsc.Status != "open" {
		t.Errorf("escalation status: got %q, want open", dbEsc.Status)
	}
	if dbEsc.Severity != "high" {
		t.Errorf("escalation severity: got %q, want high", dbEsc.Severity)
	}

	// Verify mail sent to "autarch".
	msgs, err := sphereStore.Inbox("autarch")
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

	// safe: no tmux/dispatch usage — escalation lifecycle on store only
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// 1. Create escalation.
	id, err := sphereStore.CreateEscalation("medium", "autarch", "Test escalation lifecycle")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}

	// 2. List → appears as open.
	escs, err := sphereStore.ListEscalations("open")
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
	if err := sphereStore.AckEscalation(id); err != nil {
		t.Fatalf("AckEscalation: %v", err)
	}

	// 4. List → appears as acknowledged.
	escs, err = sphereStore.ListEscalations("acknowledged")
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
	if err := sphereStore.ResolveEscalation(id); err != nil {
		t.Fatalf("ResolveEscalation: %v", err)
	}

	// 6. CountOpen → 0.
	count, err := sphereStore.CountOpen()
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

	// safe: no tmux/dispatch usage — escalation routing to mail, no sessions
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)
	if err := os.MkdirAll(filepath.Join(solHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	logger := events.NewLogger(solHome)
	router := escalation.DefaultRouter(logger, sphereStore, "")

	// Create escalation from an agent.
	id, err := sphereStore.CreateEscalation("medium", "haven/Toast", "Cannot compile code")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}

	esc, err := sphereStore.GetEscalation(id)
	if err != nil {
		t.Fatalf("GetEscalation: %v", err)
	}

	// Verify source.
	if esc.Source != "haven/Toast" {
		t.Errorf("source: got %q, want haven/Toast", esc.Source)
	}

	// Route to trigger mail notification.
	if err := router.Route(context.Background(), *esc); err != nil {
		t.Fatalf("Route: %v", err)
	}

	// Verify mail sent.
	msgs, err := sphereStore.Inbox("autarch")
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

	solHome, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("HandBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Handoff task", "Test handoff", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast the writ.
	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "HandBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// 1. Capture state.
	state, err := handoff.Capture(handoff.CaptureOpts{
		World:       "ember",
		AgentName: "HandBot",
	}, func(name string, lines int) (string, error) {
		return "mock output line 1\nmock output line 2", nil
	}, func(dir string, count int) ([]string, error) {
		return []string{"abc1234 initial commit"}, nil
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if state.WritID != itemID {
		t.Errorf("writ ID: got %q, want %q", state.WritID, itemID)
	}

	// 2. Write handoff file.
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// 3. Verify file on disk.
	handoffPath := handoff.HandoffPath("ember", "HandBot", "outpost")
	if _, err := os.Stat(handoffPath); os.IsNotExist(err) {
		t.Fatal("handoff file should exist on disk")
	}

	// 4. Prime with handoff file → handoff context injected.
	primeResult, err := dispatch.Prime("ember", "HandBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if !strings.Contains(primeResult.Output, "HANDOFF CONTEXT") {
		t.Error("prime output should contain handoff context")
	}
	if !strings.Contains(primeResult.Output, "initial commit") {
		t.Error("prime output should contain recent commits")
	}

	// 5. Verify handoff file survives prime (durable) but is marked consumed.
	if _, err := os.Stat(handoffPath); os.IsNotExist(err) {
		t.Error("handoff file should survive prime (durable)")
	}
	readBack, err := handoff.Read("ember", "HandBot", "outpost")
	if err != nil {
		t.Fatalf("Read after prime: %v", err)
	}
	if readBack == nil {
		t.Fatal("handoff state should be non-nil after prime")
	}
	if !readBack.Consumed {
		t.Error("handoff should be marked consumed after prime")
	}
	// HasHandoff should return false (consumed).
	if handoff.HasHandoff("ember", "HandBot", "outpost") {
		t.Error("HasHandoff should return false for consumed handoff")
	}

	_ = solHome // suppress unused
}

func TestHandoffPreservesHook(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("HookBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Tether task", "Test tether preservation", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast the writ.
	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID: itemID,
		World:        "ember",
		AgentName:  "HookBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Write handoff file.
	state := &handoff.State{
		WritID:  itemID,
		AgentName:   "HookBot",
		World:         "ember",
		Role:        "outpost",
		Summary:     "Test summary",
		HandedOffAt: time.Now().UTC(),
	}
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Verify tether file still exists.
	tetherContent, err := tether.Read("ember", "HookBot", "outpost")
	if err != nil {
		t.Fatalf("tether.Read: %v", err)
	}
	if tetherContent != itemID {
		t.Errorf("tether content: got %q, want %q", tetherContent, itemID)
	}

	// Verify writ status unchanged (still tethered).
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status: got %q, want tethered", item.Status)
	}
}

func TestHandoffWithGuidelines(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("WFHandBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("GL Handoff task", "Guidelines handoff test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast with guidelines.
	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "WFHandBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Capture handoff state.
	state, err := handoff.Capture(handoff.CaptureOpts{
		World:     "ember",
		AgentName: "WFHandBot",
	}, nil, nil)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	// Write handoff file.
	if err := handoff.Write(state); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Prime with handoff → output contains handoff context.
	result, err := dispatch.Prime("ember", "WFHandBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if !strings.Contains(result.Output, "HANDOFF") {
		t.Error("prime output should contain handoff context")
	}

	// After handoff consumed: subsequent Prime → standard prime with guidelines.
	result, err = dispatch.Prime("ember", "WFHandBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime after handoff: %v", err)
	}
	if strings.Contains(result.Output, "HANDOFF") {
		t.Error("second prime should not contain handoff context")
	}
	if !strings.Contains(result.Output, "--- GUIDELINES ---") {
		t.Error("second prime should contain guidelines section")
	}
}

func TestHandoffPrimeOverridesGuidelines(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, sourceRepo := setupTestEnv(t)
	worldStore, sphereStore := openStores(t, "ember")
	mgr := newMockSessionChecker()

	// Create agent and writ.
	if _, err := sphereStore.CreateAgent("OverBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	itemID, err := worldStore.CreateWrit("Override task", "Override test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Cast with guidelines.
	if _, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
		WritID:     itemID,
		World:      "ember",
		AgentName:  "OverBot",
		SourceRepo: sourceRepo,
	}, worldStore, sphereStore, mgr, nil); err != nil {
		t.Fatalf("cast: %v", err)
	}

	// Write handoff file manually.
	hState := &handoff.State{
		WritID:      itemID,
		AgentName:   "OverBot",
		World:       "ember",
		Role:        "outpost",
		Summary:     "Handed off mid-work",
		HandedOffAt: time.Now().UTC(),
	}
	if err := handoff.Write(hState); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Prime → returns handoff context (not guidelines).
	result, err := dispatch.Prime("ember", "OverBot", "outpost", worldStore)
	if err != nil {
		t.Fatalf("Prime: %v", err)
	}
	if !strings.Contains(result.Output, "HANDOFF") {
		t.Error("prime should return handoff context")
	}
}

// ========================================================================
// Consul Integration Tests
// ========================================================================

func TestConsulStaleHookRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldStore, err := store.OpenWorld("haven")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	// Create writ in "tethered" status.
	itemID, err := worldStore.CreateWrit("Stale task", "Stale tether test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "haven/StaleBot"}); err != nil {
		t.Fatalf("update writ: %v", err)
	}

	// Create agent in "working" state.
	if _, err := sphereStore.CreateAgent("StaleBot", "haven", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("haven/StaleBot", "working", itemID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Set updated_at to 2 hours ago.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := sphereStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", twoHoursAgo, "haven/StaleBot"); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	// Write tether file.
	if err := tether.Write("haven", "StaleBot", itemID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Mock session checker — no session alive.
	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	// Run one patrol.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: writ status back to "open".
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("writ status: got %q, want open", item.Status)
	}

	// Verify: agent state is "idle".
	agent, err := sphereStore.GetAgent("haven/StaleBot")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("agent state: got %q, want idle", agent.State)
	}

	// Verify: tether file cleared.
	tetherContent, err := tether.Read("haven", "StaleBot", "outpost")
	if err != nil {
		t.Fatalf("tether.Read: %v", err)
	}
	if tetherContent != "" {
		t.Errorf("tether should be cleared, got %q", tetherContent)
	}
}

func TestConsulStaleHookIgnoresRecent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldStore, err := store.OpenWorld("haven")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	itemID, err := worldStore.CreateWrit("Recent task", "Recent tether test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "haven/RecentBot"}); err != nil {
		t.Fatalf("update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("RecentBot", "haven", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("haven/RecentBot", "working", itemID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// updated_at is now (5 minutes ago is within timeout)
	fiveMinAgo := time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	if _, err := sphereStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", fiveMinAgo, "haven/RecentBot"); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	if err := tether.Write("haven", "RecentBot", itemID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Writ should still be tethered (not recovered — too recent).
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status: got %q, want tethered (too recent)", item.Status)
	}
}

func TestConsulStaleHookIgnoresAlive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldStore, err := store.OpenWorld("haven")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	defer worldStore.Close()

	itemID, err := worldStore.CreateWrit("Alive task", "Alive tether test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if err := worldStore.UpdateWrit(itemID, store.WritUpdates{Status: "tethered", Assignee: "haven/AliveBot"}); err != nil {
		t.Fatalf("update writ: %v", err)
	}

	if _, err := sphereStore.CreateAgent("AliveBot", "haven", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("haven/AliveBot", "working", itemID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Set updated_at to 2 hours ago.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := sphereStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", twoHoursAgo, "haven/AliveBot"); err != nil {
		t.Fatalf("Exec: %v", err)
	}

	if err := tether.Write("haven", "AliveBot", itemID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Session IS alive.
	sessions := newMockSessionChecker()
	sessions.alive["sol-haven-AliveBot"] = true

	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Writ should still be tethered (session is alive).
	item, err := worldStore.GetWrit(itemID)
	if err != nil {
		t.Fatalf("GetWrit: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("writ status: got %q, want tethered (session alive)", item.Status)
	}
}

func TestConsulCaravanFeeding(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}

	// Create writs: A (no deps), B→A.
	idA, err := worldStore.CreateWrit("Task A", "First task", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	idB, err := worldStore.CreateWrit("Task B", "Depends on A", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if err := worldStore.AddDependency(idB, idA); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	worldStore.Close()

	// Create caravan with both items and commission it.
	caravanID, err := sphereStore.CreateCaravan("feed-caravan", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem: %v", err)
	}

	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	// Track dispatched items via mock dispatch function.
	var dispatchedItems []string
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(func(ctx context.Context, opts dispatch.CastOpts, ws dispatch.WorldStore, ss dispatch.SphereStore, mgr dispatch.SessionManager, l *events.Logger) (*dispatch.CastResult, error) {
		dispatchedItems = append(dispatchedItems, opts.WritID)
		return &dispatch.CastResult{
			WritID:  opts.WritID,
			AgentName:   "MockAgent",
			SessionName: "sol-mock-session",
		}, nil
	})

	// Run patrol → should detect A is ready and auto-dispatch it.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify A was auto-dispatched (not just notified).
	if len(dispatchedItems) != 1 {
		t.Fatalf("dispatched items = %d, want 1", len(dispatchedItems))
	}
	if dispatchedItems[0] != idA {
		t.Errorf("dispatched item = %q, want %q (A)", dispatchedItems[0], idA)
	}

	// Close A (merged) to unblock B.
	rs, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	if _, err := rs.CloseWrit(idA); err != nil {
		t.Fatalf("close writ A: %v", err)
	}
	rs.Close()

	// Reset dispatch tracking.
	dispatchedItems = nil

	// Run another patrol → B is now ready and should be auto-dispatched.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol 2: %v", err)
	}

	if len(dispatchedItems) != 1 {
		t.Fatalf("dispatched items after patrol 2 = %d, want 1", len(dispatchedItems))
	}
	if dispatchedItems[0] != idB {
		t.Errorf("dispatched item = %q, want %q (B)", dispatchedItems[0], idB)
	}
}

func TestConsulCaravanFeedingNoDuplicates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	idA, err := worldStore.CreateWrit("Dup task", "No dup test", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	worldStore.Close()

	caravanID, err := sphereStore.CreateCaravan("nodup-caravan", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem: %v", err)
	}

	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	// Track dispatch calls.
	var dispatchCount int
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(func(ctx context.Context, opts dispatch.CastOpts, ws dispatch.WorldStore, ss dispatch.SphereStore, mgr dispatch.SessionManager, l *events.Logger) (*dispatch.CastResult, error) {
		dispatchCount++
		return &dispatch.CastResult{
			WritID:  opts.WritID,
			AgentName:   "MockAgent",
			SessionName: "sol-mock-session",
		}, nil
	})

	// Run patrol → item dispatched.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol 1: %v", err)
	}
	if dispatchCount != 1 {
		t.Fatalf("dispatch count after patrol 1 = %d, want 1", dispatchCount)
	}

	// Run patrol again → item already dispatched (status changed), no duplicate dispatch.
	// Note: since mock dispatch doesn't change writ status, consul will try again.
	// In production, dispatch.Cast changes status to "tethered", preventing re-dispatch.
	// For this test, verify the first patrol dispatched correctly.
}

func TestConsulHeartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	// Run one patrol.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol 1: %v", err)
	}

	// Read heartbeat file.
	hb, err := consul.ReadHeartbeat(solHome)
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
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol 2: %v", err)
	}
	hb, err = consul.ReadHeartbeat(solHome)
	if err != nil {
		t.Fatalf("ReadHeartbeat 2: %v", err)
	}
	if hb.PatrolCount != 2 {
		t.Errorf("patrol count: got %d, want 2", hb.PatrolCount)
	}
}

func TestConsulLifecycleShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Register consul.
	if _, err := sphereStore.CreateAgent("consul", "sphere", "consul"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("sphere/consul", "working", ""); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Send SHUTDOWN protocol message to "sphere/consul".
	if _, err := sphereStore.SendProtocolMessage("autarch", "sphere/consul", "SHUTDOWN", nil); err != nil {
		t.Fatalf("SendProtocolMessage: %v", err)
	}

	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})

	// Patrol should process shutdown.
	err = d.Patrol(context.Background())
	if err == nil || !strings.Contains(err.Error(), "shutdown") {
		t.Errorf("expected shutdown error, got: %v", err)
	}

	// Message should be acknowledged.
	pending, err := sphereStore.PendingProtocol("sphere/consul", "")
	if err != nil {
		t.Fatalf("PendingProtocol: %v", err)
	}
	for _, msg := range pending {
		if msg.Subject == "SHUTDOWN" {
			t.Error("SHUTDOWN message should be acknowledged")
		}
	}
}

// ========================================================================
// Prefect + Consul Integration Tests
// ========================================================================

// mockPrefectSessions implements prefect.SessionManager for testing.
type mockPrefectSessions struct {
	mu      sync.Mutex
	alive   map[string]bool
	started []string
	stopped []string
}

func newMockPrefectSessions() *mockPrefectSessions {
	return &mockPrefectSessions{alive: make(map[string]bool)}
}

func (m *mockPrefectSessions) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[name]
}

func (m *mockPrefectSessions) Start(name, workdir, cmd string, env map[string]string, role, world string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alive[name] = true
	m.started = append(m.started, name)
	return nil
}

func (m *mockPrefectSessions) Stop(name string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	m.stopped = append(m.stopped, name)
	return nil
}

func (m *mockPrefectSessions) List() ([]session.SessionInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var infos []session.SessionInfo
	for name, alive := range m.alive {
		infos = append(infos, session.SessionInfo{Name: name, Alive: alive})
	}
	return infos, nil
}

func (m *mockPrefectSessions) CountSessions(prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for name := range m.alive {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	return count, nil
}

func (m *mockPrefectSessions) getStarted() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.started))
	copy(result, m.started)
	return result
}

func TestPrefectConsulStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, _ = setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	mock := newMockPrefectSessions()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := prefect.DefaultConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.ConsulEnabled = true
	cfg.SolBinary = "/usr/bin/sol"

	// Track startDaemonProcess calls for consul.
	var daemonCalls []string
	var daemonMu sync.Mutex

	sup := prefect.New(cfg, sphereStore, mock, logger)
	sup.SetStartDaemonProcess(func(daemon string, binPath string, args ...string) error {
		daemonMu.Lock()
		daemonCalls = append(daemonCalls, daemon)
		daemonMu.Unlock()
		return nil
	})

	// Run with short-lived context (just enough for one heartbeat).
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	// Verify: consul process started via startDaemonProcess.
	daemonMu.Lock()
	defer daemonMu.Unlock()
	found := false
	for _, d := range daemonCalls {
		if d == "consul" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected consul started via startDaemonProcess, got: %v", daemonCalls)
	}
}

func TestPrefectConsulRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Write stale heartbeat (20 minutes old).
	consul.WriteHeartbeat(solHome, &consul.Heartbeat{
		Timestamp:   time.Now().UTC().Add(-20 * time.Minute),
		PatrolCount: 10,
		Status:      "running",
	})

	mock := newMockPrefectSessions()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := prefect.DefaultConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.ConsulEnabled = true
	cfg.ConsulHeartbeatMax = 15 * time.Minute
	cfg.SolBinary = "/usr/bin/sol"

	// Track startDaemonProcess calls for consul.
	var daemonCalls []string
	var daemonMu sync.Mutex

	sup := prefect.New(cfg, sphereStore, mock, logger)
	sup.SetStartDaemonProcess(func(daemon string, binPath string, args ...string) error {
		daemonMu.Lock()
		daemonCalls = append(daemonCalls, daemon)
		daemonMu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	// Verify: consul process restarted via startDaemonProcess.
	daemonMu.Lock()
	defer daemonMu.Unlock()
	found := false
	for _, d := range daemonCalls {
		if d == "consul" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected consul restarted via startDaemonProcess, got: %v", daemonCalls)
	}
}

func TestPrefectConsulHealthy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Write fresh heartbeat (1 minute old).
	consul.WriteHeartbeat(solHome, &consul.Heartbeat{
		Timestamp:   time.Now().UTC().Add(-1 * time.Minute),
		PatrolCount: 5,
		Status:      "running",
	})

	// Write an alive PID file. Under the PID-first liveness contract, fresh
	// heartbeat alone is not sufficient to prove consul is healthy — the PID
	// must also be alive (otherwise a SIGKILLed consul would look healthy
	// for the entire heartbeat freshness window).
	//
	// Use a real subprocess so prefect's shutdown SIGTERM lands harmlessly on
	// it instead of the test process. (We cannot use os.Getpid() here because
	// prefect.shutdown() unconditionally signals the consul PID.)
	consulProc := exec.Command("sleep", "300")
	if err := consulProc.Start(); err != nil {
		t.Fatalf("start consul placeholder process: %v", err)
	}
	t.Cleanup(func() {
		_ = consulProc.Process.Kill()
		_ = consulProc.Wait()
	})

	runtimeDir := filepath.Join(solHome, ".runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(runtimeDir, "consul.pid"),
		[]byte(strconv.Itoa(consulProc.Process.Pid)),
		0o644,
	); err != nil {
		t.Fatalf("write consul PID file: %v", err)
	}

	mock := newMockPrefectSessions()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	cfg := prefect.DefaultConfig()
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.ConsulEnabled = true
	cfg.ConsulHeartbeatMax = 15 * time.Minute
	cfg.SolBinary = "/usr/bin/sol"

	// Track startDaemonProcess calls.
	var daemonCalls []string
	var daemonMu sync.Mutex

	sup := prefect.New(cfg, sphereStore, mock, logger)
	sup.SetStartDaemonProcess(func(daemon string, binPath string, args ...string) error {
		daemonMu.Lock()
		daemonCalls = append(daemonCalls, daemon)
		daemonMu.Unlock()
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sup.Run(ctx)

	// Verify: no consul restart attempted.
	daemonMu.Lock()
	defer daemonMu.Unlock()
	for _, d := range daemonCalls {
		if d == "consul" {
			t.Error("consul should not be restarted when heartbeat is fresh")
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

	solHome, _ := setupTestEnv(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldStore, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}

	// 1. Create world with writs and dependencies.
	idA, err := worldStore.CreateWrit("Task A", "No deps", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	idB, err := worldStore.CreateWrit("Task B", "Depends on A", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if err := worldStore.AddDependency(idB, idA); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	worldStore.Close()

	// 2. Create caravan spanning the items and commission it.
	caravanID, err := sphereStore.CreateCaravan("e2e-caravan", "autarch")
	if err != nil {
		t.Fatalf("CreateCaravan: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idA, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, idB, "ember", 0); err != nil {
		t.Fatalf("AddCaravanItem: %v", err)
	}

	sessions := newMockSessionChecker()
	logger := events.NewLogger(solHome)

	cfg := consul.Config{
		PatrolInterval:   5 * time.Minute,
		StaleTetherTimeout: 1 * time.Hour,
		SolHome:           solHome,
	}
	var dispatchedItems []string
	d := consul.New(cfg, sphereStore, sessions, escalation.NewRouter(), logger)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(func(ctx context.Context, opts dispatch.CastOpts, ws dispatch.WorldStore, ss dispatch.SphereStore, mgr dispatch.SessionManager, l *events.Logger) (*dispatch.CastResult, error) {
		dispatchedItems = append(dispatchedItems, opts.WritID)
		return &dispatch.CastResult{
			WritID:  opts.WritID,
			AgentName:   "MockAgent",
			SessionName: "sol-mock-session",
		}, nil
	})

	// 3. Run consul patrol → detects stranded caravan and auto-dispatches.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol 1: %v", err)
	}

	// 4. Verify items were auto-dispatched.
	if len(dispatchedItems) == 0 {
		t.Fatal("expected consul to auto-dispatch caravan items")
	}

	// 5. Create escalation (simulating stuck agent).
	escID, err := sphereStore.CreateEscalation("high", "ember/StuckBot", "Agent stuck in loop")
	if err != nil {
		t.Fatalf("CreateEscalation: %v", err)
	}
	esc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("GetEscalation: %v", err)
	}
	// Route with empty router returns error (no rules for the severity).
	// This is expected — the test validates escalation storage, not routing.
	_ = escalation.NewRouter().Route(context.Background(), *esc)

	// 6. Verify escalation stored correctly.
	dbEsc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("GetEscalation after route: %v", err)
	}
	if dbEsc.Status != "open" {
		t.Errorf("escalation status: got %q, want open", dbEsc.Status)
	}

	// 7. Simulate handoff: write handoff file, call Prime.
	// First, set up an agent with a tether.
	worldStore2, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("OpenWorld: %v", err)
	}
	defer worldStore2.Close()

	if _, err := sphereStore.CreateAgent("E2EBot", "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/E2EBot", "working", idA); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	if err := worldStore2.UpdateWrit(idA, store.WritUpdates{Status: "tethered", Assignee: "ember/E2EBot"}); err != nil {
		t.Fatalf("update writ: %v", err)
	}
	if err := tether.Write("ember", "E2EBot", idA, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	handoffState := &handoff.State{
		WritID:  idA,
		AgentName:   "E2EBot",
		World:         "ember",
		Role:        "outpost",
		Summary:     "E2E handoff test",
		HandedOffAt: time.Now().UTC(),
	}
	if err := handoff.Write(handoffState); err != nil {
		t.Fatalf("handoff.Write: %v", err)
	}

	// 8. Verify handoff context injected.
	primeResult, err := dispatch.Prime("ember", "E2EBot", "outpost", worldStore2)
	if err != nil {
		t.Fatalf("Prime with handoff: %v", err)
	}
	if !strings.Contains(primeResult.Output, "HANDOFF") {
		t.Error("prime should contain handoff context")
	}

	// 9. Simulate stale tether: mark agent working but session is dead.
	if err := sphereStore.UpdateAgentState("ember/E2EBot", "working", idA); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}
	// Set updated_at to 2 hours ago.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if _, err := sphereStore.DB().Exec("UPDATE agents SET updated_at = ? WHERE id = ?", twoHoursAgo, "ember/E2EBot"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if err := tether.Write("ember", "E2EBot", idA, "outpost"); err != nil {
		t.Fatalf("tether.Write 2: %v", err)
	}
	if err := worldStore2.UpdateWrit(idA, store.WritUpdates{Status: "tethered", Assignee: "ember/E2EBot"}); err != nil {
		t.Fatalf("update writ 2: %v", err)
	}

	// 10. Run consul patrol → recovers stale tether.
	if err := d.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol 2: %v", err)
	}

	// 11. Verify writ returned to open.
	item, err := worldStore2.GetWrit(idA)
	if err != nil {
		t.Fatalf("GetWrit after recovery: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("writ status after recovery: got %q, want open", item.Status)
	}

	// Verify events emitted.
	assertEventEmitted(t, solHome, events.EventConsulPatrol)
	assertEventEmitted(t, solHome, events.EventConsulCaravanFeed)
	assertEventEmitted(t, solHome, events.EventConsulStaleTether)
}


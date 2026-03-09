package consul

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/escalation"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// setEscalationCreatedAt backdates an escalation's created_at for testing aging.
func setEscalationCreatedAt(t *testing.T, sphereStore *store.Store, escID string, createdAt time.Time) {
	t.Helper()
	_, err := sphereStore.DB().Exec(
		`UPDATE escalations SET created_at = ? WHERE id = ?`,
		createdAt.UTC().Format(time.RFC3339), escID,
	)
	if err != nil {
		t.Fatalf("failed to set created_at for escalation %q: %v", escID, err)
	}
}

// setEscalationLastNotifiedAt sets last_notified_at for testing aging.
func setEscalationLastNotifiedAt(t *testing.T, sphereStore *store.Store, escID string, lastNotified time.Time) {
	t.Helper()
	_, err := sphereStore.DB().Exec(
		`UPDATE escalations SET last_notified_at = ? WHERE id = ?`,
		lastNotified.UTC().Format(time.RFC3339), escID,
	)
	if err != nil {
		t.Fatalf("failed to set last_notified_at for escalation %q: %v", escID, err)
	}
}

func TestAgingAlertsFireForUnacknowledged(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	logger := events.NewLogger(solHome)

	// Create a critical escalation that's 1 hour old (past 30m threshold).
	escID, err := sphereStore.CreateEscalation("critical", "test/agent", "test critical escalation")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID, time.Now().Add(-1*time.Hour))

	// Create a high escalation that's 3 hours old (past 2h threshold).
	escID2, err := sphereStore.CreateEscalation("high", "test/agent", "test high escalation")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID2, time.Now().Add(-3*time.Hour))

	// Create a medium escalation that's 10 hours old (past 8h threshold).
	escID3, err := sphereStore.CreateEscalation("medium", "test/agent", "test medium escalation")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID3, time.Now().Add(-10*time.Hour))

	sessions := newMockSessions()
	router := escalation.NewRouter()
	// No notifiers needed — we just test the return count and DB updates.

	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, router, logger)

	renotified, err := d.checkAgingEscalations(context.Background())
	if err != nil {
		t.Fatalf("checkAgingEscalations failed: %v", err)
	}
	if renotified != 3 {
		t.Errorf("renotified = %d, want 3 (critical + high + medium)", renotified)
	}

	// Verify last_notified_at was updated for all three.
	for _, id := range []string{escID, escID2, escID3} {
		esc, err := sphereStore.GetEscalation(id)
		if err != nil {
			t.Fatalf("failed to get escalation %q: %v", id, err)
		}
		if esc.LastNotifiedAt == nil {
			t.Errorf("escalation %q: last_notified_at should be set after re-notification", id)
		}
	}
}

func TestAgingAlertsSkipAcknowledged(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create a critical escalation that's old, then acknowledge it.
	escID, err := sphereStore.CreateEscalation("critical", "test/agent", "acked escalation")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID, time.Now().Add(-2*time.Hour))
	if err := sphereStore.AckEscalation(escID); err != nil {
		t.Fatalf("failed to ack escalation: %v", err)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	renotified, err := d.checkAgingEscalations(context.Background())
	if err != nil {
		t.Fatalf("checkAgingEscalations failed: %v", err)
	}
	if renotified != 0 {
		t.Errorf("renotified = %d, want 0 (acknowledged escalations should be skipped)", renotified)
	}
}

func TestAgingAlertsSkipLowSeverity(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create a low severity escalation that's very old.
	escID, err := sphereStore.CreateEscalation("low", "test/agent", "low sev escalation")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID, time.Now().Add(-72*time.Hour))

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	renotified, err := d.checkAgingEscalations(context.Background())
	if err != nil {
		t.Fatalf("checkAgingEscalations failed: %v", err)
	}
	if renotified != 0 {
		t.Errorf("renotified = %d, want 0 (low severity should never be re-notified)", renotified)
	}
}

func TestAgingAlertsRespectLastNotifiedAt(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create a critical escalation that was created 2 hours ago
	// but was last re-notified 5 minutes ago (within 30m threshold).
	escID, err := sphereStore.CreateEscalation("critical", "test/agent", "recently notified")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID, time.Now().Add(-2*time.Hour))
	setEscalationLastNotifiedAt(t, sphereStore, escID, time.Now().Add(-5*time.Minute))

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	renotified, err := d.checkAgingEscalations(context.Background())
	if err != nil {
		t.Fatalf("checkAgingEscalations failed: %v", err)
	}
	if renotified != 0 {
		t.Errorf("renotified = %d, want 0 (last_notified_at is recent, within threshold)", renotified)
	}
}

func TestAgingAlertsNotYetDue(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create a critical escalation that's only 10 minutes old (under 30m threshold).
	escID, err := sphereStore.CreateEscalation("critical", "test/agent", "fresh escalation")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}
	setEscalationCreatedAt(t, sphereStore, escID, time.Now().Add(-10*time.Minute))

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	renotified, err := d.checkAgingEscalations(context.Background())
	if err != nil {
		t.Fatalf("checkAgingEscalations failed: %v", err)
	}
	if renotified != 0 {
		t.Errorf("renotified = %d, want 0 (escalation too young)", renotified)
	}
}

func TestBuildupAlertFiresAboveThreshold(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create enough escalations to exceed threshold.
	for i := 0; i < 6; i++ {
		_, err := sphereStore.CreateEscalation("high", "test/agent", fmt.Sprintf("esc %d", i))
		if err != nil {
			t.Fatalf("failed to create escalation: %v", err)
		}
	}

	// Set up webhook server to capture the buildup alert.
	var receivedPayload buildupPayload
	webhookCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sessions := newMockSessions()
	logger := events.NewLogger(solHome)
	cfg := Config{
		StaleTetherTimeout:  15 * time.Minute,
		SolHome:             solHome,
		EscalationWebhook:   server.URL,
		EscalationThreshold: 5,
		EscalationConfig:    config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, logger)

	alert := d.checkEscalationBuildup(context.Background())
	if !alert {
		t.Error("expected buildup alert to fire (6 >= 5 threshold)")
	}
	if !webhookCalled {
		t.Error("expected webhook to be called")
	}
	if receivedPayload.Type != "escalation_buildup" {
		t.Errorf("webhook type = %q, want %q", receivedPayload.Type, "escalation_buildup")
	}
	if receivedPayload.Count != 6 {
		t.Errorf("webhook count = %d, want 6", receivedPayload.Count)
	}
	if receivedPayload.Threshold != 5 {
		t.Errorf("webhook threshold = %d, want 5", receivedPayload.Threshold)
	}
}

func TestBuildupAlertDebounced(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	for i := 0; i < 6; i++ {
		_, err := sphereStore.CreateEscalation("high", "test/agent", fmt.Sprintf("esc %d", i))
		if err != nil {
			t.Fatalf("failed to create escalation: %v", err)
		}
	}

	webhookCallCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCallCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout:  15 * time.Minute,
		SolHome:             solHome,
		EscalationWebhook:   server.URL,
		EscalationThreshold: 5,
		EscalationConfig:    config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	// First call should fire.
	alert1 := d.checkEscalationBuildup(context.Background())
	if !alert1 {
		t.Error("expected first buildup alert to fire")
	}

	// Second call immediately after should be debounced.
	alert2 := d.checkEscalationBuildup(context.Background())
	if alert2 {
		t.Error("expected second buildup alert to be debounced (within 30 minutes)")
	}

	if webhookCallCount != 1 {
		t.Errorf("webhook calls = %d, want 1 (debounced)", webhookCallCount)
	}
}

func TestBuildupAlertBelowThreshold(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Only create 3 escalations (below threshold of 5).
	for i := 0; i < 3; i++ {
		_, err := sphereStore.CreateEscalation("high", "test/agent", fmt.Sprintf("esc %d", i))
		if err != nil {
			t.Fatalf("failed to create escalation: %v", err)
		}
	}

	webhookCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout:  15 * time.Minute,
		SolHome:             solHome,
		EscalationWebhook:   server.URL,
		EscalationThreshold: 5,
		EscalationConfig:    config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	alert := d.checkEscalationBuildup(context.Background())
	if alert {
		t.Error("expected no buildup alert (3 < 5 threshold)")
	}
	if webhookCalled {
		t.Error("webhook should not have been called")
	}
}

func TestBuildupAlertNoWebhook(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	for i := 0; i < 6; i++ {
		sphereStore.CreateEscalation("high", "test/agent", fmt.Sprintf("esc %d", i))
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout:  15 * time.Minute,
		SolHome:             solHome,
		EscalationWebhook:   "", // no webhook
		EscalationThreshold: 5,
		EscalationConfig:    config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)

	alert := d.checkEscalationBuildup(context.Background())
	if alert {
		t.Error("expected no alert when webhook URL is empty")
	}
}

func TestStaleSourceRefResolvesClosedWrit(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "resolve-world"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	// Register world so ListWorlds finds it.
	sphereStore.RegisterWorld(worldName, "")

	// Create a writ and close it.
	writID, err := worldStore.CreateWrit("closed-task", "desc", "test", 1, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}
	worldStore.UpdateWrit(writID, store.WritUpdates{Status: "closed"})

	// Create an escalation with source_ref pointing to the closed writ.
	escID, err := sphereStore.CreateEscalation("high", "test/agent", "linked to writ", "writ:"+writID)
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	d.resolveStaleSourceRefs(context.Background())

	// Verify escalation was resolved.
	esc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("failed to get escalation: %v", err)
	}
	if esc.Status != "resolved" {
		t.Errorf("escalation status = %q, want %q (should auto-resolve for closed writ)", esc.Status, "resolved")
	}
}

func TestStaleSourceRefSkipsOpenWrit(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "open-writ-world"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	sphereStore.RegisterWorld(worldName, "")

	// Create an open writ.
	writID, err := worldStore.CreateWrit("open-task", "desc", "test", 1, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Create an escalation linked to the open writ.
	escID, err := sphereStore.CreateEscalation("high", "test/agent", "linked to open writ", "writ:"+writID)
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	d.resolveStaleSourceRefs(context.Background())

	// Verify escalation is still open.
	esc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("failed to get escalation: %v", err)
	}
	if esc.Status == "resolved" {
		t.Error("escalation should NOT be resolved — writ is still open")
	}
}

func TestStaleSourceRefResolvesMergedMR(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "mr-world"
	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore.Close()

	sphereStore.RegisterWorld(worldName, "")

	// Create a writ and an MR, then merge the MR.
	writID, _ := worldStore.CreateWrit("mr-task", "desc", "test", 1, nil)
	mrID, err := worldStore.CreateMergeRequest(writID, "feature-branch", 2)
	if err != nil {
		t.Fatalf("failed to create MR: %v", err)
	}

	// Claim then merge the MR.
	worldStore.ClaimMergeRequest("forge/test")
	worldStore.UpdateMergeRequestPhase(mrID, "merged")

	// Create escalation linked to the merged MR.
	escID, err := sphereStore.CreateEscalation("high", "test/agent", "linked to MR", "mr:"+mrID)
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	})

	d.resolveStaleSourceRefs(context.Background())

	// Verify escalation was resolved.
	esc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("failed to get escalation: %v", err)
	}
	if esc.Status != "resolved" {
		t.Errorf("escalation status = %q, want %q (should auto-resolve for merged MR)", esc.Status, "resolved")
	}
}

func TestStaleSourceRefDegradeOnWorldUnavailable(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	worldName := "unavailable-world"
	sphereStore.RegisterWorld(worldName, "")

	// Create escalation with source_ref pointing to a writ in an unavailable world.
	escID, err := sphereStore.CreateEscalation("high", "test/agent", "linked to unavailable writ", "writ:sol-abc123")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		return nil, fmt.Errorf("world store unavailable (simulated)")
	})

	// Should not panic or fail — DEGRADE behavior.
	d.resolveStaleSourceRefs(context.Background())

	// Verify escalation is still open (not resolved, not errored).
	esc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("failed to get escalation: %v", err)
	}
	if esc.Status == "resolved" {
		t.Error("escalation should NOT be resolved — world was unavailable (DEGRADE)")
	}
}

func TestStaleSourceRefNoSourceRef(t *testing.T) {
	solHome := setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	defer sphereStore.Close()

	// Create escalation without source_ref — should be skipped.
	escID, err := sphereStore.CreateEscalation("high", "test/agent", "no source ref")
	if err != nil {
		t.Fatalf("failed to create escalation: %v", err)
	}

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            solHome,
		EscalationConfig:   config.DefaultEscalationConfig(),
	}

	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.Store, error) {
		t.Fatalf("world opener should not be called for escalation without source_ref")
		return nil, nil
	})

	d.resolveStaleSourceRefs(context.Background())

	// Verify escalation is still open.
	esc, err := sphereStore.GetEscalation(escID)
	if err != nil {
		t.Fatalf("failed to get escalation: %v", err)
	}
	if esc.Status == "resolved" {
		t.Error("escalation should NOT be resolved — no source_ref")
	}
}

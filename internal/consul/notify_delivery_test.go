package consul

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/maildeliver"
	"github.com/nevinsm/sol/internal/store"
)

// --- Delivery-signal wiring for the caravan-close notification
// (sol-8a0692b9201c3a1a follow-up: sol-c431eee15f926732) ---
//
// TestFeedStrandedCaravansAutoCloseAllMerged (consul_test.go) already covers
// the durable-close/insert contract. These tests cover the layer this writ
// adds on top: that feedStrandedCaravans calls the shared delivery helper
// (deliverMail, a var over maildeliver.Deliver — see maildeliver_test.go for
// why consul's tests fake it) exactly when — and only when —
// TryCloseCaravan actually inserted a completion mail.

// newNotifyTestConsul opens real sphere/world stores against the temp
// SOL_HOME set up by setupSolHome and wires a Consul with a mock dispatch
// func and session manager, mirroring the other feedStrandedCaravans tests
// in consul_test.go.
func newNotifyTestConsul(t *testing.T, worldName string) (*Consul, *store.SphereStore, *store.WorldStore) {
	t.Helper()
	setupSolHome(t)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}
	t.Cleanup(func() { sphereStore.Close() })

	worldStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	t.Cleanup(func() { worldStore.Close() })

	sessions := newMockSessions()
	cfg := Config{
		StaleTetherTimeout: 15 * time.Minute,
		SolHome:            config.Home(),
	}

	var dispatched []mockDispatchResult
	d := New(cfg, sphereStore, sessions, nil, nil)
	d.SetWorldOpener(func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	d.SetDispatchFunc(newMockDispatchFunc(&dispatched))

	return d, sphereStore, worldStore
}

// TestFeedStrandedCaravansDeliversOnActualInsert verifies that closing a
// notify-enabled caravan (all items merged) triggers exactly one deliverMail
// call whose fields match the CaravanNotifySent TryCloseCaravan returned.
func TestFeedStrandedCaravansDeliversOnActualInsert(t *testing.T) {
	worldName := "notify-deliver"
	d, sphereStore, worldStore := newNotifyTestConsul(t, worldName)
	calls := installFakeDeliverMail(t)

	caravanID, err := sphereStore.CreateCaravanWithNotify("notify-caravan", "sol-dev/Nova", true)
	if err != nil {
		t.Fatalf("CreateCaravanWithNotify failed: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus failed: %v", err)
	}

	wi1, _ := worldStore.CreateWrit("merged-1", "desc1", "test", 1, nil)
	wi2, _ := worldStore.CreateWrit("merged-2", "desc2", "test", 1, nil)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "closed"})
	worldStore.UpdateWrit(wi2, store.WritUpdates{Status: "closed"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)
	sphereStore.CreateCaravanItem(caravanID, wi2, worldName, 0)

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (no items to dispatch)", fed)
	}

	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan failed: %v", err)
	}
	if caravan.Status != "closed" {
		t.Fatalf("caravan status = %q, want closed", caravan.Status)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected exactly 1 deliverMail call, got %d: %+v", len(*calls), *calls)
	}
	got := (*calls)[0]
	if got.Recipient != "sol-dev/Nova" {
		t.Errorf("Recipient = %q, want %q", got.Recipient, "sol-dev/Nova")
	}
	if !strings.Contains(got.Subject, "Caravan complete") || !strings.Contains(got.Subject, caravanID) {
		t.Errorf("Subject = %q, want to mention caravan completion + id", got.Subject)
	}
	if !strings.Contains(got.Body, wi1) || !strings.Contains(got.Body, wi2) {
		t.Errorf("Body = %q, want to mention both writ ids", got.Body)
	}
	if got.Priority != 2 {
		t.Errorf("Priority = %d, want 2", got.Priority)
	}
	if got.MessageID == "" {
		t.Error("expected MessageID to be populated")
	}
}

// TestFeedStrandedCaravansNoDeliveryWhenNotifyOff verifies deliverMail is
// never called when the caravan didn't opt into notify_on_close —
// TryCloseCaravan closes the caravan but notifyCaravanClosed returns nil
// before ever inserting mail.
func TestFeedStrandedCaravansNoDeliveryWhenNotifyOff(t *testing.T) {
	worldName := "notify-off"
	d, sphereStore, worldStore := newNotifyTestConsul(t, worldName)
	calls := installFakeDeliverMail(t)

	caravanID, err := sphereStore.CreateCaravan("no-notify-caravan", "sol-dev/Nova")
	if err != nil {
		t.Fatalf("CreateCaravan failed: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus failed: %v", err)
	}

	wi1, _ := worldStore.CreateWrit("merged-1", "desc1", "test", 1, nil)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "closed"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans failed: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (no items to dispatch)", fed)
	}

	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan failed: %v", err)
	}
	if caravan.Status != "closed" {
		t.Fatalf("caravan status = %q, want closed", caravan.Status)
	}

	if len(*calls) != 0 {
		t.Errorf("expected no deliverMail calls when notify is off, got %d: %+v", len(*calls), *calls)
	}
}

// TestFeedStrandedCaravansNoDeliveryOnDedupSkip verifies that a repeat
// close (the caravan is already closed, and a second patrol reaches
// TryCloseCaravan again) delivers exactly once overall: the dedup_key on
// the completion mail blocks the second insert, and notifySent is nil on
// that second call — feedStrandedCaravans must not call deliverMail again.
func TestFeedStrandedCaravansNoDeliveryOnDedupSkip(t *testing.T) {
	worldName := "notify-dedup"
	d, sphereStore, worldStore := newNotifyTestConsul(t, worldName)
	calls := installFakeDeliverMail(t)

	caravanID, err := sphereStore.CreateCaravanWithNotify("dedup-caravan", "sol-dev/Nova", true)
	if err != nil {
		t.Fatalf("CreateCaravanWithNotify failed: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus failed: %v", err)
	}

	wi1, _ := worldStore.CreateWrit("merged-1", "desc1", "test", 1, nil)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "closed"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	if _, err := d.feedStrandedCaravans(context.Background()); err != nil {
		t.Fatalf("first feedStrandedCaravans failed: %v", err)
	}

	// The caravan is already "closed", so ListCaravans("open") no longer
	// returns it on a second patrol — call TryCloseCaravan directly to
	// exercise the same TOCTOU re-close path consul's patrol can hit
	// (documented on TryCloseCaravan: a re-patrol of an already-closed
	// caravan is expected to be reachable in production).
	closed, notifySent, err := sphereStore.TryCloseCaravan(caravanID, func(world string) (*store.WorldStore, error) {
		return store.OpenWorld(world)
	})
	if err != nil {
		t.Fatalf("second TryCloseCaravan failed: %v", err)
	}
	if !closed {
		t.Fatalf("expected second TryCloseCaravan to report closed=true (already-closed caravan)")
	}
	if notifySent != nil {
		t.Fatalf("expected notifySent == nil on dedup repeat, got %+v", notifySent)
	}

	if len(*calls) != 1 {
		t.Errorf("expected exactly 1 deliverMail call across both closes (dedup), got %d: %+v", len(*calls), *calls)
	}
}

// TestFeedStrandedCaravansContinuesOnDeliverError verifies that a
// deliverMail failure is best-effort: the patrol logs the error and
// continues rather than failing feedStrandedCaravans, and the caravan
// remains closed.
func TestFeedStrandedCaravansContinuesOnDeliverError(t *testing.T) {
	worldName := "notify-deliver-error"
	d, sphereStore, worldStore := newNotifyTestConsul(t, worldName)
	t.Cleanup(func() { deliverMail = noopDeliverMail })

	var callCount int
	deliverMail = func(maildeliver.Opts) error {
		callCount++
		return fmt.Errorf("simulated delivery failure")
	}

	caravanID, err := sphereStore.CreateCaravanWithNotify("deliver-error-caravan", "sol-dev/Nova", true)
	if err != nil {
		t.Fatalf("CreateCaravanWithNotify failed: %v", err)
	}
	if err := sphereStore.UpdateCaravanStatus(caravanID, "open"); err != nil {
		t.Fatalf("UpdateCaravanStatus failed: %v", err)
	}

	wi1, _ := worldStore.CreateWrit("merged-1", "desc1", "test", 1, nil)
	worldStore.UpdateWrit(wi1, store.WritUpdates{Status: "closed"})
	sphereStore.CreateCaravanItem(caravanID, wi1, worldName, 0)

	fed, err := d.feedStrandedCaravans(context.Background())
	if err != nil {
		t.Fatalf("feedStrandedCaravans should succeed despite delivery failure, got: %v", err)
	}
	if fed != 0 {
		t.Errorf("fed = %d, want 0 (no items to dispatch)", fed)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 deliverMail call, got %d", callCount)
	}

	caravan, err := sphereStore.GetCaravan(caravanID)
	if err != nil {
		t.Fatalf("GetCaravan failed: %v", err)
	}
	if caravan.Status != "closed" {
		t.Errorf("caravan status = %q, want closed (delivery failure must not roll back close)", caravan.Status)
	}
}

package forge

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// --- Delivery-signal wiring for writ-merged/writ-failed notifications
// (sol-8a0692b9201c3a1a) ---
//
// TestMarkMergedSendsNotifyMailWhenOptedIn and friends (toolbox_test.go)
// already cover the durable-insert contract (recipient, subject, dedup key,
// thread). These tests cover the layer this writ adds on top: that
// sendWritNotification calls the shared delivery helper (deliverMail, a
// var over maildeliver.Deliver — see maildeliver_test.go for why forge's
// tests fake it) exactly when — and only when — the insert actually
// happened.

func TestMarkMergedDeliversOnActualInsert(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Notify me", Status: store.WritDone,
		CreatedBy: "sol-dev/Nova", NotifyOnClose: true,
	}
	sphereStore := newMockSphereStore()
	r := newNotifyMergeForge(t, worldStore, sphereStore)
	calls := installFakeDeliverMail(t)

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected exactly 1 deliverMail call, got %d: %+v", len(*calls), *calls)
	}
	got := (*calls)[0]
	if got.Recipient != "sol-dev/Nova" {
		t.Errorf("Recipient = %q, want %q", got.Recipient, "sol-dev/Nova")
	}
	if got.Priority != 2 {
		t.Errorf("Priority = %d, want 2", got.Priority)
	}
	if !strings.Contains(got.Subject, "Writ merged") || !strings.Contains(got.Subject, "sol-aaa11111") {
		t.Errorf("Subject = %q, want to mention merge + writ id", got.Subject)
	}
	if got.MessageID == "" {
		t.Error("expected MessageID to be populated")
	}
	if got.Suppress {
		t.Error("expected Suppress to be false — writ notifications have no --no-notify equivalent")
	}
}

func TestMarkFailedDeliversOnActualInsert(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed, Attempts: 2},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Notify me", Status: store.WritDone,
		CreatedBy: "sol-dev/Nova", NotifyOnClose: true,
	}
	sphereStore := newMockSphereStore()
	r := &Forge{
		world:       "ember",
		agentID:     "ember/forge",
		worldStore:  worldStore,
		sphereStore: sphereStore,
		logger:      testLogger(),
		cfg:         DefaultConfig(),
	}
	calls := installFakeDeliverMail(t)

	if err := r.MarkFailed("mr-00000001", "quality gate failed"); err != nil {
		t.Fatalf("MarkFailed() error: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected exactly 1 deliverMail call, got %d: %+v", len(*calls), *calls)
	}
	got := (*calls)[0]
	if got.Recipient != "sol-dev/Nova" {
		t.Errorf("Recipient = %q, want %q", got.Recipient, "sol-dev/Nova")
	}
	if !strings.Contains(got.Subject, "Writ merge failed") {
		t.Errorf("Subject = %q, want to mention merge failed", got.Subject)
	}
}

// TestMarkMergedNoDeliveryOnDedupSkip verifies that a repeat MarkMerged for
// the same writ (the mock's CloseWrit has no double-close guard, unlike the
// real store — see TestMarkMergedRepeatedInvocationsDoNotDuplicateMail)
// delivers exactly once: the dedup_key blocks the second insert, and
// sendWritNotification must not call deliverMail on a skipped insert.
func TestMarkMergedNoDeliveryOnDedupSkip(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Notify me", Status: store.WritDone,
		CreatedBy: "sol-dev/Nova", NotifyOnClose: true,
	}
	sphereStore := newMockSphereStore()
	r := newNotifyMergeForge(t, worldStore, sphereStore)
	calls := installFakeDeliverMail(t)

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("first MarkMerged() error: %v", err)
	}
	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("second MarkMerged() error: %v", err)
	}

	if len(*calls) != 1 {
		t.Errorf("expected exactly 1 deliverMail call across two invocations (dedup), got %d: %+v", len(*calls), *calls)
	}
}

// TestMarkMergedNoDeliveryWhenNotifyOff verifies deliverMail is never
// called when the writ didn't opt into notify — sendWritNotification
// returns before ever touching the store or the delivery helper.
func TestMarkMergedNoDeliveryWhenNotifyOff(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "No notify", Status: store.WritDone,
		CreatedBy: "sol-dev/Nova", NotifyOnClose: false,
	}
	sphereStore := newMockSphereStore()
	r := newNotifyMergeForge(t, worldStore, sphereStore)
	calls := installFakeDeliverMail(t)

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() error: %v", err)
	}

	if len(*calls) != 0 {
		t.Errorf("expected no deliverMail calls when notify is off, got %d: %+v", len(*calls), *calls)
	}
}

// TestMarkMergedNoDeliveryWhenSendFails verifies a mail-insert failure
// (e.g. a transient store error) also means no delivery signal fires —
// there is nothing durable to signal.
func TestMarkMergedNoDeliveryWhenSendFails(t *testing.T) {
	worldStore := newMockWorldStore()
	worldStore.mrs = []store.MergeRequest{
		{ID: "mr-00000001", WritID: "sol-aaa11111", Branch: "outpost/Toast/sol-aaa11111", Phase: store.MRClaimed},
	}
	worldStore.items["sol-aaa11111"] = &store.Writ{
		ID: "sol-aaa11111", Title: "Notify me", Status: store.WritDone,
		CreatedBy: "sol-dev/Nova", NotifyOnClose: true,
	}
	sphereStore := newMockSphereStore()
	sphereStore.sendMessageErr = fmt.Errorf("smtp down")
	r := newNotifyMergeForge(t, worldStore, sphereStore)
	calls := installFakeDeliverMail(t)

	if err := r.MarkMerged("mr-00000001"); err != nil {
		t.Fatalf("MarkMerged() should succeed despite mail send failure, got: %v", err)
	}

	if len(*calls) != 0 {
		t.Errorf("expected no deliverMail calls when the insert itself failed, got %d: %+v", len(*calls), *calls)
	}
}

package integration

import (
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/maildeliver"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

// --- Caravan completion-mail delivery-signal integration tests
// (sol-8a0692b9201c3a1a) ---
//
// Before this writ, `sol caravan close` (and consul's auto-close patrol)
// wrote the completion mail durably via store.TryCloseCaravan but never
// signaled it — no doorbell nudge for a live owner session, no
// wake-on-mail for a stopped envoy owner. These tests exercise the real
// `sol caravan close` CLI path end to end (the same shared
// internal/maildeliver.Deliver the CLI's `sol mail send` and forge's
// writ-merged/failed notices now also go through) and confirm the signal
// actually fires.

// closeableCaravanWithNotify sets up a world with one writ already closed
// (merged) and a notify-enabled caravan containing it, so a subsequent
// `sol caravan close <id> --confirm` both closes the caravan and inserts
// the owner's completion mail. Returns the caravan ID.
func closeableCaravanWithNotify(t *testing.T, world, owner string) string {
	t.Helper()

	worldStore, sphereStore := openStores(t, world)
	id, err := worldStore.CreateWrit("Landed work", "", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if _, err := worldStore.CloseWrit(id); err != nil {
		t.Fatalf("CloseWrit: %v", err)
	}

	caravanID, err := sphereStore.CreateCaravanWithNotify("delivery-test", owner, true)
	if err != nil {
		t.Fatalf("CreateCaravanWithNotify: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, id, world, 0); err != nil {
		t.Fatalf("CreateCaravanItem: %v", err)
	}
	return caravanID
}

// TestCaravanCloseNudgesLiveOwnerSession verifies that closing a
// notify-enabled caravan while its owner envoy has a live session delivers
// a doorbell nudge into that session's queue — the dash-cockpit bug this
// writ fixes (a completion mail sitting unread next to a live session).
func TestCaravanCloseNudgesLiveOwnerSession(t *testing.T) {
	skipUnlessIntegration(t)
	requireTmuxAvailable(t)

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)
	createEnvoy(t, gtHome, "myworld", "scout")

	sessName := config.SessionName("myworld", "scout")
	out, err := runGT(t, gtHome, "envoy", "start", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy start failed: %v: %s", err, out)
	}
	t.Cleanup(func() { runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld") })

	if !pollUntil(defaultPollTimeout, defaultPollInterval, func() bool { return tmuxSessionExists(sessName) }) {
		t.Fatal("envoy session did not start")
	}

	caravanID := closeableCaravanWithNotify(t, "myworld", "myworld/scout")

	out, err = runGT(t, gtHome, "caravan", "close", caravanID, "--confirm")
	if err != nil {
		t.Fatalf("caravan close failed: %v: %s", err, out)
	}
	if !strings.Contains(out, caravanID) {
		t.Errorf("expected close output to mention caravan id, got: %s", out)
	}

	count, err := nudge.Peek(sessName)
	if err != nil {
		t.Fatalf("nudge.Peek: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 nudge queued for the live owner session, got %d", count)
	}
}

// TestCaravanCloseWakesOwnerNoLiveSession verifies that closing a
// notify-enabled caravan whose owner envoy has NO live session starts one
// via wake-on-mail, exactly as `sol mail send` does for a directly-sent
// message — the delivery-signal stack is shared, so caravan completion
// mail gets the same treatment.
func TestCaravanCloseWakesOwnerNoLiveSession(t *testing.T) {
	skipUnlessIntegration(t)
	requireTmuxAvailable(t)

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)
	createEnvoy(t, gtHome, "myworld", "scout")

	sessName := config.SessionName("myworld", "scout")
	if tmuxSessionExists(sessName) {
		t.Fatal("precondition failed: envoy session already running")
	}

	caravanID := closeableCaravanWithNotify(t, "myworld", "myworld/scout")

	out, err := runGT(t, gtHome, "caravan", "close", caravanID, "--confirm")
	if err != nil {
		t.Fatalf("caravan close failed: %v: %s", err, out)
	}
	t.Cleanup(func() { runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld") })

	if !pollUntil(defaultPollTimeout, defaultPollInterval, func() bool { return tmuxSessionExists(sessName) }) {
		t.Fatal("expected caravan close to wake the owner envoy via wake-on-mail")
	}

	count, err := nudge.Peek(sessName)
	if err != nil {
		t.Fatalf("nudge.Peek: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 nudge queued for the woken owner, got %d", count)
	}
}

// TestCaravanCloseAutarchOwnerNoSignal verifies the autarch-recipient
// exemption survives through the caravan close path: the completion mail
// is still inserted (an autarch owner is a normal, supported case — see
// cmd/caravan.go's default owner resolution), but there is no session to
// nudge or wake, so none is attempted.
func TestCaravanCloseAutarchOwnerNoSignal(t *testing.T) {
	skipUnlessIntegration(t)
	requireTmuxAvailable(t)

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)

	caravanID := closeableCaravanWithNotify(t, "myworld", "autarch")

	out, err := runGT(t, gtHome, "caravan", "close", caravanID, "--confirm")
	if err != nil {
		t.Fatalf("caravan close failed: %v: %s", err, out)
	}
	if !strings.Contains(out, caravanID) {
		t.Errorf("expected close output to mention caravan id, got: %s", out)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer sphereStore.Close()

	msgs, err := sphereStore.Inbox("autarch")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected the completion mail to still be durably delivered to autarch, got %d messages", len(msgs))
	}
}

// TestCaravanCloseDedupProducesExactlyOneDeliverySignal exercises the
// consul re-patrol / TOCTOU scenario directly against the store and
// production delivery wiring: TryCloseCaravan is invoked twice for the
// same caravan (the second call simulates consul's unconditional
// auto-close attempt on an already-closed caravan, or cmd/caravan.go's
// documented TOCTOU retry window), and the delivery helper must fire
// exactly once — a naive "always deliver when closed==true" wiring would
// double-nudge on every re-patrol of an already-closed caravan.
func TestCaravanCloseDedupProducesExactlyOneDeliverySignal(t *testing.T) {
	skipUnlessIntegration(t)
	requireTmuxAvailable(t)

	gtHome, sourceRepo := setupTestEnv(t)
	initWorldWithRepo(t, gtHome, "myworld", sourceRepo)
	createEnvoy(t, gtHome, "myworld", "scout")

	sessName := config.SessionName("myworld", "scout")
	out, err := runGT(t, gtHome, "envoy", "start", "scout", "--world=myworld")
	if err != nil {
		t.Fatalf("envoy start failed: %v: %s", err, out)
	}
	t.Cleanup(func() { runGT(t, gtHome, "envoy", "stop", "scout", "--world=myworld") })
	if !pollUntil(defaultPollTimeout, defaultPollInterval, func() bool { return tmuxSessionExists(sessName) }) {
		t.Fatal("envoy session did not start")
	}

	worldStore, sphereStore := openStores(t, "myworld")
	id, err := worldStore.CreateWrit("Landed work", "", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}
	if _, err := worldStore.CloseWrit(id); err != nil {
		t.Fatalf("CloseWrit: %v", err)
	}
	caravanID, err := sphereStore.CreateCaravanWithNotify("dedup-test", "myworld/scout", true)
	if err != nil {
		t.Fatalf("CreateCaravanWithNotify: %v", err)
	}
	if err := sphereStore.CreateCaravanItem(caravanID, id, "myworld", 0); err != nil {
		t.Fatalf("CreateCaravanItem: %v", err)
	}

	// Simulate two unconditional TryCloseCaravan invocations against the
	// SAME caravan (consul's patrol loop calls this every cycle regardless
	// of current status), each followed by exactly the production glue
	// cmd/caravan.go and consul.go use: deliver only when a notification
	// was actually (re-)inserted.
	deliverIfSent := func() {
		closed, sent, err := sphereStore.TryCloseCaravan(caravanID, store.OpenWorld)
		if err != nil {
			t.Fatalf("TryCloseCaravan: %v", err)
		}
		if !closed {
			t.Fatal("expected caravan to report closed")
		}
		if sent == nil {
			return
		}
		if err := maildeliver.Deliver(maildeliver.Opts{
			Recipient: sent.Recipient,
			MessageID: sent.MessageID,
			Subject:   sent.Subject,
			Body:      sent.Body,
			Priority:  sent.Priority,
		}); err != nil {
			t.Fatalf("maildeliver.Deliver: %v", err)
		}
	}

	deliverIfSent()
	deliverIfSent()

	count, err := nudge.Peek(sessName)
	if err != nil {
		t.Fatalf("nudge.Peek: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly 1 delivery signal across two TryCloseCaravan invocations, got %d nudges", count)
	}
}

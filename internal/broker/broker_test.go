package broker

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/account"
)

// ptrTime is a helper to create a *time.Time.
func ptrTime(t time.Time) *time.Time { return &t }

func TestCheckTokenExpiry_NoExpiry(t *testing.T) {
	tok := &account.Token{
		Type:  "api_key",
		Token: "sk-test",
	}
	th := checkTokenExpiry("alice", tok, nil)
	if th.Status != "no_expiry" {
		t.Errorf("expected no_expiry, got %q", th.Status)
	}
	if th.Type != "api_key" {
		t.Errorf("expected type api_key, got %q", th.Type)
	}
	if th.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for no-expiry token")
	}
}

func TestCheckTokenExpiry_Expired(t *testing.T) {
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(-1 * time.Hour)),
	}
	th := checkTokenExpiry("bob", tok, nil)
	if th.Status != "expired" {
		t.Errorf("expected expired, got %q", th.Status)
	}
	if th.Handle != "bob" {
		t.Errorf("expected handle bob, got %q", th.Handle)
	}
}

func TestCheckTokenExpiry_NearExpiry_Critical_Under1d(t *testing.T) {
	// Expires in 12 hours — within 1-day threshold.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(12 * time.Hour)),
	}
	th := checkTokenExpiry("carol", tok, nil)
	if th.Status != "critical" {
		t.Errorf("expected critical (under 1d), got %q", th.Status)
	}
}

func TestCheckTokenExpiry_NearExpiry_Warning_Under7d(t *testing.T) {
	// Expires in 3 days — within 7-day threshold but beyond 1-day threshold.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(3 * 24 * time.Hour)),
	}
	th := checkTokenExpiry("dave", tok, nil)
	if th.Status != "warning" {
		t.Errorf("expected warning (under 7d), got %q", th.Status)
	}
}

func TestCheckTokenExpiry_NearExpiry_ExpiringSoon(t *testing.T) {
	// Expires in 15 days — within 30-day threshold but beyond 7-day threshold.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(15 * 24 * time.Hour)),
	}
	th := checkTokenExpiry("eve", tok, nil)
	if th.Status != "expiring_soon" {
		t.Errorf("expected expiring_soon, got %q", th.Status)
	}
}

func TestCheckTokenExpiry_Ok(t *testing.T) {
	// Expires in 60 days — well beyond all thresholds.
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now().Add(60 * 24 * time.Hour)),
	}
	th := checkTokenExpiry("frank", tok, nil)
	if th.Status != "ok" {
		t.Errorf("expected ok, got %q", th.Status)
	}
}

func TestCheckTokenExpiry_ExactlyAtBoundary_Expired(t *testing.T) {
	// Exactly at expiry (timeLeft = 0) should be "expired".
	tok := &account.Token{
		Type:      "oauth_token",
		Token:     "tok",
		ExpiresAt: ptrTime(time.Now()),
	}
	th := checkTokenExpiry("grace", tok, nil)
	// timeLeft = time.Until(now) will be ~0 or negative.
	if th.Status != "expired" && th.Status != "critical" {
		t.Errorf("expected expired or critical at boundary, got %q", th.Status)
	}
}

func TestBrokerPatrolWritesHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	b := New(Config{}, nil)

	// Mock health probe so no real HTTP call.
	b.health.SetProbeFn(func() error { return nil })

	b.patrol()

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("heartbeat should exist after patrol")
	}
	if hb.PatrolCount != 1 {
		t.Errorf("expected patrol_count 1, got %d", hb.PatrolCount)
	}
	if hb.Status != "running" {
		t.Errorf("expected status %q, got %q", "running", hb.Status)
	}
}

func TestHeartbeatStale(t *testing.T) {
	hb := &Heartbeat{
		Timestamp: time.Now().Add(-15 * time.Minute),
	}
	if !hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 15m old should be stale with 10m threshold")
	}

	hb.Timestamp = time.Now().Add(-5 * time.Minute)
	if hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 5m old should not be stale with 10m threshold")
	}
}

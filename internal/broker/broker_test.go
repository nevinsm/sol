package broker

import (
	"testing"
	"time"
)

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

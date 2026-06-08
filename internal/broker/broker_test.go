package broker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBrokerPatrolWritesHeartbeat(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	b := New(Config{}, nil)

	// Override probe so no real binary is needed.
	b.SetProbeFn("claude", func() bool { return true })

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

func TestBrokerPatrolLiveness(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	b := New(Config{
		DiscoverFn: func() []string { return []string{"claude", "codex"} },
	}, nil)

	b.SetProbeFn("claude", func() bool { return true })
	b.SetProbeFn("codex", func() bool { return false })

	b.patrol()

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat")
	}
	if len(hb.Runtimes) != 2 {
		t.Fatalf("expected 2 runtime entries, got %d", len(hb.Runtimes))
	}

	// Sorted alphabetically: claude, codex.
	if hb.Runtimes[0].Runtime != "claude" || !hb.Runtimes[0].OK {
		t.Errorf("claude: expected ok=true, got %+v", hb.Runtimes[0])
	}
	if hb.Runtimes[1].Runtime != "codex" || hb.Runtimes[1].OK {
		t.Errorf("codex: expected ok=false, got %+v", hb.Runtimes[1])
	}
}

func TestBrokerAllOK(t *testing.T) {
	hb := &Heartbeat{
		Runtimes: []RuntimeLiveness{
			{Runtime: "claude", OK: true},
			{Runtime: "codex", OK: true},
		},
	}
	if !hb.AllOK() {
		t.Error("expected AllOK true when all runtimes are ok")
	}

	hb.Runtimes[1].OK = false
	if hb.AllOK() {
		t.Error("expected AllOK false when one runtime is not ok")
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

func TestMultiProviderDiscovery(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create two world directories with different runtimes.
	world1Dir := filepath.Join(solHome, "world1")
	os.MkdirAll(world1Dir, 0o755)
	os.WriteFile(filepath.Join(world1Dir, "world.toml"), []byte(`
[agents]
default_runtime = "claude"
`), 0o644)

	world2Dir := filepath.Join(solHome, "world2")
	os.MkdirAll(world2Dir, 0o755)
	os.WriteFile(filepath.Join(world2Dir, "world.toml"), []byte(`
[agents]
default_runtime = "claude"
[agents.runtimes]
outpost = "codex"
`), 0o644)

	runtimes := DiscoverWorldRuntimes()

	// Should find both "claude" and "codex", deduplicated and sorted.
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes, got %d: %v", len(runtimes), runtimes)
	}
	if runtimes[0] != "claude" {
		t.Errorf("expected first runtime to be claude, got %q", runtimes[0])
	}
	if runtimes[1] != "codex" {
		t.Errorf("expected second runtime to be codex, got %q", runtimes[1])
	}
}

func TestMultiProviderDiscoveryFallback(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// No world directories — should fall back to ["claude"].
	runtimes := DiscoverWorldRuntimes()
	if len(runtimes) != 1 || runtimes[0] != "claude" {
		t.Errorf("expected [claude] fallback, got %v", runtimes)
	}
}

func TestDiscoverRuntimesDefault(t *testing.T) {
	b := New(Config{}, nil)
	rts := b.discoverRuntimes()
	if len(rts) != 1 || rts[0] != "claude" {
		t.Errorf("expected [claude] default, got %v", rts)
	}
}

func TestDiscoverRuntimesCustom(t *testing.T) {
	b := New(Config{Runtime: "codex"}, nil)
	rts := b.discoverRuntimes()
	if len(rts) != 1 || rts[0] != "codex" {
		t.Errorf("expected [codex], got %v", rts)
	}
}

func TestDiscoverRuntimesDiscoverFn(t *testing.T) {
	called := false
	b := New(Config{
		DiscoverFn: func() []string {
			called = true
			return []string{"custom-runtime"}
		},
	}, nil)
	rts := b.discoverRuntimes()
	if !called {
		t.Error("expected DiscoverFn to be called")
	}
	if len(rts) != 1 || rts[0] != "custom-runtime" {
		t.Errorf("expected [custom-runtime], got %v", rts)
	}
}

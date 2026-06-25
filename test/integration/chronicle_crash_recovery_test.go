package integration

// chronicle_crash_recovery_test.go — Integration test for the chronicle
// crash-recovery path documented in docs/failure-modes.md (lines 198-203).
//
// Coverage gap addressed: the checkpoint/restart recovery path (tail from last
// checkpoint, rebuild curated feed) was previously marked "Tested manually".
// This test exercises it end-to-end using the Chronicle.Run() lifecycle.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/events"
)

// TestChronicleCrashRecovery exercises the chronicle crash-recovery path
// documented in docs/failure-modes.md (lines 198-203).
//
// Verifies:
//
//	(a) Events written before the crash are present in the curated feed.
//	(b) Events emitted during downtime are backfilled after restart.
//	(c) No duplicate events appear in the curated feed.
//
// The test exercises the checkpoint/restart mechanism: Chronicle.Run() saves
// the current position in the raw event log after each successful processing
// cycle. On restart, it resumes from that checkpoint and processes only events
// written after the last saved offset.
func TestChronicleCrashRecovery(t *testing.T) {
	skipUnlessIntegration(t)

	solHome, _ := setupTestEnv(t)

	cfg := events.DefaultChronicleConfig(solHome)
	cfg.PollInterval = 50 * time.Millisecond
	// AggWindow is irrelevant for non-aggregatable events (EventResolve,
	// EventTether), but set it large to prevent any interference.
	cfg.AggWindow = 24 * time.Hour

	logger := events.NewLogger(solHome)

	// === Phase 1: Start chronicle ===
	//
	// Chronicle starts before any events are written. No checkpoint file exists
	// and the raw log does not exist yet, so the initial offset is 0. The first
	// poll cycle will read events starting from position 0.
	ctx1, cancel1 := context.WithCancel(context.Background())
	c1 := events.NewChronicle(cfg)
	errCh1 := make(chan error, 1)
	go func() { errCh1 <- c1.Run(ctx1) }()

	// Wait for chronicle to initialise (set offset, write initial heartbeat).
	// Chronicle writes $SOL_HOME/.runtime/chronicle-heartbeat.json on startup.
	heartbeatPath := filepath.Join(solHome, ".runtime", "chronicle-heartbeat.json")
	if !pollUntil(5*time.Second, 50*time.Millisecond, func() bool {
		_, err := os.Stat(heartbeatPath)
		return err == nil
	}) {
		cancel1()
		t.Fatal("chronicle did not write initial heartbeat within 5s")
	}

	// === Phase 2: Write pre-crash events ===
	//
	// EventResolve is non-aggregatable and passes directly through the
	// dedup+aggregation pipeline. Each event uses a unique actor so the
	// dedup cache does not suppress any of them.
	const preCrashCount = 3
	for i := range preCrashCount {
		logger.Emit(events.EventResolve, "sol",
			fmt.Sprintf("pre-crash-agent-%d", i), "feed", nil)
	}

	// === Phase 3: Wait for pre-crash events in curated feed ===
	//
	// Chronicle's PollInterval is 50 ms; allow 5 s headroom for CI jitter.
	if !pollUntil(5*time.Second, 100*time.Millisecond, func() bool {
		return countEventsInFeed(t, cfg.FeedPath, events.EventResolve) >= preCrashCount
	}) {
		cancel1()
		t.Fatalf("chronicle did not process %d pre-crash events within 5s", preCrashCount)
	}

	// === Phase 4: Stop chronicle (simulates crash) ===
	//
	// Context cancellation triggers graceful shutdown. The shutdown path saves
	// the checkpoint at the current offset — the same position that the last
	// successful processCycle() committed. In a real crash the checkpoint would
	// reflect the last processCycle save; the net result is identical: the
	// checkpoint is past all pre-crash events.
	cancel1()
	select {
	case err := <-errCh1:
		if err != nil {
			t.Logf("chronicle phase 1 shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("chronicle did not shut down within 5s")
	}

	// === Phase 5: Write downtime events (chronicle not running) ===
	//
	// Events appended while chronicle is down land at raw-log positions past
	// the checkpoint. The restarted chronicle will resume from the checkpoint
	// and pick these up on its first poll cycle.
	const downtimeCount = 3
	for i := range downtimeCount {
		logger.Emit(events.EventTether, "sol",
			fmt.Sprintf("downtime-agent-%d", i), "feed", nil)
	}

	// === Phase 6: Restart chronicle ===
	//
	// A new Chronicle instance loads the checkpoint file saved by the previous
	// run and resumes reading from the saved offset — exactly the documented
	// crash-recovery path in docs/failure-modes.md lines 198-203.
	ctx2, cancel2 := context.WithCancel(context.Background())
	c2 := events.NewChronicle(cfg)
	errCh2 := make(chan error, 1)
	go func() { errCh2 <- c2.Run(ctx2) }()

	// Register cleanup: cancel the second chronicle and wait for it to stop.
	t.Cleanup(func() {
		cancel2()
		select {
		case <-errCh2:
		case <-time.After(5 * time.Second):
			t.Log("chronicle phase 2 did not shut down during cleanup")
		}
	})

	// === Phase 7: Wait for downtime events in curated feed ===
	if !pollUntil(5*time.Second, 100*time.Millisecond, func() bool {
		return countEventsInFeed(t, cfg.FeedPath, events.EventTether) >= downtimeCount
	}) {
		t.Fatalf("chronicle did not backfill %d downtime events within 5s after restart", downtimeCount)
	}

	// === Phase 8: Assertions ===
	feed := readFeedFile(t, cfg.FeedPath)

	// (a) Pre-crash events are present in the curated feed.
	preCrashFound := 0
	for _, ev := range feed {
		if ev.Type == events.EventResolve {
			preCrashFound++
		}
	}
	if preCrashFound < preCrashCount {
		t.Errorf("(a) pre-crash events: found %d in feed, want >= %d",
			preCrashFound, preCrashCount)
	}

	// (b) Downtime events are backfilled after restart.
	downtimeFound := 0
	for _, ev := range feed {
		if ev.Type == events.EventTether {
			downtimeFound++
		}
	}
	if downtimeFound < downtimeCount {
		t.Errorf("(b) downtime events: found %d in feed, want >= %d",
			downtimeFound, downtimeCount)
	}

	// (c) No duplicate events — each unique actor must appear exactly once.
	actorCount := make(map[string]int)
	for _, ev := range feed {
		actorCount[ev.Actor]++
	}
	for i := range preCrashCount {
		actor := fmt.Sprintf("pre-crash-agent-%d", i)
		if n := actorCount[actor]; n != 1 {
			t.Errorf("(c) duplicate pre-crash event for actor %q: appeared %d times (want 1)", actor, n)
		}
	}
	for i := range downtimeCount {
		actor := fmt.Sprintf("downtime-agent-%d", i)
		if n := actorCount[actor]; n != 1 {
			t.Errorf("(c) duplicate downtime event for actor %q: appeared %d times (want 1)", actor, n)
		}
	}
}

// readFeedFile reads all events from the curated feed file.
// Returns nil if the file does not exist. Skips malformed lines.
func readFeedFile(t *testing.T, feedPath string) []events.Event {
	t.Helper()
	f, err := os.Open(feedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open curated feed %q: %v", feedPath, err)
	}
	defer f.Close()

	var result []events.Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var ev events.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // skip malformed lines
		}
		result = append(result, ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan curated feed %q: %v", feedPath, err)
	}
	return result
}

// countEventsInFeed opens the curated feed and counts events of the given type.
// Returns 0 if the file does not exist.
func countEventsInFeed(t *testing.T, feedPath, eventType string) int {
	t.Helper()
	count := 0
	for _, ev := range readFeedFile(t, feedPath) {
		if ev.Type == eventType {
			count++
		}
	}
	return count
}

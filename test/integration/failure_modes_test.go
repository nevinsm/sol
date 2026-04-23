package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/sentinel"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// --- Test: Sentinel Reaps Agent Tethered to Closed Writ ---
//
// Exercises the failure mode documented in docs/failure-modes.md:234-239:
// when a writ is closed (cancelled, superseded) while an agent is working,
// sentinel detects the closed writ on its next patrol and reaps the agent.

func TestSentinelReapsAgentOnClosedWrit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	registerAgentRole(t)

	worldStore, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	const (
		agentName = "Toast"
		writID    = "sol-abc1234500000000"
	)

	// Create a working agent with a live session tethered to a writ.
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/"+agentName, store.AgentWorking, writID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Create writ in the world store.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'tethered', 3, 'test', ?, ?)`,
		writID, "Cancelled task", now, now,
	); err != nil {
		t.Fatalf("insert writ: %v", err)
	}

	// Write tether file so patrol discovers it via tether.List().
	if err := tether.Write("ember", agentName, writID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Session is alive.
	sessionName := "sol-ember-" + agentName
	mock.mu.Lock()
	mock.alive[sessionName] = true
	mock.mu.Unlock()

	// Close the writ externally (simulating operator `sol writ close`).
	if _, err := worldStore.CloseWrit(writID, "superseded"); err != nil {
		t.Fatalf("CloseWrit: %v", err)
	}

	// Create sentinel and run one patrol.
	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := sentinel.New(cfg, sphereStore, worldStore, mock, logger)

	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: session was stopped.
	mock.mu.Lock()
	stoppedCount := len(mock.stopped)
	var stoppedName string
	if stoppedCount > 0 {
		stoppedName = mock.stopped[0]
	}
	mock.mu.Unlock()

	if stoppedCount != 1 {
		t.Fatalf("expected 1 session stopped, got %d", stoppedCount)
	}
	if stoppedName != sessionName {
		t.Errorf("stopped session = %q, want %q", stoppedName, sessionName)
	}

	// Verify: agent record was deleted (outpost agents are reaped entirely).
	_, err := sphereStore.GetAgent("ember/" + agentName)
	if err == nil {
		t.Error("expected agent to be deleted after reap, but GetAgent succeeded")
	}

	// Verify: tether was cleared.
	if tether.IsTethered("ember", agentName, "outpost") {
		t.Error("tether file should be cleared after reap")
	}

	// Verify: reap event was emitted with close reason.
	eventsFile := filepath.Join(solHome, ".events.jsonl")
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	logContent := string(data)
	if !eventsContainField(logContent, "type", "reap") {
		t.Errorf("expected reap event in events log, got:\n%s", logContent)
	}
	if !strings.Contains(logContent, "superseded") {
		t.Errorf("expected close reason 'superseded' in events log, got:\n%s", logContent)
	}
}

// --- Test: Corrupt Workflow State Falls Through to Fresh Launch ---
//
// Exercises the failure mode documented in docs/failure-modes.md:67-69:
// when a workflow state file (.resume_state.json) is corrupted, the agent
// loses its place. Recovery: Respawn detects the corrupt file, logs a
// warning, and falls through to a fresh Launch rather than Resume.

func TestCorruptResumeStateFallsThrough(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	solHome, _ := setupTestEnv(t)
	registerAgentRole(t)

	worldStore, sphereStore := openStores(t, "ember")
	logger := events.NewLogger(solHome)
	mock := newMockSessionChecker()

	const (
		agentName = "Toast"
		writID    = "sol-abc1234500000000"
	)

	// Create a working agent tethered to a writ.
	if _, err := sphereStore.CreateAgent(agentName, "ember", "outpost"); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/"+agentName, store.AgentWorking, writID); err != nil {
		t.Fatalf("UpdateAgentState: %v", err)
	}

	// Create writ in the world store.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := worldStore.DB().Exec(
		`INSERT INTO writs (id, title, description, status, priority, created_by, created_at, updated_at)
		 VALUES (?, ?, '', 'tethered', 3, 'test', ?, ?)`,
		writID, "Workflow task", now, now,
	); err != nil {
		t.Fatalf("insert writ: %v", err)
	}

	// Write tether file.
	if err := tether.Write("ember", agentName, writID, "outpost"); err != nil {
		t.Fatalf("tether.Write: %v", err)
	}

	// Write corrupt .resume_state.json to the agent's directory.
	// This simulates workflow state corruption — the file exists but
	// contains invalid JSON, so ReadResumeState returns an error.
	agentDir := config.AgentDir("ember", agentName, "outpost")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("create agent dir: %v", err)
	}
	corruptState := []byte("{not valid json!!! current_step: broken")
	if err := os.WriteFile(filepath.Join(agentDir, ".resume_state.json"), corruptState, 0o644); err != nil {
		t.Fatalf("write corrupt resume state: %v", err)
	}

	// Create worktree directory so respawn doesn't fail on missing dir.
	worktreeDir := dispatch.WorktreePath("ember", agentName)
	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatalf("create worktree dir: %v", err)
	}

	// Session is dead (not in mock.alive) — sentinel will detect stalled agent.

	cfg := sentinel.DefaultConfig("ember", "", solHome)
	cfg.PatrolInterval = 50 * time.Millisecond
	cfg.MaxRespawns = 2

	w := sentinel.New(cfg, sphereStore, worldStore, mock, logger)

	// Run one patrol. Sentinel should detect dead session and attempt respawn.
	// Respawn reads corrupt .resume_state.json, logs warning, falls through
	// to a fresh Launch (graceful degradation per documented recovery path).
	if err := w.Patrol(context.Background()); err != nil {
		t.Fatalf("Patrol: %v", err)
	}

	// Verify: session was started (respawn fell through to Launch).
	mock.mu.Lock()
	startedCount := len(mock.started)
	mock.mu.Unlock()

	if startedCount != 1 {
		t.Fatalf("expected 1 session started (respawn via Launch), got %d", startedCount)
	}

	// Verify: agent is still in working state (not crashed or deleted).
	agent, err := sphereStore.GetAgent("ember/" + agentName)
	if err != nil {
		t.Fatalf("GetAgent after respawn: %v", err)
	}
	if agent.State != store.AgentWorking {
		t.Errorf("agent state = %q, want %q", agent.State, store.AgentWorking)
	}

	// Verify: respawn event was emitted.
	assertEventEmitted(t, solHome, events.EventRespawn)

	// Verify: corrupt resume state file was consumed/cleared by Respawn.
	// startup.Respawn clears the file regardless of success or failure,
	// preventing retry loops with the same corrupt state.
	resumePath := filepath.Join(agentDir, ".resume_state.json")
	if _, err := os.Stat(resumePath); !os.IsNotExist(err) {
		t.Errorf("expected resume state file to be cleared after respawn, but it still exists")
	}
}

// --- Test helpers ---

// eventsContainField checks if any JSON line in the events log has the given
// key set to the given value.
func eventsContainField(content, key, value string) bool {
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			if v, ok := m[key]; ok && v == value {
				return true
			}
		}
	}
	return false
}

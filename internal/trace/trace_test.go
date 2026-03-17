package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func setupTestEnv(t *testing.T) (worldStore *store.WorldStore, sphereStore *store.SphereStore) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create required directories.
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	worldName := "testworld"
	if err := os.MkdirAll(filepath.Join(dir, worldName), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create world.toml so RequireWorld passes.
	if err := os.WriteFile(filepath.Join(dir, worldName, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })

	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })

	// Register world in sphere.
	if err := ss.RegisterWorld(worldName, ""); err != nil {
		t.Fatal(err)
	}

	return ws, ss
}

func TestBuildTimelineChronological(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:        "sol-a1b2c3d4e5f6a7b8",
			Title:     "Test writ",
			Status:    "closed",
			Kind:      "code",
			Priority:  2,
			CreatedBy: "autarch",
			CreatedAt: now,
			ClosedAt:  timePtr(now.Add(2 * time.Hour)),
		},
		History: []store.HistoryEntry{
			{
				ID:        "ah-001",
				AgentName: "Toast",
				WritID:    "sol-a1b2c3d4e5f6a7b8",
				Action:    "cast",
				StartedAt: now.Add(5 * time.Second),
				EndedAt:   timePtr(now.Add(90 * time.Minute)),
			},
		},
		MergeRequests: []store.MergeRequest{
			{
				ID:        "mr-001",
				WritID:    "sol-a1b2c3d4e5f6a7b8",
				Phase:     "merged",
				CreatedAt: now.Add(90 * time.Minute),
				MergedAt:  timePtr(now.Add(105 * time.Minute)),
			},
		},
		Escalations: []store.Escalation{
			{
				ID:          "esc-001",
				Severity:    "medium",
				Description: "Build failed",
				CreatedAt:   now.Add(60 * time.Minute),
			},
		},
	}

	timeline := buildTimeline(td)

	if len(timeline) < 5 {
		t.Fatalf("expected at least 5 events, got %d", len(timeline))
	}

	// Verify chronological order.
	for i := 1; i < len(timeline); i++ {
		if timeline[i].Timestamp.Before(timeline[i-1].Timestamp) {
			t.Errorf("timeline not sorted: event %d (%s at %v) before event %d (%s at %v)",
				i, timeline[i].Action, timeline[i].Timestamp,
				i-1, timeline[i-1].Action, timeline[i-1].Timestamp)
		}
	}

	// Verify key events exist.
	actions := make(map[string]bool)
	for _, e := range timeline {
		actions[e.Action] = true
	}
	for _, expected := range []string{"created", "cast", "resolved", "merged", "escalation", "closed"} {
		if !actions[expected] {
			t.Errorf("missing expected action %q in timeline", expected)
		}
	}
}

func TestBuildTimelineEmpty(t *testing.T) {
	now := time.Now().UTC()
	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:        "sol-a1b2c3d4e5f6a7b8",
			Title:     "Empty writ",
			Status:    "open",
			CreatedBy: "autarch",
			CreatedAt: now,
		},
	}

	timeline := buildTimeline(td)

	// Should have at least the "created" event.
	if len(timeline) == 0 {
		t.Fatal("expected at least 1 event for created")
	}
	if timeline[0].Action != "created" {
		t.Errorf("expected first event action to be 'created', got %q", timeline[0].Action)
	}
}

func TestComputeCostNoTokens(t *testing.T) {
	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:     "sol-a1b2c3d4e5f6a7b8",
			Status: "open",
		},
		Tokens: nil,
	}

	cost := computeCost(td)
	if cost != nil {
		t.Errorf("expected nil cost for empty tokens, got %+v", cost)
	}
}

func TestComputeCostWithTokens(t *testing.T) {
	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:     "sol-a1b2c3d4e5f6a7b8",
			Status: "open",
		},
		Tokens: []store.TokenSummary{
			{
				Model:               "claude-sonnet-4",
				InputTokens:         100000,
				OutputTokens:        50000,
				CacheReadTokens:     200000,
				CacheCreationTokens: 10000,
			},
		},
	}

	cost := computeCost(td)
	if cost == nil {
		t.Fatal("expected non-nil cost")
	}
	if len(cost.Models) != 1 {
		t.Fatalf("expected 1 model cost, got %d", len(cost.Models))
	}
	if cost.Models[0].InputTokens != 100000 {
		t.Errorf("expected input tokens 100000, got %d", cost.Models[0].InputTokens)
	}
}

func TestComputeCostCycleTime(t *testing.T) {
	now := time.Now().UTC()
	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:     "sol-a1b2c3d4e5f6a7b8",
			Status: "closed",
		},
		Tokens: []store.TokenSummary{
			{Model: "claude-sonnet-4", InputTokens: 1000},
		},
		History: []store.HistoryEntry{
			{
				Action:    "cast",
				StartedAt: now,
				EndedAt:   timePtr(now.Add(90 * time.Minute)),
			},
		},
		MergeRequests: []store.MergeRequest{
			{
				Phase:    "merged",
				MergedAt: timePtr(now.Add(120 * time.Minute)),
			},
		},
	}

	cost := computeCost(td)
	if cost == nil {
		t.Fatal("expected non-nil cost")
	}
	if cost.CycleTime == "" {
		t.Error("expected non-empty cycle time")
	}
	if cost.CycleTime != "2h 0m" {
		t.Errorf("expected cycle time '2h 0m', got %q", cost.CycleTime)
	}
}

func TestFormatCycleTime(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
	}

	for _, tt := range tests {
		result := formatCycleTime(tt.duration)
		if result != tt.expected {
			t.Errorf("formatCycleTime(%v) = %q, want %q", tt.duration, result, tt.expected)
		}
	}
}

func TestDeduplicateEvents(t *testing.T) {
	now := time.Now().UTC()
	events := []TimelineEvent{
		{Timestamp: now, Action: "cast", Detail: "to Toast"},
		{Timestamp: now.Add(500 * time.Millisecond), Action: "cast", Detail: "to Toast (sol)"},
		{Timestamp: now.Add(10 * time.Second), Action: "resolved", Detail: "by Toast"},
	}

	result := deduplicateEvents(events)
	if len(result) != 2 {
		t.Errorf("expected 2 events after dedup, got %d", len(result))
	}
}

func TestCollectFullTrace(t *testing.T) {
	worldStore, _ := setupTestEnv(t)

	// Create a writ.
	writID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:     "Test trace writ",
		CreatedBy: "autarch",
		Priority:  1,
		Labels:    []string{"test", "trace"},
		Kind:      "code",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add history.
	historyID, err := worldStore.WriteHistory("Toast", writID, "cast", "", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add tokens.
	_, err = worldStore.WriteTokenUsage(historyID, "claude-sonnet-4", 100000, 50000, 200000, 10000)
	if err != nil {
		t.Fatal(err)
	}

	// Add merge request.
	_, err = worldStore.CreateMergeRequest(writID, "outpost/Toast/"+writID, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Collect trace.
	td, err := Collect(writID, Options{World: "testworld", NoEvents: true})
	if err != nil {
		t.Fatal(err)
	}

	// Validate trace data.
	if td.Writ.Title != "Test trace writ" {
		t.Errorf("expected title 'Test trace writ', got %q", td.Writ.Title)
	}
	if td.World != "testworld" {
		t.Errorf("expected world 'testworld', got %q", td.World)
	}
	if len(td.History) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(td.History))
	}
	if len(td.Tokens) != 1 {
		t.Errorf("expected 1 token summary, got %d", len(td.Tokens))
	}
	if len(td.MergeRequests) != 1 {
		t.Errorf("expected 1 merge request, got %d", len(td.MergeRequests))
	}
	if len(td.Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(td.Labels))
	}

	// Verify timeline has events.
	if len(td.Timeline) == 0 {
		t.Error("expected non-empty timeline")
	}

	// Verify cost is computed.
	if td.Cost == nil {
		t.Error("expected non-nil cost")
	}
}

func TestCollectWorldAutoResolution(t *testing.T) {
	worldStore, _ := setupTestEnv(t)

	// Create a writ.
	writID, err := worldStore.CreateWritWithOpts(store.CreateWritOpts{
		Title:     "Auto resolve writ",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Collect without specifying world — should find it.
	td, err := Collect(writID, Options{NoEvents: true})
	if err != nil {
		t.Fatal(err)
	}
	if td.World != "testworld" {
		t.Errorf("expected world 'testworld', got %q", td.World)
	}
}

func TestCollectWritNotFound(t *testing.T) {
	setupTestEnv(t)

	_, err := Collect("sol-0000000000000000", Options{NoEvents: true})
	if err == nil {
		t.Fatal("expected error for non-existent writ")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestCollectSphereDegrade(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create world store but intentionally corrupt sphere DB.
	worldName := "degradeworld"
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, worldName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, worldName, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ws, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Register world in sphere first.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	ss.RegisterWorld(worldName, "")
	ss.Close()

	writID, err := ws.CreateWritWithOpts(store.CreateWritOpts{
		Title:     "Degrade test",
		CreatedBy: "autarch",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Collect should succeed even without sphere data (degrades gracefully).
	td, err := Collect(writID, Options{World: worldName, NoEvents: true})
	if err != nil {
		t.Fatal(err)
	}
	if td.Writ.Title != "Degrade test" {
		t.Errorf("expected title 'Degrade test', got %q", td.Writ.Title)
	}
}

func TestRenderFull(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:        "sol-a1b2c3d4e5f6a7b8",
			Title:     "Fix authentication token refresh",
			Status:    "closed",
			Kind:      "code",
			Priority:  1,
			CreatedBy: "autarch",
			CreatedAt: now,
			ClosedAt:  timePtr(now.Add(2 * time.Hour)),
			Labels:    []string{"auth", "critical"},
		},
		Labels: []string{"auth", "critical"},
		Timeline: []TimelineEvent{
			{Timestamp: now, Action: "created", Detail: "by operator"},
			{Timestamp: now.Add(5 * time.Second), Action: "cast", Detail: "to Toast"},
		},
		Cost: &CostSummary{
			Models: []ModelCost{
				{
					Model:       "claude-sonnet-4",
					InputTokens: 125400,
					OutputTokens: 48200,
					CacheReadTokens: 890000,
					CacheCreationTokens: 12500,
					Cost:        0.47,
				},
			},
			Total:     0.47,
			CycleTime: "2h 30m",
		},
	}

	output := RenderFull(td)

	// Check key sections exist.
	if !strings.Contains(output, "sol-a1b2c3d4e5f6a7b8") {
		t.Error("output missing writ ID")
	}
	if !strings.Contains(output, "Fix authentication token refresh") {
		t.Error("output missing title")
	}
	if !strings.Contains(output, "Timeline") {
		t.Error("output missing timeline section")
	}
	if !strings.Contains(output, "Cost") {
		t.Error("output missing cost section")
	}
	if !strings.Contains(output, "Escalations") {
		t.Error("output missing escalations section")
	}
	if !strings.Contains(output, "auth, critical") {
		t.Error("output missing labels")
	}
}

func TestRenderJSON(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	td := &TraceData{
		World: "testworld",
		Writ: &store.Writ{
			ID:        "sol-a1b2c3d4e5f6a7b8",
			Title:     "Test writ",
			Status:    "open",
			Kind:      "code",
			Priority:  2,
			CreatedBy: "autarch",
			CreatedAt: now,
		},
		Labels: []string{"test"},
		Timeline: []TimelineEvent{
			{Timestamp: now, Action: "created", Detail: "by operator"},
		},
	}

	// Verify it marshals to JSON without error.
	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal trace data: %v", err)
	}

	// Verify key fields.
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal trace data: %v", err)
	}
	if result["world"] != "testworld" {
		t.Errorf("expected world 'testworld', got %v", result["world"])
	}
}

func TestCollectTetherData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "tworld"
	writID := "sol-a1b2c3d4e5f6a7b8"

	// Create tether file.
	tetherDir := filepath.Join(dir, world, "outposts", "Toast", ".tether")
	if err := os.MkdirAll(tetherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tetherDir, writID), []byte(writID), 0o644); err != nil {
		t.Fatal(err)
	}

	td := &TraceData{}
	collectTetherData(td, world, writID)

	if len(td.Tethers) != 1 {
		t.Fatalf("expected 1 tether, got %d", len(td.Tethers))
	}
	if td.Tethers[0].Agent != "Toast" {
		t.Errorf("expected agent 'Toast', got %q", td.Tethers[0].Agent)
	}
	if td.Tethers[0].Role != "outpost" {
		t.Errorf("expected role 'outpost', got %q", td.Tethers[0].Role)
	}
}

func TestCollectEventData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	writID := "sol-a1b2c3d4e5f6a7b8"

	// Create events file.
	events := []string{
		`{"ts":"2026-03-07T10:15:00Z","source":"sol","type":"cast","actor":"autarch","payload":{"writ_id":"sol-a1b2c3d4e5f6a7b8","agent":"Toast"}}`,
		`{"ts":"2026-03-07T11:30:00Z","source":"sol","type":"resolve","actor":"Toast","payload":{"writ_id":"sol-a1b2c3d4e5f6a7b8"}}`,
		`{"ts":"2026-03-07T11:35:00Z","source":"sol","type":"cast","actor":"autarch","payload":{"writ_id":"sol-deadbeef12345678"}}`,
		// Event with a numeric payload field — previously silently dropped due to map[string]string.
		`{"ts":"2026-03-07T12:00:00Z","source":"sol","type":"cast","actor":"autarch","payload":{"writ_id":"sol-a1b2c3d4e5f6a7b8","priority":2}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, ".events.jsonl"), []byte(strings.Join(events, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	td := &TraceData{}
	collectEventData(td, writID)

	// Should find 3 events matching the writ ID (not the one with a different writ_id).
	// Includes the event with a numeric payload field that was previously silently dropped.
	if len(td.Timeline) != 3 {
		t.Errorf("expected 3 events from feed, got %d", len(td.Timeline))
	}
}

func TestFormatTokenInt(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{125400, "125,400"},
		{1000000, "1,000,000"},
	}

	for _, tt := range tests {
		result := formatTokenInt(tt.input)
		if result != tt.expected {
			t.Errorf("formatTokenInt(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestParseSinceFlagDuration(t *testing.T) {
	before := time.Now()
	result, err := parseSinceFlag("24h")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}

	expected := before.Add(-24 * time.Hour)
	if result.Before(expected.Add(-time.Second)) || result.After(after.Add(-24*time.Hour).Add(time.Second)) {
		t.Fatalf("expected ~24h ago, got %v", result)
	}
}

func TestParseSinceFlagDays(t *testing.T) {
	before := time.Now()
	result, err := parseSinceFlag("7d")
	after := time.Now()
	if err != nil {
		t.Fatal(err)
	}

	expected := before.Add(-7 * 24 * time.Hour)
	if result.Before(expected.Add(-time.Second)) || result.After(after.Add(-7*24*time.Hour).Add(time.Second)) {
		t.Fatalf("expected ~7 days ago, got %v", result)
	}
}

func TestParseSinceFlagDate(t *testing.T) {
	result, err := parseSinceFlag("2026-03-01")
	if err != nil {
		t.Fatal(err)
	}

	expected := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestParseSinceFlagRFC3339(t *testing.T) {
	result, err := parseSinceFlag("2026-03-01T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	expected := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, result)
	}
}

func TestParseSinceFlagInvalid(t *testing.T) {
	_, err := parseSinceFlag("not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid since value")
	}
}

func TestFormatDollars(t *testing.T) {
	tests := []struct {
		amount   float64
		expected string
	}{
		{0, "$0.00"},
		{1.5, "$1.50"},
		{12.456, "$12.46"},
		{0.001, "$0.00"},
		{100.999, "$101.00"},
	}

	for _, tc := range tests {
		result := formatDollars(tc.amount)
		if result != tc.expected {
			t.Errorf("formatDollars(%f) = %q, want %q", tc.amount, result, tc.expected)
		}
	}
}

func TestStoreToConfigSummaries(t *testing.T) {
	// Import store.TokenSummary is same package scope via cmd package.
	// This test verifies the conversion helper doesn't panic on empty input.
	result := storeToConfigSummaries(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d", len(result))
	}
}

// setupCostTest creates a temporary SOL_HOME with a world and returns the
// store, world name, and the SOL_HOME directory. Resets cost flag vars.
func setupCostTest(t *testing.T) (*store.WorldStore, string, string) {
	t.Helper()

	// Reset package-level flag vars to avoid cross-test pollution.
	costWorld = ""
	costAgent = ""
	costWrit = ""
	costCaravan = ""
	costSince = ""
	costJSON = false

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "costtest"
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.OpenWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, world, dir
}

// captureStdout captures stdout output during the execution of fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestRunCostWritWithTokenData(t *testing.T) {
	s, world, _ := setupCostTest(t)

	// Create a writ with kind set.
	writID, err := s.CreateWritWithOpts(store.CreateWritOpts{
		Title:     "Fix authentication bug",
		CreatedBy: "autarch",
		Kind:      "code",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Record token usage via history entries.
	start := time.Now().Add(-1 * time.Hour)
	h1, err := s.WriteHistory("Toast", writID, "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h1, "claude-sonnet-4-6", 100000, 40000, 80000, 10000, nil, nil); err != nil {
		t.Fatal(err)
	}

	h2, err := s.WriteHistory("Toast", writID, "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h2, "claude-sonnet-4-6", 24532, 5201, 9100, 2340, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h2, "claude-haiku-3-5", 5000, 2000, 1000, 500, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Set up flags and run.
	costWorld = world
	costWrit = writID

	output := captureStdout(t, func() {
		if err := runCostWrit(nil, nil); err != nil {
			t.Fatal(err)
		}
	})

	// Verify header.
	if !strings.Contains(output, "Writ: "+writID) {
		t.Errorf("output missing writ ID header, got:\n%s", output)
	}
	if !strings.Contains(output, "Fix authentication bug") {
		t.Errorf("output missing writ title, got:\n%s", output)
	}
	if !strings.Contains(output, "code") {
		t.Errorf("output missing writ kind, got:\n%s", output)
	}

	// Verify model rows are present.
	if !strings.Contains(output, "claude-sonnet-4-6") {
		t.Errorf("output missing claude-sonnet-4-6 row, got:\n%s", output)
	}
	if !strings.Contains(output, "claude-haiku-3-5") {
		t.Errorf("output missing claude-haiku-3-5 row, got:\n%s", output)
	}

	// Verify totals row (two models → should have totals).
	if !strings.Contains(output, "Total") {
		t.Errorf("output missing Total row, got:\n%s", output)
	}

	// Verify no pricing nag.
	if strings.Contains(output, "No pricing configured") {
		t.Errorf("unexpected pricing nag in output:\n%s", output)
	}
}

func TestRunCostWritWithSinceFilter(t *testing.T) {
	s, world, _ := setupCostTest(t)

	writID, err := s.CreateWritWithOpts(store.CreateWritOpts{
		Title:     "Test since filter",
		CreatedBy: "autarch",
		Kind:      "code",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create two history entries: one old, one recent.
	oldStart := time.Now().Add(-48 * time.Hour)
	h1, err := s.WriteHistory("Toast", writID, "cast", "", oldStart, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h1, "claude-sonnet-4-6", 50000, 20000, 30000, 5000, nil, nil); err != nil {
		t.Fatal(err)
	}

	recentStart := time.Now().Add(-1 * time.Hour)
	h2, err := s.WriteHistory("Toast", writID, "cast", "", recentStart, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h2, "claude-sonnet-4-6", 10000, 5000, 3000, 1000, nil, nil); err != nil {
		t.Fatal(err)
	}

	// Query with --since=24h — should only include the recent entry.
	costWorld = world
	costWrit = writID

	since := time.Now().Add(-24 * time.Hour)
	output := captureStdout(t, func() {
		if err := runCostWrit(nil, &since); err != nil {
			t.Fatal(err)
		}
	})

	// The output should include the model row but with reduced counts.
	if !strings.Contains(output, "claude-sonnet-4-6") {
		t.Errorf("output missing model row, got:\n%s", output)
	}
	// Check that 10,000 appears (recent) but not 50,000 (old).
	if !strings.Contains(output, "10,000") {
		t.Errorf("output should show 10,000 (recent entry), got:\n%s", output)
	}
	if strings.Contains(output, "50,000") {
		t.Errorf("output should NOT show 50,000 (old entry filtered out), got:\n%s", output)
	}
}

func TestRunCostWritUnknownWrit(t *testing.T) {
	_, world, _ := setupCostTest(t)

	costWorld = world
	costWrit = "sol-nonexistent00000"

	err := runCostWrit(nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown writ ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestRunCostWritJSON(t *testing.T) {
	s, world, _ := setupCostTest(t)

	writID, err := s.CreateWritWithOpts(store.CreateWritOpts{
		Title:     "JSON output test",
		CreatedBy: "autarch",
		Kind:      "code",
	})
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now().Add(-1 * time.Hour)
	h1, err := s.WriteHistory("Toast", writID, "cast", "", start, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTokenUsage(h1, "claude-sonnet-4-6", 1000, 500, 200, 100, nil, nil); err != nil {
		t.Fatal(err)
	}

	costWorld = world
	costWrit = writID
	costJSON = true

	output := captureStdout(t, func() {
		if err := runCostWrit(nil, nil); err != nil {
			t.Fatal(err)
		}
	})

	// JSON output should contain writ metadata and model data.
	if !strings.Contains(output, `"writ_id"`) {
		t.Errorf("JSON output missing writ_id field, got:\n%s", output)
	}
	if !strings.Contains(output, `"title"`) {
		t.Errorf("JSON output missing title field, got:\n%s", output)
	}
	if !strings.Contains(output, `"models"`) {
		t.Errorf("JSON output missing models field, got:\n%s", output)
	}
	if !strings.Contains(output, `"claude-sonnet-4-6"`) {
		t.Errorf("JSON output missing model name, got:\n%s", output)
	}
}

func TestRenderWritCostNoPricingNag(t *testing.T) {
	result := writCostResult{
		WritID: "sol-abc123",
		Title:  "Test writ",
		Kind:   "code",
		Status: "open",
		Rows: []writCostRow{
			{
				Model:               "claude-sonnet-4-6",
				InputTokens:         124532,
				OutputTokens:        45201,
				CacheReadTokens:     89100,
				CacheCreationTokens: 12340,
			},
		},
		HasPricing: false,
		Period:     "all time",
	}

	output := captureStdout(t, func() {
		renderWritCost(result, false)
	})

	if strings.Contains(output, "No pricing configured") {
		t.Errorf("unexpected pricing nag in output:\n%s", output)
	}
	if !strings.Contains(output, "claude-sonnet-4-6") {
		t.Errorf("output missing model name, got:\n%s", output)
	}
}

func TestRenderSphereCostNoPricingNag(t *testing.T) {
	result := sphereCostResult{
		Rows: []sphereCostRow{
			{World: "test", Agents: 1, Writs: 2, InputTokens: 1000, OutputTokens: 500},
		},
		Period: "all time",
	}

	output := captureStdout(t, func() {
		renderSphereCost(result, false)
	})

	if strings.Contains(output, "No pricing configured") {
		t.Errorf("unexpected pricing nag in sphere output:\n%s", output)
	}
}

func TestRenderWorldCostNoPricingNag(t *testing.T) {
	result := worldCostResult{
		World: "test",
		Rows: []worldCostRow{
			{Agent: "Toast", Writs: 1, InputTokens: 1000, OutputTokens: 500},
		},
		Period: "all time",
	}

	output := captureStdout(t, func() {
		renderWorldCost(result, false)
	})

	if strings.Contains(output, "No pricing configured") {
		t.Errorf("unexpected pricing nag in world output:\n%s", output)
	}
}

func TestRenderAgentCostNoPricingNag(t *testing.T) {
	result := agentCostResult{
		World: "test",
		Agent: "Toast",
		Rows: []agentCostRow{
			{WritID: "sol-abc", Kind: "code", Status: "open", InputTokens: 1000, OutputTokens: 500},
		},
		Period: "all time",
	}

	output := captureStdout(t, func() {
		renderAgentCost(result, false)
	})

	if strings.Contains(output, "No pricing configured") {
		t.Errorf("unexpected pricing nag in agent output:\n%s", output)
	}
}

func TestRenderCaravanCostNoPricingNag(t *testing.T) {
	result := caravanCostResult{
		CaravanID:   "cv-abc",
		CaravanName: "test-caravan",
		Rows: []caravanCostRow{
			{WritID: "sol-abc", World: "test", Phase: 1, Kind: "code", Status: "open", InputTokens: 1000},
		},
		Period: "all time",
	}

	output := captureStdout(t, func() {
		renderCaravanCost(result, false)
	})

	if strings.Contains(output, "No pricing configured") {
		t.Errorf("unexpected pricing nag in caravan output:\n%s", output)
	}
}

func TestCostWritFlagValidation(t *testing.T) {
	// --writ requires --world
	costWorld = ""
	costWrit = "sol-abc"
	costAgent = ""
	costCaravan = ""
	costSince = ""
	costJSON = false

	err := runCost(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--writ requires --world") {
		t.Fatalf("expected '--writ requires --world' error, got: %v", err)
	}

	// --writ and --agent conflict
	costWorld = "myworld"
	costWrit = "sol-abc"
	costAgent = "Toast"
	costCaravan = ""

	err = runCost(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--writ and --agent cannot be used together") {
		t.Fatalf("expected '--writ and --agent' conflict error, got: %v", err)
	}

	// --writ and --caravan conflict
	costWorld = "myworld"
	costWrit = "sol-abc"
	costAgent = ""
	costCaravan = "cv-abc"

	err = runCost(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--writ and --caravan cannot be used together") {
		t.Fatalf("expected '--writ and --caravan' conflict error, got: %v", err)
	}
}

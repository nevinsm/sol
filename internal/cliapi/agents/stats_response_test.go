package agents

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreTokenSummary(t *testing.T) {
	cost := 1.23
	dur := int64(4500)

	st := store.TokenSummary{
		Model:               "claude-sonnet-4-20250514",
		InputTokens:         1000,
		OutputTokens:        500,
		CacheReadTokens:     200,
		CacheCreationTokens: 100,
		ReasoningTokens:     50,
		CostUSD:             &cost,
		DurationMS:          &dur,
	}

	got := FromStoreTokenSummary(st)

	if got.Model != st.Model {
		t.Errorf("Model = %q, want %q", got.Model, st.Model)
	}
	if got.InputTokens != st.InputTokens {
		t.Errorf("InputTokens = %d, want %d", got.InputTokens, st.InputTokens)
	}
	if got.OutputTokens != st.OutputTokens {
		t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, st.OutputTokens)
	}
	if got.CacheReadTokens != st.CacheReadTokens {
		t.Errorf("CacheReadTokens = %d, want %d", got.CacheReadTokens, st.CacheReadTokens)
	}
	if got.CacheCreationTokens != st.CacheCreationTokens {
		t.Errorf("CacheCreationTokens = %d, want %d", got.CacheCreationTokens, st.CacheCreationTokens)
	}
	if got.ReasoningTokens != st.ReasoningTokens {
		t.Errorf("ReasoningTokens = %d, want %d", got.ReasoningTokens, st.ReasoningTokens)
	}
	if got.CostUSD == nil || *got.CostUSD != cost {
		t.Errorf("CostUSD = %v, want %v", got.CostUSD, &cost)
	}
	if got.DurationMS == nil || *got.DurationMS != dur {
		t.Errorf("DurationMS = %v, want %v", got.DurationMS, &dur)
	}
}

func TestFromStoreTokenSummaryNilOptionals(t *testing.T) {
	st := store.TokenSummary{
		Model:       "claude-sonnet-4-20250514",
		InputTokens: 100,
	}

	got := FromStoreTokenSummary(st)

	if got.CostUSD != nil {
		t.Errorf("CostUSD = %v, want nil", got.CostUSD)
	}
	if got.DurationMS != nil {
		t.Errorf("DurationMS = %v, want nil", got.DurationMS)
	}
}

func TestFromStoreTokenSummaries(t *testing.T) {
	ts := []store.TokenSummary{
		{Model: "model-a", InputTokens: 10},
		{Model: "model-b", InputTokens: 20},
	}

	got := FromStoreTokenSummaries(ts)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Model != "model-a" {
		t.Errorf("got[0].Model = %q, want %q", got[0].Model, "model-a")
	}
	if got[1].Model != "model-b" {
		t.Errorf("got[1].Model = %q, want %q", got[1].Model, "model-b")
	}
}

func TestFromStoreTokenSummariesNil(t *testing.T) {
	got := FromStoreTokenSummaries(nil)
	if got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}

func TestTokenSummaryJSONShape(t *testing.T) {
	// Verify the cliapi TokenSummary JSON output matches the store.TokenSummary
	// shape (PascalCase keys, since store has no JSON tags).
	cost := 2.50
	st := store.TokenSummary{
		Model:               "test-model",
		InputTokens:         100,
		OutputTokens:        50,
		CacheReadTokens:     25,
		CacheCreationTokens: 10,
		ReasoningTokens:     5,
		CostUSD:             &cost,
	}

	storeJSON, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal store: %v", err)
	}

	cliJSON, err := json.Marshal(FromStoreTokenSummary(st))
	if err != nil {
		t.Fatalf("marshal cliapi: %v", err)
	}

	// Parse both to maps and compare keys.
	var storeMap, cliMap map[string]interface{}
	if err := json.Unmarshal(storeJSON, &storeMap); err != nil {
		t.Fatalf("unmarshal store: %v", err)
	}
	if err := json.Unmarshal(cliJSON, &cliMap); err != nil {
		t.Fatalf("unmarshal cliapi: %v", err)
	}

	for key := range storeMap {
		if _, ok := cliMap[key]; !ok {
			t.Errorf("cliapi JSON missing key %q present in store JSON", key)
		}
	}
	for key := range cliMap {
		if _, ok := storeMap[key]; !ok {
			t.Errorf("cliapi JSON has extra key %q not in store JSON", key)
		}
	}
}

func TestStatsReportJSONShape(t *testing.T) {
	// Verify the StatsReport JSON field names match the expected shape.
	median := 120.5
	p90 := 300.0
	rate := 80.0
	cost := 5.00

	report := StatsReport{
		Name:             "TestAgent",
		TotalCasts:       10,
		CompletedCasts:   8,
		CycleTimeMedianS: &median,
		CycleTimeP90S:    &p90,
		FirstPassRate:    &rate,
		FirstPassMRs:     6,
		MergedMRs:        8,
		FailedMRs:        1,
		ReworkCount:      2,
		Tokens: []TokenSummary{
			{Model: "model-a", InputTokens: 100, OutputTokens: 50},
		},
		TotalTokens:   150,
		EstimatedCost: &cost,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectedKeys := []string{
		"name", "total_casts", "completed_casts",
		"cycle_time_median_s", "cycle_time_p90_s",
		"first_pass_rate", "first_pass_mrs", "merged_mrs", "failed_mrs",
		"rework_count", "tokens", "total_tokens", "estimated_cost",
	}

	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing expected JSON key %q", key)
		}
	}
}

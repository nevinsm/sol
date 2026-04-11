package writs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/trace"
)

func TestFromTraceData(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	closed := now.Add(time.Hour)
	costUSD := 0.42

	td := &trace.TraceData{
		World: "sol-dev",
		Writ: &store.Writ{
			ID:          "sol-a1b2c3d4e5f6a7b8",
			Title:       "Test writ",
			Description: "Do something",
			Status:      "closed",
			Priority:    2,
			Kind:        "code",
			CreatedBy:   "autarch",
			CreatedAt:   now,
			UpdatedAt:   now,
			ClosedAt:    &closed,
			CloseReason: "completed",
			Labels:      []string{"cli"},
		},
		History: []store.HistoryEntry{
			{
				ID:        "ah-0001",
				AgentName: "Nova",
				WritID:    "sol-a1b2c3d4e5f6a7b8",
				Action:    "cast",
				StartedAt: now,
				EndedAt:   &closed,
				Summary:   "done",
			},
		},
		Tokens: []store.TokenSummary{
			{
				Model:               "claude-sonnet-4-20250514",
				InputTokens:         1000,
				OutputTokens:        500,
				CacheReadTokens:     200,
				CacheCreationTokens: 100,
				ReasoningTokens:     50,
				CostUSD:             &costUSD,
			},
		},
		MergeRequests: []store.MergeRequest{
			{
				ID:        "mr-0001",
				WritID:    "sol-a1b2c3d4e5f6a7b8",
				Branch:    "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
				Phase:     "merged",
				CreatedAt: now,
				UpdatedAt: now,
				MergedAt:  &closed,
			},
		},
		Dependencies: []string{"sol-0000000000000001"},
		Dependents:   []string{"sol-0000000000000002"},
		Labels:       []string{"cli"},
		Escalations: []store.Escalation{
			{
				ID:          "esc-0001",
				Severity:    "medium",
				Source:      "Nova",
				Description: "stuck",
				SourceRef:   "writ:sol-a1b2c3d4e5f6a7b8",
				Status:      "open",
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
		CaravanItems: []store.CaravanItem{
			{
				CaravanID: "car-0001",
				WritID:    "sol-a1b2c3d4e5f6a7b8",
				World:     "sol-dev",
				Phase:     1,
			},
		},
		Caravans: map[string]*store.Caravan{
			"car-0001": {
				ID:        "car-0001",
				Name:      "cli-api-firm-up",
				Status:    "open",
				Owner:     "autarch",
				CreatedAt: now,
			},
		},
		ActiveAgents: []store.Agent{
			{
				ID:         "sol-dev/Nova",
				Name:       "Nova",
				World:      "sol-dev",
				Role:       "outpost",
				State:      "working",
				ActiveWrit: "sol-a1b2c3d4e5f6a7b8",
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		},
		Tethers: []trace.TetherInfo{
			{Agent: "Nova", Role: "outpost"},
		},
		Timeline: []trace.TimelineEvent{
			{Timestamp: now, Action: "created", Detail: "by autarch"},
		},
		Cost: &trace.CostSummary{
			Models: []trace.ModelCost{
				{
					Model:        "claude-sonnet-4-20250514",
					InputTokens:  1000,
					OutputTokens: 500,
					Cost:         0.42,
				},
			},
			Total:     0.42,
			CycleTime: "1h 0m",
		},
		Degradations: []string{"(sphere unavailable)"},
	}

	resp := FromTraceData(td)

	// Top-level fields.
	if resp.World != "sol-dev" {
		t.Errorf("World = %q, want %q", resp.World, "sol-dev")
	}
	if resp.Writ == nil {
		t.Fatal("Writ should not be nil")
	}
	if resp.Writ.ID != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("Writ.ID = %q, want %q", resp.Writ.ID, "sol-a1b2c3d4e5f6a7b8")
	}
	if resp.Writ.CloseReason != "completed" {
		t.Errorf("Writ.CloseReason = %q, want %q", resp.Writ.CloseReason, "completed")
	}

	// History.
	if len(resp.History) != 1 {
		t.Fatalf("History len = %d, want 1", len(resp.History))
	}
	if resp.History[0].AgentName != "Nova" {
		t.Errorf("History[0].AgentName = %q, want %q", resp.History[0].AgentName, "Nova")
	}

	// Tokens.
	if len(resp.Tokens) != 1 {
		t.Fatalf("Tokens len = %d, want 1", len(resp.Tokens))
	}
	if resp.Tokens[0].InputTokens != 1000 {
		t.Errorf("Tokens[0].InputTokens = %d, want 1000", resp.Tokens[0].InputTokens)
	}
	if resp.Tokens[0].CostUSD == nil || *resp.Tokens[0].CostUSD != 0.42 {
		t.Errorf("Tokens[0].CostUSD = %v, want 0.42", resp.Tokens[0].CostUSD)
	}

	// Merge requests.
	if len(resp.MergeRequests) != 1 {
		t.Fatalf("MergeRequests len = %d, want 1", len(resp.MergeRequests))
	}
	if resp.MergeRequests[0].Phase != "merged" {
		t.Errorf("MergeRequests[0].Phase = %q, want %q", resp.MergeRequests[0].Phase, "merged")
	}

	// Dependencies / Dependents.
	if len(resp.Dependencies) != 1 || resp.Dependencies[0] != "sol-0000000000000001" {
		t.Errorf("Dependencies = %v, want [sol-0000000000000001]", resp.Dependencies)
	}
	if len(resp.Dependents) != 1 || resp.Dependents[0] != "sol-0000000000000002" {
		t.Errorf("Dependents = %v, want [sol-0000000000000002]", resp.Dependents)
	}

	// Escalations.
	if len(resp.Escalations) != 1 {
		t.Fatalf("Escalations len = %d, want 1", len(resp.Escalations))
	}
	if resp.Escalations[0].Severity != "medium" {
		t.Errorf("Escalations[0].Severity = %q, want %q", resp.Escalations[0].Severity, "medium")
	}

	// Caravan items.
	if len(resp.CaravanItems) != 1 {
		t.Fatalf("CaravanItems len = %d, want 1", len(resp.CaravanItems))
	}
	if resp.CaravanItems[0].Phase != 1 {
		t.Errorf("CaravanItems[0].Phase = %d, want 1", resp.CaravanItems[0].Phase)
	}

	// Caravans.
	if len(resp.Caravans) != 1 {
		t.Fatalf("Caravans len = %d, want 1", len(resp.Caravans))
	}
	if resp.Caravans["car-0001"].Name != "cli-api-firm-up" {
		t.Errorf("Caravans[car-0001].Name = %q, want %q", resp.Caravans["car-0001"].Name, "cli-api-firm-up")
	}

	// Active agents.
	if len(resp.ActiveAgents) != 1 {
		t.Fatalf("ActiveAgents len = %d, want 1", len(resp.ActiveAgents))
	}
	if resp.ActiveAgents[0].Name != "Nova" {
		t.Errorf("ActiveAgents[0].Name = %q, want %q", resp.ActiveAgents[0].Name, "Nova")
	}

	// Tethers.
	if len(resp.Tethers) != 1 {
		t.Fatalf("Tethers len = %d, want 1", len(resp.Tethers))
	}
	if resp.Tethers[0].Agent != "Nova" {
		t.Errorf("Tethers[0].Agent = %q, want %q", resp.Tethers[0].Agent, "Nova")
	}

	// Timeline.
	if len(resp.Timeline) != 1 {
		t.Fatalf("Timeline len = %d, want 1", len(resp.Timeline))
	}
	if resp.Timeline[0].Action != "created" {
		t.Errorf("Timeline[0].Action = %q, want %q", resp.Timeline[0].Action, "created")
	}

	// Cost.
	if resp.Cost == nil {
		t.Fatal("Cost should not be nil")
	}
	if resp.Cost.Total != 0.42 {
		t.Errorf("Cost.Total = %f, want 0.42", resp.Cost.Total)
	}
	if resp.Cost.CycleTime != "1h 0m" {
		t.Errorf("Cost.CycleTime = %q, want %q", resp.Cost.CycleTime, "1h 0m")
	}
	if len(resp.Cost.Models) != 1 {
		t.Fatalf("Cost.Models len = %d, want 1", len(resp.Cost.Models))
	}

	// Degradations.
	if len(resp.Degradations) != 1 {
		t.Fatalf("Degradations len = %d, want 1", len(resp.Degradations))
	}
}

func TestFromTraceDataEmptySlices(t *testing.T) {
	td := &trace.TraceData{
		World: "test",
		Writ: &store.Writ{
			ID:        "sol-0000000000000001",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}

	resp := FromTraceData(td)

	// Empty slices should marshal as [] not null.
	if resp.History == nil {
		t.Error("History should be empty slice, not nil")
	}
	if resp.Tokens == nil {
		t.Error("Tokens should be empty slice, not nil")
	}
	if resp.MergeRequests == nil {
		t.Error("MergeRequests should be empty slice, not nil")
	}
	if resp.Escalations == nil {
		t.Error("Escalations should be empty slice, not nil")
	}
	if resp.CaravanItems == nil {
		t.Error("CaravanItems should be empty slice, not nil")
	}
	if resp.ActiveAgents == nil {
		t.Error("ActiveAgents should be empty slice, not nil")
	}
	if resp.Tethers == nil {
		t.Error("Tethers should be empty slice, not nil")
	}
	if resp.Timeline == nil {
		t.Error("Timeline should be empty slice, not nil")
	}

	// Cost and Caravans should be nil (omitted).
	if resp.Cost != nil {
		t.Error("Cost should be nil")
	}
	if resp.Caravans != nil {
		t.Error("Caravans should be nil when empty")
	}
}

func TestTraceResponseJSONFieldNames(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	costUSD := 1.0

	td := &trace.TraceData{
		World: "test",
		Writ: &store.Writ{
			ID:        "sol-0000000000000001",
			Title:     "Test",
			Status:    "open",
			Kind:      "code",
			CreatedBy: "autarch",
			CreatedAt: now,
			UpdatedAt: now,
		},
		History: []store.HistoryEntry{
			{ID: "ah-0001", AgentName: "Nova", Action: "cast", StartedAt: now},
		},
		Tokens: []store.TokenSummary{
			{Model: "claude-sonnet-4-20250514", InputTokens: 100, CostUSD: &costUSD},
		},
		MergeRequests: []store.MergeRequest{
			{ID: "mr-0001", WritID: "sol-0000000000000001", Phase: "ready", CreatedAt: now, UpdatedAt: now},
		},
		Escalations: []store.Escalation{
			{ID: "esc-0001", Severity: "low", Status: "open", CreatedAt: now, UpdatedAt: now},
		},
		CaravanItems: []store.CaravanItem{
			{CaravanID: "car-0001", WritID: "sol-0000000000000001", World: "test", Phase: 1},
		},
		ActiveAgents: []store.Agent{
			{ID: "test/Nova", Name: "Nova", World: "test", Role: "outpost", CreatedAt: now, UpdatedAt: now},
		},
		Tethers: []trace.TetherInfo{
			{Agent: "Nova", Role: "outpost"},
		},
		Timeline: []trace.TimelineEvent{
			{Timestamp: now, Action: "created", Detail: "by autarch"},
		},
		Labels:       []string{"cli"},
		Dependencies: []string{},
		Dependents:   []string{},
	}

	resp := FromTraceData(td)
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Top-level fields should be snake_case.
	topLevelKeys := []string{"world", "writ", "history", "tokens", "merge_requests",
		"dependencies", "dependents", "labels", "escalations", "caravan_items",
		"active_agents", "tethers", "timeline"}
	for _, key := range topLevelKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}

	// Writ fields should be snake_case (TraceWrit has snake_case json tags).
	writMap, ok := m["writ"].(map[string]any)
	if !ok {
		t.Fatal("writ should be a JSON object")
	}
	writKeys := []string{"id", "title", "status", "kind", "created_by", "created_at", "updated_at"}
	for _, key := range writKeys {
		if _, ok := writMap[key]; !ok {
			t.Errorf("writ missing snake_case key %q", key)
		}
	}

	// History entries should be snake_case (TraceHistoryEntry has snake_case json tags).
	histArr, ok := m["history"].([]any)
	if !ok || len(histArr) == 0 {
		t.Fatal("history should be a non-empty array")
	}
	histMap := histArr[0].(map[string]any)
	histKeys := []string{"id", "agent_name", "action", "started_at"}
	for _, key := range histKeys {
		if _, ok := histMap[key]; !ok {
			t.Errorf("history[0] missing snake_case key %q", key)
		}
	}

	// Token entries should be snake_case (TraceTokenSummary has snake_case json tags).
	tokArr := m["tokens"].([]any)
	tokMap := tokArr[0].(map[string]any)
	tokKeys := []string{"model", "input_tokens", "output_tokens", "cost_usd"}
	for _, key := range tokKeys {
		if _, ok := tokMap[key]; !ok {
			t.Errorf("tokens[0] missing snake_case key %q", key)
		}
	}

	// MergeRequest entries should be snake_case (TraceMergeRequest has snake_case json tags).
	mrArr := m["merge_requests"].([]any)
	mrMap := mrArr[0].(map[string]any)
	mrKeys := []string{"id", "writ_id", "phase", "created_at"}
	for _, key := range mrKeys {
		if _, ok := mrMap[key]; !ok {
			t.Errorf("merge_requests[0] missing snake_case key %q", key)
		}
	}

	// Escalation entries should be snake_case (TraceEscalation has snake_case json tags).
	escArr := m["escalations"].([]any)
	escMap := escArr[0].(map[string]any)
	escKeys := []string{"id", "severity", "status", "created_at"}
	for _, key := range escKeys {
		if _, ok := escMap[key]; !ok {
			t.Errorf("escalations[0] missing snake_case key %q", key)
		}
	}

	// CaravanItem entries should be snake_case (TraceCaravanItem has snake_case json tags).
	ciArr := m["caravan_items"].([]any)
	ciMap := ciArr[0].(map[string]any)
	ciKeys := []string{"caravan_id", "writ_id", "world", "phase"}
	for _, key := range ciKeys {
		if _, ok := ciMap[key]; !ok {
			t.Errorf("caravan_items[0] missing snake_case key %q", key)
		}
	}

	// Agent entries should be snake_case (TraceAgent has snake_case json tags).
	agArr := m["active_agents"].([]any)
	agMap := agArr[0].(map[string]any)
	agKeys := []string{"id", "name", "world", "role"}
	for _, key := range agKeys {
		if _, ok := agMap[key]; !ok {
			t.Errorf("active_agents[0] missing snake_case key %q", key)
		}
	}

	// Tether entries should be snake_case.
	tArr := m["tethers"].([]any)
	tMap := tArr[0].(map[string]any)
	tKeys := []string{"agent", "role"}
	for _, key := range tKeys {
		if _, ok := tMap[key]; !ok {
			t.Errorf("tethers[0] missing snake_case key %q", key)
		}
	}

	// Timeline entries should be snake_case with "occurred_at" (normalized from "timestamp").
	tlArr := m["timeline"].([]any)
	tlMap := tlArr[0].(map[string]any)
	tlKeys := []string{"occurred_at", "action", "detail"}
	for _, key := range tlKeys {
		if _, ok := tlMap[key]; !ok {
			t.Errorf("timeline[0] missing snake_case key %q", key)
		}
	}
}

func TestFromTraceDataNilWrit(t *testing.T) {
	td := &trace.TraceData{
		World: "test",
	}

	resp := FromTraceData(td)

	if resp.Writ != nil {
		t.Error("Writ should be nil when source is nil")
	}
}

func TestFromTraceDataNilCost(t *testing.T) {
	td := &trace.TraceData{
		World: "test",
		Writ: &store.Writ{
			ID:        "sol-0000000000000001",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	}

	resp := FromTraceData(td)

	if resp.Cost != nil {
		t.Error("Cost should be nil when source is nil")
	}

	// Verify omitempty works.
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if _, ok := m["cost"]; ok {
		t.Error("cost should be omitted from JSON when nil")
	}
}

package status

import (
	"encoding/json"
	"testing"

	internstatus "github.com/nevinsm/sol/internal/status"
)

func TestFromWorldStatus(t *testing.T) {
	input := &internstatus.WorldStatus{
		World:     "sol-dev",
		MaxActive: 5,
		Prefect:   internstatus.PrefectInfo{Running: true, PID: 1234},
		Forge: internstatus.ForgeInfo{
			Running:     true,
			PID:         2345,
			Paused:      false,
			Status:      "idle",
			QueueDepth:  3,
			MergesTotal: 10,
		},
		Sentinel: internstatus.SentinelInfo{
			Running:       true,
			PID:           3456,
			PatrolCount:   20,
			AgentsChecked: 5,
		},
		Agents: []internstatus.AgentStatus{
			{Name: "Nova", State: "working", SessionAlive: true, ActiveWrit: "sol-abc123", WorkTitle: "some task"},
			{Name: "Echo", State: "idle", SessionAlive: true},
		},
		Envoys: []internstatus.EnvoyStatus{
			{Name: "Curator", State: "working", SessionAlive: true, TetheredCount: 2},
		},
		MergeQueue: internstatus.MergeQueueInfo{
			Ready: 1, Claimed: 0, Failed: 0, Merged: 5, Total: 6,
		},
		MergeRequests: []internstatus.MergeRequestInfo{
			{ID: "mr-1", WritID: "sol-abc123", Phase: "ready", Title: "some task"},
		},
		Caravans: []internstatus.CaravanInfo{
			{ID: "c-1", Name: "caravan", Status: "active", TotalItems: 5},
		},
		Tokens: internstatus.TokenInfo{
			InputTokens:  100000,
			OutputTokens: 20000,
			AgentCount:   2,
		},
		Summary: internstatus.Summary{
			Total: 2, Working: 1, Idle: 1,
		},
	}

	resp := FromWorldStatus(input)

	// Verify top-level fields.
	if resp.World != "sol-dev" {
		t.Errorf("World = %q, want %q", resp.World, "sol-dev")
	}
	if resp.MaxActive != 5 {
		t.Errorf("MaxActive = %d, want %d", resp.MaxActive, 5)
	}

	// Verify agents.
	if len(resp.Agents) != 2 {
		t.Fatalf("Agents len = %d, want 2", len(resp.Agents))
	}
	if resp.Agents[0].Name != "Nova" || resp.Agents[0].ActiveWrit != "sol-abc123" {
		t.Errorf("Agents[0] = %+v, unexpected", resp.Agents[0])
	}

	// Verify envoys.
	if len(resp.Envoys) != 1 || resp.Envoys[0].TetheredCount != 2 {
		t.Errorf("Envoys = %+v, unexpected", resp.Envoys)
	}

	// Verify merge queue.
	if resp.MergeQueue.Ready != 1 || resp.MergeQueue.Total != 6 {
		t.Errorf("MergeQueue = %+v, unexpected", resp.MergeQueue)
	}

	// Verify merge requests.
	if len(resp.MergeRequests) != 1 || resp.MergeRequests[0].ID != "mr-1" {
		t.Errorf("MergeRequests = %+v, unexpected", resp.MergeRequests)
	}

	// Verify caravans.
	if len(resp.Caravans) != 1 || resp.Caravans[0].Name != "caravan" {
		t.Errorf("Caravans = %+v, unexpected", resp.Caravans)
	}

	// Verify forge.
	if !resp.Forge.Running || resp.Forge.QueueDepth != 3 || resp.Forge.Status != "idle" {
		t.Errorf("Forge = %+v, unexpected", resp.Forge)
	}

	// Verify sentinel.
	if !resp.Sentinel.Running || resp.Sentinel.PatrolCount != 20 {
		t.Errorf("Sentinel = %+v, unexpected", resp.Sentinel)
	}

	// Verify summary.
	if resp.Summary.Total != 2 || resp.Summary.Working != 1 {
		t.Errorf("Summary = %+v, unexpected", resp.Summary)
	}
}

func TestWorldStatusResponseHealth(t *testing.T) {
	tests := []struct {
		name     string
		resp     WorldStatusResponse
		wantCode int
	}{
		{
			name:     "healthy",
			resp:     WorldStatusResponse{Prefect: PrefectInfo{Running: true}},
			wantCode: 0,
		},
		{
			name:     "degraded no prefect",
			resp:     WorldStatusResponse{},
			wantCode: 2,
		},
		{
			name: "unhealthy dead agents",
			resp: WorldStatusResponse{
				Prefect: PrefectInfo{Running: true},
				Summary: Summary{Dead: 1},
			},
			wantCode: 1,
		},
		{
			name: "unhealthy failed MRs",
			resp: WorldStatusResponse{
				Prefect:    PrefectInfo{Running: true},
				MergeQueue: MergeQueueInfo{Failed: 1},
			},
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resp.Health(); got != tt.wantCode {
				t.Errorf("Health() = %d, want %d", got, tt.wantCode)
			}
		})
	}
}

func TestFromWorldStatusMinimal(t *testing.T) {
	input := &internstatus.WorldStatus{
		World: "test",
	}

	resp := FromWorldStatus(input)

	if resp.World != "test" {
		t.Errorf("World = %q, want %q", resp.World, "test")
	}
	if resp.Agents != nil {
		t.Errorf("Agents = %v, want nil", resp.Agents)
	}
	if resp.Envoys != nil {
		t.Errorf("Envoys = %v, want nil", resp.Envoys)
	}
	if resp.MergeRequests != nil {
		t.Errorf("MergeRequests = %v, want nil", resp.MergeRequests)
	}
	if resp.Caravans != nil {
		t.Errorf("Caravans = %v, want nil", resp.Caravans)
	}
}

func TestFromWorldStatusJSONShape(t *testing.T) {
	input := &internstatus.WorldStatus{
		World:     "test",
		MaxActive: 3,
		Prefect:   internstatus.PrefectInfo{Running: true, PID: 100},
		Forge:     internstatus.ForgeInfo{Running: true, PID: 200},
		Sentinel:  internstatus.SentinelInfo{Running: true},
		MergeQueue: internstatus.MergeQueueInfo{
			Ready: 1, Total: 1,
		},
		Summary: internstatus.Summary{Total: 1, Working: 1},
	}

	internalJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal internal: %v", err)
	}

	resp := FromWorldStatus(input)
	cliJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal cliapi: %v", err)
	}

	var internalMap, cliMap map[string]any
	if err := json.Unmarshal(internalJSON, &internalMap); err != nil {
		t.Fatalf("unmarshal internal: %v", err)
	}
	if err := json.Unmarshal(cliJSON, &cliMap); err != nil {
		t.Fatalf("unmarshal cliapi: %v", err)
	}

	for key := range internalMap {
		if _, ok := cliMap[key]; !ok {
			t.Errorf("key %q present in internal JSON but missing in cliapi JSON", key)
		}
	}
	for key := range cliMap {
		if _, ok := internalMap[key]; !ok {
			t.Errorf("key %q present in cliapi JSON but missing in internal JSON", key)
		}
	}
}

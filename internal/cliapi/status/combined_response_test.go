package status

import (
	"encoding/json"
	"testing"

	internstatus "github.com/nevinsm/sol/internal/status"
)

func TestFromCombinedStatus(t *testing.T) {
	consul := internstatus.ConsulInfo{
		Running:      true,
		HeartbeatAge: "3m",
		PatrolCount:  15,
		Stale:        false,
	}

	escalations := &internstatus.EscalationSummary{
		Total:      3,
		BySeverity: map[string]int{"high": 2, "low": 1},
	}

	ws := &internstatus.WorldStatus{
		World:     "sol-dev",
		MaxActive: 5,
		Prefect:   internstatus.PrefectInfo{Running: true, PID: 1234},
		Summary:   internstatus.Summary{Total: 2, Working: 1, Idle: 1},
	}

	resp := FromCombinedStatus(consul, escalations, ws)

	// Verify consul info.
	if !resp.Consul.Running || resp.Consul.PatrolCount != 15 {
		t.Errorf("Consul = %+v, unexpected", resp.Consul)
	}

	// Verify escalations.
	if resp.Escalations == nil || resp.Escalations.Total != 3 {
		t.Errorf("Escalations = %+v, want Total=3", resp.Escalations)
	}

	// Verify embedded world status.
	if resp.WorldStatusResponse == nil {
		t.Fatal("WorldStatusResponse is nil")
	}
	if resp.World != "sol-dev" {
		t.Errorf("World = %q, want %q", resp.World, "sol-dev")
	}
	if resp.MaxActive != 5 {
		t.Errorf("MaxActive = %d, want %d", resp.MaxActive, 5)
	}
}

func TestFromCombinedStatusNilEscalations(t *testing.T) {
	consul := internstatus.ConsulInfo{Running: false}
	ws := &internstatus.WorldStatus{World: "test"}

	resp := FromCombinedStatus(consul, nil, ws)

	if resp.Escalations != nil {
		t.Errorf("Escalations = %+v, want nil", resp.Escalations)
	}
}

func TestFromCombinedStatusJSONShape(t *testing.T) {
	// Verify the combined response has the same JSON shape as the original
	// anonymous struct: consul and escalations at the top level, plus all
	// WorldStatus fields promoted via embedding.
	consul := internstatus.ConsulInfo{Running: true, Stale: false}
	ws := &internstatus.WorldStatus{
		World:   "test",
		Prefect: internstatus.PrefectInfo{Running: true},
		Summary: internstatus.Summary{Total: 1},
	}

	// Build the original anonymous struct.
	original := struct {
		Consul      internstatus.ConsulInfo         `json:"consul"`
		Escalations *internstatus.EscalationSummary `json:"escalations,omitempty"`
		*internstatus.WorldStatus
	}{
		Consul:      consul,
		WorldStatus: ws,
	}

	// Build the cliapi response.
	resp := FromCombinedStatus(consul, nil, ws)

	origJSON, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal cliapi: %v", err)
	}

	// Compare the JSON key sets.
	var origMap, respMap map[string]any
	if err := json.Unmarshal(origJSON, &origMap); err != nil {
		t.Fatalf("unmarshal original: %v", err)
	}
	if err := json.Unmarshal(respJSON, &respMap); err != nil {
		t.Fatalf("unmarshal cliapi: %v", err)
	}

	for key := range origMap {
		if _, ok := respMap[key]; !ok {
			t.Errorf("key %q present in original JSON but missing in cliapi JSON", key)
		}
	}
	for key := range respMap {
		if _, ok := origMap[key]; !ok {
			t.Errorf("key %q present in cliapi JSON but missing in original JSON", key)
		}
	}
}

func TestCombinedStatusHealth(t *testing.T) {
	// Health() on CombinedStatusResponse should delegate to the embedded
	// WorldStatusResponse.Health().
	consul := internstatus.ConsulInfo{Running: true}
	ws := &internstatus.WorldStatus{
		World:   "test",
		Prefect: internstatus.PrefectInfo{Running: true},
		Summary: internstatus.Summary{Dead: 1},
	}

	resp := FromCombinedStatus(consul, nil, ws)

	if code := resp.Health(); code != 1 {
		t.Errorf("Health() = %d, want 1 (unhealthy due to dead agents)", code)
	}
}

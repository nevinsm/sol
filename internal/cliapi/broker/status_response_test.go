package broker

import (
	"encoding/json"
	"testing"
	"time"

	ibroker "github.com/nevinsm/sol/internal/broker"
)

func TestStatusResponse_MinimalFields(t *testing.T) {
	resp := StatusResponse{
		Status:         "running",
		Timestamp:      "2025-01-15T10:30:00Z",
		PatrolCount:    5,
		Stale:          false,
		ProviderHealth: "healthy",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["status"] != "running" {
		t.Errorf("status = %v, want running", got["status"])
	}
	if got["timestamp"] != "2025-01-15T10:30:00Z" {
		t.Errorf("timestamp = %v, want 2025-01-15T10:30:00Z", got["timestamp"])
	}
	if got["patrol_count"] != float64(5) {
		t.Errorf("patrol_count = %v, want 5", got["patrol_count"])
	}
	if got["stale"] != false {
		t.Errorf("stale = %v, want false", got["stale"])
	}
	if got["provider_health"] != "healthy" {
		t.Errorf("provider_health = %v, want healthy", got["provider_health"])
	}
	if got["consecutive_failures"] != float64(0) {
		t.Errorf("consecutive_failures = %v, want 0", got["consecutive_failures"])
	}

	// Optional fields should be omitted when not set.
	if _, ok := got["last_probe"]; ok {
		t.Error("last_probe should be omitted when nil")
	}
	if _, ok := got["last_healthy"]; ok {
		t.Error("last_healthy should be omitted when nil")
	}
	if _, ok := got["providers"]; ok {
		t.Error("providers should be omitted when nil")
	}
}

func TestStatusResponse_AllFields(t *testing.T) {
	lastProbe := "2025-01-15T10:29:00Z"
	lastHealthy := "2025-01-15T10:28:00Z"
	resp := StatusResponse{
		Status:              "running",
		Timestamp:           "2025-01-15T10:30:00Z",
		PatrolCount:         42,
		Stale:               false,
		ProviderHealth:      "degraded",
		ConsecutiveFailures: 3,
		LastProbe:           &lastProbe,
		LastHealthy:         &lastHealthy,
		Providers: []ProviderEntry{
			{
				Provider:            "claude",
				Health:              "healthy",
				ConsecutiveFailures: 0,
			},
			{
				Provider:            "codex",
				Health:              "degraded",
				ConsecutiveFailures: 3,
				LastProbe:           "2025-01-15T10:29:00Z",
				LastHealthy:         "2025-01-15T10:25:00Z",
			},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["provider_health"] != "degraded" {
		t.Errorf("provider_health = %v, want degraded", got["provider_health"])
	}
	if got["consecutive_failures"] != float64(3) {
		t.Errorf("consecutive_failures = %v, want 3", got["consecutive_failures"])
	}
	if got["last_probe"] != "2025-01-15T10:29:00Z" {
		t.Errorf("last_probe = %v, want 2025-01-15T10:29:00Z", got["last_probe"])
	}
	if got["last_healthy"] != "2025-01-15T10:28:00Z" {
		t.Errorf("last_healthy = %v, want 2025-01-15T10:28:00Z", got["last_healthy"])
	}

	providers, ok := got["providers"].([]any)
	if !ok || len(providers) != 2 {
		t.Fatalf("providers = %v, want 2 entries", got["providers"])
	}
}

func TestFromHeartbeat_HealthyDefault(t *testing.T) {
	hb := &ibroker.Heartbeat{
		Status:      "running",
		Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 5,
	}

	resp := FromHeartbeat(hb, 10*time.Minute)

	if resp.ProviderHealth != "healthy" {
		t.Errorf("ProviderHealth = %q, want %q", resp.ProviderHealth, "healthy")
	}
	if resp.Status != "running" {
		t.Errorf("Status = %q, want %q", resp.Status, "running")
	}
	if resp.Timestamp != "2025-01-15T10:30:00Z" {
		t.Errorf("Timestamp = %q, want %q", resp.Timestamp, "2025-01-15T10:30:00Z")
	}
	if resp.PatrolCount != 5 {
		t.Errorf("PatrolCount = %d, want 5", resp.PatrolCount)
	}
	if resp.LastProbe != nil {
		t.Error("LastProbe should be nil when zero")
	}
	if resp.LastHealthy != nil {
		t.Error("LastHealthy should be nil when zero")
	}
	if resp.Providers != nil {
		t.Error("Providers should be nil when empty")
	}
}

func TestFromHeartbeat_WithProviderHealth(t *testing.T) {
	hb := &ibroker.Heartbeat{
		Status:              "running",
		Timestamp:           time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount:         10,
		ProviderHealth:      ibroker.HealthDegraded,
		ConsecutiveFailures: 2,
		LastProbe:           time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC),
		LastHealthy:         time.Date(2025, 1, 15, 10, 25, 0, 0, time.UTC),
	}

	resp := FromHeartbeat(hb, 10*time.Minute)

	if resp.ProviderHealth != "degraded" {
		t.Errorf("ProviderHealth = %q, want %q", resp.ProviderHealth, "degraded")
	}
	if resp.ConsecutiveFailures != 2 {
		t.Errorf("ConsecutiveFailures = %d, want 2", resp.ConsecutiveFailures)
	}
	if resp.LastProbe == nil || *resp.LastProbe != "2025-01-15T10:29:00Z" {
		t.Errorf("LastProbe = %v, want 2025-01-15T10:29:00Z", resp.LastProbe)
	}
	if resp.LastHealthy == nil || *resp.LastHealthy != "2025-01-15T10:25:00Z" {
		t.Errorf("LastHealthy = %v, want 2025-01-15T10:25:00Z", resp.LastHealthy)
	}
}

func TestFromHeartbeat_WithProviders(t *testing.T) {
	hb := &ibroker.Heartbeat{
		Status:      "running",
		Timestamp:   time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount: 20,
		Providers: []ibroker.ProviderHealthEntry{
			{
				Provider:            "claude",
				Health:              ibroker.HealthHealthy,
				ConsecutiveFailures: 0,
			},
			{
				Provider:            "codex",
				Health:              ibroker.HealthDown,
				ConsecutiveFailures: 5,
				LastProbe:           time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC),
				LastHealthy:         time.Date(2025, 1, 15, 10, 20, 0, 0, time.UTC),
			},
		},
	}

	resp := FromHeartbeat(hb, 10*time.Minute)

	if len(resp.Providers) != 2 {
		t.Fatalf("len(Providers) = %d, want 2", len(resp.Providers))
	}

	p0 := resp.Providers[0]
	if p0.Provider != "claude" {
		t.Errorf("Providers[0].Provider = %q, want %q", p0.Provider, "claude")
	}
	if p0.Health != "healthy" {
		t.Errorf("Providers[0].Health = %q, want %q", p0.Health, "healthy")
	}
	if p0.LastProbe != "" {
		t.Errorf("Providers[0].LastProbe = %q, want empty", p0.LastProbe)
	}

	p1 := resp.Providers[1]
	if p1.Provider != "codex" {
		t.Errorf("Providers[1].Provider = %q, want %q", p1.Provider, "codex")
	}
	if p1.Health != "down" {
		t.Errorf("Providers[1].Health = %q, want %q", p1.Health, "down")
	}
	if p1.ConsecutiveFailures != 5 {
		t.Errorf("Providers[1].ConsecutiveFailures = %d, want 5", p1.ConsecutiveFailures)
	}
	if p1.LastProbe != "2025-01-15T10:29:00Z" {
		t.Errorf("Providers[1].LastProbe = %q, want %q", p1.LastProbe, "2025-01-15T10:29:00Z")
	}
	if p1.LastHealthy != "2025-01-15T10:20:00Z" {
		t.Errorf("Providers[1].LastHealthy = %q, want %q", p1.LastHealthy, "2025-01-15T10:20:00Z")
	}
}

func TestFromHeartbeat_JSONShape(t *testing.T) {
	// Verify that FromHeartbeat + json.Marshal produces the same shape
	// as the old map[string]any approach.
	hb := &ibroker.Heartbeat{
		Status:              "running",
		Timestamp:           time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
		PatrolCount:         7,
		ProviderHealth:      ibroker.HealthDegraded,
		ConsecutiveFailures: 1,
		LastProbe:           time.Date(2025, 1, 15, 10, 29, 0, 0, time.UTC),
	}

	resp := FromHeartbeat(hb, 10*time.Minute)
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Required fields always present.
	for _, key := range []string{"status", "timestamp", "patrol_count", "stale", "provider_health", "consecutive_failures"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing required field %q", key)
		}
	}

	// last_probe present (set), last_healthy absent (zero), providers absent (empty).
	if _, ok := got["last_probe"]; !ok {
		t.Error("last_probe should be present when set")
	}
	if _, ok := got["last_healthy"]; ok {
		t.Error("last_healthy should be omitted when zero")
	}
	if _, ok := got["providers"]; ok {
		t.Error("providers should be omitted when empty")
	}
}

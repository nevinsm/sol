package agents

import (
	"encoding/json"
	"testing"
)

func TestEnvoyStatusJSON(t *testing.T) {
	s := EnvoyStatus{
		World:       "prod",
		Name:        "Alpha",
		Running:     true,
		SessionName: "sol-prod-Alpha",
		State:       "working",
		ActiveWrit:  "sol-a1b2c3d4e5f6a7b8",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if m["world"] != "prod" {
		t.Errorf("world = %v, want %q", m["world"], "prod")
	}
	if m["name"] != "Alpha" {
		t.Errorf("name = %v, want %q", m["name"], "Alpha")
	}
	if m["running"] != true {
		t.Errorf("running = %v, want true", m["running"])
	}
	if m["session_name"] != "sol-prod-Alpha" {
		t.Errorf("session_name = %v, want %q", m["session_name"], "sol-prod-Alpha")
	}
	if m["state"] != "working" {
		t.Errorf("state = %v, want %q", m["state"], "working")
	}
	if m["active_writ"] != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("active_writ = %v, want %q", m["active_writ"], "sol-a1b2c3d4e5f6a7b8")
	}
}

func TestEnvoyStatusJSONOmitsEmpty(t *testing.T) {
	s := EnvoyStatus{
		World:       "dev",
		Name:        "Beta",
		Running:     false,
		SessionName: "sol-dev-Beta",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, ok := m["state"]; ok {
		t.Error("state should be omitted when empty")
	}
	if _, ok := m["active_writ"]; ok {
		t.Error("active_writ should be omitted when empty")
	}
}

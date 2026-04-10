package service

import (
	"encoding/json"
	"testing"
)

func TestServiceStatusJSON(t *testing.T) {
	s := ServiceStatus{
		Name:      "prefect",
		Installed: true,
		Active:    true,
		Enabled:   true,
		Manager:   "launchd",
		UnitPath:  "/Library/LaunchDaemons/com.sol.prefect.plist",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["name"] != "prefect" {
		t.Errorf("name = %v, want %q", m["name"], "prefect")
	}
	if m["installed"] != true {
		t.Errorf("installed = %v, want true", m["installed"])
	}
	if m["active"] != true {
		t.Errorf("active = %v, want true", m["active"])
	}
	if m["manager"] != "launchd" {
		t.Errorf("manager = %v, want %q", m["manager"], "launchd")
	}
}

func TestServiceStatusNotInstalled(t *testing.T) {
	s := ServiceStatus{
		Name:    "consul",
		Manager: "launchd",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["installed"] != false {
		t.Errorf("installed = %v, want false", m["installed"])
	}
	// unit_path should be omitted when empty.
	if _, exists := m["unit_path"]; exists {
		t.Error("unit_path should be omitted when empty")
	}
}

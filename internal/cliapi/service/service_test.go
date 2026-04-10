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

func TestUninstallResultJSON(t *testing.T) {
	r := UninstallResult{
		Name:        "prefect",
		Uninstalled: true,
	}

	data, err := json.Marshal(r)
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
	if m["uninstalled"] != true {
		t.Errorf("uninstalled = %v, want true", m["uninstalled"])
	}
}

func TestUninstallResultSliceJSON(t *testing.T) {
	results := []UninstallResult{
		{Name: "prefect", Uninstalled: true},
		{Name: "consul", Uninstalled: true},
	}

	data, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(arr) != 2 {
		t.Fatalf("len = %d, want 2", len(arr))
	}
	if arr[0]["name"] != "prefect" {
		t.Errorf("arr[0].name = %v, want %q", arr[0]["name"], "prefect")
	}
	if arr[1]["name"] != "consul" {
		t.Errorf("arr[1].name = %v, want %q", arr[1]["name"], "consul")
	}
}

func TestServiceStatusSliceJSON(t *testing.T) {
	statuses := []ServiceStatus{
		{
			Name:      "prefect",
			Installed: true,
			Active:    true,
			Enabled:   true,
			Manager:   "systemd",
			UnitPath:  "/home/user/.config/systemd/user/sol-prefect.service",
		},
		{
			Name:      "consul",
			Installed: true,
			Active:    false,
			Enabled:   true,
			Manager:   "systemd",
			UnitPath:  "/home/user/.config/systemd/user/sol-consul.service",
		},
	}

	data, err := json.Marshal(statuses)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var arr []map[string]any
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if len(arr) != 2 {
		t.Fatalf("len = %d, want 2", len(arr))
	}
	if arr[0]["active"] != true {
		t.Errorf("arr[0].active = %v, want true", arr[0]["active"])
	}
	if arr[1]["active"] != false {
		t.Errorf("arr[1].active = %v, want false", arr[1]["active"])
	}
}

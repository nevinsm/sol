package workflows

import (
	"encoding/json"
	"testing"
)

func TestInitResponseJSON(t *testing.T) {
	resp := InitResponse{
		Name:  "deploy",
		Scope: "project",
		Path:  "/repo/.sol/workflows/deploy",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["name"] != "deploy" {
		t.Errorf("name = %v, want %q", m["name"], "deploy")
	}
	if m["scope"] != "project" {
		t.Errorf("scope = %v, want %q", m["scope"], "project")
	}
	if m["path"] != "/repo/.sol/workflows/deploy" {
		t.Errorf("path = %v, want %q", m["path"], "/repo/.sol/workflows/deploy")
	}
}

package workflows

import (
	"encoding/json"
	"testing"

	"github.com/nevinsm/sol/internal/workflow"
)

func TestShowResponseJSON(t *testing.T) {
	resp := ShowResponse{
		Name:        "deploy",
		Type:        "workflow",
		Description: "Deploy to production",
		Manifest:    true,
		Tier:        workflow.TierProject,
		Path:        "/repo/.sol/workflows/deploy",
		Valid:       true,
		Variables: map[string]ShowVariable{
			"env": {Required: true},
			"tag": {Default: "latest"},
		},
		Steps: []ShowStep{
			{ID: "build", Title: "Build", Instructions: "run make", Needs: nil},
			{ID: "test", Title: "Test", Instructions: "run tests", Needs: []string{"build"}},
		},
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
	if m["type"] != "workflow" {
		t.Errorf("type = %v, want %q", m["type"], "workflow")
	}
	if m["description"] != "Deploy to production" {
		t.Errorf("description = %v, want %q", m["description"], "Deploy to production")
	}
	if m["manifest"] != true {
		t.Errorf("manifest = %v, want true", m["manifest"])
	}
	if m["tier"] != "project" {
		t.Errorf("tier = %v, want %q", m["tier"], "project")
	}
	if m["valid"] != true {
		t.Errorf("valid = %v, want true", m["valid"])
	}

	vars, ok := m["variables"].(map[string]any)
	if !ok {
		t.Fatal("variables not a map")
	}
	if len(vars) != 2 {
		t.Errorf("variables len = %d, want 2", len(vars))
	}

	steps, ok := m["steps"].([]any)
	if !ok {
		t.Fatal("steps not an array")
	}
	if len(steps) != 2 {
		t.Errorf("steps len = %d, want 2", len(steps))
	}
}

func TestShowResponseOmitsEmpty(t *testing.T) {
	resp := ShowResponse{
		Name:  "simple",
		Type:  "workflow",
		Tier:  "user",
		Path:  "/home/user/.sol/workflows/simple",
		Valid: true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := m["description"]; ok {
		t.Error("description should be omitted when empty")
	}
	if _, ok := m["error"]; ok {
		t.Error("error should be omitted when empty")
	}
	if _, ok := m["variables"]; ok {
		t.Error("variables should be omitted when nil")
	}
	if _, ok := m["steps"]; ok {
		t.Error("steps should be omitted when nil")
	}
}

func TestShowVariableJSON(t *testing.T) {
	v := ShowVariable{Required: true, Default: "foo"}

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["required"] != true {
		t.Errorf("required = %v, want true", m["required"])
	}
	if m["default"] != "foo" {
		t.Errorf("default = %v, want %q", m["default"], "foo")
	}
}

func TestShowStepJSON(t *testing.T) {
	s := ShowStep{
		ID:           "build",
		Title:        "Build",
		Instructions: "run make",
		Needs:        []string{"setup"},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if m["id"] != "build" {
		t.Errorf("id = %v, want %q", m["id"], "build")
	}
	if m["title"] != "Build" {
		t.Errorf("title = %v, want %q", m["title"], "Build")
	}
	needs, ok := m["needs"].([]any)
	if !ok {
		t.Fatal("needs not an array")
	}
	if len(needs) != 1 || needs[0] != "setup" {
		t.Errorf("needs = %v, want [setup]", needs)
	}
}

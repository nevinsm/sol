package workflows

import (
	"encoding/json"
	"testing"
)

func TestWorkflowJSON(t *testing.T) {
	w := Workflow{
		Name:   "deploy",
		Scope:  "world",
		Path:   ".workflow/deploy.toml",
		Source: "project",
	}

	data, err := json.Marshal(w)
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
	if m["scope"] != "world" {
		t.Errorf("scope = %v, want %q", m["scope"], "world")
	}
	if m["path"] != ".workflow/deploy.toml" {
		t.Errorf("path = %v, want %q", m["path"], ".workflow/deploy.toml")
	}
	if m["source"] != "project" {
		t.Errorf("source = %v, want %q", m["source"], "project")
	}
}

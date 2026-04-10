package caravans

import (
	"encoding/json"
	"testing"
)

func TestDepListResponseJSON(t *testing.T) {
	resp := DepListResponse{
		ID:   "car-0000000000000001",
		Name: "test-caravan",
		DependsOn: []DepInfo{
			{ID: "car-0000000000000002", Name: "upstream", Status: "open"},
		},
		DependedBy: []DepInfo{
			{ID: "car-0000000000000003", Name: "downstream", Status: "drydock"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded["id"] != "car-0000000000000001" {
		t.Errorf("id = %v, want car-0000000000000001", decoded["id"])
	}
	if decoded["name"] != "test-caravan" {
		t.Errorf("name = %v, want test-caravan", decoded["name"])
	}

	dependsOn, ok := decoded["depends_on"].([]any)
	if !ok || len(dependsOn) != 1 {
		t.Fatalf("depends_on length = %d, want 1", len(dependsOn))
	}
	dep := dependsOn[0].(map[string]any)
	if dep["id"] != "car-0000000000000002" {
		t.Errorf("depends_on[0].id = %v, want car-0000000000000002", dep["id"])
	}

	dependedBy, ok := decoded["depended_by"].([]any)
	if !ok || len(dependedBy) != 1 {
		t.Fatalf("depended_by length = %d, want 1", len(dependedBy))
	}
	by := dependedBy[0].(map[string]any)
	if by["id"] != "car-0000000000000003" {
		t.Errorf("depended_by[0].id = %v, want car-0000000000000003", by["id"])
	}
}

func TestDepListResponseEmptyArrays(t *testing.T) {
	resp := DepListResponse{
		ID:         "car-0000000000000001",
		Name:       "lonely",
		DependsOn:  []DepInfo{},
		DependedBy: []DepInfo{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Empty arrays should be present, not omitted.
	dependsOn, ok := decoded["depends_on"].([]any)
	if !ok {
		t.Fatal("depends_on should be present as empty array")
	}
	if len(dependsOn) != 0 {
		t.Errorf("depends_on length = %d, want 0", len(dependsOn))
	}

	dependedBy, ok := decoded["depended_by"].([]any)
	if !ok {
		t.Fatal("depended_by should be present as empty array")
	}
	if len(dependedBy) != 0 {
		t.Errorf("depended_by length = %d, want 0", len(dependedBy))
	}
}

func TestDepInfoJSON(t *testing.T) {
	info := DepInfo{
		ID:     "car-0000000000000001",
		Name:   "test",
		Status: "open",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded["id"] != "car-0000000000000001" {
		t.Errorf("id = %v, want car-0000000000000001", decoded["id"])
	}
	if decoded["name"] != "test" {
		t.Errorf("name = %v, want test", decoded["name"])
	}
	if decoded["status"] != "open" {
		t.Errorf("status = %v, want open", decoded["status"])
	}
}

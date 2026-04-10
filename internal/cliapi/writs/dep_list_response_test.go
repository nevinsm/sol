package writs

import (
	"encoding/json"
	"testing"
)

func TestNewDepListResponse(t *testing.T) {
	resp := NewDepListResponse(
		"sol-a1b2c3d4e5f6a7b8",
		[]string{"sol-1111111111111111"},
		[]string{"sol-2222222222222222", "sol-3333333333333333"},
	)

	if resp.WritID != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("WritID = %q, want %q", resp.WritID, "sol-a1b2c3d4e5f6a7b8")
	}
	if len(resp.DependsOn) != 1 {
		t.Errorf("DependsOn len = %d, want 1", len(resp.DependsOn))
	}
	if len(resp.DependedBy) != 2 {
		t.Errorf("DependedBy len = %d, want 2", len(resp.DependedBy))
	}
}

func TestNewDepListResponseNilSlices(t *testing.T) {
	resp := NewDepListResponse("sol-0000000000000001", nil, nil)

	if resp.DependsOn == nil {
		t.Fatal("DependsOn should be empty slice, not nil")
	}
	if resp.DependedBy == nil {
		t.Fatal("DependedBy should be empty slice, not nil")
	}

	// Verify JSON output includes empty arrays, not null.
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	dependsOn, ok := m["depends_on"].([]any)
	if !ok {
		t.Fatal("depends_on should be a JSON array")
	}
	if len(dependsOn) != 0 {
		t.Errorf("depends_on len = %d, want 0", len(dependsOn))
	}
	dependedBy, ok := m["depended_by"].([]any)
	if !ok {
		t.Fatal("depended_by should be a JSON array")
	}
	if len(dependedBy) != 0 {
		t.Errorf("depended_by len = %d, want 0", len(dependedBy))
	}
}

func TestDepListResponseJSONFieldNames(t *testing.T) {
	resp := NewDepListResponse(
		"sol-a1b2c3d4e5f6a7b8",
		[]string{"sol-1111111111111111"},
		[]string{"sol-2222222222222222"},
	)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify snake_case field names.
	for _, key := range []string{"writ_id", "depends_on", "depended_by"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q", key)
		}
	}
	if len(m) != 3 {
		t.Errorf("expected 3 JSON keys, got %d", len(m))
	}
}

package worlds

import (
	"encoding/json"
	"testing"
)

func TestDeleteResponseJSON(t *testing.T) {
	resp := DeleteResponse{
		Name:    "sol-dev",
		Deleted: true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["name"] != "sol-dev" {
		t.Errorf("name = %v, want %q", got["name"], "sol-dev")
	}
	if got["deleted"] != true {
		t.Errorf("deleted = %v, want true", got["deleted"])
	}

	// Should have exactly 2 keys.
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2: %v", len(got), got)
	}
}

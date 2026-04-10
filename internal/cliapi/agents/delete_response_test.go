package agents

import (
	"encoding/json"
	"testing"
)

func TestDeleteResponseJSON(t *testing.T) {
	r := DeleteResponse{
		Name:    "Alpha",
		World:   "prod",
		Deleted: true,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if m["name"] != "Alpha" {
		t.Errorf("name = %v, want %q", m["name"], "Alpha")
	}
	if m["world"] != "prod" {
		t.Errorf("world = %v, want %q", m["world"], "prod")
	}
	if m["deleted"] != true {
		t.Errorf("deleted = %v, want true", m["deleted"])
	}
}

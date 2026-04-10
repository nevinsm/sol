package agents

import (
	"encoding/json"
	"testing"
)

func TestSyncResponseJSON(t *testing.T) {
	r := SyncResponse{
		Name:   "Alpha",
		World:  "prod",
		Synced: true,
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
	if m["synced"] != true {
		t.Errorf("synced = %v, want true", m["synced"])
	}
}

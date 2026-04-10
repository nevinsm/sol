package worlds

import (
	"encoding/json"
	"testing"
)

func TestSyncResponseJSON(t *testing.T) {
	resp := SyncResponse{
		Name:       "sol-dev",
		Fetched:    true,
		HeadCommit: "abc1234",
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
	if got["fetched"] != true {
		t.Errorf("fetched = %v, want true", got["fetched"])
	}
	if got["head_commit"] != "abc1234" {
		t.Errorf("head_commit = %v, want %q", got["head_commit"], "abc1234")
	}

	if len(got) != 3 {
		t.Errorf("got %d keys, want 3: %v", len(got), got)
	}
}

func TestSyncResponseNoFetch(t *testing.T) {
	resp := SyncResponse{
		Name:       "sol-dev",
		Fetched:    false,
		HeadCommit: "abc1234",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["fetched"] != false {
		t.Errorf("fetched = %v, want false", got["fetched"])
	}
}

package forge

import (
	"encoding/json"
	"testing"
)

func TestForgeSyncResponse_Fetched(t *testing.T) {
	resp := ForgeSyncResponse{
		World:      "sol-dev",
		Fetched:    true,
		HeadCommit: "abc1234",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["world"] != "sol-dev" {
		t.Errorf("world = %v, want sol-dev", got["world"])
	}
	if got["fetched"] != true {
		t.Errorf("fetched = %v, want true", got["fetched"])
	}
	if got["head_commit"] != "abc1234" {
		t.Errorf("head_commit = %v, want abc1234", got["head_commit"])
	}
}

func TestForgeSyncResponse_NotFetched(t *testing.T) {
	resp := ForgeSyncResponse{
		World:      "sol-dev",
		Fetched:    false,
		HeadCommit: "abc1234",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["fetched"] != false {
		t.Errorf("fetched = %v, want false", got["fetched"])
	}
}

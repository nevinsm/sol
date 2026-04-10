package caravans

import (
	"encoding/json"
	"testing"
)

func TestDeleteResponseJSONShape(t *testing.T) {
	resp := DeleteResponse{
		ID:      "car-0000000000000001",
		Deleted: true,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if _, ok := raw["id"]; !ok {
		t.Error("missing key \"id\"")
	}
	if _, ok := raw["deleted"]; !ok {
		t.Error("missing key \"deleted\"")
	}

	// Round-trip check.
	var decoded DeleteResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("round-trip Unmarshal failed: %v", err)
	}
	if decoded.ID != resp.ID || decoded.Deleted != resp.Deleted {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, resp)
	}
}

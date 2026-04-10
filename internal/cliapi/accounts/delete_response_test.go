package accounts

import (
	"encoding/json"
	"testing"
)

func TestDeleteResponseJSON(t *testing.T) {
	resp := DeleteResponse{
		Handle:  "old-account",
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

	if got["handle"] != "old-account" {
		t.Errorf("handle = %v, want %q", got["handle"], "old-account")
	}
	if got["deleted"] != true {
		t.Errorf("deleted = %v, want true", got["deleted"])
	}
}

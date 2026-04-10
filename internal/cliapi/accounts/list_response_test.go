package accounts

import (
	"encoding/json"
	"testing"
)

func TestListEntryJSON(t *testing.T) {
	entry := ListEntry{
		Handle:      "primary",
		Email:       "test@example.com",
		Description: "Primary account",
		Default:     true,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["handle"] != "primary" {
		t.Errorf("handle = %v, want %q", got["handle"], "primary")
	}
	if got["email"] != "test@example.com" {
		t.Errorf("email = %v, want %q", got["email"], "test@example.com")
	}
	if got["description"] != "Primary account" {
		t.Errorf("description = %v, want %q", got["description"], "Primary account")
	}
	if got["default"] != true {
		t.Errorf("default = %v, want true", got["default"])
	}
}

func TestListEntryOmitsEmpty(t *testing.T) {
	entry := ListEntry{
		Handle:  "minimal",
		Default: false,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, ok := got["email"]; ok {
		t.Error("email should be omitted when empty")
	}
	if _, ok := got["description"]; ok {
		t.Error("description should be omitted when empty")
	}
}

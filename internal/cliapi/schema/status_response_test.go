package schema

import (
	"encoding/json"
	"testing"
)

func TestStatusEntryJSON(t *testing.T) {
	entry := StatusEntry{
		Database: "sphere",
		Type:     "sphere",
		Version:  3,
		Target:   5,
		Status:   "needs migration to v5",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify snake_case keys.
	for _, key := range []string{"database", "type", "version", "target", "status"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}

	// Error field omitted when empty.
	if _, ok := got["error"]; ok {
		t.Error("error field should be omitted when empty")
	}

	if got["database"] != "sphere" {
		t.Errorf("database = %v, want %q", got["database"], "sphere")
	}
	if got["version"] != float64(3) {
		t.Errorf("version = %v, want 3", got["version"])
	}
	if got["target"] != float64(5) {
		t.Errorf("target = %v, want 5", got["target"])
	}
}

func TestStatusEntryJSON_WithError(t *testing.T) {
	entry := StatusEntry{
		Database: "myworld",
		Type:     "world",
		Error:    "database not found",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["error"] != "database not found" {
		t.Errorf("error = %v, want %q", got["error"], "database not found")
	}
}

func TestStatusEntryRoundTrip(t *testing.T) {
	entry := StatusEntry{
		Database: "dev",
		Type:     "world",
		Version:  2,
		Target:   4,
		Status:   "needs migration to v4",
		Error:    "something went wrong",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got StatusEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got != entry {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", got, entry)
	}
}

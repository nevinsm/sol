package writs

import (
	"encoding/json"
	"testing"
)

func TestWritCleanResultJSON(t *testing.T) {
	r := WritCleanResult{
		WritsCleaned:  5,
		DirsRemoved:   5,
		BytesFreed:    1048576,
		RetentionDays: 15,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify snake_case field names.
	for _, key := range []string{"writs_cleaned", "dirs_removed", "bytes_freed", "retention_days"} {
		if _, ok := m[key]; !ok {
			t.Errorf("expected key %q in JSON output", key)
		}
	}

	// Verify values.
	if v := m["writs_cleaned"].(float64); v != 5 {
		t.Errorf("writs_cleaned = %v, want 5", v)
	}
	if v := m["dirs_removed"].(float64); v != 5 {
		t.Errorf("dirs_removed = %v, want 5", v)
	}
	if v := m["bytes_freed"].(float64); v != 1048576 {
		t.Errorf("bytes_freed = %v, want 1048576", v)
	}
	if v := m["retention_days"].(float64); v != 15 {
		t.Errorf("retention_days = %v, want 15", v)
	}
}

func TestWritCleanResultZeroValues(t *testing.T) {
	r := WritCleanResult{
		RetentionDays: 30,
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Zero-value fields should still be present (not omitted).
	if v := m["writs_cleaned"].(float64); v != 0 {
		t.Errorf("writs_cleaned = %v, want 0", v)
	}
	if v := m["dirs_removed"].(float64); v != 0 {
		t.Errorf("dirs_removed = %v, want 0", v)
	}
	if v := m["bytes_freed"].(float64); v != 0 {
		t.Errorf("bytes_freed = %v, want 0", v)
	}
	if v := m["retention_days"].(float64); v != 30 {
		t.Errorf("retention_days = %v, want 30", v)
	}
}

func TestWritCleanResultRoundTrip(t *testing.T) {
	original := WritCleanResult{
		WritsCleaned:  12,
		DirsRemoved:   12,
		BytesFreed:    5368709120, // 5 GiB — tests int64 range
		RetentionDays: 7,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded WritCleanResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", decoded, original)
	}
}

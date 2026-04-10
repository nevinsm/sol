package schema

import (
	"encoding/json"
	"testing"
)

func TestMigrateResponseJSON(t *testing.T) {
	resp := MigrateResponse{
		AppliedMigrations: []MigratedDatabase{
			{
				Database:    "sphere",
				Type:        "sphere",
				FromVersion: 1,
				ToVersion:   3,
				Status:      "migrated",
			},
			{
				Database:    "dev",
				Type:        "world",
				FromVersion: 2,
				ToVersion:   5,
				Status:      "migrated",
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify top-level key.
	migrations, ok := got["applied_migrations"]
	if !ok {
		t.Fatal("missing JSON key \"applied_migrations\"")
	}

	arr, ok := migrations.([]any)
	if !ok {
		t.Fatalf("applied_migrations is not an array: %T", migrations)
	}
	if len(arr) != 2 {
		t.Fatalf("len(applied_migrations) = %d, want 2", len(arr))
	}

	// Verify first entry keys.
	first, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("first entry is not an object: %T", arr[0])
	}
	for _, key := range []string{"database", "type", "from_version", "to_version", "status"} {
		if _, ok := first[key]; !ok {
			t.Errorf("missing JSON key %q in migration entry", key)
		}
	}

	if first["database"] != "sphere" {
		t.Errorf("database = %v, want %q", first["database"], "sphere")
	}
	if first["from_version"] != float64(1) {
		t.Errorf("from_version = %v, want 1", first["from_version"])
	}
	if first["to_version"] != float64(3) {
		t.Errorf("to_version = %v, want 3", first["to_version"])
	}
	if first["status"] != "migrated" {
		t.Errorf("status = %v, want %q", first["status"], "migrated")
	}
}

func TestMigrateResponseEmptyMigrations(t *testing.T) {
	resp := MigrateResponse{
		AppliedMigrations: []MigratedDatabase{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Empty array should be present, not omitted.
	migrations, ok := got["applied_migrations"]
	if !ok {
		t.Fatal("applied_migrations should be present even when empty")
	}
	arr, ok := migrations.([]any)
	if !ok {
		t.Fatalf("applied_migrations is not an array: %T", migrations)
	}
	if len(arr) != 0 {
		t.Errorf("len(applied_migrations) = %d, want 0", len(arr))
	}
}

func TestMigrateResponseRoundTrip(t *testing.T) {
	resp := MigrateResponse{
		AppliedMigrations: []MigratedDatabase{
			{
				Database:    "sphere",
				Type:        "sphere",
				FromVersion: 0,
				ToVersion:   1,
				Status:      "created",
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got MigrateResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(got.AppliedMigrations) != 1 {
		t.Fatalf("len = %d, want 1", len(got.AppliedMigrations))
	}
	if got.AppliedMigrations[0] != resp.AppliedMigrations[0] {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", got.AppliedMigrations[0], resp.AppliedMigrations[0])
	}
}

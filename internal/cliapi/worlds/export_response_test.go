package worlds

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorldExportResultJSON(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	result := WorldExportResult{
		World:       "sol-dev",
		ArchivePath: "/tmp/sol-dev-export.tar.gz",
		SizeBytes:   1048576,
		ExportedAt:  now,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	// Verify snake_case keys
	for _, key := range []string{"world", "archive_path", "size_bytes", "exported_at"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing expected JSON key %q", key)
		}
	}

	if got["world"] != "sol-dev" {
		t.Errorf("world = %v, want %q", got["world"], "sol-dev")
	}
	if got["archive_path"] != "/tmp/sol-dev-export.tar.gz" {
		t.Errorf("archive_path = %v, want %q", got["archive_path"], "/tmp/sol-dev-export.tar.gz")
	}
	// JSON numbers are float64
	if got["size_bytes"] != float64(1048576) {
		t.Errorf("size_bytes = %v, want %v", got["size_bytes"], 1048576)
	}
	if got["exported_at"] != "2026-04-10T12:00:00Z" {
		t.Errorf("exported_at = %v, want %q", got["exported_at"], "2026-04-10T12:00:00Z")
	}
}

func TestWorldExportResultRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	original := WorldExportResult{
		World:       "myworld",
		ArchivePath: "/backups/myworld-export.tar.gz",
		SizeBytes:   0,
		ExportedAt:  now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded WorldExportResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.World != original.World {
		t.Errorf("World = %q, want %q", decoded.World, original.World)
	}
	if decoded.ArchivePath != original.ArchivePath {
		t.Errorf("ArchivePath = %q, want %q", decoded.ArchivePath, original.ArchivePath)
	}
	if decoded.SizeBytes != original.SizeBytes {
		t.Errorf("SizeBytes = %d, want %d", decoded.SizeBytes, original.SizeBytes)
	}
	if !decoded.ExportedAt.Equal(original.ExportedAt) {
		t.Errorf("ExportedAt = %v, want %v", decoded.ExportedAt, original.ExportedAt)
	}
}

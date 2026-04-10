package worlds

import (
	"encoding/json"
	"testing"
)

func TestExportResponseJSON(t *testing.T) {
	resp := ExportResponse{
		ArchivePath: "/tmp/sol-dev-export.tar.gz",
		World:       "sol-dev",
		SizeBytes:   1048576,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["archive_path"] != "/tmp/sol-dev-export.tar.gz" {
		t.Errorf("archive_path = %v, want %q", got["archive_path"], "/tmp/sol-dev-export.tar.gz")
	}
	if got["world"] != "sol-dev" {
		t.Errorf("world = %v, want %q", got["world"], "sol-dev")
	}
	if got["size_bytes"].(float64) != 1048576 {
		t.Errorf("size_bytes = %v, want 1048576", got["size_bytes"])
	}

	if len(got) != 3 {
		t.Errorf("got %d keys, want 3: %v", len(got), got)
	}
}

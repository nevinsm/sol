package worldexport

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// createTestArchive builds a minimal export .tar.gz for testing.
func createTestArchive(t *testing.T, dir string, manifest Manifest, worldToml string, worldDB []byte, sphereData map[string]interface{}) string {
	t.Helper()
	archivePath := filepath.Join(dir, "test-export.tar.gz")

	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive file: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	prefix := "sol-export-" + manifest.World + "-test/"

	// Write directory entry.
	tw.WriteHeader(&tar.Header{
		Name:     prefix,
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	})

	// Write manifest.json.
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	writeArchiveFile(t, tw, prefix+"manifest.json", manifestJSON)

	// Write world.toml.
	writeArchiveFile(t, tw, prefix+"world.toml", []byte(worldToml))

	// Write world.db.
	if worldDB != nil {
		writeArchiveFile(t, tw, prefix+"world.db", worldDB)
	}

	// Write sphere-data/ files.
	if len(sphereData) > 0 {
		tw.WriteHeader(&tar.Header{
			Name:     prefix + "sphere-data/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		})
		for name, data := range sphereData {
			jsonData, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				t.Fatalf("marshal %s: %v", name, err)
			}
			writeArchiveFile(t, tw, prefix+"sphere-data/"+name, jsonData)
		}
	}

	return archivePath
}

func writeArchiveFile(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Size:     int64(len(data)),
		Mode:     0o644,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write header %s: %v", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("write data %s: %v", name, err)
	}
}

// createWorldDB creates a minimal valid world.db for testing.
func createWorldDB(t *testing.T) []byte {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Set SOL_HOME so store.OpenWorld uses our temp dir.
	t.Setenv("SOL_HOME", tmpDir)
	os.MkdirAll(filepath.Join(tmpDir, ".store"), 0o755)

	// Create a world database by opening it (triggers migration).
	// We need to copy the file directly since OpenWorld expects a specific path.
	s, err := store.OpenWorld("test")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(tmpDir, ".store", "test.db"))
	if err != nil {
		t.Fatalf("read world db: %v", err)
	}

	// Clean up for the real test to use its own SOL_HOME.
	_ = dbPath
	return data
}

func TestManifestValidation(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)

	tests := []struct {
		name    string
		m       Manifest
		wantErr string
	}{
		{
			name:    "valid",
			m:       Manifest{Version: 1, World: "myworld", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 7, Sphere: 8}},
			wantErr: "",
		},
		{
			name:    "zero version",
			m:       Manifest{Version: 0, World: "myworld", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 7, Sphere: 8}},
			wantErr: "unsupported manifest version",
		},
		{
			name:    "future manifest version",
			m:       Manifest{Version: 99, World: "myworld", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 7, Sphere: 8}},
			wantErr: "newer than supported",
		},
		{
			name:    "missing world",
			m:       Manifest{Version: 1, World: "", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 7, Sphere: 8}},
			wantErr: "missing world name",
		},
		{
			name:    "bad timestamp",
			m:       Manifest{Version: 1, World: "myworld", ExportedAt: "not-a-time", SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 7, Sphere: 8}},
			wantErr: "invalid exported_at",
		},
		{
			name:    "future world schema",
			m:       Manifest{Version: 1, World: "myworld", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 99, Sphere: 8}},
			wantErr: "world schema v99 is newer",
		},
		{
			name:    "future sphere schema",
			m:       Manifest{Version: 1, World: "myworld", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 7, Sphere: 99}},
			wantErr: "sphere schema v99 is newer",
		},
		{
			name:    "older schemas ok",
			m:       Manifest{Version: 1, World: "myworld", ExportedAt: now, SolVersion: "0.1.0", SchemaVersions: SchemaVersions{World: 5, Sphere: 6}},
			wantErr: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if got := err.Error(); !contains(got, tc.wantErr) {
					t.Fatalf("expected error containing %q, got: %s", tc.wantErr, got)
				}
			}
		})
	}
}

func TestRewriteAgentID(t *testing.T) {
	tests := []struct {
		id       string
		oldWorld string
		newWorld string
		want     string
	}{
		{"myworld/Toast", "myworld", "newworld", "newworld/Toast"},
		{"other/Agent", "myworld", "newworld", "other/Agent"},
		{"myworld-extra/Toast", "myworld", "newworld", "myworld-extra/Toast"},
		{"operator", "myworld", "newworld", "operator"},
	}

	for _, tc := range tests {
		got := rewriteAgentID(tc.id, tc.oldWorld, tc.newWorld)
		if got != tc.want {
			t.Errorf("rewriteAgentID(%q, %q, %q) = %q, want %q", tc.id, tc.oldWorld, tc.newWorld, got, tc.want)
		}
	}
}

func TestImportBasic(t *testing.T) {
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create a valid world.db.
	worldDB := createWorldDB(t)

	// Reset SOL_HOME for the actual import.
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	now := time.Now().UTC().Format(time.RFC3339)
	manifest := Manifest{
		Version:    1,
		World:      "testworld",
		ExportedAt: now,
		SolVersion: "0.1.0",
		SchemaVersions: SchemaVersions{
			World:  CurrentWorldSchema,
			Sphere: CurrentSphereSchema,
		},
	}

	worldToml := `[world]
source_repo = ""

[agents]
model_tier = "sonnet"

[forge]
target_branch = "main"
gate_timeout = "5m"
`

	agents := []ExportAgent{
		{
			ID: "testworld/Alpha", Name: "Alpha", World: "testworld",
			Role: "agent", State: "working", CreatedAt: now, UpdatedAt: now,
		},
	}

	archivePath := createTestArchive(t, t.TempDir(), manifest, worldToml, worldDB, map[string]interface{}{
		"agents.json":        agents,
		"messages.json":      []ExportMessage{},
		"escalations.json":   []ExportEscalation{},
		"caravans.json":      []ExportCaravan{},
		"caravan_items.json": []ExportCaravanItem{},
	})

	result, err := Import(ImportOptions{ArchivePath: archivePath})
	if err != nil {
		t.Fatalf("Import() error: %v", err)
	}

	if result.World != "testworld" {
		t.Errorf("expected world 'testworld', got %q", result.World)
	}

	// Verify world.toml exists.
	tomlPath := filepath.Join(gtHome, "testworld", "world.toml")
	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		t.Fatal("world.toml not created")
	}

	// Verify world.db exists.
	dbPath := filepath.Join(gtHome, ".store", "testworld.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("world.db not created")
	}

	// Verify agent was imported with state=idle.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	defer sphereStore.Close()

	agent, err := sphereStore.GetAgent("testworld/Alpha")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}

	// Verify world registered.
	world, err := sphereStore.GetWorld("testworld")
	if err != nil {
		t.Fatalf("get world: %v", err)
	}
	if world.Name != "testworld" {
		t.Errorf("expected world name 'testworld', got %q", world.Name)
	}
}

func TestImportConflict(t *testing.T) {
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create the world directory and config to simulate existing world.
	worldDir := filepath.Join(gtHome, "testworld")
	os.MkdirAll(worldDir, 0o755)
	os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644)

	worldDB := createWorldDB(t)
	t.Setenv("SOL_HOME", gtHome)

	now := time.Now().UTC().Format(time.RFC3339)
	manifest := Manifest{
		Version:    1,
		World:      "testworld",
		ExportedAt: now,
		SolVersion: "0.1.0",
		SchemaVersions: SchemaVersions{
			World:  CurrentWorldSchema,
			Sphere: CurrentSphereSchema,
		},
	}

	archivePath := createTestArchive(t, t.TempDir(), manifest, "[world]\n", worldDB, nil)

	_, err := Import(ImportOptions{ArchivePath: archivePath})
	if err == nil {
		t.Fatal("expected error on name conflict, got nil")
	}
	if !contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got: %v", err)
	}
}

func TestImportWithRename(t *testing.T) {
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	worldDB := createWorldDB(t)
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	now := time.Now().UTC().Format(time.RFC3339)
	manifest := Manifest{
		Version:    1,
		World:      "oldworld",
		ExportedAt: now,
		SolVersion: "0.1.0",
		SchemaVersions: SchemaVersions{
			World:  CurrentWorldSchema,
			Sphere: CurrentSphereSchema,
		},
	}

	agents := []ExportAgent{
		{
			ID: "oldworld/Beta", Name: "Beta", World: "oldworld",
			Role: "agent", State: "working", CreatedAt: now, UpdatedAt: now,
		},
	}

	caravanItems := []ExportCaravanItem{
		{CaravanID: "car-test123", WorkItemID: "sol-item1", World: "oldworld", Phase: 0},
		{CaravanID: "car-test123", WorkItemID: "sol-item2", World: "otherworld", Phase: 0},
	}

	caravans := []ExportCaravan{
		{ID: "car-test123", Name: "test", Status: "open", CreatedAt: now},
	}

	archivePath := createTestArchive(t, t.TempDir(), manifest, "[world]\n", worldDB, map[string]interface{}{
		"agents.json":        agents,
		"messages.json":      []ExportMessage{},
		"escalations.json":   []ExportEscalation{},
		"caravans.json":      caravans,
		"caravan_items.json": caravanItems,
	})

	result, err := Import(ImportOptions{
		ArchivePath: archivePath,
		Name:        "newworld",
	})
	if err != nil {
		t.Fatalf("Import() error: %v", err)
	}

	if result.World != "newworld" {
		t.Errorf("expected world 'newworld', got %q", result.World)
	}

	// Verify agent was renamed.
	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	defer sphereStore.Close()

	agent, err := sphereStore.GetAgent("newworld/Beta")
	if err != nil {
		t.Fatalf("get renamed agent: %v", err)
	}
	if agent.World != "newworld" {
		t.Errorf("expected agent world 'newworld', got %q", agent.World)
	}

	// Verify caravan items: world-scoped item renamed, cross-world preserved.
	items, err := sphereStore.ListCaravanItems("car-test123")
	if err != nil {
		t.Fatalf("list caravan items: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 caravan items, got %d", len(items))
	}

	worldMap := map[string]string{}
	for _, item := range items {
		worldMap[item.WorkItemID] = item.World
	}
	if worldMap["sol-item1"] != "newworld" {
		t.Errorf("expected caravan item sol-item1 world 'newworld', got %q", worldMap["sol-item1"])
	}
	if worldMap["sol-item2"] != "otherworld" {
		t.Errorf("expected caravan item sol-item2 world 'otherworld', got %q", worldMap["sol-item2"])
	}
}

func TestImportSchemaForwardCompat(t *testing.T) {
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	worldDB := createWorldDB(t)
	t.Setenv("SOL_HOME", gtHome)

	now := time.Now().UTC().Format(time.RFC3339)
	manifest := Manifest{
		Version:    1,
		World:      "futureworld",
		ExportedAt: now,
		SolVersion: "99.0.0",
		SchemaVersions: SchemaVersions{
			World:  99,
			Sphere: CurrentSphereSchema,
		},
	}

	archivePath := createTestArchive(t, t.TempDir(), manifest, "[world]\n", worldDB, nil)

	_, err := Import(ImportOptions{ArchivePath: archivePath})
	if err == nil {
		t.Fatal("expected error for future schema, got nil")
	}
	if !contains(err.Error(), "newer than supported") {
		t.Fatalf("expected 'newer than supported' error, got: %v", err)
	}
}

func TestImportInvalidArchive(t *testing.T) {
	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	// Create a file that isn't a valid archive.
	badPath := filepath.Join(t.TempDir(), "not-an-archive.tar.gz")
	os.WriteFile(badPath, []byte("not a tar.gz"), 0o644)

	_, err := Import(ImportOptions{ArchivePath: badPath})
	if err == nil {
		t.Fatal("expected error for invalid archive, got nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package integration

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

func TestWorldExportBasic(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	outputDir := t.TempDir()
	output := filepath.Join(outputDir, "myworld-export.tar.gz")

	out, err := runGT(t, gtHome, "world", "export", "myworld", "--output="+output)
	if err != nil {
		t.Fatalf("world export failed: %v: %s", err, out)
	}

	if !strings.Contains(out, "exported to") {
		t.Errorf("expected 'exported to' message, got: %s", out)
	}

	// Verify archive exists and is valid.
	entries := listTarEntries(t, output)

	// Archive root should use the sol-export-{world}-{timestamp}/ prefix.
	archiveRoot := findArchiveRoot(entries, "sol-export-myworld-")
	if archiveRoot == "" {
		t.Fatalf("archive root not found, entries: %v", entries)
	}

	// Must contain manifest.json at archive root.
	if !containsEntry(entries, archiveRoot+"manifest.json") {
		t.Errorf("archive missing manifest.json, entries: %v", entries)
	}

	// Must contain world database at expected path for Import().
	if !containsEntry(entries, archiveRoot+"world.db") {
		t.Errorf("archive missing world.db, entries: %v", entries)
	}

	// Must contain world.toml at expected path for Import().
	if !containsEntry(entries, archiveRoot+"world.toml") {
		t.Errorf("archive missing world.toml, entries: %v", entries)
	}
}

func TestWorldExportWithData(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Create a writ so the DB has data.
	t.Setenv("SOL_HOME", gtHome)
	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	_, err = worldStore.CreateWrit("Test item", "Test description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	worldStore.Close()

	outputDir := t.TempDir()
	output := filepath.Join(outputDir, "export.tar.gz")

	out, err := runGT(t, gtHome, "world", "export", "myworld", "-o", output)
	if err != nil {
		t.Fatalf("world export failed: %v: %s", err, out)
	}

	entries := listTarEntries(t, output)

	archiveRoot := findArchiveRoot(entries, "sol-export-myworld-")
	if archiveRoot == "" {
		t.Fatalf("archive root not found, entries: %v", entries)
	}

	// sphere-data/ should be present with agent/message/escalation JSON files.
	if !containsEntry(entries, archiveRoot+"sphere-data/agents.json") {
		t.Errorf("archive missing sphere-data/agents.json, entries: %v", entries)
	}
	if !containsEntry(entries, archiveRoot+"sphere-data/messages.json") {
		t.Errorf("archive missing sphere-data/messages.json, entries: %v", entries)
	}
	if !containsEntry(entries, archiveRoot+"sphere-data/caravans.json") {
		t.Errorf("archive missing sphere-data/caravans.json, entries: %v", entries)
	}

	// world.db and world.toml must be present for Import() compatibility.
	if !containsEntry(entries, archiveRoot+"world.db") {
		t.Errorf("archive missing world.db, entries: %v", entries)
	}
	if !containsEntry(entries, archiveRoot+"world.toml") {
		t.Errorf("archive missing world.toml, entries: %v", entries)
	}

	// Ephemeral paths must not appear.
	for _, e := range entries {
		if strings.HasSuffix(e, ".tether") {
			t.Errorf("archive should not contain .tether files, found: %s", e)
		}
		if strings.Contains(e, "worktree/") {
			t.Errorf("archive should not contain worktree/ paths, found: %s", e)
		}
		if strings.Contains(e, "/repo/") {
			t.Errorf("archive should not contain repo/ paths, found: %s", e)
		}
	}
}

func TestWorldExportManifest(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	outputDir := t.TempDir()
	output := filepath.Join(outputDir, "export.tar.gz")

	out, err := runGT(t, gtHome, "world", "export", "myworld", "-o", output)
	if err != nil {
		t.Fatalf("world export failed: %v: %s", err, out)
	}

	// Find the archive root prefix.
	entries := listTarEntries(t, output)
	archiveRoot := findArchiveRoot(entries, "sol-export-myworld-")
	if archiveRoot == "" {
		t.Fatalf("archive root not found, entries: %v", entries)
	}

	// Extract and parse manifest.json from the correct path.
	manifestData := extractFileFromTar(t, output, archiveRoot+"manifest.json")

	var manifest struct {
		Version        int    `json:"version"`
		World          string `json:"world"`
		ExportedAt     string `json:"exported_at"`
		SolVersion     string `json:"sol_version"`
		SchemaVersions struct {
			World  int `json:"world"`
			Sphere int `json:"sphere"`
		} `json:"schema_versions"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if manifest.Version != 1 {
		t.Errorf("expected manifest version 1, got %d", manifest.Version)
	}
	if manifest.World != "myworld" {
		t.Errorf("expected world 'myworld', got %q", manifest.World)
	}
	if manifest.ExportedAt == "" {
		t.Error("expected non-empty exported_at")
	}
	if manifest.SchemaVersions.World == 0 {
		t.Error("expected non-zero world schema version")
	}
	if manifest.SchemaVersions.Sphere == 0 {
		t.Error("expected non-zero sphere schema version")
	}
}

// findArchiveRoot finds the top-level directory entry in an archive whose name
// starts with the given prefix and returns it (including trailing slash).
func findArchiveRoot(entries []string, prefix string) string {
	for _, e := range entries {
		if strings.HasPrefix(e, prefix) && strings.HasSuffix(e, "/") {
			return e
		}
	}
	return ""
}

func TestWorldExportDefaultOutput(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Run from a temp dir so the default output goes there.
	workDir := t.TempDir()
	cmd := runGTWithDir(t, gtHome, workDir, "world", "export", "myworld")
	if cmd.err != nil {
		t.Fatalf("world export failed: %v: %s", cmd.err, cmd.out)
	}

	// Default output should be myworld-export.tar.gz in cwd.
	expectedOutput := filepath.Join(workDir, "myworld-export.tar.gz")
	if _, err := os.Stat(expectedOutput); os.IsNotExist(err) {
		t.Fatalf("expected default output at %s", expectedOutput)
	}
}

func TestWorldExportNonexistent(t *testing.T) {
	skipUnlessIntegration(t)
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	out, err := runGT(t, gtHome, "world", "export", "nonexistent")
	if err == nil {
		t.Fatalf("expected error, got success: %s", out)
	}
	if !strings.Contains(out, "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %s", out)
	}
}

// listTarEntries returns all entry names in a tar.gz archive.
func listTarEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var entries []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		entries = append(entries, hdr.Name)
	}
	return entries
}

// extractFileFromTar extracts a named file's contents from a tar.gz archive.
func extractFileFromTar(t *testing.T, archivePath, name string) []byte {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			t.Fatalf("file %q not found in archive", name)
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Name == name {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %q: %v", name, err)
			}
			return data
		}
	}
}

func containsEntry(entries []string, name string) bool {
	for _, e := range entries {
		if e == name {
			return true
		}
	}
	return false
}


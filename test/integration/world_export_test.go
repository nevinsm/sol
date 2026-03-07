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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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

	// Must contain manifest.json.
	if !containsEntry(entries, "myworld/manifest.json") {
		t.Errorf("archive missing manifest.json, entries: %v", entries)
	}

	// Must contain world database.
	if !containsEntry(entries, "myworld/database/myworld.db") {
		t.Errorf("archive missing database/myworld.db, entries: %v", entries)
	}

	// Must contain world.toml.
	if !containsEntry(entries, "myworld/tree/world.toml") {
		t.Errorf("archive missing tree/world.toml, entries: %v", entries)
	}
}

func TestWorldExportWithData(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	// Create a writ so the DB has data.
	t.Setenv("SOL_HOME", gtHome)
	worldStore, err := store.OpenWorld("myworld")
	if err != nil {
		t.Fatalf("open world store: %v", err)
	}
	_, err = worldStore.CreateWrit("Test item", "Test description", "operator", 2, nil)
	if err != nil {
		t.Fatalf("create writ: %v", err)
	}
	worldStore.Close()

	// Create some agent config dirs to verify they're included.
	agentConfigDir := filepath.Join(gtHome, "myworld", ".claude-config", "outposts", "TestAgent")
	os.MkdirAll(agentConfigDir, 0o755)
	os.WriteFile(filepath.Join(agentConfigDir, "config.json"), []byte(`{"test": true}`), 0o644)

	// Create an outpost dir with a .tether and worktree (should be excluded).
	outpostDir := filepath.Join(gtHome, "myworld", "outposts", "TestAgent")
	os.MkdirAll(filepath.Join(outpostDir, "worktree"), 0o755)
	os.WriteFile(filepath.Join(outpostDir, ".tether"), []byte("sol-abc123"), 0o644)
	os.WriteFile(filepath.Join(outpostDir, "worktree", "file.go"), []byte("package main"), 0o644)

	outputDir := t.TempDir()
	output := filepath.Join(outputDir, "export.tar.gz")

	out, err := runGT(t, gtHome, "world", "export", "myworld", "-o", output)
	if err != nil {
		t.Fatalf("world export failed: %v: %s", err, out)
	}

	entries := listTarEntries(t, output)

	// Agent config should be included.
	if !containsEntry(entries, "myworld/tree/.claude-config/outposts/TestAgent/config.json") {
		t.Errorf("archive missing agent config, entries: %v", entries)
	}

	// Outposts dir should be included.
	if !containsEntryPrefix(entries, "myworld/tree/outposts/") {
		t.Errorf("archive missing outposts dir, entries: %v", entries)
	}

	// .tether should be excluded.
	for _, e := range entries {
		if strings.HasSuffix(e, ".tether") {
			t.Errorf("archive should not contain .tether files, found: %s", e)
		}
	}

	// worktree/ should be excluded.
	for _, e := range entries {
		if strings.Contains(e, "worktree/") {
			t.Errorf("archive should not contain worktree/ paths, found: %s", e)
		}
	}

	// repo/ should be excluded (doesn't exist here but verify no repo entries).
	for _, e := range entries {
		if strings.Contains(e, "/repo/") {
			t.Errorf("archive should not contain repo/ paths, found: %s", e)
		}
	}
}

func TestWorldExportManifest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)

	initWorld(t, gtHome, "myworld")

	outputDir := t.TempDir()
	output := filepath.Join(outputDir, "export.tar.gz")

	out, err := runGT(t, gtHome, "world", "export", "myworld", "-o", output)
	if err != nil {
		t.Fatalf("world export failed: %v: %s", err, out)
	}

	// Extract and parse manifest.json.
	manifestData := extractFileFromTar(t, output, "myworld/manifest.json")

	var manifest struct {
		World      string   `json:"world"`
		ExportedAt string   `json:"exported_at"`
		Contents   []string `json:"contents"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if manifest.World != "myworld" {
		t.Errorf("expected world 'myworld', got %q", manifest.World)
	}
	if manifest.ExportedAt == "" {
		t.Error("expected non-empty exported_at")
	}
	if len(manifest.Contents) == 0 {
		t.Error("expected non-empty contents list")
	}
}

func TestWorldExportDefaultOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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

func containsEntryPrefix(entries []string, prefix string) bool {
	for _, e := range entries {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

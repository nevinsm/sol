package tether

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func setupTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
}

func TestWriteAndRead(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	id, err := Read("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("expected sol-a1b2c3d4e5f6a7b8, got %q", id)
	}
}

func TestWriteCreatesDirectoryAndFile(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify the directory was created.
	dir := TetherDir("myworld", "Toast", "outpost")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected tether directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .tether to be a directory, got file")
	}

	// Verify the writ file exists inside the directory.
	filePath := filepath.Join(dir, "sol-a1b2c3d4e5f6a7b8")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("expected writ file to exist: %v", err)
	}
	if string(data) != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("expected file content %q, got %q", "sol-a1b2c3d4e5f6a7b8", string(data))
	}
}

func TestWriteVerifiesContent(t *testing.T) {
	setupTest(t)

	// Write and verify the file content matches via Read.
	writID := "sol-ae01f21234567800"
	if err := Write("myworld", "Toast", writID, "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify via direct file read (not through Read()).
	dir := TetherDir("myworld", "Toast", "outpost")
	data, err := os.ReadFile(filepath.Join(dir, writID))
	if err != nil {
		t.Fatalf("failed to read tether file: %v", err)
	}
	if string(data) != writID {
		t.Errorf("tether file content mismatch: got %q, want %q", string(data), writID)
	}
}

func TestReadNoTether(t *testing.T) {
	setupTest(t)

	id, err := Read("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty string, got %q", id)
	}
}

func TestListReturnsAllWritIDs(t *testing.T) {
	setupTest(t)

	writs := []string{"sol-a1b2c3d4e5f6a7b8", "sol-e5f6a7b8c9d0e1f2", "sol-1122334455667788"}
	for _, w := range writs {
		if err := Write("myworld", "Toast", w, "outpost"); err != nil {
			t.Fatalf("Write %s failed: %v", w, err)
		}
	}

	ids, err := List("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	sort.Strings(writs)
	if len(ids) != len(writs) {
		t.Fatalf("expected %d tethers, got %d", len(writs), len(ids))
	}
	for i, id := range ids {
		if id != writs[i] {
			t.Errorf("expected %q at index %d, got %q", writs[i], i, id)
		}
	}
}

func TestListEmpty(t *testing.T) {
	setupTest(t)

	ids, err := List("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list, got %v", ids)
	}
}

func TestClear(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Write("myworld", "Toast", "sol-e5f6a7b8c9d0e1f2", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := Clear("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	ids, err := List("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("List after Clear failed: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list after Clear, got %v", ids)
	}
}

func TestClearNoTether(t *testing.T) {
	setupTest(t)

	// Clear should be a no-op if no tether exists.
	if err := Clear("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Clear on non-existent tether failed: %v", err)
	}
}

func TestClearOneRemovesOneFile(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Write("myworld", "Toast", "sol-e5f6a7b8c9d0e1f2", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if err := ClearOne("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("ClearOne failed: %v", err)
	}

	ids, err := List("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("List after ClearOne failed: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 tether after ClearOne, got %d", len(ids))
	}
	if ids[0] != "sol-e5f6a7b8c9d0e1f2" {
		t.Errorf("expected remaining tether sol-e5f6a7b8c9d0e1f2, got %q", ids[0])
	}
}

func TestClearOneNoOp(t *testing.T) {
	setupTest(t)

	// ClearOne should be a no-op if the specific tether doesn't exist.
	if err := ClearOne("myworld", "Toast", "sol-nonexistent", "outpost"); err != nil {
		t.Fatalf("ClearOne on non-existent tether failed: %v", err)
	}
}

func TestIsTethered(t *testing.T) {
	setupTest(t)

	if IsTethered("myworld", "Toast", "outpost") {
		t.Error("expected IsTethered=false before Write")
	}

	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if !IsTethered("myworld", "Toast", "outpost") {
		t.Error("expected IsTethered=true after Write")
	}

	if err := Clear("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if IsTethered("myworld", "Toast", "outpost") {
		t.Error("expected IsTethered=false after Clear")
	}
}

func TestIsTetheredTo(t *testing.T) {
	setupTest(t)

	if IsTetheredTo("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost") {
		t.Error("expected IsTetheredTo=false before Write")
	}

	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if !IsTetheredTo("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost") {
		t.Error("expected IsTetheredTo=true for written writ")
	}

	if IsTetheredTo("myworld", "Toast", "sol-other", "outpost") {
		t.Error("expected IsTetheredTo=false for different writ")
	}
}

func TestTetherDir(t *testing.T) {
	setupTest(t)

	dir := TetherDir("myworld", "Toast", "outpost")
	solHome := os.Getenv("SOL_HOME")
	expected := solHome + "/myworld/outposts/Toast/.tether"
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

func TestWriteOverwrite(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-1111111122222222", "outpost"); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}
	if err := Write("myworld", "Toast", "sol-2222222233333333", "outpost"); err != nil {
		t.Fatalf("second Write failed: %v", err)
	}

	ids, err := List("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 tethers, got %d: %v", len(ids), ids)
	}
}

func TestMigrateLegacyFile(t *testing.T) {
	setupTest(t)

	solHome := os.Getenv("SOL_HOME")
	agentDir := filepath.Join(solHome, "myworld", "outposts", "Toast")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Create a legacy .tether file (not directory).
	legacyPath := filepath.Join(agentDir, ".tether")
	if err := os.WriteFile(legacyPath, []byte("sol-1e9ac4123456abcd"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify it's a file, not a directory.
	info, err := os.Stat(legacyPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.IsDir() {
		t.Fatal("expected legacy .tether to be a file")
	}

	// Run migration.
	if err := Migrate("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify the .tether is now a directory.
	info, err = os.Stat(legacyPath)
	if err != nil {
		t.Fatalf("Stat after migration failed: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected .tether to be a directory after migration")
	}

	// Verify the writ ID file exists inside.
	id, err := Read("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read after migration failed: %v", err)
	}
	if id != "sol-1e9ac4123456abcd" {
		t.Errorf("expected sol-1e9ac4123456abcd, got %q", id)
	}
}

func TestMigrateEmptyLegacyFile(t *testing.T) {
	setupTest(t)

	solHome := os.Getenv("SOL_HOME")
	agentDir := filepath.Join(solHome, "myworld", "outposts", "Toast")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Create a legacy .tether file with empty content.
	legacyPath := filepath.Join(agentDir, ".tether")
	if err := os.WriteFile(legacyPath, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Run migration.
	if err := Migrate("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify the empty file was removed.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("empty legacy .tether file should be removed after migration")
	}

	// Verify no tether is active (Read returns "").
	id, err := Read("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read after empty migration failed: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty tether after empty migration, got %q", id)
	}
}

func TestMigrateNoLegacyFile(t *testing.T) {
	setupTest(t)

	// Migration should be a no-op if no legacy file exists.
	if err := Migrate("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Migrate on non-existent tether failed: %v", err)
	}
}

func TestMigrateAlreadyDirectory(t *testing.T) {
	setupTest(t)

	// Write using new model (creates directory).
	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Migration should be a no-op if already a directory.
	if err := Migrate("myworld", "Toast", "outpost"); err != nil {
		t.Fatalf("Migrate on existing directory failed: %v", err)
	}

	// Verify existing tether is preserved.
	id, err := Read("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read after no-op migration failed: %v", err)
	}
	if id != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("expected sol-a1b2c3d4e5f6a7b8, got %q", id)
	}
}

func TestReadReturnsSingleTether(t *testing.T) {
	setupTest(t)

	// Write multiple tethers.
	if err := Write("myworld", "Toast", "sol-a1b2c3d4e5f6a7b8", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Write("myworld", "Toast", "sol-e5f6a7b8c9d0e1f2", "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read returns the first (sorted) tether for backward compat.
	id, err := Read("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty tether from Read")
	}
}

func TestListFiltersJunkFiles(t *testing.T) {
	setupTest(t)

	writID := "sol-a1b2c3d4e5f6a7b8"
	if err := Write("myworld", "Toast", writID, "outpost"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Plant junk files directly in the tether directory.
	dir := TetherDir("myworld", "Toast", "outpost")
	junkFiles := []string{".DS_Store", ".sol-a1b2c3d4e5f6a7b8.swp", "random-file", "sol-tooshort"}
	for _, junk := range junkFiles {
		if err := os.WriteFile(filepath.Join(dir, junk), []byte("junk"), 0o644); err != nil {
			t.Fatalf("failed to create junk file %q: %v", junk, err)
		}
	}

	ids, err := List("myworld", "Toast", "outpost")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 valid tether, got %d: %v", len(ids), ids)
	}
	if ids[0] != writID {
		t.Errorf("expected %q, got %q", writID, ids[0])
	}
}

func TestEnvoyTetherDir(t *testing.T) {
	setupTest(t)

	dir := TetherDir("myworld", "Polaris", "envoy")
	solHome := os.Getenv("SOL_HOME")
	expected := solHome + "/myworld/envoys/Polaris/.tether"
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaVersionFreshDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")

	s, err := OpenNoMigrate(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("expected version 0 for fresh database, got %d", v)
	}
}

func TestSchemaVersionAfterWorldMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	s, err := OpenWorld("versiontest")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentWorldSchema {
		t.Fatalf("expected version %d, got %d", CurrentWorldSchema, v)
	}
}

func TestSchemaVersionAfterSphereMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	s, err := OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	v, err := s.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentSphereSchema {
		t.Fatalf("expected version %d, got %d", CurrentSphereSchema, v)
	}
}

func TestOpenNoMigrateDoesNotMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nomigrate.db")

	// Create a V1-only database manually.
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(worldSchemaV1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO schema_version VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Open without migration.
	s2, err := OpenNoMigrate(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	v, err := s2.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("expected version 1 (no migration), got %d", v)
	}

	// Verify merge_requests table does NOT exist (V2 not applied).
	exists, err := tableExists(s2.db, "merge_requests")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected merge_requests table to not exist (no migration)")
	}
}

func TestBackupDatabase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.MkdirAll(filepath.Join(dir, ".store"), 0o755)

	// Create a world database with some data.
	s, err := OpenWorld("backuptest")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateWorkItem("backup item", "test", "operator", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Backup the database.
	dbPath := filepath.Join(dir, ".store", "backuptest.db")
	backupPath, err := BackupDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	// Verify backup file exists.
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("backup file does not exist: %s", backupPath)
	}

	// Verify backup path format.
	if !strings.HasPrefix(backupPath, dbPath+".backup.") {
		t.Fatalf("unexpected backup path: %s", backupPath)
	}

	// Verify backup is a valid database with the same data.
	backupStore, err := OpenNoMigrate(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backupStore.Close()

	v, err := backupStore.SchemaVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != CurrentWorldSchema {
		t.Fatalf("expected backup version %d, got %d", CurrentWorldSchema, v)
	}
}

func TestBackupDatabaseNonexistent(t *testing.T) {
	_, err := BackupDatabase("/nonexistent/path.db")
	if err == nil {
		t.Fatal("expected error for nonexistent database")
	}
}

func TestCurrentSchemaConstants(t *testing.T) {
	// Verify constants are positive and match the expected values.
	if CurrentWorldSchema != 7 {
		t.Fatalf("CurrentWorldSchema = %d, expected 7", CurrentWorldSchema)
	}
	if CurrentSphereSchema != 8 {
		t.Fatalf("CurrentSphereSchema = %d, expected 8", CurrentSphereSchema)
	}
}

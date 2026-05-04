package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBackupDatabaseUniqueFilenames verifies that two backups created in quick
// succession produce distinct filenames. Before the timestamp resolution was
// raised to nanoseconds, two backups inside the same wall-clock second would
// share a filename and the second would clobber the first.
func TestBackupDatabaseUniqueFilenames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a real world database so VACUUM INTO has something to copy.
	s, err := OpenWorld("backuptest")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, ".store", "backuptest.db")

	// Two back-to-back backups must produce distinct paths even when invoked
	// within the same wall-clock second.
	const n = 5
	seen := make(map[string]struct{}, n)
	for i := range n {
		path, err := BackupDatabase(dbPath)
		if err != nil {
			t.Fatalf("BackupDatabase[%d] failed: %v", i, err)
		}
		if !strings.HasPrefix(path, dbPath+".backup.") {
			t.Errorf("backup path %q missing expected prefix", path)
		}
		if _, dup := seen[path]; dup {
			t.Fatalf("duplicate backup filename %q after %d iterations", path, i+1)
		}
		seen[path] = struct{}{}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("backup file %q not created: %v", path, err)
		}
	}
}

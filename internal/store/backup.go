package store

import (
	"fmt"
	"time"
)

// BackupDatabase creates a consistent copy of a database file at path.backup.{timestamp}.
// Returns the backup path. The original file is not modified.
//
// The timestamp uses nanosecond resolution so back-to-back invocations within
// the same wall-clock second produce distinct filenames and do not clobber
// each other.
//
// BackupDatabase uses SQLite's VACUUM INTO to create an atomic, consistent backup.
// This avoids the race condition where concurrent writers could produce a torn backup
// during a manual file copy (pages from different transaction states).
func BackupDatabase(path string) (string, error) {
	s, err := OpenNoMigrate(path)
	if err != nil {
		return "", fmt.Errorf("failed to open database for backup %q: %w", path, err)
	}
	defer s.Close()

	backupPath := fmt.Sprintf("%s.backup.%s", path, time.Now().UTC().Format("20060102T150405.000000000Z"))

	_, err = s.db.Exec("VACUUM INTO ?", backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to backup database %q: %w", path, err)
	}

	return backupPath, nil
}

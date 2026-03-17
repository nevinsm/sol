package store

import (
	"fmt"
	"io"
	"os"
	"time"
)

// BackupDatabase creates a copy of a database file at path.backup.{timestamp}.
// Returns the backup path. The original file is not modified.
//
// BackupDatabase performs a WAL checkpoint (TRUNCATE mode) before copying,
// ensuring all committed transactions are flushed into the main .db file so
// the backup is complete.
func BackupDatabase(path string) (string, error) {
	// Checkpoint the WAL before copying so all committed writes are in the .db file.
	s, err := OpenNoMigrate(path)
	if err != nil {
		return "", fmt.Errorf("failed to open database for checkpoint %q: %w", path, err)
	}
	if err := s.Checkpoint(); err != nil {
		s.Close()
		return "", fmt.Errorf("failed to checkpoint database before backup %q: %w", path, err)
	}
	if err := s.Close(); err != nil {
		return "", fmt.Errorf("failed to close database after checkpoint %q: %w", path, err)
	}

	backupPath := fmt.Sprintf("%s.backup.%s", path, time.Now().UTC().Format("20060102T150405Z"))

	src, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open database for backup %q: %w", path, err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file %q: %w", backupPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		os.Remove(backupPath)
		return "", fmt.Errorf("failed to copy database to backup %q: %w", backupPath, err)
	}

	if err := dst.Sync(); err != nil {
		os.Remove(backupPath)
		return "", fmt.Errorf("failed to sync backup file %q: %w", backupPath, err)
	}

	return backupPath, nil
}

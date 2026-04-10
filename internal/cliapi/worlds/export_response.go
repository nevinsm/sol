package worlds

import "time"

// WorldExportResult is the CLI API response for "sol world export --json".
type WorldExportResult struct {
	World       string    `json:"world"`
	ArchivePath string    `json:"archive_path"`
	SizeBytes   int64     `json:"size_bytes"`
	ExportedAt  time.Time `json:"exported_at"`
}

package worlds

// ExportResponse is the CLI API response for world export --json.
type ExportResponse struct {
	ArchivePath string `json:"archive_path"`
	World       string `json:"world"`
	SizeBytes   int64  `json:"size_bytes"`
}

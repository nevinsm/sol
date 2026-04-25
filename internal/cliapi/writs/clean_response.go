package writs

// WritCleanResult is the CLI API response for `sol writ clean --json`.
type WritCleanResult struct {
	WritsCleaned  int              `json:"writs_cleaned"`
	DirsRemoved   int              `json:"dirs_removed"`
	BytesFreed    int64            `json:"bytes_freed"`
	RetentionDays int              `json:"retention_days"`
	Candidates    []CleanCandidate `json:"candidates,omitempty"`
}

// CleanCandidate describes a writ output directory eligible for cleanup.
type CleanCandidate struct {
	WritID   string `json:"writ_id"`
	ClosedAt string `json:"closed_at,omitempty"`
	Size     int64  `json:"size"`
}

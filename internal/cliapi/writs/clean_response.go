package writs

// WritCleanResult is the CLI API response for `sol writ clean --json`.
type WritCleanResult struct {
	WritsCleaned  int   `json:"writs_cleaned"`
	DirsRemoved   int   `json:"dirs_removed"`
	BytesFreed    int64 `json:"bytes_freed"`
	RetentionDays int   `json:"retention_days"`
}

package schema

// MigrateResponse is the CLI API response for `sol schema migrate --json`.
type MigrateResponse struct {
	AppliedMigrations []MigratedDatabase `json:"applied_migrations"`
}

// MigratedDatabase describes the migration result for a single database.
type MigratedDatabase struct {
	Database    string `json:"database"`
	Type        string `json:"type"`
	FromVersion int    `json:"from_version"`
	ToVersion   int    `json:"to_version"`
	Status      string `json:"status"`
}

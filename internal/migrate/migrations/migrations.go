package migrations

// This file is intentionally empty: sol's built-in migrations will be
// registered from dedicated files in this package (one per migration) in
// future phases of the envoy-memory-migration caravan. The framework
// itself lives in the parent internal/migrate package; importing this
// package for side effects from cmd/root.go ensures any future
// init()-based Register calls fire before any command runs.

// Package cliapi defines the stable CLI output API surface for sol.
//
// Each sub-package contains the canonical named types for sol's --json output.
// Types in cliapi wrap corresponding store types via From* conversion functions,
// decoupling the public API surface from internal storage. This ensures that
// future store refactors cannot accidentally break the API contract.
//
// Field naming conventions:
//   - snake_case JSON tags throughout
//   - Timestamps named *_at, type time.Time (or *time.Time for nullable)
//   - Primary entity ID is just "id"; foreign references are <entity>_id
//   - Enums are lowercase strings (open, closed, drydock, working, idle, ...)
//   - Nullable scalars use *T with omitempty
//   - Empty arrays present (not omitted) — initialize as []T{} not nil
//   - All time fields use time.Time — let the JSON encoder handle RFC3339
package cliapi

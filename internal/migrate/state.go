package migrate

// This file is intentionally minimal — the migrations_applied state table
// and the SphereStore methods that read and write it live in
// internal/store (schema.go and migrations_applied.go). The migrate
// package uses those methods via store.SphereStore.
//
// Keeping the actual table implementation in the store package means:
//   - schema creation is part of the sphere store's normal migrateSphere
//     step, so the table is present as soon as the sphere DB is opened
//   - other packages that need to read applied migration state (doctor,
//     sol migrate list, sol migrate history) do so through the same
//     SphereStore interface they already use for everything else
//
// The functions in migrate.go call through to SphereStore.

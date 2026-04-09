package store

import (
	"testing"
	"time"
)

func TestRecordAndListApplied(t *testing.T) {
	ss := setupSphere(t)

	details := map[string]any{"worlds": 2.0, "agents": 5.0}
	if err := ss.RecordMigrationApplied("envoy-memory", "0.2.0", "migrated 2 worlds", details); err != nil {
		t.Fatalf("RecordMigrationApplied: %v", err)
	}

	rows, err := ss.ListAppliedMigrations()
	if err != nil {
		t.Fatalf("ListAppliedMigrations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Name != "envoy-memory" || r.Version != "0.2.0" || r.Summary != "migrated 2 worlds" {
		t.Errorf("unexpected row: %+v", r)
	}
	if r.Details["worlds"].(float64) != 2.0 || r.Details["agents"].(float64) != 5.0 {
		t.Errorf("details round-trip mismatch: %+v", r.Details)
	}
	if r.AppliedAt.IsZero() || time.Since(r.AppliedAt) > time.Minute {
		t.Errorf("AppliedAt looks wrong: %v", r.AppliedAt)
	}
}

func TestIsMigrationAppliedTrueFalse(t *testing.T) {
	ss := setupSphere(t)

	ok, err := ss.IsMigrationApplied("nope")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("IsMigrationApplied returned true for unknown migration")
	}

	if err := ss.RecordMigrationApplied("something", "0.1.0", "done", nil); err != nil {
		t.Fatal(err)
	}

	ok, err = ss.IsMigrationApplied("something")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("IsMigrationApplied returned false for recorded migration")
	}
}

func TestMigrationsAppliedIdempotentSchema(t *testing.T) {
	ss := setupSphere(t)
	// The schema migration ran once via setupSphere. Running migrateSphere
	// again should be a no-op (CREATE TABLE IF NOT EXISTS).
	if err := ss.migrateSphere(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	// Table must still be usable.
	if err := ss.RecordMigrationApplied("idem", "0.1.0", "ok", nil); err != nil {
		t.Fatalf("record after second migrate: %v", err)
	}
}

func TestListAppliedMigrationsOrdering(t *testing.T) {
	ss := setupSphere(t)

	// Insert three records with explicit applied_at values to control
	// ordering, since ListAppliedMigrations uses DESC applied_at.
	_, err := ss.db.Exec(
		`INSERT INTO migrations_applied (name, version, applied_at, summary, details) VALUES
		 ('mid',  '0.1.0', '2026-01-02T00:00:00Z', 'mid',  '{}'),
		 ('old',  '0.1.0', '2026-01-01T00:00:00Z', 'old',  '{}'),
		 ('new',  '0.1.0', '2026-01-03T00:00:00Z', 'new',  '{}')`,
	)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ss.ListAppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	want := []string{"new", "mid", "old"}
	for i, r := range rows {
		if r.Name != want[i] {
			t.Errorf("index %d: got %q want %q", i, r.Name, want[i])
		}
	}
}

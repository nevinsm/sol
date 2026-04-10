package migrations

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreMigration(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	sm := store.AppliedMigration{
		Name:      "add-labels-column",
		Version:   "v1.2.0",
		AppliedAt: now,
		Summary:   "Added labels column to writs table",
		Details:   map[string]any{"rows_affected": 42},
	}

	m := FromStoreMigration(sm)

	if m.Name != "add-labels-column" {
		t.Errorf("Name = %q, want %q", m.Name, "add-labels-column")
	}
	if m.Version != "v1.2.0" {
		t.Errorf("Version = %q, want %q", m.Version, "v1.2.0")
	}
	if !m.AppliedAt.Equal(now) {
		t.Errorf("AppliedAt = %v, want %v", m.AppliedAt, now)
	}
	if m.Summary != "Added labels column to writs table" {
		t.Errorf("Summary = %q, want %q", m.Summary, "Added labels column to writs table")
	}
}

func TestFromStoreMigrations(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rows := []store.AppliedMigration{
		{Name: "m1", Version: "v1.0.0", AppliedAt: now, Summary: "first"},
		{Name: "m2", Version: "v1.1.0", AppliedAt: now, Summary: "second"},
	}

	result := FromStoreMigrations(rows)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].Name != "m1" {
		t.Errorf("result[0].Name = %q, want %q", result[0].Name, "m1")
	}
	if result[1].Name != "m2" {
		t.Errorf("result[1].Name = %q, want %q", result[1].Name, "m2")
	}
}

func TestFromStoreMigrations_Empty(t *testing.T) {
	result := FromStoreMigrations([]store.AppliedMigration{})
	if len(result) != 0 {
		t.Fatalf("len = %d, want 0", len(result))
	}
	// Convention: present empty arrays.
	if result == nil {
		t.Error("expected non-nil empty slice")
	}
}

func TestFromMigrateStatus_Applied(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := migrate.Status{
		Migration: migrate.Migration{
			Name:    "test-migration",
			Version: "v1.0.0",
			Title:   "Test Migration",
		},
		Applied:   true,
		AppliedAt: now,
	}

	ms := FromMigrateStatus(s)

	if ms.Name != "test-migration" {
		t.Errorf("Name = %q, want %q", ms.Name, "test-migration")
	}
	if ms.Version != "v1.0.0" {
		t.Errorf("Version = %q, want %q", ms.Version, "v1.0.0")
	}
	if ms.Title != "Test Migration" {
		t.Errorf("Title = %q, want %q", ms.Title, "Test Migration")
	}
	if ms.Status != "applied" {
		t.Errorf("Status = %q, want %q", ms.Status, "applied")
	}
	if ms.AppliedAt == nil || !ms.AppliedAt.Equal(now) {
		t.Errorf("AppliedAt = %v, want %v", ms.AppliedAt, now)
	}
	if ms.Reason != "" {
		t.Errorf("Reason = %q, want empty", ms.Reason)
	}
}

func TestFromMigrateStatus_Pending(t *testing.T) {
	s := migrate.Status{
		Migration: migrate.Migration{
			Name:    "pending-migration",
			Version: "v1.0.0",
			Title:   "Pending",
		},
		Needed: true,
		Reason: "world needs upgrade",
	}

	ms := FromMigrateStatus(s)

	if ms.Status != "pending" {
		t.Errorf("Status = %q, want %q", ms.Status, "pending")
	}
	if ms.Reason != "world needs upgrade" {
		t.Errorf("Reason = %q, want %q", ms.Reason, "world needs upgrade")
	}
	if ms.AppliedAt != nil {
		t.Errorf("AppliedAt = %v, want nil", ms.AppliedAt)
	}
}

func TestFromMigrateStatus_NotNeeded(t *testing.T) {
	s := migrate.Status{
		Migration: migrate.Migration{
			Name:    "not-needed",
			Version: "v1.0.0",
			Title:   "Not Needed",
		},
		Needed: false,
		Reason: "already in target state",
	}

	ms := FromMigrateStatus(s)

	if ms.Status != "not-needed" {
		t.Errorf("Status = %q, want %q", ms.Status, "not-needed")
	}
}

func TestFromMigrateStatus_Error(t *testing.T) {
	s := migrate.Status{
		Migration: migrate.Migration{
			Name:    "error-migration",
			Version: "v1.0.0",
			Title:   "Error",
		},
		Reason: "detect error: something went wrong",
	}

	ms := FromMigrateStatus(s)

	if ms.Status != "error" {
		t.Errorf("Status = %q, want %q", ms.Status, "error")
	}
	if ms.Reason != "detect error: something went wrong" {
		t.Errorf("Reason = %q, want %q", ms.Reason, "detect error: something went wrong")
	}
}

func TestFromMigrateStatuses(t *testing.T) {
	statuses := []migrate.Status{
		{Migration: migrate.Migration{Name: "a", Version: "v1.0.0"}, Applied: true},
		{Migration: migrate.Migration{Name: "b", Version: "v1.0.0"}, Needed: true},
	}

	result := FromMigrateStatuses(statuses)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].Status != "applied" {
		t.Errorf("result[0].Status = %q, want %q", result[0].Status, "applied")
	}
	if result[1].Status != "pending" {
		t.Errorf("result[1].Status = %q, want %q", result[1].Status, "pending")
	}
}

func TestMigrationApplied_JSONShape(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	m := MigrationApplied{
		Name:      "test-migration",
		Version:   "v1.0.0",
		AppliedAt: now,
		Summary:   "Applied successfully",
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Verify snake_case keys.
	for _, key := range []string{"name", "version", "applied_at", "summary"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing expected key %q", key)
		}
	}
}

func TestMigrationStatus_JSONShape(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	ms := MigrationStatus{
		Name:      "test-migration",
		Version:   "v1.0.0",
		Title:     "Test",
		Status:    "applied",
		AppliedAt: &now,
	}

	data, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Verify snake_case keys.
	for _, key := range []string{"name", "version", "title", "status", "applied_at"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing expected key %q", key)
		}
	}
	// Reason should be omitted when empty.
	if _, ok := raw["reason"]; ok {
		t.Error("reason should be omitted when empty")
	}
}

func TestMigrationStatus_JSONOmitsNulls(t *testing.T) {
	ms := MigrationStatus{
		Name:    "test",
		Version: "v1.0.0",
		Title:   "Test",
		Status:  "pending",
		Reason:  "needs upgrade",
	}

	data, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// applied_at should be omitted when nil.
	if _, ok := raw["applied_at"]; ok {
		t.Error("applied_at should be omitted when nil")
	}
	// reason should be present when non-empty.
	if _, ok := raw["reason"]; !ok {
		t.Error("reason should be present when non-empty")
	}
}

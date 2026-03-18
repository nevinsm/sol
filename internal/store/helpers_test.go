package store

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseRFC3339(t *testing.T) {
	t.Parallel()
	t.Run("valid timestamp", func(t *testing.T) {
		t.Parallel()
		ts, err := parseRFC3339("2024-06-15T10:30:00Z", "created_at", "writ sol-abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
		if !ts.Equal(want) {
			t.Fatalf("got %v, want %v", ts, want)
		}
	})

	t.Run("invalid timestamp", func(t *testing.T) {
		t.Parallel()
		_, err := parseRFC3339("not-a-date", "created_at", "writ sol-abc")
		if err == nil {
			t.Fatal("expected error for invalid timestamp")
		}
		if !strings.Contains(err.Error(), "created_at") {
			t.Errorf("error should mention field name, got: %v", err)
		}
		if !strings.Contains(err.Error(), "writ sol-abc") {
			t.Errorf("error should mention entity ID, got: %v", err)
		}
	})
}

func TestParseOptionalRFC3339(t *testing.T) {
	t.Parallel()
	t.Run("null value", func(t *testing.T) {
		t.Parallel()
		result, err := parseOptionalRFC3339(sql.NullString{Valid: false}, "closed_at", "writ sol-abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Fatal("expected nil for null value")
		}
	})

	t.Run("valid value", func(t *testing.T) {
		t.Parallel()
		ns := sql.NullString{String: "2024-06-15T10:30:00Z", Valid: true}
		result, err := parseOptionalRFC3339(ns, "closed_at", "writ sol-abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		want := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
		if !result.Equal(want) {
			t.Fatalf("got %v, want %v", *result, want)
		}
	})

	t.Run("invalid value", func(t *testing.T) {
		t.Parallel()
		ns := sql.NullString{String: "bad-date", Valid: true}
		_, err := parseOptionalRFC3339(ns, "closed_at", "writ sol-abc")
		if err == nil {
			t.Fatal("expected error for invalid timestamp")
		}
		if !strings.Contains(err.Error(), "closed_at") {
			t.Errorf("error should mention field name, got: %v", err)
		}
	})
}

// mockResult implements sql.Result for testing checkRowsAffected.
type mockResult struct {
	rowsAffected int64
	err          error
}

func (m mockResult) LastInsertId() (int64, error) { return 0, nil }
func (m mockResult) RowsAffected() (int64, error) { return m.rowsAffected, m.err }

func TestCheckRowsAffected(t *testing.T) {
	t.Parallel()
	t.Run("one row affected", func(t *testing.T) {
		t.Parallel()
		err := checkRowsAffected(mockResult{rowsAffected: 1}, "writ", "sol-abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("zero rows — not found", func(t *testing.T) {
		t.Parallel()
		err := checkRowsAffected(mockResult{rowsAffected: 0}, "writ", "sol-abc")
		if err == nil {
			t.Fatal("expected error for zero rows")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got: %v", err)
		}
		if !strings.Contains(err.Error(), "writ") {
			t.Errorf("error should mention entity type, got: %v", err)
		}
		if !strings.Contains(err.Error(), "sol-abc") {
			t.Errorf("error should mention entity ID, got: %v", err)
		}
	})

	t.Run("driver error", func(t *testing.T) {
		t.Parallel()
		driverErr := errors.New("driver failure")
		err := checkRowsAffected(mockResult{err: driverErr}, "writ", "sol-abc")
		if err == nil {
			t.Fatal("expected error for driver failure")
		}
		if !strings.Contains(err.Error(), "rows affected") {
			t.Errorf("error should mention rows affected, got: %v", err)
		}
	})
}

func TestGeneratePrefixedID(t *testing.T) {
	t.Parallel()
	prefixes := []string{"sol-", "msg-", "mr-", "car-", "esc-", "mem-", "ah-", "tu-"}

	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			id, err := generatePrefixedID(prefix)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasPrefix(id, prefix) {
				t.Errorf("ID %q should start with %q", id, prefix)
			}
			// prefix + 16 hex chars
			wantLen := len(prefix) + 16
			if len(id) != wantLen {
				t.Errorf("ID %q length = %d, want %d", id, len(id), wantLen)
			}
		})
	}

	t.Run("uniqueness", func(t *testing.T) {
		t.Parallel()
		seen := map[string]bool{}
		for i := 0; i < 100; i++ {
			id, err := generatePrefixedID("test-")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if seen[id] {
				t.Fatalf("duplicate ID generated: %q", id)
			}
			seen[id] = true
		}
	})
}

func TestDetectCycle(t *testing.T) {
	t.Parallel()
	// Build a simple graph: A → B → C
	graph := map[string][]string{
		"A": {"B"},
		"B": {"C"},
		"C": {},
	}
	getNeighbors := func(id string) ([]string, error) {
		return graph[id], nil
	}

	t.Run("no cycle", func(t *testing.T) {
		t.Parallel()
		// Adding D → A would not create a cycle (A → B → C, no path from A to D).
		cycle, err := detectCycle(getNeighbors, "D", "A")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cycle {
			t.Error("expected no cycle")
		}
	})

	t.Run("direct cycle", func(t *testing.T) {
		t.Parallel()
		// Adding A → C: C's neighbors don't reach A, but we're checking
		// if from (A) is reachable from to (C). C → (nothing), so no cycle.
		// Actually A → B: from=A, to=B. BFS from B: B→C→(end). A not found, no cycle.
		// Let's test: Adding C → A: from=C, to=A. BFS from A: A→B→C. C == fromID. Cycle!
		cycle, err := detectCycle(getNeighbors, "C", "A")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cycle {
			t.Error("expected cycle")
		}
	})

	t.Run("self-reference", func(t *testing.T) {
		t.Parallel()
		// Adding A → A: from=A, to=A. BFS starts at A, first check: A == A. Cycle!
		cycle, err := detectCycle(getNeighbors, "A", "A")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cycle {
			t.Error("expected cycle for self-reference")
		}
	})

	t.Run("transitive cycle", func(t *testing.T) {
		t.Parallel()
		// Adding C → B: from=C, to=B. BFS from B: B→C. C == fromID. Cycle!
		cycle, err := detectCycle(getNeighbors, "C", "B")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cycle {
			t.Error("expected transitive cycle")
		}
	})

	t.Run("error propagation", func(t *testing.T) {
		t.Parallel()
		failNeighbors := func(id string) ([]string, error) {
			return nil, errors.New("db error")
		}
		_, err := detectCycle(failNeighbors, "A", "B")
		if err == nil {
			t.Fatal("expected error to propagate")
		}
	})

	t.Run("disconnected nodes", func(t *testing.T) {
		t.Parallel()
		// X and Y are not in the graph at all.
		cycle, err := detectCycle(getNeighbors, "X", "Y")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cycle {
			t.Error("expected no cycle for disconnected nodes")
		}
	})
}

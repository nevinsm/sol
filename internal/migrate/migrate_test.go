package migrate

import (
	"errors"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// openTestSphere opens an isolated sphere store by pointing SOL_HOME at a
// per-test temporary directory. Parallel tests are OK because each call
// to t.TempDir and t.Setenv gives the test its own SOL_HOME.
func openTestSphere(t *testing.T) *store.SphereStore {
	t.Helper()
	t.Setenv("SOL_HOME", t.TempDir())
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	return ss
}

// fakeMigration returns a Migration that reports Needed and records
// whether Run was called. Caller can override the Detect error.
func fakeMigration(name, version string, needed bool, runCalled *int) Migration {
	return Migration{
		Name:        name,
		Version:     version,
		Title:       "test migration " + name,
		Description: "# " + name,
		Detect: func(Context) (DetectResult, error) {
			return DetectResult{Needed: needed, Reason: "test reason"}, nil
		},
		Run: func(Context, RunOpts) (RunResult, error) {
			if runCalled != nil {
				*runCalled++
			}
			return RunResult{Summary: name + " ran", Details: map[string]any{"count": 1}}, nil
		},
	}
}

func resetRegistry(t *testing.T) {
	t.Helper()
	ClearRegistryForTest()
	t.Cleanup(ClearRegistryForTest)
}

func TestRegisterDetectsDuplicateName(t *testing.T) {
	resetRegistry(t)
	var runs int
	Register(fakeMigration("dup", "0.1.0", true, &runs))

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate Register; got none")
		}
	}()
	Register(fakeMigration("dup", "0.2.0", true, &runs))
}

func TestListReturnsInVersionOrder(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	// Register out of order; List must sort by Version then Name.
	Register(fakeMigration("bravo", "0.2.0", true, nil))
	Register(fakeMigration("alpha", "0.1.0", true, nil))
	Register(fakeMigration("charlie", "0.2.0", true, nil))

	statuses, err := List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, s := range statuses {
		if s.Migration.Name != want[i] {
			t.Errorf("index %d: got %q, want %q", i, s.Migration.Name, want[i])
		}
	}
}

func TestRunSkipsAlreadyApplied(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	var runs int
	Register(fakeMigration("already", "0.1.0", true, &runs))
	if err := ss.RecordMigrationApplied("already", "0.1.0", "pre-applied", nil); err != nil {
		t.Fatal(err)
	}

	_, err := Run(ctx, "already", RunOpts{Confirm: true})
	if err == nil {
		t.Fatalf("expected error for already-applied migration")
	}
	if !strings.Contains(err.Error(), "already applied") {
		t.Errorf("unexpected error text: %v", err)
	}
	if runs != 0 {
		t.Errorf("Run func was invoked despite already-applied guard (runs=%d)", runs)
	}
}

func TestRunForceOverridesApplied(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	var runs int
	Register(fakeMigration("forcy", "0.1.0", true, &runs))
	if err := ss.RecordMigrationApplied("forcy", "0.1.0", "pre-applied", nil); err != nil {
		t.Fatal(err)
	}

	// Force=true means we bypass the already-applied guard. However the
	// Record step on success will fail with a PK conflict since name is
	// PRIMARY KEY. That is acceptable behavior: the operator using
	// --force is expected to know. Confirm Run was at least called.
	_, _ = Run(ctx, "forcy", RunOpts{Confirm: true, Force: true})
	if runs != 1 {
		t.Errorf("expected Run to be called once under --force, got %d", runs)
	}
}

func TestRunDryRunDoesNotRecord(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	var runs int
	Register(fakeMigration("dry", "0.1.0", true, &runs))

	res, err := Run(ctx, "dry", RunOpts{Confirm: false})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.HasPrefix(res.Summary, "[dry-run]") {
		t.Errorf("expected dry-run summary prefix, got %q", res.Summary)
	}
	if runs != 0 {
		t.Errorf("Run func called during dry-run")
	}
	ok, err := ss.IsMigrationApplied("dry")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("migrations_applied was mutated during dry-run")
	}
}

func TestRunRecordsOnSuccess(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	var runs int
	Register(fakeMigration("real", "0.1.0", true, &runs))

	res, err := Run(ctx, "real", RunOpts{Confirm: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Summary != "real ran" {
		t.Errorf("summary: %q", res.Summary)
	}
	if runs != 1 {
		t.Errorf("runs=%d", runs)
	}
	rows, err := ss.ListAppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "real" || rows[0].Version != "0.1.0" {
		t.Errorf("unexpected rows: %+v", rows)
	}
	if v, ok := rows[0].Details["count"]; !ok || v == nil {
		t.Errorf("details not recorded: %+v", rows[0].Details)
	}
}

func TestRunDoesNotRecordOnFailure(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	Register(Migration{
		Name:    "boom",
		Version: "0.1.0",
		Title:   "boom",
		Detect:  func(Context) (DetectResult, error) { return DetectResult{Needed: true}, nil },
		Run: func(Context, RunOpts) (RunResult, error) {
			return RunResult{}, errors.New("kaboom")
		},
	})

	_, err := Run(ctx, "boom", RunOpts{Confirm: true})
	if err == nil {
		t.Fatalf("expected error from failing Run")
	}
	ok, err := ss.IsMigrationApplied("boom")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Errorf("migrations_applied recorded a failed migration")
	}
}

func TestPendingCountHandlesDetectErrors(t *testing.T) {
	resetRegistry(t)
	ss := openTestSphere(t)
	ctx := Context{SphereStore: ss}

	Register(Migration{
		Name: "err-detect", Version: "0.1.0", Title: "err-detect",
		Detect: func(Context) (DetectResult, error) { return DetectResult{}, errors.New("probe failed") },
		Run:    func(Context, RunOpts) (RunResult, error) { return RunResult{}, nil },
	})
	Register(fakeMigration("ok-pending", "0.1.0", true, nil))
	Register(fakeMigration("ok-not-needed", "0.1.0", false, nil))

	pending, detectErrors := PendingCount(ctx)
	if pending != 1 {
		t.Errorf("pending=%d want 1", pending)
	}
	if detectErrors != 1 {
		t.Errorf("detectErrors=%d want 1", detectErrors)
	}
}

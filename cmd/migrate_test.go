package cmd

import (
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

// captureStdout is defined in cost_test.go; reused here.

func setupMigrateHome(t *testing.T) {
	t.Helper()
	t.Setenv("SOL_HOME", t.TempDir())
	migrate.ClearRegistryForTest()
	t.Cleanup(migrate.ClearRegistryForTest)
	// Reset the run flags between tests.
	t.Cleanup(func() {
		migrateRunConfirm = false
		migrateRunForce = false
		migrateRunWorld = ""
		migrateListJSON = false
		migrateHistoryJSON = false
	})
}

func registerFake(name, version string, needed bool) {
	migrate.Register(migrate.Migration{
		Name: name, Version: version, Title: "fake " + name,
		Description: "# " + name,
		Detect:      func(migrate.Context) (migrate.DetectResult, error) { return migrate.DetectResult{Needed: needed, Reason: "test"}, nil },
		Run: func(migrate.Context, migrate.RunOpts) (migrate.RunResult, error) {
			return migrate.RunResult{Summary: name + " done", Details: map[string]any{"k": "v"}}, nil
		},
	})
}

func TestMigrateListShowsPendingAndApplied(t *testing.T) {
	setupMigrateHome(t)
	registerFake("pending-one", "0.1.0", true)
	registerFake("applied-one", "0.1.0", true)

	// Pre-apply one via the store.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.RecordMigrationApplied("applied-one", "0.1.0", "pre-applied", nil); err != nil {
		t.Fatal(err)
	}
	ss.Close()

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"migrate", "list"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("migrate list: %v", err)
		}
	})

	if !strings.Contains(out, "NAME") || !strings.Contains(out, "VERSION") || !strings.Contains(out, "STATUS") {
		t.Errorf("missing header columns in output: %s", out)
	}
	if !strings.Contains(out, "applied-one") || !strings.Contains(out, "applied") {
		t.Errorf("expected applied-one as applied, got: %s", out)
	}
	if !strings.Contains(out, "pending-one") || !strings.Contains(out, "pending") {
		t.Errorf("expected pending-one as pending, got: %s", out)
	}
}

func TestMigrateRunRequiresConfirm(t *testing.T) {
	setupMigrateHome(t)
	registerFake("needs-confirm", "0.1.0", true)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"migrate", "run", "needs-confirm"})
		err := rootCmd.Execute()
		if ExitCode(err) != 1 {
			t.Errorf("expected exit 1 for dry-run, got %d (err=%v)", ExitCode(err), err)
		}
	})

	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run marker, got: %s", out)
	}

	// Confirm nothing was recorded.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	ok, _ := ss.IsMigrationApplied("needs-confirm")
	if ok {
		t.Errorf("dry-run recorded state")
	}
}

func TestMigrateRunConfirmsAndRecords(t *testing.T) {
	setupMigrateHome(t)
	registerFake("for-real", "0.1.0", true)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"migrate", "run", "for-real", "--confirm"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("migrate run --confirm: %v", err)
		}
	})
	if !strings.Contains(out, "for-real done") {
		t.Errorf("expected success summary in output, got: %s", out)
	}

	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	ok, err := ss.IsMigrationApplied("for-real")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("expected migration to be recorded after --confirm")
	}
}

func TestMigrateHistoryShowsAppliedOrder(t *testing.T) {
	setupMigrateHome(t)

	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	// Insert two rows with explicit applied_at to control order.
	if _, err := ss.DB().Exec(
		`INSERT INTO migrations_applied (name, version, applied_at, summary, details) VALUES
		 ('older', '0.1.0', '2026-01-01T00:00:00Z', 'older summary', '{}'),
		 ('newer', '0.2.0', '2026-02-01T00:00:00Z', 'newer summary', '{}')`,
	); err != nil {
		t.Fatal(err)
	}
	ss.Close()

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"migrate", "history"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("migrate history: %v", err)
		}
	})

	newerIdx := strings.Index(out, "newer")
	olderIdx := strings.Index(out, "older")
	if newerIdx < 0 || olderIdx < 0 {
		t.Fatalf("expected both entries in output, got: %s", out)
	}
	if newerIdx > olderIdx {
		t.Errorf("expected newer before older in history output, got:\n%s", out)
	}
}

func TestMigrateListEmpty(t *testing.T) {
	setupMigrateHome(t)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"migrate", "list"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("migrate list: %v", err)
		}
	})

	if !strings.Contains(out, "No migrations registered") {
		t.Errorf("expected empty-table message, got: %s", out)
	}
}

func TestMigrateRunUnknownName(t *testing.T) {
	setupMigrateHome(t)

	rootCmd.SetArgs([]string{"migrate", "run", "does-not-exist"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected error for unknown migration")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("expected 'not registered' in error, got: %v", err)
	}
}

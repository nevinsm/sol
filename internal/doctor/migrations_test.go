package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

func openDoctorSphere(t *testing.T) *store.SphereStore {
	t.Helper()
	t.Setenv("SOL_HOME", t.TempDir())
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere: %v", err)
	}
	t.Cleanup(func() { ss.Close() })
	return ss
}

func TestCheckMigrationsNoPending(t *testing.T) {
	migrate.ClearRegistryForTest()
	t.Cleanup(migrate.ClearRegistryForTest)
	ss := openDoctorSphere(t)

	res := checkMigrationsWith(migrate.Context{SphereStore: ss})
	if !res.Passed || res.Warning {
		t.Errorf("expected pass w/o warning, got %+v", res)
	}
	if res.Name != "migrations" {
		t.Errorf("Name=%q", res.Name)
	}
}

func TestCheckMigrationsPendingReturnsWarn(t *testing.T) {
	migrate.ClearRegistryForTest()
	t.Cleanup(migrate.ClearRegistryForTest)
	ss := openDoctorSphere(t)

	migrate.Register(migrate.Migration{
		Name: "needs-run", Version: "0.2.0", Title: "needs-run",
		Detect: func(migrate.Context) (migrate.DetectResult, error) {
			return migrate.DetectResult{Needed: true, Reason: "applicable"}, nil
		},
		Run: func(migrate.Context, migrate.RunOpts) (migrate.RunResult, error) { return migrate.RunResult{}, nil },
	})

	res := checkMigrationsWith(migrate.Context{SphereStore: ss})
	if !res.Passed {
		t.Errorf("expected Passed=true (warning does not block), got %+v", res)
	}
	if !res.Warning {
		t.Errorf("expected Warning=true for pending migration, got %+v", res)
	}
	if !strings.Contains(res.Message, "pending") || !strings.Contains(res.Message, "sol migrate list") {
		t.Errorf("message should mention pending and sol migrate list: %q", res.Message)
	}
}

func TestCheckMigrationsDetectErrorReturnsWarn(t *testing.T) {
	migrate.ClearRegistryForTest()
	t.Cleanup(migrate.ClearRegistryForTest)
	ss := openDoctorSphere(t)

	migrate.Register(migrate.Migration{
		Name: "flaky", Version: "0.2.0", Title: "flaky",
		Detect: func(migrate.Context) (migrate.DetectResult, error) { return migrate.DetectResult{}, errors.New("probe failed") },
		Run:    func(migrate.Context, migrate.RunOpts) (migrate.RunResult, error) { return migrate.RunResult{}, nil },
	})

	res := checkMigrationsWith(migrate.Context{SphereStore: ss})
	if !res.Passed || !res.Warning {
		t.Errorf("expected pass+warning for detect error, got %+v", res)
	}
	if !strings.Contains(res.Message, "could not be checked") {
		t.Errorf("message should call out unchecked migrations: %q", res.Message)
	}
}

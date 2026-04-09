package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/migrate"
	"github.com/nevinsm/sol/internal/store"
)

// CheckMigrations reports pending migrations registered via the
// internal/migrate package. Pending migrations are a warning, not a
// failure: sol still runs, but the operator should know that manual
// action is available. See the migrations_applied state table and the
// migrate.PendingCount function.
//
// If SOL_HOME does not exist yet (fresh install, no sphere store), the
// check passes silently: there is nothing to check.
func CheckMigrations() CheckResult {
	home := config.Home()
	// Skip silently if SOL_HOME or sphere.db does not exist yet. A fresh
	// install has no migrations to run, and opening/creating the sphere
	// store from a read-only doctor check would be surprising.
	spherePath := filepath.Join(config.StoreDir(), "sphere.db")
	if _, err := os.Stat(spherePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CheckResult{
				Name:    "migrations",
				Passed:  true,
				Message: "no sphere store yet (skipped)",
			}
		}
		return CheckResult{
			Name:    "migrations",
			Passed:  false,
			Message: fmt.Sprintf("cannot stat sphere.db: %v", err),
			Fix:     "check permissions on " + home,
		}
	}

	ss, err := store.OpenSphere()
	if err != nil {
		return CheckResult{
			Name:    "migrations",
			Passed:  false,
			Message: fmt.Sprintf("failed to open sphere store: %v", err),
			Fix:     "run 'sol doctor' after resolving the sphere store error",
		}
	}
	defer ss.Close()

	ctx := migrate.Context{SolHome: home, SphereStore: ss}
	return checkMigrationsWith(ctx)
}

// checkMigrationsWith is the testable core: it accepts a migrate.Context
// so tests can inject a temporary sphere store without hitting SOL_HOME.
func checkMigrationsWith(ctx migrate.Context) CheckResult {
	pending, detectErrors := migrate.PendingCount(ctx)

	switch {
	case pending > 0 && detectErrors > 0:
		return CheckResult{
			Name:    "migrations",
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("%d pending, %d could not be checked — run 'sol migrate list' to see them", pending, detectErrors),
		}
	case pending > 0:
		return CheckResult{
			Name:    "migrations",
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("%d pending migration(s) — run 'sol migrate list' to see them", pending),
		}
	case detectErrors > 0:
		return CheckResult{
			Name:    "migrations",
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf("%d migration(s) could not be checked; investigate", detectErrors),
		}
	default:
		return CheckResult{
			Name:    "migrations",
			Passed:  true,
			Message: "no pending migrations",
		}
	}
}

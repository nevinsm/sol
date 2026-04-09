// Package migrate provides sol's first-class, discoverable migration system.
//
// A Migration is a registered upgrade step that shifts an existing sol
// installation from one state to another. Migrations are registered via
// Register() from init() functions in the internal/migrate/migrations
// subpackage, so importing that package (typically for side effects from
// cmd/root.go) is what makes them discoverable at runtime.
//
// The framework is forward-only: there is no rollback. Each migration is
// expected to be idempotent so re-running is always safe.
//
// Pending migrations are surfaced to operators via `sol doctor` and the
// banner printed by `sol up`, so operators see breaking changes the moment
// they matter rather than discovering them as mysterious failures.
package migrate

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

// Migration is a registered upgrade step that shifts an existing sol
// installation from one state to another. Each migration is registered
// via Register() in an init() from the migrations subpackage.
type Migration struct {
	// Name is a stable identifier, domain-based ("envoy-memory",
	// "caravan-rename", etc.). Do NOT version-prefix — Version is a
	// separate field.
	Name string
	// Version is the semantic version in which this migration was
	// introduced ("0.2.0"). Used for ordering and recorded at apply time.
	Version string
	// Title is a short human-readable title for list output.
	Title string
	// Description is markdown: what it does, why, operator pre/post
	// conditions, safety notes.
	Description string
	// Detect reports whether the migration is applicable to the current
	// sphere. A migration may be "not needed" because it was already
	// applied (state table) or because the sphere does not have the
	// preconditions (e.g. no envoys with briefs).
	Detect func(ctx Context) (DetectResult, error)
	// Run executes the migration. It must be idempotent — on failure,
	// nothing is recorded in the migrations_applied state table, and the
	// operator may re-run.
	Run func(ctx Context, opts RunOpts) (RunResult, error)
}

// DetectResult captures whether a migration is applicable to the current
// sphere.
type DetectResult struct {
	Needed bool
	Reason string // human-readable explanation surfaced in sol migrate list
}

// Context carries environment data that migration functions need. Passed
// by value; migrations should not retain the Context beyond the call.
type Context struct {
	SolHome     string
	SphereStore *store.SphereStore
	Logger      *slog.Logger
}

// RunOpts controls migration execution.
type RunOpts struct {
	Confirm bool   // required to mutate; without it, Run is a dry-run
	Force   bool   // override guardrails (e.g. already-applied detection)
	World   string // optional: scope a migration to a single world where applicable
}

// RunResult is returned by Migration.Run and recorded in migrations_applied.
type RunResult struct {
	// Summary is a short human-readable summary for history output.
	Summary string
	// Details is structured data (per-world counts, affected agents, etc.)
	// serialized to JSON for storage.
	Details map[string]any
}

// Status is the output shape for List.
type Status struct {
	Migration Migration
	Applied   bool      // present in migrations_applied table
	Needed    bool      // from Detect, only meaningful when !Applied
	Reason    string    // from Detect
	AppliedAt time.Time // only when Applied is true
}

// ——— Registry ———

var (
	registryMu sync.RWMutex
	registry   = map[string]Migration{}
)

// Register adds a migration to the global registry. Intended to be called
// from init() in the internal/migrate/migrations subpackage. Panics on
// duplicate name — duplicate names are a programmer error.
func Register(m Migration) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[m.Name]; exists {
		panic(fmt.Sprintf("migrate: duplicate migration name %q", m.Name))
	}
	if m.Name == "" {
		panic("migrate: migration name must not be empty")
	}
	if m.Version == "" {
		panic(fmt.Sprintf("migrate: migration %q has empty Version", m.Name))
	}
	if m.Detect == nil {
		panic(fmt.Sprintf("migrate: migration %q has nil Detect", m.Name))
	}
	if m.Run == nil {
		panic(fmt.Sprintf("migrate: migration %q has nil Run", m.Name))
	}
	registry[m.Name] = m
}

// ClearRegistryForTest wipes the registry. Test-only helper — never call
// from non-test code. Exported so external packages (cmd, doctor) can
// isolate their tests from migrations registered by other packages.
func ClearRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = map[string]Migration{}
}

// all returns a snapshot of registered migrations, sorted by Version then
// Name for deterministic ordering.
func all() []Migration {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Migration, 0, len(registry))
	for _, m := range registry {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Version != out[j].Version {
			return out[i].Version < out[j].Version
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get returns a migration by name, or false if not registered.
func Get(name string) (Migration, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	m, ok := registry[name]
	return m, ok
}

// List returns all registered migrations with their current status
// (applied / pending / not-applicable), in Version order with Name as
// tiebreaker.
func List(ctx Context) ([]Status, error) {
	migrations := all()
	applied, err := loadAppliedMap(ctx.SphereStore)
	if err != nil {
		return nil, fmt.Errorf("migrate: failed to load applied migrations: %w", err)
	}
	out := make([]Status, 0, len(migrations))
	for _, m := range migrations {
		s := Status{Migration: m}
		if am, ok := applied[m.Name]; ok {
			s.Applied = true
			s.AppliedAt = am.AppliedAt
		} else {
			dr, derr := m.Detect(ctx)
			if derr != nil {
				s.Reason = "detect error: " + derr.Error()
			} else {
				s.Needed = dr.Needed
				s.Reason = dr.Reason
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// Run executes a named migration. Without opts.Confirm it is a dry-run:
// Detect is called and its Reason returned via RunResult.Summary, but the
// migration's own Run is not invoked and no state is recorded.
//
// With opts.Confirm, Run executes the migration and records the result in
// the migrations_applied state table on success. On failure, nothing is
// recorded — migrations must be idempotent so re-running is safe.
//
// opts.Force bypasses the "already applied" guard, but not the migration's
// own Detect.Needed guard (that is the migration's own safety).
func Run(ctx Context, name string, opts RunOpts) (RunResult, error) {
	m, ok := Get(name)
	if !ok {
		return RunResult{}, fmt.Errorf("migrate: migration %q is not registered", name)
	}

	// Already-applied guard.
	if !opts.Force && ctx.SphereStore != nil {
		applied, err := ctx.SphereStore.IsMigrationApplied(name)
		if err != nil {
			return RunResult{}, fmt.Errorf("migrate: failed to check applied status: %w", err)
		}
		if applied {
			return RunResult{}, fmt.Errorf("migrate: migration %q already applied (use --force to override)", name)
		}
	}

	// Detect always runs — it is the migration's own safety.
	dr, err := m.Detect(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("migrate: detect for %q failed: %w", name, err)
	}

	// Dry-run short-circuit.
	if !opts.Confirm {
		summary := dr.Reason
		if summary == "" {
			if dr.Needed {
				summary = "migration is applicable; re-run with --confirm to apply"
			} else {
				summary = "migration is not applicable; nothing to do"
			}
		}
		return RunResult{Summary: "[dry-run] " + summary}, nil
	}

	// Execute. On error, do not record.
	res, err := m.Run(ctx, opts)
	if err != nil {
		return res, fmt.Errorf("migrate: run %q failed: %w", name, err)
	}

	// Record on success.
	if ctx.SphereStore != nil {
		if rerr := ctx.SphereStore.RecordMigrationApplied(m.Name, m.Version, res.Summary, res.Details); rerr != nil {
			return res, fmt.Errorf("migrate: record applied for %q: %w", name, rerr)
		}
	}
	return res, nil
}

// PendingCount returns the number of registered migrations whose Detect
// reports Needed=true. Used by sol doctor and sol up to surface a warning.
// Detect errors are counted as "unknown" and logged on ctx.Logger when set,
// but do not cause PendingCount to fail.
func PendingCount(ctx Context) (pending int, detectErrors int) {
	migrations := all()
	applied, err := loadAppliedMap(ctx.SphereStore)
	if err != nil {
		// Can't tell applied vs pending — report all as errors so the
		// operator is made aware rather than silently reporting zero.
		if ctx.Logger != nil {
			ctx.Logger.Warn("migrate: failed to load applied migrations", "err", err)
		}
		return 0, len(migrations)
	}
	for _, m := range migrations {
		if _, ok := applied[m.Name]; ok {
			continue
		}
		dr, derr := m.Detect(ctx)
		if derr != nil {
			if ctx.Logger != nil {
				ctx.Logger.Warn("migrate: detect failed", "migration", m.Name, "err", derr)
			}
			detectErrors++
			continue
		}
		if dr.Needed {
			pending++
		}
	}
	return pending, detectErrors
}

// loadAppliedMap reads the migrations_applied table and returns it keyed
// by Name. Returns an empty map if the store is nil.
func loadAppliedMap(ss *store.SphereStore) (map[string]store.AppliedMigration, error) {
	if ss == nil {
		return map[string]store.AppliedMigration{}, nil
	}
	rows, err := ss.ListAppliedMigrations()
	if err != nil {
		return nil, err
	}
	out := make(map[string]store.AppliedMigration, len(rows))
	for _, r := range rows {
		out[r.Name] = r
	}
	return out, nil
}

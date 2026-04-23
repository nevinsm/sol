package integration

// infra_test.go — Integration tests for infrastructure/schema management CLI
// commands that previously had no integration coverage:
//   sol service  (cmd/service.go) — system service lifecycle
//   sol migrate  (cmd/migrate.go) — migration framework
//   sol schema   (cmd/schema.go)  — database schema management
//
// All tests use setupTestEnv() from helpers_test.go for isolation. None of
// these tests spawn a real claude process or touch the real tmux server.

import (
	"encoding/json"
	"strings"
	"testing"

	clischema "github.com/nevinsm/sol/internal/cliapi/schema"
)

// ---------- sol service ----------

func TestCLIServiceHelpSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "service", "--help")
	if err != nil {
		t.Fatalf("sol service --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "service") {
		t.Errorf("service help missing 'service': %s", out)
	}
	// Verify all subcommands are listed.
	for _, sub := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		if !strings.Contains(out, sub) {
			t.Errorf("service help missing subcommand %q: %s", sub, out)
		}
	}
}

func TestCLIServiceSubcommandHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// Each subcommand's --help should succeed and mention --json.
	for _, sub := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		t.Run(sub, func(t *testing.T) {
			out, err := runGT(t, gtHome, "service", sub, "--help")
			if err != nil {
				t.Fatalf("sol service %s --help failed: %v: %s", sub, err, out)
			}
			if !strings.Contains(out, "--json") {
				t.Errorf("sol service %s --help missing --json flag: %s", sub, out)
			}
		})
	}
}

func TestCLIServiceStatusLongDescription(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "service", "status", "--help")
	if err != nil {
		t.Fatalf("sol service status --help failed: %v: %s", err, out)
	}
	// The Long description documents exit codes — verify it's surfaced.
	if !strings.Contains(out, "Exit codes") {
		t.Errorf("service status help missing exit code documentation: %s", out)
	}
}

func TestCLIServiceRejectsExtraArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// All service subcommands use cobra.NoArgs — passing an argument should fail.
	for _, sub := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		t.Run(sub, func(t *testing.T) {
			out, err := runGT(t, gtHome, "service", sub, "bogus-arg")
			if err == nil {
				t.Fatalf("sol service %s with extra arg should fail, got: %s", sub, out)
			}
			if !strings.Contains(out, "unknown") && !strings.Contains(out, "arg") {
				// Cobra's NoArgs error message mentions "unknown command" or
				// similar — we just verify the command rejected the argument.
				t.Logf("sol service %s error output: %s", sub, out)
			}
		})
	}
}

// ---------- sol migrate ----------

func TestCLIMigrateHelpSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "migrate", "--help")
	if err != nil {
		t.Fatalf("sol migrate --help failed: %v: %s", err, out)
	}
	// Verify all subcommands are mentioned.
	for _, sub := range []string{"list", "show", "run", "history"} {
		if !strings.Contains(out, sub) {
			t.Errorf("migrate help missing subcommand %q: %s", sub, out)
		}
	}
}

func TestCLIMigrateListHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	// With a fresh sphere store, list should succeed and show the
	// envoy-memory migration (registered via import side effects).
	out, err := runGT(t, gtHome, "migrate", "list")
	if err != nil {
		t.Fatalf("sol migrate list failed: %v: %s", err, out)
	}
	// The envoy-memory migration is always registered.
	if !strings.Contains(out, "envoy-memory") {
		t.Errorf("expected envoy-memory in list output, got: %s", out)
	}
	// Table header should be present.
	if !strings.Contains(out, "NAME") {
		t.Errorf("expected table header NAME in list output, got: %s", out)
	}
}

func TestCLIMigrateListJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "migrate", "list", "--json")
	if err != nil {
		t.Fatalf("sol migrate list --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol migrate list --json produced invalid JSON: %s", out)
	}
	// Verify it's an array with at least one entry.
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("failed to parse list JSON: %v: %s", err, out)
	}
	if len(entries) == 0 {
		t.Errorf("expected at least one migration in list, got empty array")
	}
}

func TestCLIMigrateShowHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// show a known migration — envoy-memory is always registered.
	out, err := runGT(t, gtHome, "migrate", "show", "envoy-memory")
	if err != nil {
		t.Fatalf("sol migrate show envoy-memory failed: %v: %s", err, out)
	}
	// Should print the description or title.
	if len(out) == 0 {
		t.Errorf("expected non-empty show output for envoy-memory")
	}
}

func TestCLIMigrateShowUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "migrate", "show", "nonexistent-migration")
	if err == nil {
		t.Fatalf("expected error for unknown migration, got: %s", out)
	}
	if !strings.Contains(out, "not registered") {
		t.Errorf("expected 'not registered' in error, got: %s", out)
	}
}

func TestCLIMigrateShowMissingArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// show requires exactly 1 argument.
	_, err := runGT(t, gtHome, "migrate", "show")
	if err == nil {
		t.Fatal("expected error for missing migration name argument")
	}
}

func TestCLIMigrateRunDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	// Default (no --confirm) is dry-run which exits 1.
	out, err := runGT(t, gtHome, "migrate", "run", "envoy-memory")
	if err == nil {
		t.Fatalf("expected dry-run exit code 1, got success: %s", out)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected 'dry-run' in output, got: %s", out)
	}
}

func TestCLIMigrateRunDryRunJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "migrate", "run", "envoy-memory", "--json")
	if err == nil {
		t.Fatalf("expected dry-run exit code 1 in JSON mode, got success: %s", out)
	}
	// Even on exit 1, combined output should contain valid JSON.
	if !json.Valid([]byte(out)) {
		t.Errorf("sol migrate run --json dry-run produced invalid JSON: %s", out)
	}
}

func TestCLIMigrateRunUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "migrate", "run", "nonexistent-migration")
	if err == nil {
		t.Fatalf("expected error for unknown migration, got: %s", out)
	}
	if !strings.Contains(out, "not registered") {
		t.Errorf("expected 'not registered' in error, got: %s", out)
	}
}

func TestCLIMigrateRunMissingArg(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// run requires exactly 1 argument.
	_, err := runGT(t, gtHome, "migrate", "run")
	if err == nil {
		t.Fatal("expected error for missing migration name argument")
	}
}

func TestCLIMigrateHistoryEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "migrate", "history")
	if err != nil {
		t.Fatalf("sol migrate history failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No migrations applied") {
		t.Errorf("expected empty-state message, got: %s", out)
	}
}

func TestCLIMigrateHistoryJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	out, err := runGT(t, gtHome, "migrate", "history", "--json")
	if err != nil {
		t.Fatalf("sol migrate history --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol migrate history --json produced invalid JSON: %s", out)
	}
}

// ---------- sol schema ----------

func TestCLISchemaHelpSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "schema", "--help")
	if err != nil {
		t.Fatalf("sol schema --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "status") {
		t.Errorf("schema help missing 'status' subcommand: %s", out)
	}
	if !strings.Contains(out, "migrate") {
		t.Errorf("schema help missing 'migrate' subcommand: %s", out)
	}
}

func TestCLISchemaStatusHappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)

	// After setupTestEnv, there's a .store directory but the sphere DB
	// may or may not exist. The command should not panic either way.
	out, err := runGT(t, gtHome, "schema", "status")
	if err != nil {
		t.Fatalf("sol schema status failed: %v: %s", err, out)
	}
	// Output should mention "Sphere database".
	if !strings.Contains(out, "Sphere") {
		t.Errorf("expected 'Sphere' in schema status output, got: %s", out)
	}
}

func TestCLISchemaStatusWithWorld(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "schema", "status")
	if err != nil {
		t.Fatalf("sol schema status failed: %v: %s", err, out)
	}
	// Should show both sphere and world databases.
	if !strings.Contains(out, "Sphere") {
		t.Errorf("expected 'Sphere' in output, got: %s", out)
	}
	if !strings.Contains(out, "ember") {
		t.Errorf("expected world name 'ember' in output, got: %s", out)
	}
	if !strings.Contains(out, "(current)") {
		t.Errorf("expected '(current)' version status, got: %s", out)
	}
}

func TestCLISchemaStatusJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "schema", "status", "--json")
	if err != nil {
		t.Fatalf("sol schema status --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol schema status --json produced invalid JSON: %s", out)
	}

	var entries []clischema.StatusEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("failed to parse schema status JSON: %v: %s", err, out)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries (sphere + world), got %d", len(entries))
	}

	// Verify sphere entry comes first and has correct type.
	if entries[0].Type != "sphere" {
		t.Errorf("expected first entry type 'sphere', got %q", entries[0].Type)
	}
	if entries[0].Database != "sphere" {
		t.Errorf("expected first entry database 'sphere', got %q", entries[0].Database)
	}
	if entries[0].Version == 0 && entries[0].Error == "" {
		t.Errorf("expected non-zero version or error for sphere entry")
	}

	// Verify world entry.
	found := false
	for _, e := range entries {
		if e.Database == "ember" {
			found = true
			if e.Type != "world" {
				t.Errorf("expected type 'world' for ember, got %q", e.Type)
			}
			if e.Status != "(current)" {
				t.Errorf("expected status '(current)' for ember, got %q", e.Status)
			}
		}
	}
	if !found {
		t.Errorf("expected entry for world 'ember' in JSON output")
	}
}

func TestCLISchemaMigratePreview(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// Default (no --confirm) is preview mode which exits 1.
	out, err := runGT(t, gtHome, "schema", "migrate")
	if err == nil {
		t.Fatalf("expected preview exit code 1, got success: %s", out)
	}
	if !strings.Contains(out, "--confirm") {
		t.Errorf("expected '--confirm' hint in preview output, got: %s", out)
	}
}

func TestCLISchemaMigrateConfirm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// With --confirm, databases are already at current version, so should
	// succeed with "(current)" messages.
	out, err := runGT(t, gtHome, "schema", "migrate", "--confirm")
	if err != nil {
		t.Fatalf("sol schema migrate --confirm failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "(current)") || !strings.Contains(out, "current") {
		t.Errorf("expected 'current' status in migrate output, got: %s", out)
	}
}

func TestCLISchemaMigrateJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// JSON with --confirm should produce valid JSON.
	out, err := runGT(t, gtHome, "schema", "migrate", "--confirm", "--json")
	if err != nil {
		t.Fatalf("sol schema migrate --confirm --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("sol schema migrate --confirm --json produced invalid JSON: %s", out)
	}

	var resp clischema.MigrateResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("failed to parse migrate response: %v: %s", err, out)
	}
	if len(resp.AppliedMigrations) < 2 {
		t.Fatalf("expected at least 2 migration results (sphere + world), got %d", len(resp.AppliedMigrations))
	}

	// Verify all entries report "current" since we just initialized.
	for _, m := range resp.AppliedMigrations {
		if m.Status != "current" {
			t.Errorf("expected status 'current' for %s, got %q", m.Database, m.Status)
		}
	}
}

func TestCLISchemaMigratePreviewJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome, _ := setupTestEnv(t)
	initWorld(t, gtHome, "ember")

	// In JSON mode the output is returned before the dry-run exit-code
	// path, so preview with --json exits 0 when all databases are current.
	out, err := runGT(t, gtHome, "schema", "migrate", "--json")
	if err != nil {
		t.Fatalf("sol schema migrate --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("sol schema migrate --json preview produced invalid JSON: %s", out)
	}

	var resp clischema.MigrateResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("failed to parse preview JSON: %v: %s", err, out)
	}
	if len(resp.AppliedMigrations) < 2 {
		t.Errorf("expected at least 2 entries, got %d", len(resp.AppliedMigrations))
	}
}

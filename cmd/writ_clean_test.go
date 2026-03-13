package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// setupCleanTest creates a temporary SOL_HOME with a world and returns the
// store, world name, and the SOL_HOME directory. The world is created with
// world.toml so RequireWorld passes.
func setupCleanTest(t *testing.T) (*store.WorldStore, string, string) {
	t.Helper()

	// Reset package-level flag vars to avoid cross-test pollution.
	cleanOlderThan = ""
	cleanDryRun = false

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "cleantest"
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create world.toml so RequireWorld passes.
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.OpenWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, world, dir
}

// createClosedWritWithAge creates a writ and closes it, then backdates closed_at.
func createClosedWritWithAge(t *testing.T, s *store.WorldStore, title string, age time.Duration) string {
	t.Helper()
	id, err := s.CreateWrit(title, "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CloseWrit(id); err != nil {
		t.Fatal(err)
	}
	// Backdate closed_at.
	closedAt := time.Now().UTC().Add(-age).Format(time.RFC3339)
	_, err = s.DB().Exec(`UPDATE writs SET closed_at = ? WHERE id = ?`, closedAt, id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// createOutputDir creates a writ output directory with a test file.
func createOutputDir(t *testing.T, solHome, world, writID string) string {
	t.Helper()
	dir := filepath.Join(solHome, world, "writ-outputs", writID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a small file so we have some size to reclaim.
	if err := os.WriteFile(filepath.Join(dir, "output.txt"), []byte("test output data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestWritClean_EligibleWrits(t *testing.T) {
	s, world, solHome := setupCleanTest(t)

	// Create a closed writ older than 15 days.
	id := createClosedWritWithAge(t, s, "Old closed writ", 20*24*time.Hour)
	outputDir := createOutputDir(t, solHome, world, id)

	// Run clean.
	cmd := rootCmd
	cmd.SetArgs([]string{"writ", "clean", "--world", world})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("writ clean failed: %v", err)
	}

	// Verify output directory was removed.
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatalf("expected output directory to be removed, but it still exists")
	}

	// Verify metadata was updated with cleaned_at.
	meta, err := s.GetWritMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil {
		t.Fatal("expected metadata to be set after cleaning")
	}
	if _, ok := meta["cleaned_at"]; !ok {
		t.Fatal("expected cleaned_at in metadata")
	}
}

func TestWritClean_SkipOpenWrits(t *testing.T) {
	s, world, solHome := setupCleanTest(t)

	// Create an open writ (not closed).
	id, err := s.CreateWrit("Open writ", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	outputDir := createOutputDir(t, solHome, world, id)

	cmd := rootCmd
	cmd.SetArgs([]string{"writ", "clean", "--world", world})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("writ clean failed: %v", err)
	}

	// Output directory should still exist.
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatal("expected output directory to still exist for open writ")
	}
}

func TestWritClean_SkipClosedWritsWithOpenDependents(t *testing.T) {
	s, world, solHome := setupCleanTest(t)

	// Create writ A (closed, old).
	idA := createClosedWritWithAge(t, s, "Writ A (upstream)", 20*24*time.Hour)
	outputDir := createOutputDir(t, solHome, world, idA)

	// Create writ B (open, depends on A).
	idB, err := s.CreateWrit("Writ B (downstream)", "", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddDependency(idB, idA); err != nil {
		t.Fatal(err)
	}

	cmd := rootCmd
	cmd.SetArgs([]string{"writ", "clean", "--world", world})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("writ clean failed: %v", err)
	}

	// Output directory should still exist because B is open.
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatal("expected output directory to still exist for writ with open dependent")
	}

	// Verify no metadata was set.
	meta, err := s.GetWritMetadata(idA)
	if err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		if _, ok := meta["cleaned_at"]; ok {
			t.Fatal("expected no cleaned_at in metadata for skipped writ")
		}
	}

	_ = idB // keep linter happy
}

func TestWritClean_DryRun(t *testing.T) {
	s, world, solHome := setupCleanTest(t)

	// Create a closed writ older than 15 days.
	id := createClosedWritWithAge(t, s, "Old closed writ", 20*24*time.Hour)
	outputDir := createOutputDir(t, solHome, world, id)

	cmd := rootCmd
	cmd.SetArgs([]string{"writ", "clean", "--world", world, "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("writ clean --dry-run failed: %v", err)
	}

	// Output directory should still exist (dry run).
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatal("expected output directory to still exist in dry-run mode")
	}

	// No metadata should be set.
	meta, err := s.GetWritMetadata(id)
	if err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		if _, ok := meta["cleaned_at"]; ok {
			t.Fatal("expected no cleaned_at in metadata during dry run")
		}
	}
}

func TestWritClean_ConfigOverridesDefault(t *testing.T) {
	s, world, solHome := setupCleanTest(t)

	// Write world.toml with retention_days = 30.
	worldToml := filepath.Join(solHome, world, "world.toml")
	if err := os.WriteFile(worldToml, []byte("[world]\n\n[writ-clean]\nretention_days = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a closed writ 20 days old — within 30-day retention, should NOT be cleaned.
	id := createClosedWritWithAge(t, s, "20 day old writ", 20*24*time.Hour)
	outputDir := createOutputDir(t, solHome, world, id)

	cmd := rootCmd
	cmd.SetArgs([]string{"writ", "clean", "--world", world})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("writ clean failed: %v", err)
	}

	// Output directory should still exist (within 30-day retention).
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		t.Fatal("expected output directory to still exist within config retention window")
	}
}

func TestWritClean_CLIFlagOverridesConfig(t *testing.T) {
	s, world, solHome := setupCleanTest(t)

	// Write world.toml with retention_days = 30.
	worldToml := filepath.Join(solHome, world, "world.toml")
	if err := os.WriteFile(worldToml, []byte("[world]\n\n[writ-clean]\nretention_days = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a closed writ 20 days old.
	id := createClosedWritWithAge(t, s, "20 day old writ", 20*24*time.Hour)
	outputDir := createOutputDir(t, solHome, world, id)

	// Use --older-than=10d to override config (20 > 10, so should be cleaned).
	cmd := rootCmd
	cmd.SetArgs([]string{"writ", "clean", "--world", world, "--older-than", "10d"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("writ clean failed: %v", err)
	}

	// Output directory should be removed (CLI flag overrides config).
	if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
		t.Fatal("expected output directory to be removed when CLI flag overrides config")
	}
}

func TestParseDaysDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"7d", 7, false},
		{"15d", 15, false},
		{"30d", 30, false},
		{"1d", 1, false},
		{"0d", 0, true},      // must be positive
		{"d", 0, true},       // missing number
		{"15", 0, true},      // missing d suffix
		{"15h", 0, true},     // wrong suffix
		{"abc", 0, true},     // not a number
		{"-5d", 0, true},     // negative
		{"", 0, true},        // empty
	}

	for _, tc := range tests {
		got, err := parseDaysDuration(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseDaysDuration(%q) = %d, want error", tc.input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDaysDuration(%q) error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDaysDuration(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestDirSize(t *testing.T) {
	dir := t.TempDir()
	// Write two files.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644) // 5 bytes
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world!"), 0o644) // 6 bytes

	got := dirSize(dir)
	if got != 11 {
		t.Errorf("dirSize = %d, want 11", got)
	}
}

func TestDirSize_Empty(t *testing.T) {
	dir := t.TempDir()
	got := dirSize(dir)
	if got != 0 {
		t.Errorf("dirSize of empty dir = %d, want 0", got)
	}
}

// Suppress unused import warning.
var _ = config.Home

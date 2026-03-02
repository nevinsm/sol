package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunBasic(t *testing.T) {
	// Set SOL_HOME to a non-existent path inside t.TempDir().
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	result, err := Run(Params{
		WorldName:  "myworld",
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify Result has correct paths.
	if result.SOLHome != solHome {
		t.Errorf("SOLHome = %q, want %q", result.SOLHome, solHome)
	}
	if result.WorldName != "myworld" {
		t.Errorf("WorldName = %q, want %q", result.WorldName, "myworld")
	}
	if result.ConfigPath != filepath.Join(solHome, "myworld", "world.toml") {
		t.Errorf("ConfigPath = %q, want %q", result.ConfigPath, filepath.Join(solHome, "myworld", "world.toml"))
	}
	if result.DBPath != filepath.Join(solHome, ".store", "myworld.db") {
		t.Errorf("DBPath = %q, want %q", result.DBPath, filepath.Join(solHome, ".store", "myworld.db"))
	}

	// Verify SOL_HOME directory created.
	if _, err := os.Stat(solHome); os.IsNotExist(err) {
		t.Error("SOL_HOME directory not created")
	}

	// Verify .store/ directory created.
	if _, err := os.Stat(filepath.Join(solHome, ".store")); os.IsNotExist(err) {
		t.Error(".store/ directory not created")
	}

	// Verify world.toml exists.
	if _, err := os.Stat(result.ConfigPath); os.IsNotExist(err) {
		t.Error("world.toml not created")
	}

	// Verify myworld.db exists.
	if _, err := os.Stat(result.DBPath); os.IsNotExist(err) {
		t.Error("myworld.db not created")
	}

	// Verify myworld/outposts/ directory exists.
	outpostsDir := filepath.Join(solHome, "myworld", "outposts")
	if _, err := os.Stat(outpostsDir); os.IsNotExist(err) {
		t.Error("myworld/outposts/ directory not created")
	}
}

func TestRunWithSourceRepo(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Create a temp directory as a fake source repo.
	sourceRepo := t.TempDir()

	result, err := Run(Params{
		WorldName:  "myworld",
		SourceRepo: sourceRepo,
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify Result.SourceRepo is set.
	if result.SourceRepo != sourceRepo {
		t.Errorf("SourceRepo = %q, want %q", result.SourceRepo, sourceRepo)
	}

	// Load world.toml and verify source_repo field.
	data, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	if !strings.Contains(string(data), sourceRepo) {
		t.Errorf("world.toml does not contain source_repo %q: %s", sourceRepo, data)
	}
}

func TestRunInvalidWorldName(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Empty world name.
	_, err := Run(Params{WorldName: "", SkipChecks: true})
	if err == nil {
		t.Fatal("expected error for empty world name")
	}
	if !strings.Contains(err.Error(), "world name is required") {
		t.Errorf("expected 'world name is required' error, got: %v", err)
	}

	// Reserved name.
	_, err = Run(Params{WorldName: "store", SkipChecks: true})
	if err == nil {
		t.Fatal("expected error for reserved world name")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("expected 'reserved' error, got: %v", err)
	}
}

func TestRunDoctorFails(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// Run with SkipChecks=false.
	// If all prerequisites are present, doctor will pass and setup succeeds.
	// If doctor reports failures, the error message should include failure details.
	result, err := Run(Params{
		WorldName:  "myworld",
		SkipChecks: false,
	})
	if err != nil {
		// Doctor failed — verify error message includes failure details.
		if !strings.Contains(err.Error(), "prerequisite check(s) failed") {
			t.Errorf("expected 'prerequisite check(s) failed' in error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "sol doctor") {
			t.Errorf("expected 'sol doctor' hint in error, got: %v", err)
		}
	} else {
		// Doctor passed — verify setup completed.
		if result == nil {
			t.Fatal("expected non-nil result when doctor passes")
		}
	}
}

func TestRunSkipChecks(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	result, err := Run(Params{
		WorldName:  "myworld",
		SkipChecks: true,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunIdempotent(t *testing.T) {
	solHome := filepath.Join(t.TempDir(), "sol-test")
	t.Setenv("SOL_HOME", solHome)

	// First run — success.
	_, err := Run(Params{WorldName: "myworld", SkipChecks: true})
	if err != nil {
		t.Fatalf("first Run() error: %v", err)
	}

	// Second run — should fail with "already initialized".
	_, err = Run(Params{WorldName: "myworld", SkipChecks: true})
	if err == nil {
		t.Fatal("expected error on second run")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Errorf("expected 'already initialized' error, got: %v", err)
	}
}

func TestValidateParams(t *testing.T) {
	// Empty WorldName → error.
	p := Params{WorldName: ""}
	if err := p.Validate(); err == nil {
		t.Error("expected error for empty WorldName")
	}

	// Reserved name → error.
	p = Params{WorldName: "store"}
	if err := p.Validate(); err == nil {
		t.Error("expected error for reserved name")
	}

	// Valid name → nil.
	p = Params{WorldName: "myworld"}
	if err := p.Validate(); err != nil {
		t.Errorf("unexpected error for valid name: %v", err)
	}
}

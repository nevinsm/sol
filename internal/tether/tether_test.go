package tether

import (
	"os"
	"testing"
)

func setupTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
}

func TestWriteAndRead(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-a1b2c3d4"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	id, err := Read("myworld", "Toast")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "sol-a1b2c3d4" {
		t.Errorf("expected sol-a1b2c3d4, got %q", id)
	}
}

func TestReadNoTether(t *testing.T) {
	setupTest(t)

	id, err := Read("myworld", "Toast")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty string, got %q", id)
	}
}

func TestClear(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-a1b2c3d4"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Clear("myworld", "Toast"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	id, err := Read("myworld", "Toast")
	if err != nil {
		t.Fatalf("Read after Clear failed: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty string after Clear, got %q", id)
	}
}

func TestClearNoTether(t *testing.T) {
	setupTest(t)

	// Clear should be a no-op if no tether exists.
	if err := Clear("myworld", "Toast"); err != nil {
		t.Fatalf("Clear on non-existent tether failed: %v", err)
	}
}

func TestIsTethered(t *testing.T) {
	setupTest(t)

	if IsTethered("myworld", "Toast") {
		t.Error("expected IsTethered=false before Write")
	}

	if err := Write("myworld", "Toast", "sol-a1b2c3d4"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if !IsTethered("myworld", "Toast") {
		t.Error("expected IsTethered=true after Write")
	}

	if err := Clear("myworld", "Toast"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if IsTethered("myworld", "Toast") {
		t.Error("expected IsTethered=false after Clear")
	}
}

func TestTetherPath(t *testing.T) {
	setupTest(t)

	path := TetherPath("myworld", "Toast")
	solHome := os.Getenv("SOL_HOME")
	expected := solHome + "/myworld/outposts/Toast/.tether"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestWriteOverwrite(t *testing.T) {
	setupTest(t)

	if err := Write("myworld", "Toast", "sol-11111111"); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}
	if err := Write("myworld", "Toast", "sol-22222222"); err != nil {
		t.Fatalf("second Write failed: %v", err)
	}

	id, err := Read("myworld", "Toast")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "sol-22222222" {
		t.Errorf("expected sol-22222222, got %q", id)
	}
}

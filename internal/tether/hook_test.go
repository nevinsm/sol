package hook

import (
	"os"
	"testing"
)

func setupTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GT_HOME", dir)
}

func TestWriteAndRead(t *testing.T) {
	setupTest(t)

	if err := Write("myrig", "Toast", "gt-a1b2c3d4"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	id, err := Read("myrig", "Toast")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "gt-a1b2c3d4" {
		t.Errorf("expected gt-a1b2c3d4, got %q", id)
	}
}

func TestReadNoHook(t *testing.T) {
	setupTest(t)

	id, err := Read("myrig", "Toast")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty string, got %q", id)
	}
}

func TestClear(t *testing.T) {
	setupTest(t)

	if err := Write("myrig", "Toast", "gt-a1b2c3d4"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := Clear("myrig", "Toast"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	id, err := Read("myrig", "Toast")
	if err != nil {
		t.Fatalf("Read after Clear failed: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty string after Clear, got %q", id)
	}
}

func TestClearNoHook(t *testing.T) {
	setupTest(t)

	// Clear should be a no-op if no hook exists.
	if err := Clear("myrig", "Toast"); err != nil {
		t.Fatalf("Clear on non-existent hook failed: %v", err)
	}
}

func TestIsHooked(t *testing.T) {
	setupTest(t)

	if IsHooked("myrig", "Toast") {
		t.Error("expected IsHooked=false before Write")
	}

	if err := Write("myrig", "Toast", "gt-a1b2c3d4"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if !IsHooked("myrig", "Toast") {
		t.Error("expected IsHooked=true after Write")
	}

	if err := Clear("myrig", "Toast"); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	if IsHooked("myrig", "Toast") {
		t.Error("expected IsHooked=false after Clear")
	}
}

func TestHookPath(t *testing.T) {
	setupTest(t)

	path := HookPath("myrig", "Toast")
	gtHome := os.Getenv("GT_HOME")
	expected := gtHome + "/myrig/polecats/Toast/.hook"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestWriteOverwrite(t *testing.T) {
	setupTest(t)

	if err := Write("myrig", "Toast", "gt-11111111"); err != nil {
		t.Fatalf("first Write failed: %v", err)
	}
	if err := Write("myrig", "Toast", "gt-22222222"); err != nil {
		t.Fatalf("second Write failed: %v", err)
	}

	id, err := Read("myrig", "Toast")
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if id != "gt-22222222" {
		t.Errorf("expected gt-22222222, got %q", id)
	}
}

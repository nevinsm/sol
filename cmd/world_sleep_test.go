package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
)

// resetWorldSleepFlags resets world-sleep package-level flag vars between
// test runs (worldSleepCmd is a package-level singleton *cobra.Command).
func resetWorldSleepFlags() {
	worldSleepForce = false
	worldSleepConfirm = false
	worldSleepJSON = false
}

// captureWorldSleep runs `sol world sleep <world> <args...>` and returns
// captured stdout plus the command error.
func captureWorldSleep(t *testing.T, world string, args ...string) (string, error) {
	t.Helper()
	resetWorldSleepFlags()
	t.Cleanup(resetWorldSleepFlags)

	rootCmd.SetArgs(append([]string{"world", "sleep", world}, args...))

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	var captured bytes.Buffer
	captured.ReadFrom(r)
	os.Stdout = oldStdout
	return captured.String(), err
}

// TestWorldSleepForceWithoutConfirmPreviewsAndDoesNotMutate verifies the
// confirmed fix: --force alone used to sleep the world and force-stop
// sessions with zero confirmation gate. It should now require --confirm,
// printing a preview and exiting 1 without touching world state at all.
func TestWorldSleepForceWithoutConfirmPreviewsAndDoesNotMutate(t *testing.T) {
	world := "sleepconfirmtest"
	initTestWorld(t, world)

	output, err := captureWorldSleep(t, world, "--force")
	if err == nil {
		t.Fatal("expected non-nil error (no --confirm passed): preview should exit 1")
	}
	if code := ExitCode(err); code != 1 {
		t.Errorf("expected exit code 1 (unconfirmed preview), got %d (err: %v)", code, err)
	}
	if !strings.Contains(output, "Run with --confirm to proceed.") {
		t.Errorf("expected confirm-gate preview text, got: %s", output)
	}

	// Nothing should have been mutated: the world must still be active.
	cfg, cfgErr := config.LoadWorldConfig(world)
	if cfgErr != nil {
		t.Fatalf("load world config: %v", cfgErr)
	}
	if cfg.World.Sleeping {
		t.Error("world should NOT be marked sleeping after an unconfirmed --force preview")
	}
}

// TestWorldSleepForceWithConfirmProceeds verifies --force --confirm together
// still perform the hard sleep (world marked sleeping).
func TestWorldSleepForceWithConfirmProceeds(t *testing.T) {
	world := "sleepconfirmtest2"
	initTestWorld(t, world)

	_, err := captureWorldSleep(t, world, "--force", "--confirm")
	if err != nil {
		t.Fatalf("world sleep --force --confirm: %v", err)
	}

	cfg, cfgErr := config.LoadWorldConfig(world)
	if cfgErr != nil {
		t.Fatalf("load world config: %v", cfgErr)
	}
	if !cfg.World.Sleeping {
		t.Error("world should be marked sleeping after --force --confirm")
	}
}

// TestWorldSleepWithoutForceNeedsNoConfirm verifies the soft-sleep path
// (no --force) is unaffected by the new gate: it never killed sessions or
// reopened writs, so it should not require --confirm.
func TestWorldSleepWithoutForceNeedsNoConfirm(t *testing.T) {
	world := "sleepsofttest"
	initTestWorld(t, world)

	_, err := captureWorldSleep(t, world)
	if err != nil {
		t.Fatalf("world sleep (soft): %v", err)
	}

	cfg, cfgErr := config.LoadWorldConfig(world)
	if cfgErr != nil {
		t.Fatalf("load world config: %v", cfgErr)
	}
	if !cfg.World.Sleeping {
		t.Error("world should be marked sleeping after soft sleep")
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// setupUpTestEnv creates a temporary SOL_HOME with a sphere store and
// optional worlds. Returns a cleanup function.
func setupUpTestEnv(t *testing.T, worlds []string, sleeping map[string]bool) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("SOL_HOME", home)

	// Create sphere store and register worlds.
	if err := os.MkdirAll(config.StoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}

	for _, w := range worlds {
		if err := sphereStore.RegisterWorld(w, "https://example.com/"+w); err != nil {
			t.Fatal(err)
		}
		// Create world.toml.
		worldDir := filepath.Join(home, w)
		if err := os.MkdirAll(worldDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "[world]\nsource_repo = \"https://example.com/" + w + "\"\n"
		if sleeping[w] {
			content += "sleeping = true\n"
		}
		if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	sphereStore.Close()
}

func TestActiveWorldsAllNonSleeping(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta", "gamma"}, map[string]bool{
		"beta": true,
	})

	worlds, err := activeWorlds("")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 2 {
		t.Fatalf("expected 2 active worlds, got %d: %v", len(worlds), worlds)
	}

	// ListWorlds returns alphabetical order.
	if worlds[0] != "alpha" || worlds[1] != "gamma" {
		t.Errorf("expected [alpha, gamma], got %v", worlds)
	}
}

func TestActiveWorldsSpecificWorld(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta"}, nil)

	worlds, err := activeWorlds("alpha")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 1 || worlds[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", worlds)
	}
}

func TestActiveWorldsSpecificSleepingWorld(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha"}, map[string]bool{"alpha": true})

	_, err := activeWorlds("alpha")
	if err == nil {
		t.Fatal("expected error for sleeping world, got nil")
	}
}

func TestActiveWorldsSpecificNonexistent(t *testing.T) {
	setupUpTestEnv(t, nil, nil)

	_, err := activeWorlds("nope")
	if err == nil {
		t.Fatal("expected error for nonexistent world, got nil")
	}
}

func TestActiveWorldsNoWorlds(t *testing.T) {
	setupUpTestEnv(t, nil, nil)

	worlds, err := activeWorlds("")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 0 {
		t.Fatalf("expected 0 worlds, got %d", len(worlds))
	}
}

func TestListAllWorlds(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta", "gamma"}, map[string]bool{
		"beta": true,
	})

	worlds, err := listAllWorlds()
	if err != nil {
		t.Fatal(err)
	}

	// listAllWorlds returns all worlds, including sleeping.
	if len(worlds) != 3 {
		t.Fatalf("expected 3 worlds, got %d: %v", len(worlds), worlds)
	}
}

func TestResolveWorldsForDownAll(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha", "beta"}, map[string]bool{
		"beta": true,
	})

	// Should return all worlds regardless of sleeping state.
	worlds, err := resolveWorldsForDown("")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 2 {
		t.Fatalf("expected 2 worlds, got %d: %v", len(worlds), worlds)
	}
}

func TestResolveWorldsForDownSpecific(t *testing.T) {
	setupUpTestEnv(t, []string{"alpha"}, nil)

	worlds, err := resolveWorldsForDown("alpha")
	if err != nil {
		t.Fatal(err)
	}

	if len(worlds) != 1 || worlds[0] != "alpha" {
		t.Fatalf("expected [alpha], got %v", worlds)
	}
}

func TestResolveWorldsForDownNonexistent(t *testing.T) {
	setupUpTestEnv(t, nil, nil)

	_, err := resolveWorldsForDown("nope")
	if err == nil {
		t.Fatal("expected error for nonexistent world, got nil")
	}
}

func TestWorldServicesContents(t *testing.T) {
	found := map[string]bool{}
	for _, svc := range worldServices {
		found[svc] = true
	}

	if !found["sentinel"] {
		t.Error("worldServices missing sentinel")
	}
	if !found["forge"] {
		t.Error("worldServices missing forge")
	}
}

func TestUpCmdHasWorldFlag(t *testing.T) {
	f := upCmd.Flags().Lookup("world")
	if f == nil {
		t.Fatal("up command missing --world flag")
	}
	if f.NoOptDefVal != "" {
		t.Errorf("--world NoOptDefVal should be empty string, got %q", f.NoOptDefVal)
	}
}

// NOTE: Tests for classifyDaemonStartup, clearDaemonPIDIfMine, and the
// startSphereDaemons pidfile-ownership / crash-detection cases previously
// lived here. Those code paths moved into internal/daemon.Start and are
// covered by internal/daemon/lifecycle_test.go:
//   - TestStartReportsRunningWhenPidfileMatchesLiveProcess
//   - TestStartReportsStartedWhenChildWritesOwnPID
//   - TestStartReportsFailedWhenChildExitsImmediately
//   - TestStartNeverClobbersForeignLivePid
// See sol-06e76378be1408bf.

func TestDownCmdHasWorldFlag(t *testing.T) {
	f := downCmd.Flags().Lookup("world")
	if f == nil {
		t.Fatal("down command missing --world flag")
	}
	if f.NoOptDefVal != "" {
		t.Errorf("--world NoOptDefVal should be empty string, got %q", f.NoOptDefVal)
	}
}

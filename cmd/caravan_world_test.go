package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/store"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetCaravanCmdFlags resets every flag under the caravan command tree back
// to its registered default and clears Changed(), so state set by one
// rootCmd.Execute call (these are package-level singleton *cobra.Command
// values) doesn't leak into the next test. Most caravan flags are anonymous
// (String/Bool, not *Var-bound), so resetting requires walking pflag.Flag
// directly rather than reassigning package vars.
func resetCaravanCmdFlags(t *testing.T) {
	t.Helper()
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
		for _, c := range cmd.Commands() {
			walk(c)
		}
	}
	walk(caravanCmd)
}

// setupCaravanWorldTest creates a fresh SOL_HOME with a real world (so
// --world validation against config.ResolveWorld has something to
// succeed against) and a drydock caravan with no items, returning the
// caravan ID. Sphere-level caravan subcommands don't need a world to
// operate, but this exercises the --world flag against a world that
// genuinely exists.
func setupCaravanWorldTest(t *testing.T, world string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	defer sphereStore.Close()

	caravanID, err := sphereStore.CreateCaravan("world-flag-test", "autarch")
	if err != nil {
		t.Fatalf("create caravan: %v", err)
	}
	return caravanID
}

// runCaravanCmd executes rootCmd with the given args and returns the error,
// if any. Cobra flag values persist across Execute calls on package-level
// commands, so each subtest uses a caravan-scoped flag reset via --world=""
// where relevant (an explicit empty value overrides any prior run's value).
func runCaravanCmd(t *testing.T, args ...string) error {
	t.Helper()
	resetCaravanCmdFlags(t)
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func TestCaravanCommissionAcceptsWorldFlag(t *testing.T) {
	world := "carworldtest"
	caravanID := setupCaravanWorldTest(t, world)

	if err := runCaravanCmd(t, "caravan", "commission", caravanID, "--world="+world); err != nil {
		t.Fatalf("caravan commission --world=%s: %v", world, err)
	}
}

func TestCaravanCommissionRejectsUnknownWorld(t *testing.T) {
	world := "carworldtest2"
	caravanID := setupCaravanWorldTest(t, world)

	err := runCaravanCmd(t, "caravan", "commission", caravanID, "--world=does-not-exist")
	if err == nil {
		t.Fatal("expected error for nonexistent world")
	}
	if strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--world should be accepted, not rejected as unknown flag: %v", err)
	}
	if !strings.Contains(err.Error(), `world "does-not-exist"`) {
		t.Errorf("expected helpful world-not-found error, got: %v", err)
	}
}

func TestCaravanCommissionWithoutWorldStillWorks(t *testing.T) {
	world := "carworldtest3"
	caravanID := setupCaravanWorldTest(t, world)

	// No --world at all: sphere-level commands never required one before
	// this change and must not require one now.
	if err := runCaravanCmd(t, "caravan", "commission", caravanID); err != nil {
		t.Fatalf("caravan commission (no --world): %v", err)
	}
}

func TestCaravanSphereLevelSubcommandsAcceptWorldFlag(t *testing.T) {
	world := "carworldtest4"

	// Each subcommand gets its own fresh caravan in the right starting
	// state so we're only exercising --world acceptance, not lifecycle
	// transition rules.
	cases := []struct {
		name string
		run  func(t *testing.T, caravanID string)
	}{
		{"drydock", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "commission", caravanID); err != nil {
				t.Fatalf("commission precondition: %v", err)
			}
			if err := runCaravanCmd(t, "caravan", "drydock", caravanID, "--world="+world); err != nil {
				t.Fatalf("caravan drydock --world: %v", err)
			}
		}},
		{"reopen", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "close", caravanID, "--confirm", "--force"); err != nil {
				t.Fatalf("close precondition: %v", err)
			}
			if err := runCaravanCmd(t, "caravan", "reopen", caravanID, "--world="+world); err != nil {
				t.Fatalf("caravan reopen --world: %v", err)
			}
		}},
		{"status", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "status", caravanID, "--world="+world); err != nil {
				t.Fatalf("caravan status --world: %v", err)
			}
		}},
		{"list", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "list", "--world="+world); err != nil {
				t.Fatalf("caravan list --world: %v", err)
			}
		}},
		{"set-phase", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "set-phase", caravanID, "--all", "1", "--world="+world); err != nil {
				t.Fatalf("caravan set-phase --world: %v", err)
			}
		}},
		{"close", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "close", caravanID, "--confirm", "--force", "--world="+world); err != nil {
				t.Fatalf("caravan close --world: %v", err)
			}
		}},
		{"delete", func(t *testing.T, caravanID string) {
			if err := runCaravanCmd(t, "caravan", "close", caravanID, "--confirm", "--force"); err != nil {
				t.Fatalf("close precondition: %v", err)
			}
			if err := runCaravanCmd(t, "caravan", "delete", caravanID, "--confirm", "--world="+world); err != nil {
				t.Fatalf("caravan delete --world: %v", err)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caravanID := setupCaravanWorldTest(t, world)
			tc.run(t, caravanID)
		})
	}
}

func TestCaravanRemoveAcceptsWorldFlag(t *testing.T) {
	world := "carworldtest5"
	caravanID := setupCaravanWorldTest(t, world)

	sphereStore, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("open sphere store: %v", err)
	}
	writID := "sol-1234567890abcdef"
	if err := sphereStore.CreateCaravanItem(caravanID, writID, world, 0); err != nil {
		t.Fatalf("add caravan item: %v", err)
	}
	sphereStore.Close()

	if err := runCaravanCmd(t, "caravan", "remove", caravanID, writID, "--world="+world); err != nil {
		t.Fatalf("caravan remove --world: %v", err)
	}
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/store"
)

// setupCaravanTestHome creates a fresh SOL_HOME with just a sphere store
// directory — caravans are sphere-scoped, so no world is required unless a
// test explicitly passes --world or seeds items.
func setupCaravanTestHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// resetCaravanCreateFlags resets caravan-create package-level flag vars so
// tests don't leak state through cobra's persistent package globals.
func resetCaravanCreateFlags() {
	caravanOwner = ""
	caravanCreateWorld = ""
	caravanCreatePhase = 0
	caravanCreateJSON = false
}

// findCaravanByName looks up a caravan by name via ListCaravans, failing the
// test if it's not found or ambiguous.
func findCaravanByName(t *testing.T, sphereStore *store.SphereStore, name string) store.Caravan {
	t.Helper()
	caravans, err := sphereStore.ListCaravans("")
	if err != nil {
		t.Fatalf("list caravans: %v", err)
	}
	var found []store.Caravan
	for _, c := range caravans {
		if c.Name == name {
			found = append(found, c)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 caravan named %q, found %d", name, len(found))
	}
	return found[0]
}

// TestCaravanCreateOwnerDefault covers Task B's owner-default resolution: no
// --owner flag defaults to the resolved actor identity (autarch at an
// operator terminal, "{world}/{agent}" inside an agent session), and an
// explicit --owner always wins regardless of environment.
func TestCaravanCreateOwnerDefault(t *testing.T) {
	t.Run("operator terminal defaults to autarch", func(t *testing.T) {
		setupCaravanTestHome(t)
		t.Setenv("SOL_AGENT", "")
		t.Setenv("SOL_WORLD", "")

		resetCaravanCreateFlags()
		rootCmd.SetArgs([]string{"caravan", "create", "operator-caravan"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("caravan create: %v", err)
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			t.Fatalf("open sphere store: %v", err)
		}
		defer sphereStore.Close()

		c := findCaravanByName(t, sphereStore, "operator-caravan")
		if c.Owner != config.Autarch {
			t.Errorf("owner = %q, want %q", c.Owner, config.Autarch)
		}
	})

	t.Run("agent session defaults to world/agent with no flag", func(t *testing.T) {
		setupCaravanTestHome(t)
		t.Setenv("SOL_AGENT", "Envoy")
		t.Setenv("SOL_WORLD", "sol-dev")

		resetCaravanCreateFlags()
		rootCmd.SetArgs([]string{"caravan", "create", "envoy-caravan"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("caravan create: %v", err)
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			t.Fatalf("open sphere store: %v", err)
		}
		defer sphereStore.Close()

		c := findCaravanByName(t, sphereStore, "envoy-caravan")
		want := "sol-dev/Envoy"
		if c.Owner != want {
			t.Errorf("owner = %q, want %q", c.Owner, want)
		}
	})

	t.Run("explicit --owner overrides resolved identity", func(t *testing.T) {
		setupCaravanTestHome(t)
		t.Setenv("SOL_AGENT", "Envoy")
		t.Setenv("SOL_WORLD", "sol-dev")

		resetCaravanCreateFlags()
		rootCmd.SetArgs([]string{"caravan", "create", "explicit-owner-caravan", "--owner", "someone-else"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("caravan create: %v", err)
		}

		sphereStore, err := store.OpenSphere()
		if err != nil {
			t.Fatalf("open sphere store: %v", err)
		}
		defer sphereStore.Close()

		c := findCaravanByName(t, sphereStore, "explicit-owner-caravan")
		if c.Owner != "someone-else" {
			t.Errorf("owner = %q, want %q", c.Owner, "someone-else")
		}
	})
}

// TestCaravanCreateEventActorAttribution covers Task A: the
// EventCaravanCreated actor reflects the resolved caller identity instead of
// a hardcoded autarch literal, mirroring the Owner-field fix so the audit
// event and the stored owner agree.
func TestCaravanCreateEventActorAttribution(t *testing.T) {
	t.Run("operator terminal records autarch", func(t *testing.T) {
		setupCaravanTestHome(t)
		solHome := os.Getenv("SOL_HOME")
		t.Setenv("SOL_AGENT", "")
		t.Setenv("SOL_WORLD", "")

		resetCaravanCreateFlags()
		rootCmd.SetArgs([]string{"caravan", "create", "operator-actor-caravan"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("caravan create: %v", err)
		}

		evs := readMailEvents(t, solHome, events.EventCaravanCreated)
		if len(evs) != 1 {
			t.Fatalf("expected 1 caravan_created event, got %d", len(evs))
		}
		if evs[0].Actor != config.Autarch {
			t.Errorf("actor = %q, want %q", evs[0].Actor, config.Autarch)
		}
	})

	t.Run("agent session records world/agent", func(t *testing.T) {
		setupCaravanTestHome(t)
		solHome := os.Getenv("SOL_HOME")
		t.Setenv("SOL_AGENT", "Envoy")
		t.Setenv("SOL_WORLD", "sol-dev")

		resetCaravanCreateFlags()
		rootCmd.SetArgs([]string{"caravan", "create", "agent-actor-caravan"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("caravan create: %v", err)
		}

		evs := readMailEvents(t, solHome, events.EventCaravanCreated)
		if len(evs) != 1 {
			t.Fatalf("expected 1 caravan_created event, got %d", len(evs))
		}
		want := "sol-dev/Envoy"
		if evs[0].Actor != want {
			t.Errorf("actor = %q, want %q", evs[0].Actor, want)
		}
	})
}

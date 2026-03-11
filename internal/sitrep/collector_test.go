package sitrep_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/sitrep"
	"github.com/nevinsm/sol/internal/store"
)

func setupTestEnv(t *testing.T) (sphere *store.Store, worldOpener sitrep.WorldOpener) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	s, err := store.OpenSphere()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	opener := func(world string) (*store.Store, error) {
		return store.OpenWorld(world)
	}

	return s, opener
}

func TestCollectSphereEmpty(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{Sphere: true})
	if err != nil {
		t.Fatal(err)
	}

	if data.Scope != "sphere" {
		t.Errorf("expected scope %q, got %q", "sphere", data.Scope)
	}
	if len(data.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(data.Agents))
	}
	if len(data.Caravans) != 0 {
		t.Errorf("expected 0 caravans, got %d", len(data.Caravans))
	}
	if len(data.Worlds) != 0 {
		t.Errorf("expected 0 worlds, got %d", len(data.Worlds))
	}
}

func TestCollectWorldScoped(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	// Register a world and create some data.
	if err := sphere.RegisterWorld("test-world", "/tmp/test"); err != nil {
		t.Fatal(err)
	}

	// Create an agent.
	if _, err := sphere.CreateAgent("Alpha", "test-world", "outpost"); err != nil {
		t.Fatal(err)
	}

	// Create writs in the world.
	ws, err := store.OpenWorld("test-world")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if _, err := ws.CreateWrit("Test writ", "Description", "autarch", 2, nil); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{World: "test-world"})
	if err != nil {
		t.Fatal(err)
	}

	if data.Scope != "test-world" {
		t.Errorf("expected scope %q, got %q", "test-world", data.Scope)
	}
	if len(data.Agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(data.Agents))
	}
	if len(data.Worlds) != 1 {
		t.Errorf("expected 1 world, got %d", len(data.Worlds))
	}
	if len(data.Worlds) > 0 && len(data.Worlds[0].Writs) != 1 {
		t.Errorf("expected 1 writ, got %d", len(data.Worlds[0].Writs))
	}
}

func TestCollectSphereWithWorlds(t *testing.T) {
	sphere, opener := setupTestEnv(t)

	// Register two worlds.
	if err := sphere.RegisterWorld("alpha", "/tmp/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := sphere.RegisterWorld("bravo", "/tmp/bravo"); err != nil {
		t.Fatal(err)
	}

	// Create agents in different worlds.
	if _, err := sphere.CreateAgent("A1", "alpha", "outpost"); err != nil {
		t.Fatal(err)
	}
	if _, err := sphere.CreateAgent("B1", "bravo", "outpost"); err != nil {
		t.Fatal(err)
	}

	data, err := sitrep.Collect(sphere, opener, sitrep.Scope{Sphere: true})
	if err != nil {
		t.Fatal(err)
	}

	if data.Scope != "sphere" {
		t.Errorf("expected scope %q, got %q", "sphere", data.Scope)
	}
	if len(data.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(data.Agents))
	}
	if len(data.Worlds) != 2 {
		t.Errorf("expected 2 worlds, got %d", len(data.Worlds))
	}
}

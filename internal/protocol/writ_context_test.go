package protocol_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// setupWritContextEnv creates a temp SOL_HOME with sphere and world stores,
// registers an agent, and returns cleanup-ready store handles.
func setupWritContextEnv(t *testing.T, world, agent, role string) (sphereStore *store.Store, worldStore *store.Store) {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// Create store directory.
	storeDir := filepath.Join(tmp, ".store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Open sphere store and create agent.
	ss, err := store.OpenSphere()
	if err != nil {
		t.Fatalf("failed to open sphere store: %v", err)
	}

	if _, err := ss.CreateAgent(agent, world, role); err != nil {
		ss.Close()
		t.Fatalf("failed to create agent: %v", err)
	}

	// Open world store.
	ws, err := store.OpenWorld(world)
	if err != nil {
		ss.Close()
		t.Fatalf("failed to open world store: %v", err)
	}

	t.Cleanup(func() {
		ss.Close()
		ws.Close()
	})

	return ss, ws
}

func TestPopulateWritContextNoTethers(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("SOL_HOME", tmp)

	// No tether directory — should return empty context with no error.
	ctx, err := protocol.PopulateWritContext("myworld", "Echo", "envoy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctx.TetheredWrits) != 0 {
		t.Error("expected no tethered writs")
	}
	if ctx.ActiveWritID != "" {
		t.Error("expected empty active writ ID")
	}
}

func TestPopulateWritContextSingleWrit(t *testing.T) {
	ss, ws := setupWritContextEnv(t, "myworld", "Echo", "envoy")

	// Create a writ.
	writID, err := ws.CreateWrit("Build feature", "Build the new feature", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ: %v", err)
	}

	// Tether the writ.
	if err := tether.Write("myworld", "Echo", writID, "envoy"); err != nil {
		t.Fatalf("failed to write tether: %v", err)
	}

	// Set as active writ.
	if err := ss.UpdateAgentState("myworld/Echo", "working", writID); err != nil {
		t.Fatalf("failed to set active writ: %v", err)
	}

	ctx, err := protocol.PopulateWritContext("myworld", "Echo", "envoy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have one tethered writ.
	if len(ctx.TetheredWrits) != 1 {
		t.Fatalf("expected 1 tethered writ, got %d", len(ctx.TetheredWrits))
	}
	if ctx.TetheredWrits[0].ID != writID {
		t.Errorf("tethered writ ID = %q, want %q", ctx.TetheredWrits[0].ID, writID)
	}
	if ctx.TetheredWrits[0].Title != "Build feature" {
		t.Errorf("tethered writ title = %q, want %q", ctx.TetheredWrits[0].Title, "Build feature")
	}
	if ctx.TetheredWrits[0].Kind != "code" {
		t.Errorf("tethered writ kind = %q, want \"code\"", ctx.TetheredWrits[0].Kind)
	}

	// Should have active writ populated.
	if ctx.ActiveWritID != writID {
		t.Errorf("active writ ID = %q, want %q", ctx.ActiveWritID, writID)
	}
	if ctx.ActiveTitle != "Build feature" {
		t.Errorf("active title = %q, want %q", ctx.ActiveTitle, "Build feature")
	}
	if ctx.ActiveDesc != "Build the new feature" {
		t.Errorf("active desc = %q, want %q", ctx.ActiveDesc, "Build the new feature")
	}
	if ctx.ActiveKind != "code" {
		t.Errorf("active kind = %q, want \"code\"", ctx.ActiveKind)
	}
	if ctx.ActiveOutput == "" {
		t.Error("expected non-empty active output directory")
	}
}

func TestPopulateWritContextMultipleWrits(t *testing.T) {
	ss, ws := setupWritContextEnv(t, "myworld", "Echo", "envoy")

	// Create two writs.
	writ1, err := ws.CreateWrit("First task", "First description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 1: %v", err)
	}
	writ2, err := ws.CreateWrit("Second task", "Second description", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("failed to create writ 2: %v", err)
	}

	// Tether both writs.
	if err := tether.Write("myworld", "Echo", writ1, "envoy"); err != nil {
		t.Fatal(err)
	}
	if err := tether.Write("myworld", "Echo", writ2, "envoy"); err != nil {
		t.Fatal(err)
	}

	// Set writ2 as active.
	if err := ss.UpdateAgentState("myworld/Echo", "working", writ2); err != nil {
		t.Fatal(err)
	}

	ctx, err := protocol.PopulateWritContext("myworld", "Echo", "envoy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have two tethered writs.
	if len(ctx.TetheredWrits) != 2 {
		t.Fatalf("expected 2 tethered writs, got %d", len(ctx.TetheredWrits))
	}

	// Active writ should be writ2.
	if ctx.ActiveWritID != writ2 {
		t.Errorf("active writ ID = %q, want %q", ctx.ActiveWritID, writ2)
	}
	if ctx.ActiveTitle != "Second task" {
		t.Errorf("active title = %q, want %q", ctx.ActiveTitle, "Second task")
	}
}

func TestPopulateWritContextNoActiveWrit(t *testing.T) {
	_, ws := setupWritContextEnv(t, "myworld", "Echo", "envoy")

	// Create a writ and tether it, but don't set it as active.
	writID, err := ws.CreateWrit("Background task", "Background work", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tether.Write("myworld", "Echo", writID, "envoy"); err != nil {
		t.Fatal(err)
	}

	ctx, err := protocol.PopulateWritContext("myworld", "Echo", "envoy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have tethered writs but no active writ.
	if len(ctx.TetheredWrits) != 1 {
		t.Fatalf("expected 1 tethered writ, got %d", len(ctx.TetheredWrits))
	}
	if ctx.ActiveWritID != "" {
		t.Errorf("expected empty active writ ID, got %q", ctx.ActiveWritID)
	}
}

func TestPopulateWritContextWithDependencies(t *testing.T) {
	ss, ws := setupWritContextEnv(t, "myworld", "Echo", "envoy")

	// Create dependency writ.
	depID, err := ws.CreateWrit("Upstream task", "Upstream work", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create main writ that depends on depID.
	mainID, err := ws.CreateWrit("Main task", "Main work", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.AddDependency(mainID, depID); err != nil {
		t.Fatal(err)
	}

	// Tether and activate the main writ.
	if err := tether.Write("myworld", "Echo", mainID, "envoy"); err != nil {
		t.Fatal(err)
	}
	if err := ss.UpdateAgentState("myworld/Echo", "working", mainID); err != nil {
		t.Fatal(err)
	}

	ctx, err := protocol.PopulateWritContext("myworld", "Echo", "envoy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have active deps populated.
	if len(ctx.ActiveDeps) != 1 {
		t.Fatalf("expected 1 active dep, got %d", len(ctx.ActiveDeps))
	}
	if ctx.ActiveDeps[0].WritID != depID {
		t.Errorf("dep writ ID = %q, want %q", ctx.ActiveDeps[0].WritID, depID)
	}
	if ctx.ActiveDeps[0].Title != "Upstream task" {
		t.Errorf("dep title = %q, want %q", ctx.ActiveDeps[0].Title, "Upstream task")
	}
	if ctx.ActiveDeps[0].Kind != "code" {
		t.Errorf("dep kind = %q, want \"code\"", ctx.ActiveDeps[0].Kind)
	}
	if ctx.ActiveDeps[0].OutputDir == "" {
		t.Error("expected non-empty dep output directory")
	}
}

func TestPopulateWritContextEmptyKindDefaultsToCode(t *testing.T) {
	ss, ws := setupWritContextEnv(t, "myworld", "Echo", "envoy")

	// CreateWrit doesn't set Kind by default — should default to "code" in the context.
	writID, err := ws.CreateWrit("Task", "Desc", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tether.Write("myworld", "Echo", writID, "envoy"); err != nil {
		t.Fatal(err)
	}
	if err := ss.UpdateAgentState("myworld/Echo", "working", writID); err != nil {
		t.Fatal(err)
	}

	ctx, err := protocol.PopulateWritContext("myworld", "Echo", "envoy")
	if err != nil {
		t.Fatal(err)
	}

	if ctx.ActiveKind != "code" {
		t.Errorf("active kind = %q, want \"code\" (default)", ctx.ActiveKind)
	}
	if ctx.TetheredWrits[0].Kind != "code" {
		t.Errorf("tethered writ kind = %q, want \"code\" (default)", ctx.TetheredWrits[0].Kind)
	}
}

func TestPopulateWritContextGovernorRole(t *testing.T) {
	ss, ws := setupWritContextEnv(t, "myworld", "governor", "governor")

	writID, err := ws.CreateWrit("Governor task", "Governor work", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tether.Write("myworld", "governor", writID, "governor"); err != nil {
		t.Fatal(err)
	}
	if err := ss.UpdateAgentState("myworld/governor", "working", writID); err != nil {
		t.Fatal(err)
	}

	ctx, err := protocol.PopulateWritContext("myworld", "governor", "governor")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ctx.TetheredWrits) != 1 {
		t.Fatalf("expected 1 tethered writ, got %d", len(ctx.TetheredWrits))
	}
	if ctx.ActiveWritID != writID {
		t.Errorf("active writ ID = %q, want %q", ctx.ActiveWritID, writID)
	}
	if ctx.ActiveTitle != "Governor task" {
		t.Errorf("active title = %q, want %q", ctx.ActiveTitle, "Governor task")
	}
}

func TestWritContextEmbeddingInEnvoyContext(t *testing.T) {
	// Verify that WritContext fields are accessible through embedding.
	ctx := protocol.EnvoyClaudeMDContext{
		AgentName: "Echo",
		World:     "myworld",
		SolBinary: "sol",
		WritContext: protocol.WritContext{
			TetheredWrits: []protocol.WritSummary{
				{ID: "sol-1234", Title: "Test", Kind: "code", Status: "tethered"},
			},
			ActiveWritID: "sol-1234",
			ActiveTitle:  "Test",
			ActiveDesc:   "Test description",
			ActiveKind:   "code",
		},
	}

	content := protocol.GenerateEnvoyClaudeMD(ctx)

	if len(content) == 0 {
		t.Fatal("expected non-empty generated content")
	}
	// The generated content should include the active writ section.
	if !containsAll(content, "sol-1234", "Active Writ") {
		t.Error("envoy CLAUDE.md should contain active writ info from embedded WritContext")
	}
}

func TestWritContextEmbeddingInGovernorContext(t *testing.T) {
	// Verify that WritContext fields are accessible through embedding.
	ctx := protocol.GovernorClaudeMDContext{
		World:     "myworld",
		SolBinary: "sol",
		MirrorDir: "../repo",
		WritContext: protocol.WritContext{
			TetheredWrits: []protocol.WritSummary{
				{ID: "sol-5678", Title: "Gov task", Kind: "code", Status: "tethered"},
			},
			ActiveWritID: "sol-5678",
			ActiveTitle:  "Gov task",
			ActiveDesc:   "Governor task description",
			ActiveKind:   "code",
		},
	}

	content := protocol.GenerateGovernorClaudeMD(ctx)

	if len(content) == 0 {
		t.Fatal("expected non-empty generated content")
	}
	if !containsAll(content, "sol-5678", "Active Writ") {
		t.Error("governor CLAUDE.md should contain active writ info from embedded WritContext")
	}
}

// containsAll checks that s contains all of the given substrings.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

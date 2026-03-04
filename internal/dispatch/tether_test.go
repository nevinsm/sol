package dispatch

import (
	"testing"

	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// --- Tether tests ---

func TestTetherHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Fix bug", "Fix the bug", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := Tether(TetherOpts{
		AgentName:  "Meridian",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Meridian" {
		t.Errorf("expected agent name Meridian, got %q", result.AgentName)
	}
	if result.AgentRole != "envoy" {
		t.Errorf("expected role envoy, got %q", result.AgentRole)
	}

	// Verify tether file was written with correct role-aware path.
	tetherID, err := tether.Read("ember", "Meridian", "envoy")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether has %q, expected %q", tetherID, itemID)
	}

	// Verify work item was updated.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "tethered" {
		t.Errorf("expected work item status 'tethered', got %q", item.Status)
	}
	if item.Assignee != "ember/Meridian" {
		t.Errorf("expected assignee 'ember/Meridian', got %q", item.Assignee)
	}

	// Verify agent state was updated.
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "working" {
		t.Errorf("expected agent state 'working', got %q", agent.State)
	}
	if agent.TetherItem != itemID {
		t.Errorf("expected tether item %q, got %q", itemID, agent.TetherItem)
	}
}

func TestTetherGovernor(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Plan sprint", "Plan the sprint", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("governor", "ember", "governor"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	result, err := Tether(TetherOpts{
		AgentName:  "governor",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	if result.AgentRole != "governor" {
		t.Errorf("expected role governor, got %q", result.AgentRole)
	}

	// Verify tether file was written with governor role-aware path.
	tetherID, err := tether.Read("ember", "governor", "governor")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != itemID {
		t.Errorf("tether has %q, expected %q", tetherID, itemID)
	}
}

func TestTetherOutpostAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Add feature", "Add the feature", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Toast", "ember", "agent"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether should work even for outpost agents (no restriction like cast).
	result, err := Tether(TetherOpts{
		AgentName:  "Toast",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	if result.AgentRole != "agent" {
		t.Errorf("expected role agent, got %q", result.AgentRole)
	}
}

func TestTetherRejectsNonOpenItem(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Done task", "Already done", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}
	if err := worldStore.UpdateWorkItem(itemID, store.WorkItemUpdates{Status: "tethered"}); err != nil {
		t.Fatalf("failed to update work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	_, err = Tether(TetherOpts{
		AgentName:  "Meridian",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for non-open work item")
	}
	if want := `work item "` + itemID + `" has status "tethered", expected "open"`; err.Error() != want {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTetherRejectsNonIdleAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("New task", "Something new", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	if err := sphereStore.UpdateAgentState("ember/Meridian", "working", "sol-other"); err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	_, err = Tether(TetherOpts{
		AgentName:  "Meridian",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for non-idle agent")
	}
	if want := `agent "ember/Meridian" has state "working", expected "idle"`; err.Error() != want {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTetherRejectsUnknownAgent(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Task", "Do it", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	_, err = Tether(TetherOpts{
		AgentName:  "Ghost",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

// --- Untether tests ---

func TestUntetherHappyPath(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Fix bug", "Fix the bug", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// First tether.
	_, err = Tether(TetherOpts{
		AgentName:  "Meridian",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	// Now untether.
	result, err := Untether(UntetherOpts{
		AgentName: "Meridian",
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Untether failed: %v", err)
	}

	if result.WorkItemID != itemID {
		t.Errorf("expected work item ID %q, got %q", itemID, result.WorkItemID)
	}
	if result.AgentName != "Meridian" {
		t.Errorf("expected agent name Meridian, got %q", result.AgentName)
	}
	if result.AgentRole != "envoy" {
		t.Errorf("expected role envoy, got %q", result.AgentRole)
	}

	// Verify tether was cleared.
	tetherID, err := tether.Read("ember", "Meridian", "envoy")
	if err != nil {
		t.Fatalf("failed to read tether: %v", err)
	}
	if tetherID != "" {
		t.Errorf("expected empty tether, got %q", tetherID)
	}

	// Verify work item was reset.
	item, err := worldStore.GetWorkItem(itemID)
	if err != nil {
		t.Fatalf("failed to get work item: %v", err)
	}
	if item.Status != "open" {
		t.Errorf("expected work item status 'open', got %q", item.Status)
	}
	if item.Assignee != "" {
		t.Errorf("expected empty assignee, got %q", item.Assignee)
	}

	// Verify agent was reset.
	agent, err := sphereStore.GetAgent("ember/Meridian")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if agent.State != "idle" {
		t.Errorf("expected agent state 'idle', got %q", agent.State)
	}
	if agent.TetherItem != "" {
		t.Errorf("expected empty tether item, got %q", agent.TetherItem)
	}
}

func TestUntetherRejectsUntetheredAgent(t *testing.T) {
	_, sphereStore := setupStores(t)

	if _, err := sphereStore.CreateAgent("Meridian", "ember", "envoy"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	worldStore2, err := store.OpenWorld("ember")
	if err != nil {
		t.Fatalf("failed to open world store: %v", err)
	}
	defer worldStore2.Close()

	_, err = Untether(UntetherOpts{
		AgentName: "Meridian",
		World:     "ember",
	}, worldStore2, sphereStore, nil)
	if err == nil {
		t.Fatal("expected error for untethered agent")
	}
	if want := `no work tethered for agent "Meridian" in world "ember"`; err.Error() != want {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTetherThenUntetherRoundTrip(t *testing.T) {
	worldStore, sphereStore := setupStores(t)

	itemID, err := worldStore.CreateWorkItem("Round trip", "Test full cycle", "operator", 2, nil)
	if err != nil {
		t.Fatalf("failed to create work item: %v", err)
	}

	if _, err := sphereStore.CreateAgent("governor", "ember", "governor"); err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Tether.
	_, err = Tether(TetherOpts{
		AgentName:  "governor",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Tether failed: %v", err)
	}

	// Untether.
	_, err = Untether(UntetherOpts{
		AgentName: "governor",
		World:     "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Untether failed: %v", err)
	}

	// Agent should be idle and retetherable.
	_, err = Tether(TetherOpts{
		AgentName:  "governor",
		WorkItemID: itemID,
		World:      "ember",
	}, worldStore, sphereStore, nil)
	if err != nil {
		t.Fatalf("Re-tether after untether failed: %v", err)
	}
}

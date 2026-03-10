package sitrep_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/sitrep"
	"github.com/nevinsm/sol/internal/store"
)

func TestBuildPromptEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	data := &sitrep.CollectedData{
		Scope:            "sphere",
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Should contain the system prompt and the data payload.
	if !strings.Contains(prompt, "situation report") {
		t.Error("prompt should contain system prompt text")
	}
	if !strings.Contains(prompt, "Scope: sphere") {
		t.Error("prompt should contain scope")
	}
	if !strings.Contains(prompt, "No agents registered") {
		t.Error("prompt should indicate no agents")
	}
}

func TestBuildPromptWithData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope: "test-world",
		Agents: []store.Agent{
			{ID: "test-world/Alpha", Name: "Alpha", World: "test-world", Role: "agent", State: "working", ActiveWrit: "sol-abc123", CreatedAt: now, UpdatedAt: now},
			{ID: "test-world/Beta", Name: "Beta", World: "test-world", Role: "agent", State: "idle", CreatedAt: now, UpdatedAt: now},
		},
		Caravans:         []store.Caravan{},
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		Worlds: []sitrep.WorldData{
			{
				Name: "test-world",
				Writs: []store.Writ{
					{ID: "sol-abc123", Title: "Implement feature X", Status: "tethered", Priority: 1, Assignee: "Alpha", CreatedAt: now, UpdatedAt: now},
					{ID: "sol-def456", Title: "Fix bug Y", Status: "open", Priority: 2, CreatedAt: now, UpdatedAt: now},
				},
				MergeRequests: []store.MergeRequest{
					{ID: "mr-111", WritID: "sol-abc123", Branch: "feat/x", Phase: "ready", CreatedAt: now, UpdatedAt: now},
				},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Check that prompt contains key data elements.
	checks := []string{
		"Scope: test-world",
		"working: 1",
		"Alpha",
		"sol-abc123",
		"Implement feature X",
		"Fix bug Y",
		"mr-111",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt should contain %q", check)
		}
	}
}

func TestEjectAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Load should return embedded default when no ejected file exists.
	prompt1, err := sitrep.LoadSystemPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt1, "situation report") {
		t.Error("default prompt should contain 'situation report'")
	}

	// Eject.
	dest, err := sitrep.Eject(false)
	if err != nil {
		t.Fatal(err)
	}
	expectedDest := filepath.Join(dir, "sitrep-prompt.md")
	if dest != expectedDest {
		t.Errorf("expected dest %q, got %q", expectedDest, dest)
	}

	// File should exist.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("ejected file should exist: %v", err)
	}

	// Eject again without force should fail.
	_, err = sitrep.Eject(false)
	if err == nil {
		t.Error("expected error when ejecting without force")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err)
	}

	// Modify the ejected file.
	customPrompt := "Custom sitrep prompt for testing."
	if err := os.WriteFile(dest, []byte(customPrompt), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load should return the custom prompt.
	prompt2, err := sitrep.LoadSystemPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if prompt2 != customPrompt {
		t.Errorf("expected custom prompt, got %q", prompt2)
	}

	// Eject with force should overwrite.
	_, err = sitrep.Eject(true)
	if err != nil {
		t.Fatal(err)
	}

	// Should be back to default.
	prompt3, err := sitrep.LoadSystemPrompt()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt3, "situation report") {
		t.Error("after force eject, should have default prompt")
	}
}

func TestFormatDataPayloadBlockedMRs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope:            "test-world",
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		Worlds: []sitrep.WorldData{
			{
				Name: "test-world",
				BlockedMRs: []store.MergeRequest{
					{ID: "mr-999", WritID: "sol-aaa", BlockedBy: "sol-bbb", CreatedAt: now, UpdatedAt: now},
				},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "Blocked Merge Requests") {
		t.Error("prompt should contain blocked MR section")
	}
	if !strings.Contains(prompt, "mr-999") {
		t.Error("prompt should contain blocked MR ID")
	}
	if !strings.Contains(prompt, "blocked by: sol-bbb") {
		t.Error("prompt should contain blocker ID")
	}
}

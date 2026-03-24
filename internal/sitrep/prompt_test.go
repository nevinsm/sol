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

func TestFormatDataPayloadEscalations(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope: "sphere",
		Escalations: []store.Escalation{
			{ID: "esc-001", Severity: "critical", Source: "forge", Description: "Merge conflict on main", SourceRef: "mr:mr-abc123", Status: "open", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now},
			{ID: "esc-002", Severity: "low", Source: "sentinel", Description: "Health check slow", Status: "open", CreatedAt: now.Add(-30 * time.Minute), UpdatedAt: now},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Section should exist.
	if !strings.Contains(prompt, "## Escalations") {
		t.Error("prompt should contain Escalations section")
	}
	// Should contain escalation details.
	if !strings.Contains(prompt, "esc-001") {
		t.Error("prompt should contain escalation ID esc-001")
	}
	if !strings.Contains(prompt, "critical") {
		t.Error("prompt should contain severity 'critical'")
	}
	if !strings.Contains(prompt, "Merge conflict on main") {
		t.Error("prompt should contain escalation description")
	}
	if !strings.Contains(prompt, "mr:mr-abc123") {
		t.Error("prompt should contain source ref")
	}
	if !strings.Contains(prompt, "sentinel") {
		t.Error("prompt should contain source for escalation without source_ref")
	}
}

func TestFormatDataPayloadNoEscalations(t *testing.T) {
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

	// Section should NOT exist when no escalations.
	if strings.Contains(prompt, "## Escalations") {
		t.Error("prompt should not contain Escalations section when there are none")
	}
}

func TestFormatDataPayloadForgeStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope:            "test-world",
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		ForgeStatuses: map[string]sitrep.ForgeStatus{
			"test-world": {
				Running:       true,
				Paused:        false,
				Merging:       true,
				QueueReady:    3,
				QueueFailed:   1,
				QueueBlocked:  2,
				MergedTotal:   15,
				MergedLast1h:  2,
				MergedLast24h: 8,
				ClaimedMR: &sitrep.ForgeMRDetail{
					ID:     "mr-claim1",
					WritID: "sol-abc123",
					Title:  "Implement feature X",
					Branch: "feat/x",
					Age:    "5m30s",
				},
				LastMerge: &sitrep.ForgeEvent{
					MRID:      "mr-merge1",
					Title:     "Fix bug Y",
					Branch:    "fix/y",
					Timestamp: now.Add(-10 * time.Minute),
				},
				LastFailure: &sitrep.ForgeEvent{
					MRID:      "mr-fail1",
					Title:     "Broken thing",
					Branch:    "feat/broken",
					Timestamp: now.Add(-2 * time.Hour),
				},
			},
		},
		Worlds: []sitrep.WorldData{
			{Name: "test-world"},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	checks := []string{
		"## Forge: test-world",
		"Process: running, merging: active",
		"Queue: 3 ready, 1 failed, 2 blocked",
		"Velocity: 2 merged in last hour, 8 in last 24h (15 total)",
		"Claimed: mr-claim1",
		"Implement feature X",
		"Last merge: mr-merge1",
		"Fix bug Y",
		"Last failure: mr-fail1",
		"Broken thing",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt should contain %q", check)
		}
	}
}

func TestFormatDataPayloadForgeStatusStopped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	data := &sitrep.CollectedData{
		Scope:            "test-world",
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		ForgeStatuses: map[string]sitrep.ForgeStatus{
			"test-world": {
				Running: false,
				Paused:  false,
				Merging: false,
			},
		},
		Worlds: []sitrep.WorldData{
			{Name: "test-world"},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "Process: stopped, merging: inactive") {
		t.Error("prompt should show stopped/inactive for non-running forge")
	}
}

func TestFormatDataPayloadForgeStatusPaused(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	data := &sitrep.CollectedData{
		Scope:            "test-world",
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		ForgeStatuses: map[string]sitrep.ForgeStatus{
			"test-world": {
				Running: true,
				Paused:  true,
				Merging: false,
			},
		},
		Worlds: []sitrep.WorldData{
			{Name: "test-world"},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "Process: paused, merging: inactive") {
		t.Error("prompt should show paused for running+paused forge")
	}
}

func TestFormatDataPayloadCaravanPhaseBreakdown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	data := &sitrep.CollectedData{
		Scope: "sphere",
		Caravans: []store.Caravan{
			{ID: "car-1", Name: "Arc 3", Status: "open"},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{
			"car-1": {
				{WritID: "sol-aaa", World: "dev", Phase: 0, WritStatus: "closed", Ready: false},
				{WritID: "sol-bbb", World: "dev", Phase: 0, WritStatus: "closed", Ready: false},
				{WritID: "sol-ccc", World: "dev", Phase: 1, WritStatus: "done", Ready: false},
				{WritID: "sol-ddd", World: "dev", Phase: 1, WritStatus: "open", Ready: true},
				{WritID: "sol-eee", World: "dev", Phase: 2, WritStatus: "open", Ready: false},
			},
		},
		CaravanDeps:            map[string][]string{},
		CaravanUnsatisfiedDeps: map[string][]string{},
		Worlds: []sitrep.WorldData{
			{
				Name: "dev",
				Writs: []store.Writ{
					{ID: "sol-aaa", Title: "Setup infra"},
					{ID: "sol-bbb", Title: "Init DB"},
					{ID: "sol-ccc", Title: "Build API"},
					{ID: "sol-ddd", Title: "Add auth"},
					{ID: "sol-eee", Title: "Deploy service"},
				},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Check phase breakdown is present.
	checks := []string{
		"Phase 0 (2 items): 2 closed, 0 done, 0 ready, 0 active, 0 blocked",
		"Phase 1 (2 items): 0 closed, 1 done, 1 ready, 0 active, 0 blocked",
		"Phase 2 (1 items): 0 closed, 0 done, 0 ready, 0 active, 1 blocked",
		"ready: Add auth (sol-ddd)",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt should contain %q\ngot:\n%s", check, prompt)
		}
	}
}

func TestFormatDataPayloadCaravanDependencies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	data := &sitrep.CollectedData{
		Scope: "sphere",
		Caravans: []store.Caravan{
			{ID: "car-1", Name: "Health Scan", Status: "open"},
			{ID: "car-2", Name: "Session Concurrency", Status: "open"},
		},
		CaravanReadiness:       map[string][]store.CaravanItemStatus{},
		CaravanDeps:            map[string][]string{"car-2": {"car-1"}},
		CaravanUnsatisfiedDeps: map[string][]string{"car-2": {"car-1"}},
		Worlds:                 []sitrep.WorldData{},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Session Concurrency should show blocked by Health Scan.
	if !strings.Contains(prompt, "Blocked by: Health Scan (unsatisfied)") {
		t.Errorf("prompt should show unsatisfied dependency\ngot:\n%s", prompt)
	}

	// Health Scan should NOT show any dependency line.
	// Find the Health Scan section and check it doesn't contain "Blocked" or "Dependencies".
	healthIdx := strings.Index(prompt, "### Health Scan")
	sessionIdx := strings.Index(prompt, "### Session Concurrency")
	if healthIdx < 0 || sessionIdx < 0 {
		t.Fatal("expected both caravan headers in prompt")
	}
	healthSection := prompt[healthIdx:sessionIdx]
	if strings.Contains(healthSection, "Blocked by") || strings.Contains(healthSection, "Dependencies") {
		t.Errorf("Health Scan should have no dependency line, got: %s", healthSection)
	}
}

func TestFormatDataPayloadCaravanDepsSatisfied(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	data := &sitrep.CollectedData{
		Scope: "sphere",
		Caravans: []store.Caravan{
			{ID: "car-1", Name: "Prereq", Status: "open"},
			{ID: "car-2", Name: "Main Work", Status: "open"},
		},
		CaravanReadiness:       map[string][]store.CaravanItemStatus{},
		CaravanDeps:            map[string][]string{"car-2": {"car-1"}},
		CaravanUnsatisfiedDeps: map[string][]string{},
		Worlds:                 []sitrep.WorldData{},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "Dependencies: Prereq (all satisfied)") {
		t.Errorf("prompt should show satisfied dependencies\ngot:\n%s", prompt)
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

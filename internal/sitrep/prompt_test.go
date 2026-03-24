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
		// Age annotations.
		"last updated just now",  // agent age
		"updated just now",       // writ age
		"created just now",       // MR ready age
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
	// Should contain age annotations.
	if !strings.Contains(prompt, "opened 2h ago") {
		t.Errorf("prompt should contain 'opened 2h ago' for 2-hour-old escalation, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "opened 30m ago") {
		t.Errorf("prompt should contain 'opened 30m ago' for 30-minute-old escalation, got:\n%s", prompt)
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
		"(10m ago)",  // last merge age
		"Last failure: mr-fail1",
		"Broken thing",
		"(2h ago)",   // last failure age
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

func TestFormatDataPayloadDispatchable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope: "sphere",
		Agents: []store.Agent{
			{ID: "dev/Alpha", Name: "Alpha", World: "dev", Role: "outpost", State: "idle", CreatedAt: now, UpdatedAt: now},
			{ID: "dev/Beta", Name: "Beta", World: "dev", Role: "outpost", State: "working", ActiveWrit: "sol-aaa", CreatedAt: now, UpdatedAt: now},
			{ID: "dev/Gamma", Name: "Gamma", World: "dev", Role: "outpost", State: "idle", CreatedAt: now, UpdatedAt: now},
		},
		Caravans: []store.Caravan{
			{ID: "car-1", Name: "Health Scan", Status: "open"},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{
			"car-1": {
				{WritID: "sol-bbb", World: "dev", Phase: 2, WritStatus: "open", Ready: true},
			},
		},
		CaravanDeps:            map[string][]string{},
		CaravanUnsatisfiedDeps: map[string][]string{},
		Worlds: []sitrep.WorldData{
			{
				Name: "dev",
				ReadyToDispatch: []store.Writ{
					{ID: "sol-bbb", Title: "Fix consul gaps", Status: "open", Priority: 2, CreatedAt: now, UpdatedAt: now},
					{ID: "sol-ccc", Title: "Add metrics", Status: "open", Priority: 3, CreatedAt: now, UpdatedAt: now},
				},
				Writs: []store.Writ{
					{ID: "sol-aaa", Title: "Active writ", Status: "tethered"},
					{ID: "sol-bbb", Title: "Fix consul gaps", Status: "open"},
					{ID: "sol-ccc", Title: "Add metrics", Status: "open"},
				},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	checks := []string{
		"## Dispatchable",
		"2 idle outpost agents, 2 writs ready for dispatch",
		"Idle agents: Alpha, Gamma",
		"Ready writs:",
		"sol-bbb — Fix consul gaps [dev] (Phase 2, Health Scan caravan)",
		"sol-ccc — Add metrics [dev]",
	}
	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt should contain %q\ngot:\n%s", check, prompt)
		}
	}

	// Dispatchable section should appear before Agents section.
	dispIdx := strings.Index(prompt, "## Dispatchable")
	agentIdx := strings.Index(prompt, "## Agents")
	if dispIdx < 0 || agentIdx < 0 {
		t.Fatal("expected both Dispatchable and Agents sections")
	}
	if dispIdx >= agentIdx {
		t.Error("Dispatchable section should appear before Agents section")
	}
}

func TestFormatDataPayloadDispatchableOmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope: "sphere",
		Agents: []store.Agent{
			// Working agent, not idle.
			{ID: "dev/Alpha", Name: "Alpha", World: "dev", Role: "outpost", State: "working", ActiveWrit: "sol-aaa", CreatedAt: now, UpdatedAt: now},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		Worlds: []sitrep.WorldData{
			{
				Name:            "dev",
				ReadyToDispatch: []store.Writ{}, // No ready writs.
				Writs:           []store.Writ{{ID: "sol-aaa", Title: "Active writ", Status: "tethered"}},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(prompt, "## Dispatchable") {
		t.Error("Dispatchable section should be omitted when no idle agents and no ready writs")
	}
}

func TestFormatDataPayloadDispatchableSingular(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope: "sphere",
		Agents: []store.Agent{
			{ID: "dev/Alpha", Name: "Alpha", World: "dev", Role: "outpost", State: "idle", CreatedAt: now, UpdatedAt: now},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		Worlds: []sitrep.WorldData{
			{
				Name: "dev",
				ReadyToDispatch: []store.Writ{
					{ID: "sol-aaa", Title: "Fix bug", Status: "open", Priority: 2, CreatedAt: now, UpdatedAt: now},
				},
				Writs: []store.Writ{{ID: "sol-aaa", Title: "Fix bug", Status: "open"}},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(prompt, "1 idle outpost agent, 1 writ ready for dispatch") {
		t.Errorf("prompt should use singular form\ngot:\n%s", prompt)
	}
}

func TestFormatDataPayloadDispatchableIdleOnlyNoWrits(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	data := &sitrep.CollectedData{
		Scope: "sphere",
		Agents: []store.Agent{
			{ID: "dev/Alpha", Name: "Alpha", World: "dev", Role: "outpost", State: "idle", CreatedAt: now, UpdatedAt: now},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		Worlds: []sitrep.WorldData{
			{Name: "dev", ReadyToDispatch: []store.Writ{}},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Should still show section because there's an idle agent.
	if !strings.Contains(prompt, "## Dispatchable") {
		t.Error("Dispatchable section should appear when idle agents exist")
	}
	if !strings.Contains(prompt, "1 idle outpost agent, no writs ready") {
		t.Errorf("prompt should show idle agents with no writs\ngot:\n%s", prompt)
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

func TestRelativeAgeSince(t *testing.T) {
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		t        time.Time
		expected string
	}{
		{"just now - 0 seconds", now, "just now"},
		{"just now - 30 seconds", now.Add(-30 * time.Second), "just now"},
		{"just now - 59 seconds", now.Add(-59 * time.Second), "just now"},
		{"1 minute", now.Add(-1 * time.Minute), "1m ago"},
		{"5 minutes", now.Add(-5 * time.Minute), "5m ago"},
		{"30 minutes", now.Add(-30 * time.Minute), "30m ago"},
		{"59 minutes", now.Add(-59 * time.Minute), "59m ago"},
		{"1 hour", now.Add(-1 * time.Hour), "1h ago"},
		{"2 hours", now.Add(-2 * time.Hour), "2h ago"},
		{"23 hours", now.Add(-23 * time.Hour), "23h ago"},
		{"1 day", now.Add(-24 * time.Hour), "1d ago"},
		{"3 days", now.Add(-3 * 24 * time.Hour), "3d ago"},
		{"7 days", now.Add(-7 * 24 * time.Hour), "7d ago"},
		{"14 days", now.Add(-14 * 24 * time.Hour), "14d ago"},
		{"future time", now.Add(5 * time.Minute), "just now"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sitrep.RelativeAgeSince(tt.t, now)
			if got != tt.expected {
				t.Errorf("RelativeAgeSince(%v, %v) = %q, want %q", tt.t, now, got, tt.expected)
			}
		})
	}
}

func TestAgeStringsInFormattedOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	now := time.Now().UTC()
	claimedAt := now.Add(-5 * time.Minute)
	data := &sitrep.CollectedData{
		Scope: "test-world",
		Agents: []store.Agent{
			{ID: "test-world/Alpha", Name: "Alpha", World: "test-world", Role: "agent", State: "working", ActiveWrit: "sol-aaa", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-3 * time.Minute)},
			{ID: "test-world/Beta", Name: "Beta", World: "test-world", Role: "agent", State: "stalled", ActiveWrit: "sol-bbb", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-45 * time.Minute)},
			{ID: "test-world/Gamma", Name: "Gamma", World: "test-world", Role: "agent", State: "idle", CreatedAt: now, UpdatedAt: now},
		},
		Escalations: []store.Escalation{
			{ID: "esc-001", Severity: "critical", Source: "forge", Description: "Merge conflict", SourceRef: "mr:mr-abc", Status: "open", CreatedAt: now.Add(-90 * time.Minute), UpdatedAt: now},
		},
		CaravanReadiness: map[string][]store.CaravanItemStatus{},
		Worlds: []sitrep.WorldData{
			{
				Name: "test-world",
				Writs: []store.Writ{
					{ID: "sol-aaa", Title: "Feature A", Status: "tethered", Priority: 1, Assignee: "Alpha", CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-10 * time.Minute)},
					{ID: "sol-bbb", Title: "Feature B", Status: "open", Priority: 2, CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-30 * time.Minute)},
				},
				MergeRequests: []store.MergeRequest{
					{ID: "mr-001", WritID: "sol-aaa", Branch: "feat/a", Phase: "ready", CreatedAt: now.Add(-20 * time.Minute), UpdatedAt: now},
					{ID: "mr-002", WritID: "sol-bbb", Branch: "feat/b", Phase: "claimed", ClaimedAt: &claimedAt, CreatedAt: now.Add(-15 * time.Minute), UpdatedAt: now},
					{ID: "mr-003", WritID: "sol-aaa", Branch: "feat/a-v2", Phase: "failed", CreatedAt: now.Add(-1 * time.Hour), UpdatedAt: now.Add(-25 * time.Minute)},
				},
			},
		},
	}

	prompt, err := sitrep.BuildPrompt(data)
	if err != nil {
		t.Fatal(err)
	}

	// Agent ages.
	if !strings.Contains(prompt, "working (last updated 3m ago)") {
		t.Errorf("prompt should contain working agent age\n%s", prompt)
	}
	if !strings.Contains(prompt, "stalled (last updated 45m ago)") {
		t.Errorf("prompt should contain stalled agent age\n%s", prompt)
	}
	// Idle agents should NOT have age.
	if strings.Contains(prompt, "Gamma") && strings.Contains(prompt, "last updated") {
		// Gamma is idle, so it should not appear in detail lines at all.
		gammaIdx := strings.Index(prompt, "Gamma")
		if gammaIdx >= 0 {
			// Check 100 chars around Gamma for "last updated".
			end := gammaIdx + 100
			if end > len(prompt) {
				end = len(prompt)
			}
			section := prompt[gammaIdx:end]
			if strings.Contains(section, "last updated") {
				t.Errorf("idle agent Gamma should not have age annotation")
			}
		}
	}

	// Writ ages.
	if !strings.Contains(prompt, "p1, updated 10m ago") {
		t.Errorf("prompt should contain writ age 'updated 10m ago'\n%s", prompt)
	}
	if !strings.Contains(prompt, "p2, updated 30m ago") {
		t.Errorf("prompt should contain writ age 'updated 30m ago'\n%s", prompt)
	}

	// MR ages.
	if !strings.Contains(prompt, "created 20m ago") {
		t.Errorf("prompt should contain MR ready age 'created 20m ago'\n%s", prompt)
	}
	if !strings.Contains(prompt, "claimed 5m ago") {
		t.Errorf("prompt should contain MR claimed age 'claimed 5m ago'\n%s", prompt)
	}
	if !strings.Contains(prompt, "failed 25m ago") {
		t.Errorf("prompt should contain MR failed age 'failed 25m ago'\n%s", prompt)
	}

	// Escalation age.
	if !strings.Contains(prompt, "opened 1h ago") {
		t.Errorf("prompt should contain escalation age 'opened 1h ago'\n%s", prompt)
	}
}

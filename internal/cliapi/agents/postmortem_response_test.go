package agents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/store"
)

func TestPostmortemAgentFromStore(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sa := store.Agent{
		ID:         "sol-dev/Nova",
		Name:       "Nova",
		World:      "sol-dev",
		Role:       "outpost",
		State:      "working",
		ActiveWrit: "sol-a1b2c3d4e5f6a7b8",
		CreatedAt:  now.Add(-time.Hour),
		UpdatedAt:  now,
	}

	pa := PostmortemAgentFromStore(sa)

	if pa.Name != "Nova" {
		t.Errorf("Name = %q, want %q", pa.Name, "Nova")
	}
	if pa.World != "sol-dev" {
		t.Errorf("World = %q, want %q", pa.World, "sol-dev")
	}
	if pa.Role != "outpost" {
		t.Errorf("Role = %q, want %q", pa.Role, "outpost")
	}
	if pa.State != "working" {
		t.Errorf("State = %q, want %q", pa.State, "working")
	}
	if pa.ActiveWrit != "sol-a1b2c3d4e5f6a7b8" {
		t.Errorf("ActiveWrit = %q, want %q", pa.ActiveWrit, "sol-a1b2c3d4e5f6a7b8")
	}
	if !pa.CreatedAt.Equal(sa.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", pa.CreatedAt, sa.CreatedAt)
	}
	if !pa.UpdatedAt.Equal(sa.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", pa.UpdatedAt, sa.UpdatedAt)
	}
}

func TestPostmortemAgentFromStoreMinimal(t *testing.T) {
	sa := store.Agent{
		Name:  "Idle1",
		World: "test",
		Role:  "outpost",
		State: "idle",
	}

	pa := PostmortemAgentFromStore(sa)

	if pa.ActiveWrit != "" {
		t.Errorf("ActiveWrit = %q, want empty", pa.ActiveWrit)
	}
}

func TestPostmortemWritFromStore(t *testing.T) {
	sw := store.Writ{
		ID:     "sol-abcdef1234567890",
		Title:  "Test writ",
		Status: "tethered",
	}

	pw := PostmortemWritFromStore(sw)

	if pw.ID != "sol-abcdef1234567890" {
		t.Errorf("ID = %q, want %q", pw.ID, "sol-abcdef1234567890")
	}
	if pw.Title != "Test writ" {
		t.Errorf("Title = %q, want %q", pw.Title, "Test writ")
	}
	if pw.Status != "tethered" {
		t.Errorf("Status = %q, want %q", pw.Status, "tethered")
	}
}

func TestPostmortemHandoffFromState(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	hs := handoff.State{
		Summary:       "context exhaustion mid-task",
		HandedOffAt:   now,
		RecentCommits: []string{"abc1234 feat: partial work", "def5678 progress: investigation"},
	}

	ph := PostmortemHandoffFromState(hs)

	if ph.Summary != "context exhaustion mid-task" {
		t.Errorf("Summary = %q, want %q", ph.Summary, "context exhaustion mid-task")
	}
	if !ph.HandedOffAt.Equal(now) {
		t.Errorf("HandedOffAt = %v, want %v", ph.HandedOffAt, now)
	}
	if len(ph.Commits) != 2 {
		t.Fatalf("Commits len = %d, want 2", len(ph.Commits))
	}
	if ph.Commits[0] != "abc1234 feat: partial work" {
		t.Errorf("Commits[0] = %q, want %q", ph.Commits[0], "abc1234 feat: partial work")
	}
}

func TestPostmortemHandoffFromStateNoCommits(t *testing.T) {
	hs := handoff.State{
		Summary:     "clean handoff",
		HandedOffAt: time.Now().UTC(),
	}

	ph := PostmortemHandoffFromState(hs)

	if ph.Commits != nil {
		t.Errorf("Commits = %v, want nil", ph.Commits)
	}
}

func TestPostmortemReportJSONShape(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	startedAt := now.Add(-30 * time.Minute)

	report := PostmortemReport{
		Agent: PostmortemAgent{
			Name:       "Nova",
			World:      "sol-dev",
			Role:       "outpost",
			State:      "working",
			ActiveWrit: "sol-a1b2c3d4e5f6a7b8",
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now,
		},
		Session: PostmortemSession{
			Name:      "sol-sol-dev-Nova",
			Alive:     false,
			StartedAt: &startedAt,
			Lifetime:  "30m0s",
		},
		Writ: &PostmortemWrit{
			ID:     "sol-a1b2c3d4e5f6a7b8",
			Title:  "Test writ",
			Status: "tethered",
		},
		Commits:    []string{"abc1234 feat: something"},
		LastOutput: "last line of output",
		Handoff: &PostmortemHandoff{
			Summary:     "context exhaustion",
			HandedOffAt: now,
			Commits:     []string{"abc1234 feat: something"},
		},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Verify key fields exist in JSON output.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Top-level keys.
	for _, key := range []string{"agent", "session", "writ", "commits", "last_output", "handoff"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}

	// Agent keys.
	agent := m["agent"].(map[string]any)
	for _, key := range []string{"name", "world", "role", "state", "active_writ", "created_at", "updated_at"} {
		if _, ok := agent[key]; !ok {
			t.Errorf("missing agent key %q", key)
		}
	}

	// Session keys.
	sess := m["session"].(map[string]any)
	for _, key := range []string{"name", "alive", "started_at", "lifetime"} {
		if _, ok := sess[key]; !ok {
			t.Errorf("missing session key %q", key)
		}
	}

	// Writ keys.
	writ := m["writ"].(map[string]any)
	for _, key := range []string{"id", "title", "status"} {
		if _, ok := writ[key]; !ok {
			t.Errorf("missing writ key %q", key)
		}
	}

	// Handoff keys.
	ho := m["handoff"].(map[string]any)
	for _, key := range []string{"summary", "handed_off_at", "commits"} {
		if _, ok := ho[key]; !ok {
			t.Errorf("missing handoff key %q", key)
		}
	}
}

func TestPostmortemReportJSONOmitsNullOptionals(t *testing.T) {
	report := PostmortemReport{
		Agent: PostmortemAgent{
			Name:  "Test",
			World: "test",
			Role:  "outpost",
			State: "idle",
		},
		Session: PostmortemSession{
			Name:  "sol-test-Test",
			Alive: false,
		},
		Commits: []string{},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// writ, last_output, and handoff should be omitted.
	if _, ok := m["writ"]; ok {
		t.Error("writ should be omitted when nil")
	}
	if _, ok := m["last_output"]; ok {
		t.Error("last_output should be omitted when empty")
	}
	if _, ok := m["handoff"]; ok {
		t.Error("handoff should be omitted when nil")
	}

	// Session started_at and lifetime should be omitted.
	sess := m["session"].(map[string]any)
	if _, ok := sess["started_at"]; ok {
		t.Error("session.started_at should be omitted when nil")
	}
	if _, ok := sess["lifetime"]; ok {
		t.Error("session.lifetime should be omitted when empty")
	}
}

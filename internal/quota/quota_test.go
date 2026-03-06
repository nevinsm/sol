package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantLimit  bool
		wantReset  bool
	}{
		{
			name:      "no rate limit",
			output:    "Working on task...\nCode updated successfully.",
			wantLimit: false,
		},
		{
			name:      "hit limit pattern",
			output:    "Error: You've hit your daily limit\nPlease wait.",
			wantLimit: true,
		},
		{
			name:      "limit resets pattern",
			output:    "Usage limit · resets 3:45pm\nStop and wait.",
			wantLimit: true,
			wantReset: true,
		},
		{
			name:      "stop and wait pattern",
			output:    "Stop and wait for limit to reset",
			wantLimit: true,
		},
		{
			name:      "API rate limit",
			output:    "API Error: Rate limit reached\nRetrying...",
			wantLimit: true,
		},
		{
			name:      "OAuth token revoked",
			output:    "OAuth token revoked\nPlease re-authenticate.",
			wantLimit: true,
		},
		{
			name:      "OAuth token expired",
			output:    "OAuth token has expired\nPlease log in again.",
			wantLimit: true,
		},
		{
			name:      "limit resets with hour only",
			output:    "limit · resets 4pm",
			wantLimit: true,
			wantReset: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limited, resetsAt := DetectRateLimit(tt.output)
			if limited != tt.wantLimit {
				t.Errorf("limited = %v, want %v", limited, tt.wantLimit)
			}
			if tt.wantReset && resetsAt == nil {
				t.Error("expected non-nil resetsAt")
			}
			if !tt.wantReset && resetsAt != nil {
				t.Errorf("expected nil resetsAt, got %v", resetsAt)
			}
		})
	}
}

func TestParseResetTime(t *testing.T) {
	tests := []struct {
		input    string
		wantHour int
		wantMin  int
		wantErr  bool
	}{
		{"3:45pm", 15, 45, false},
		{"4pm", 16, 0, false},
		{"12pm", 12, 0, false},
		{"12am", 0, 0, false},
		{"11:30am", 11, 30, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseResetTime(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Result is in UTC, so check relative to now.
			if result.IsZero() {
				t.Error("expected non-zero time")
			}
		})
	}
}

func TestStateSaveLoad(t *testing.T) {
	// Use a temp dir as SOL_HOME.
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	now := time.Now().UTC().Truncate(time.Second)
	state := &State{
		Accounts: map[string]*AccountState{
			"alice": {
				Status:    Limited,
				LimitedAt: &now,
				LastUsed:  &now,
			},
			"bob": {
				Status: Available,
			},
		},
	}

	if err := Save(state); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(tmpDir, ".accounts", "runtime", "quota.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("quota.json not created: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(loaded.Accounts))
	}

	if loaded.Accounts["alice"].Status != Limited {
		t.Errorf("alice status = %s, want limited", loaded.Accounts["alice"].Status)
	}
	if loaded.Accounts["bob"].Status != Available {
		t.Errorf("bob status = %s, want available", loaded.Accounts["bob"].Status)
	}
}

func TestLoadNonexistentReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("SOL_HOME", tmpDir)

	state, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(state.Accounts) != 0 {
		t.Errorf("expected empty accounts, got %d", len(state.Accounts))
	}
}

func TestMarkLimited(t *testing.T) {
	state := &State{Accounts: make(map[string]*AccountState)}

	resetsAt := time.Now().Add(1 * time.Hour).UTC()
	state.MarkLimited("alice", &resetsAt)

	acct := state.Accounts["alice"]
	if acct.Status != Limited {
		t.Errorf("status = %s, want limited", acct.Status)
	}
	if acct.LimitedAt == nil {
		t.Error("expected non-nil LimitedAt")
	}
	if acct.ResetsAt == nil || !acct.ResetsAt.Equal(resetsAt) {
		t.Error("ResetsAt mismatch")
	}
}

func TestMarkAvailable(t *testing.T) {
	now := time.Now().UTC()
	state := &State{
		Accounts: map[string]*AccountState{
			"alice": {
				Status:    Limited,
				LimitedAt: &now,
			},
		},
	}

	state.MarkAvailable("alice")
	if state.Accounts["alice"].Status != Available {
		t.Errorf("status = %s, want available", state.Accounts["alice"].Status)
	}
	if state.Accounts["alice"].LimitedAt != nil {
		t.Error("expected nil LimitedAt after marking available")
	}
}

func TestExpireCooldowns(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour).UTC()
	future := time.Now().Add(1 * time.Hour).UTC()

	state := &State{
		Accounts: map[string]*AccountState{
			"expired": {
				Status:   Limited,
				ResetsAt: &past,
			},
			"still-limited": {
				Status:   Limited,
				ResetsAt: &future,
			},
		},
	}

	state.ExpireCooldowns()

	if state.Accounts["expired"].Status != Available {
		t.Errorf("expired account status = %s, want available", state.Accounts["expired"].Status)
	}
	if state.Accounts["still-limited"].Status != Limited {
		t.Errorf("still-limited account status = %s, want limited", state.Accounts["still-limited"].Status)
	}
}

func TestExtractAgentName(t *testing.T) {
	tests := []struct {
		session string
		world   string
		want    string
	}{
		{"sol-myworld-Toast", "myworld", "Toast"},
		{"sol-dev-Nova", "dev", "Nova"},
		{"other-prefix", "dev", ""},
		{"sol-dev-", "dev", ""},
	}

	for _, tt := range tests {
		got := extractAgentName(tt.session, tt.world)
		if got != tt.want {
			t.Errorf("extractAgentName(%q, %q) = %q, want %q", tt.session, tt.world, got, tt.want)
		}
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(1 * time.Hour)

	state := &State{
		Accounts: map[string]*AccountState{
			"work": {
				Status:    Limited,
				LimitedAt: &now,
				ResetsAt:  &reset,
				LastUsed:  &now,
			},
		},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Accounts["work"].Status != Limited {
		t.Errorf("status = %s, want limited", decoded.Accounts["work"].Status)
	}
	if decoded.Accounts["work"].LimitedAt == nil || !decoded.Accounts["work"].LimitedAt.Equal(now) {
		t.Error("LimitedAt mismatch after round-trip")
	}
}

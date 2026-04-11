package quota

import (
	"encoding/json"
	"testing"
	"time"

	iquota "github.com/nevinsm/sol/internal/quota"
)

func TestNewStatusAccount(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	resets := now.Add(time.Hour)

	state := iquota.AccountState{
		Status:    iquota.Limited,
		LimitedAt: &now,
		ResetsAt:  &resets,
		LastUsed:  &now,
	}

	sa := NewStatusAccount("primary", state)

	if sa.Handle != "primary" {
		t.Errorf("Handle = %q, want %q", sa.Handle, "primary")
	}
	if sa.Status != iquota.Limited {
		t.Errorf("Status = %q, want %q", sa.Status, iquota.Limited)
	}
	if sa.LimitedAt == nil || !sa.LimitedAt.Equal(now) {
		t.Errorf("LimitedAt = %v, want %v", sa.LimitedAt, now)
	}
	if sa.ResetsAt == nil || !sa.ResetsAt.Equal(resets) {
		t.Errorf("ResetsAt = %v, want %v", sa.ResetsAt, resets)
	}
	if sa.LastUsed == nil || !sa.LastUsed.Equal(now) {
		t.Errorf("LastUsed = %v, want %v", sa.LastUsed, now)
	}
	if sa.Window != "" {
		t.Errorf("Window = %q, want empty", sa.Window)
	}
	if sa.Remaining != nil {
		t.Errorf("Remaining = %v, want nil", sa.Remaining)
	}
}

func TestNewStatusAccountAvailable(t *testing.T) {
	state := iquota.AccountState{
		Status: iquota.Available,
	}

	sa := NewStatusAccount("secondary", state)

	if sa.Status != iquota.Available {
		t.Errorf("Status = %q, want %q", sa.Status, iquota.Available)
	}
	if sa.LimitedAt != nil {
		t.Errorf("LimitedAt = %v, want nil", sa.LimitedAt)
	}
	if sa.LastUsed != nil {
		t.Errorf("LastUsed = %v, want nil", sa.LastUsed)
	}
}

func TestStatusResponseJSON(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	resp := StatusResponse{
		Accounts: []StatusAccount{
			{
				Handle:   "acct1",
				Status:   iquota.Available,
				LastUsed: &now,
			},
			{
				Handle: "acct2",
				Status: iquota.Limited,
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Verify JSON field names match expected shape.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := raw["accounts"]; !ok {
		t.Error("expected 'accounts' key in JSON output")
	}

	// Verify per-account field names.
	var accounts []map[string]json.RawMessage
	if err := json.Unmarshal(raw["accounts"], &accounts); err != nil {
		t.Fatalf("Unmarshal accounts error: %v", err)
	}

	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}

	// First account should have handle, status, last_used_at.
	first := accounts[0]
	for _, key := range []string{"handle", "status", "last_used_at"} {
		if _, ok := first[key]; !ok {
			t.Errorf("expected key %q in first account", key)
		}
	}

	// Omitempty: first account should not have limited_at.
	if _, ok := first["limited_at"]; ok {
		t.Error("expected limited_at to be omitted for available account")
	}
}

func TestStatusResponseEmptyAccounts(t *testing.T) {
	resp := StatusResponse{
		Accounts: []StatusAccount{},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Empty arrays should be present, not null.
	expected := `{"accounts":[]}`
	if string(data) != expected {
		t.Errorf("got %s, want %s", string(data), expected)
	}
}

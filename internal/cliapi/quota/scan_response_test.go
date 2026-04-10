package quota

import (
	"encoding/json"
	"testing"

	iquota "github.com/nevinsm/sol/internal/quota"
)

func TestNewScanSession(t *testing.T) {
	r := iquota.ScanResult{
		Session: "sol-dev-Toast",
		Account: "primary",
		Limited: true,
	}

	ss := NewScanSession(r)

	if ss.Session != "sol-dev-Toast" {
		t.Errorf("Session = %q, want %q", ss.Session, "sol-dev-Toast")
	}
	if ss.Account != "primary" {
		t.Errorf("Account = %q, want %q", ss.Account, "primary")
	}
	if !ss.Limited {
		t.Error("Limited = false, want true")
	}
}

func TestNewScanSessions(t *testing.T) {
	results := []iquota.ScanResult{
		{Session: "sol-dev-A", Account: "acct1", Limited: false},
		{Session: "sol-dev-B", Account: "acct2", Limited: true},
	}

	sessions := NewScanSessions(results)

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].Session != "sol-dev-A" {
		t.Errorf("sessions[0].Session = %q, want %q", sessions[0].Session, "sol-dev-A")
	}
	if sessions[1].Limited != true {
		t.Error("sessions[1].Limited = false, want true")
	}
}

func TestNewScanSessionsEmpty(t *testing.T) {
	sessions := NewScanSessions(nil)

	data, err := json.Marshal(sessions)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	// Empty arrays should render as [], not null.
	if string(data) != "[]" {
		t.Errorf("got %s, want []", string(data))
	}
}

func TestScanSessionJSON(t *testing.T) {
	ss := ScanSession{
		Session: "sol-dev-Toast",
		Account: "primary",
		Limited: true,
	}

	data, err := json.Marshal(ss)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	for _, key := range []string{"session", "account", "limited"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected key %q in JSON output", key)
		}
	}
}

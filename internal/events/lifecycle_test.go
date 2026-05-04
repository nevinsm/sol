package events

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestEmitLifecycleNilLogger verifies the helper is a no-op when given
// a nil logger — matches the optional-logger pattern in the daemons.
func TestEmitLifecycleNilLogger(t *testing.T) {
	// Should not panic.
	EmitLifecycle(nil, "ledger_start", "ledger", map[string]any{"port": 4318})
}

// TestEmitLifecycleShape verifies a single emitted event has the
// uniform shape: source = component, actor = component, visibility = "audit".
func TestEmitLifecycleShape(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	EmitLifecycle(logger, "test_lifecycle", "widget", map[string]any{"k": "v"})

	data, err := os.ReadFile(filepath.Join(dir, ".events.jsonl"))
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	var ev Event
	if err := json.Unmarshal(trimTrailingNewline(data), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	if ev.Type != "test_lifecycle" {
		t.Errorf("type: got %q, want %q", ev.Type, "test_lifecycle")
	}
	if ev.Source != "widget" {
		t.Errorf("source: got %q, want %q", ev.Source, "widget")
	}
	if ev.Actor != "widget" {
		t.Errorf("actor: got %q, want %q", ev.Actor, "widget")
	}
	if ev.Visibility != "audit" {
		t.Errorf("visibility: got %q, want %q", ev.Visibility, "audit")
	}
}

// TestLifecycleParityChronicleVsLedger verifies the structural parity
// requirement from V10: a chronicle lifecycle event and a ledger
// lifecycle event share the same source/actor/visibility shape so that
// audit-log consumers can filter them uniformly.
//
// "Same shape" means: source == component for each, actor == component
// for each, visibility == "audit" for both.
func TestLifecycleParityChronicleVsLedger(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger(dir)

	EmitLifecycle(logger, EventChronicleStart, "chronicle",
		map[string]any{"checkpoint_offset": int64(0)})
	EmitLifecycle(logger, EventLedgerStart, "ledger",
		map[string]any{"port": 4318})

	data, err := os.ReadFile(filepath.Join(dir, ".events.jsonl"))
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	var chronicleEv, ledgerEv Event
	for _, line := range splitLines(data) {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		switch ev.Type {
		case EventChronicleStart:
			chronicleEv = ev
		case EventLedgerStart:
			ledgerEv = ev
		}
	}

	if chronicleEv.Type == "" {
		t.Fatal("did not find chronicle_start in events file")
	}
	if ledgerEv.Type == "" {
		t.Fatal("did not find ledger_start in events file")
	}

	// Source/actor must equal the component name, not be cross-pollinated.
	if chronicleEv.Source != "chronicle" || chronicleEv.Actor != "chronicle" {
		t.Errorf("chronicle event source/actor: got %q/%q, want %q/%q",
			chronicleEv.Source, chronicleEv.Actor, "chronicle", "chronicle")
	}
	if ledgerEv.Source != "ledger" || ledgerEv.Actor != "ledger" {
		t.Errorf("ledger event source/actor: got %q/%q, want %q/%q",
			ledgerEv.Source, ledgerEv.Actor, "ledger", "ledger")
	}

	// Structural parity: source == actor on each event, visibility matches.
	if chronicleEv.Source != chronicleEv.Actor {
		t.Errorf("chronicle: source %q != actor %q", chronicleEv.Source, chronicleEv.Actor)
	}
	if ledgerEv.Source != ledgerEv.Actor {
		t.Errorf("ledger: source %q != actor %q", ledgerEv.Source, ledgerEv.Actor)
	}
	if chronicleEv.Visibility != ledgerEv.Visibility {
		t.Errorf("visibility differs: chronicle=%q, ledger=%q",
			chronicleEv.Visibility, ledgerEv.Visibility)
	}
	if chronicleEv.Visibility != "audit" {
		t.Errorf("expected lifecycle visibility=%q, got %q", "audit", chronicleEv.Visibility)
	}
}

func trimTrailingNewline(data []byte) []byte {
	for len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
		data = data[:len(data)-1]
	}
	return data
}

package softfail

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/events"
)

// fakeEmitter captures Emit calls for assertion in tests.
type fakeEmitter struct {
	calls []emittedEvent
}

type emittedEvent struct {
	Type       string
	Source     string
	Actor      string
	Visibility string
	Payload    any
}

func (f *fakeEmitter) Emit(eventType, source, actor, visibility string, payload any) {
	f.calls = append(f.calls, emittedEvent{
		Type:       eventType,
		Source:     source,
		Actor:      actor,
		Visibility: visibility,
		Payload:    payload,
	})
}

func TestLogNilErrorReturnsFalse(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if Log(logger, "test.op", nil) {
		t.Fatalf("Log(nil) should return false")
	}
	if buf.Len() != 0 {
		t.Fatalf("Log(nil) should not emit; got %q", buf.String())
	}
}

func TestLogEmitsWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	err := errors.New("boom")
	if !Log(logger, "test.op", err) {
		t.Fatalf("Log(err) should return true")
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected WARN level, got: %s", out)
	}
	if !strings.Contains(out, "soft failure") {
		t.Errorf("expected 'soft failure' message, got: %s", out)
	}
	if !strings.Contains(out, "op=test.op") {
		t.Errorf("expected op key, got: %s", out)
	}
	if !strings.Contains(out, "error=boom") {
		t.Errorf("expected error key, got: %s", out)
	}
}

func TestLogNilLoggerUsesDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if !Log(nil, "default.op", errors.New("kaboom")) {
		t.Fatalf("Log(err) should return true")
	}
	if !strings.Contains(buf.String(), "op=default.op") {
		t.Errorf("expected default logger to receive event, got: %s", buf.String())
	}
}

func TestEmitNilErrorReturnsFalseAndEmitsNothing(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	em := &fakeEmitter{}
	if Emit(logger, em, "test.op", nil, nil) {
		t.Fatalf("Emit(nil err) should return false")
	}
	if buf.Len() != 0 {
		t.Fatalf("Emit(nil err) should not log; got %q", buf.String())
	}
	if len(em.calls) != 0 {
		t.Fatalf("Emit(nil err) should not emit; got %+v", em.calls)
	}
}

func TestEmitEmitsStructuredEventWithPayload(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	em := &fakeEmitter{}
	err := errors.New("boom")
	payload := map[string]any{"agent": "Toast", "writ": "sol-abc"}

	if !Emit(logger, em, "dispatch.rollback_agent_state", err, payload) {
		t.Fatalf("Emit(err) should return true")
	}

	// Logs the warning like Log does.
	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected WARN level, got: %s", out)
	}
	if !strings.Contains(out, "soft failure") {
		t.Errorf("expected 'soft failure' message, got: %s", out)
	}
	if !strings.Contains(out, "op=dispatch.rollback_agent_state") {
		t.Errorf("expected op key, got: %s", out)
	}

	// Emits exactly one structured event.
	if len(em.calls) != 1 {
		t.Fatalf("expected 1 emitted event, got %d: %+v", len(em.calls), em.calls)
	}
	ev := em.calls[0]
	if ev.Type != events.EventSoftFailure {
		t.Errorf("type=%q want %q", ev.Type, events.EventSoftFailure)
	}
	if ev.Source != "dispatch" {
		t.Errorf("source=%q want %q (component prefix of op)", ev.Source, "dispatch")
	}
	if ev.Actor != "dispatch" {
		t.Errorf("actor=%q want %q", ev.Actor, "dispatch")
	}
	if ev.Visibility != "audit" {
		t.Errorf("visibility=%q want %q", ev.Visibility, "audit")
	}
	pm, ok := ev.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload not map[string]any: %T", ev.Payload)
	}
	if pm["op"] != "dispatch.rollback_agent_state" {
		t.Errorf("payload op=%v want dispatch.rollback_agent_state", pm["op"])
	}
	if pm["error"] != "boom" {
		t.Errorf("payload error=%v want boom", pm["error"])
	}
	if pm["agent"] != "Toast" {
		t.Errorf("caller payload field agent=%v want Toast", pm["agent"])
	}
	if pm["writ"] != "sol-abc" {
		t.Errorf("caller payload field writ=%v want sol-abc", pm["writ"])
	}
}

func TestEmitNilEventLoggerStillLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if !Emit(logger, nil, "x.y", errors.New("oops"), nil) {
		t.Fatalf("Emit(err, nil eventLogger) should return true")
	}
	out := buf.String()
	if !strings.Contains(out, "soft failure") {
		t.Errorf("expected log to contain 'soft failure': %s", out)
	}
	if !strings.Contains(out, "op=x.y") {
		t.Errorf("expected log contains op=x.y: %s", out)
	}
	if !strings.Contains(out, "error=oops") {
		t.Errorf("expected log contains error=oops: %s", out)
	}
}

func TestEmitNilLoggerUsesDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	em := &fakeEmitter{}
	if !Emit(nil, em, "comp.act", errors.New("kaboom"), nil) {
		t.Fatalf("Emit(err) should return true")
	}
	if !strings.Contains(buf.String(), "op=comp.act") {
		t.Errorf("expected default logger to receive log; got %s", buf.String())
	}
	if len(em.calls) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(em.calls))
	}
}

func TestEmitOverwritesReservedPayloadKeys(t *testing.T) {
	em := &fakeEmitter{}
	Emit(nil, em, "comp.act", errors.New("real-error"), map[string]any{
		"op":    "fake-op",
		"error": "fake-error",
		"keep":  "yes",
	})
	if len(em.calls) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(em.calls))
	}
	pm := em.calls[0].Payload.(map[string]any)
	if pm["op"] != "comp.act" {
		t.Errorf("op should be canonical 'comp.act' even when caller set it; got %v", pm["op"])
	}
	if pm["error"] != "real-error" {
		t.Errorf("error should be err.Error() even when caller set it; got %v", pm["error"])
	}
	if pm["keep"] != "yes" {
		t.Errorf("non-reserved caller field 'keep' should be preserved; got %v", pm["keep"])
	}
}

func TestEmitOpWithoutDotUsesFullOpAsSource(t *testing.T) {
	em := &fakeEmitter{}
	Emit(nil, em, "standalone", errors.New("e"), nil)
	if len(em.calls) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(em.calls))
	}
	if em.calls[0].Source != "standalone" {
		t.Errorf("expected source=standalone for dot-less op, got %q", em.calls[0].Source)
	}
	if em.calls[0].Actor != "standalone" {
		t.Errorf("expected actor=standalone for dot-less op, got %q", em.calls[0].Actor)
	}
}

func TestEmitNilPayloadStillIncludesOpAndError(t *testing.T) {
	em := &fakeEmitter{}
	Emit(nil, em, "comp.act", errors.New("e"), nil)
	if len(em.calls) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(em.calls))
	}
	pm, ok := em.calls[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload not map[string]any: %T", em.calls[0].Payload)
	}
	if pm["op"] != "comp.act" {
		t.Errorf("op missing from payload built from nil caller payload; got %v", pm)
	}
	if pm["error"] != "e" {
		t.Errorf("error missing from payload built from nil caller payload; got %v", pm)
	}
}

package softfail

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

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

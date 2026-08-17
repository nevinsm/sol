package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cliforge "github.com/nevinsm/sol/internal/cliapi/forge"
	"github.com/nevinsm/sol/internal/nudge"
	"github.com/nevinsm/sol/internal/store"
)

func setupForgeTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatalf("failed to create .runtime: %v", err)
	}
	return dir
}

func TestForgeAwaitImmediateNudge(t *testing.T) {
	setupForgeTestDir(t)
	session := "sol-testworld-forge"

	// Enqueue a nudge before await starts.
	err := nudge.Enqueue(session, nudge.Message{
		Sender:   "TestAgent",
		Type:     "MR_READY",
		Subject:  "MR ready",
		Body:     `{"writ_id":"sol-abc123","merge_request_id":"mr-1"}`,
		Priority: "normal",
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	// Drain should return immediately.
	messages, err := nudge.Drain(session)
	if err != nil {
		t.Fatalf("drain failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	result := cliforge.ForgeAwaitResponse{
		Woke:          true,
		Messages:      messages,
		WaitedSeconds: 0,
	}
	if !result.Woke {
		t.Error("expected woke=true")
	}
	if result.Messages[0].Type != "MR_READY" {
		t.Errorf("expected MR_READY, got %s", result.Messages[0].Type)
	}
}

func TestForgeAwaitTimeout(t *testing.T) {
	setupForgeTestDir(t)
	session := "sol-testworld-forge"

	// No nudges — drain should return empty.
	start := time.Now()
	messages, err := nudge.Drain(session)
	if err != nil {
		t.Fatalf("drain failed: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(messages))
	}

	// Simulate a short poll loop (2 iterations at 100ms).
	timeout := 200 * time.Millisecond
	deadline := start.Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		messages, err = nudge.Drain(session)
		if err != nil {
			t.Fatalf("drain failed: %v", err)
		}
		if len(messages) > 0 {
			t.Fatal("unexpected messages during timeout test")
		}
	}

	elapsed := time.Since(start)
	if elapsed < timeout {
		t.Errorf("expected to wait at least %v, waited %v", timeout, elapsed)
	}

	result := cliforge.ForgeAwaitResponse{
		Woke:          false,
		Messages:      []nudge.Message{},
		WaitedSeconds: elapsed.Seconds(),
	}
	if result.Woke {
		t.Error("expected woke=false on timeout")
	}
}

func TestForgeAwaitWatchWakeup(t *testing.T) {
	setupForgeTestDir(t)
	session := "sol-testworld-forge"

	// Start polling in a goroutine, enqueue after a short delay.
	done := make(chan cliforge.ForgeAwaitResponse, 1)
	go func() {
		start := time.Now()
		deadline := start.Add(5 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			messages, err := nudge.Drain(session)
			if err != nil {
				return
			}
			if len(messages) > 0 {
				done <- cliforge.ForgeAwaitResponse{
					Woke:          true,
					Messages:      messages,
					WaitedSeconds: time.Since(start).Seconds(),
				}
				return
			}
		}
		done <- cliforge.ForgeAwaitResponse{Woke: false, Messages: []nudge.Message{}}
	}()

	// Wait briefly, then enqueue.
	time.Sleep(200 * time.Millisecond)
	err := nudge.Enqueue(session, nudge.Message{
		Sender:   "TestAgent",
		Type:     "MR_READY",
		Subject:  "New MR",
		Body:     `{"merge_request_id":"mr-2"}`,
		Priority: "normal",
	})
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	result := <-done
	if !result.Woke {
		t.Error("expected woke=true after nudge")
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Type != "MR_READY" {
		t.Errorf("expected MR_READY, got %s", result.Messages[0].Type)
	}
	if result.WaitedSeconds > 2 {
		t.Errorf("expected wakeup within 2s, took %.1fs", result.WaitedSeconds)
	}
}

func TestForgeAwaitResultJSON(t *testing.T) {
	result := cliforge.ForgeAwaitResponse{
		Woke: true,
		Messages: []nudge.Message{
			{
				Sender:   "agent",
				Type:     "MR_READY",
				Subject:  "test",
				Priority: "normal",
			},
		},
		WaitedSeconds: 5.2,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded cliforge.ForgeAwaitResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !decoded.Woke {
		t.Error("expected woke=true in decoded result")
	}
	if len(decoded.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(decoded.Messages))
	}
	if decoded.WaitedSeconds != 5.2 {
		t.Errorf("expected waited_seconds=5.2, got %v", decoded.WaitedSeconds)
	}
}

func TestForgeAwaitEmptyResult(t *testing.T) {
	result := cliforge.ForgeAwaitResponse{
		Woke:          false,
		Messages:      []nudge.Message{},
		WaitedSeconds: 30.0,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Verify empty messages is [] not null.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw failed: %v", err)
	}
	if string(raw["messages"]) != "[]" {
		t.Errorf("expected messages=[], got %s", string(raw["messages"]))
	}
	if string(raw["woke"]) != "false" {
		t.Errorf("expected woke=false, got %s", string(raw["woke"]))
	}
}

// --- forge queue / forge history print tests ---

// mkMR is a tiny helper for constructing MergeRequest rows in tests.
func mkMR(id, phase string, created time.Time, blockedBy string, merged *time.Time) store.MergeRequest {
	return store.MergeRequest{
		ID:        id,
		WritID:    "sol-" + id,
		Branch:    "writs/" + id,
		Phase:     phase,
		BlockedBy: blockedBy,
		CreatedAt: created,
		UpdatedAt: created,
		MergedAt:  merged,
	}
}

// captureForgeStdout runs fn and returns whatever it wrote to os.Stdout.
func captureForgeStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.Bytes()
	}()

	fn()
	w.Close()
	return string(<-done)
}

func TestPrintMRTableColumnsAndFooter(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	mrs := []store.MergeRequest{
		mkMR("m1", store.MRReady, now.Add(-3*time.Hour), "sol-blocker", nil),
		mkMR("m2", store.MRClaimed, now.Add(-30*time.Minute), "", nil),
	}

	out := captureForgeStdout(t, func() { printMRTable("test", "Merge Queue", mrs, now) })

	// Header: every expected column must appear.
	for _, col := range []string{"ID", "WRIT", "BRANCH", "PHASE", "AGE", "BLOCKED BY", "ATTEMPTS"} {
		if !strings.Contains(out, col) {
			t.Errorf("expected column %q in output, got:\n%s", col, out)
		}
	}
	// Relative age rendered by cliformat.
	if !strings.Contains(out, "3h ago") {
		t.Errorf("expected '3h ago' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "30m ago") {
		t.Errorf("expected '30m ago' in output, got:\n%s", out)
	}
	// Blocked cell populated for m1, empty marker '-' for m2.
	if !strings.Contains(out, "sol-blocker") {
		t.Errorf("expected blocker writ in output, got:\n%s", out)
	}
	if !strings.Contains(out, "-") {
		t.Errorf("expected EmptyMarker '-' in output, got:\n%s", out)
	}
	// Footer uses "N MRs" pluralisation (2 MRs).
	if !strings.Contains(out, "2 MRs") {
		t.Errorf("expected '2 MRs' footer, got:\n%s", out)
	}
}

func TestPrintMRTableEmpty(t *testing.T) {
	out := captureForgeStdout(t, func() { printMRTable("test", "Merge History", nil, time.Now()) })
	if !strings.Contains(out, "empty") {
		t.Errorf("expected 'empty' sentinel, got:\n%s", out)
	}
}

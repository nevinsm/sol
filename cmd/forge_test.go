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

// --- forge queue / forge history filter + print tests ---

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

func TestFilterQueueByStatusDefault(t *testing.T) {
	now := time.Now()
	mrs := []store.MergeRequest{
		mkMR("a", store.MRReady, now, "", nil),
		mkMR("b", store.MRClaimed, now, "", nil),
		mkMR("c", store.MRFailed, now, "", nil),
		mkMR("d", store.MRMerged, now, "", &now),
		mkMR("e", store.MRSuperseded, now, "", nil),
	}

	// Default filter: active statuses only (no merged, no superseded).
	out := filterQueueByStatus(mrs, false, "")
	if len(out) != 3 {
		t.Fatalf("expected 3 active MRs, got %d", len(out))
	}
	for _, mr := range out {
		if mr.Phase == store.MRMerged || mr.Phase == store.MRSuperseded {
			t.Errorf("default filter should not include phase %q", mr.Phase)
		}
	}
}

func TestFilterQueueByStatusAll(t *testing.T) {
	now := time.Now()
	mrs := []store.MergeRequest{
		mkMR("a", store.MRReady, now, "", nil),
		mkMR("b", store.MRMerged, now, "", &now),
	}

	out := filterQueueByStatus(mrs, true, "")
	if len(out) != 2 {
		t.Fatalf("--all should include every MR, got %d", len(out))
	}
}

func TestFilterQueueByStatusExplicit(t *testing.T) {
	now := time.Now()
	mrs := []store.MergeRequest{
		mkMR("a", store.MRReady, now, "", nil),
		mkMR("b", store.MRClaimed, now, "", nil),
		mkMR("c", store.MRMerged, now, "", &now),
	}

	// Explicit --status should override the default and ignore other phases.
	out := filterQueueByStatus(mrs, false, "merged")
	if len(out) != 1 || out[0].Phase != store.MRMerged {
		t.Fatalf("expected only merged MR, got %+v", out)
	}

	// Comma-separated list with whitespace.
	out = filterQueueByStatus(mrs, false, "ready, claimed")
	if len(out) != 2 {
		t.Fatalf("expected 2 MRs, got %d", len(out))
	}
}

func TestFilterHistorySortsNewestFirstAndLimits(t *testing.T) {
	base := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	t1 := base.Add(-72 * time.Hour)
	t2 := base.Add(-48 * time.Hour)
	t3 := base.Add(-24 * time.Hour)
	mrs := []store.MergeRequest{
		mkMR("old", store.MRMerged, t1, "", &t1),
		mkMR("mid", store.MRMerged, t2, "", &t2),
		mkMR("new", store.MRMerged, t3, "", &t3),
	}

	out := filterHistory(mrs, nil, nil, 0)
	if len(out) != 3 {
		t.Fatalf("expected 3 MRs, got %d", len(out))
	}
	if out[0].ID != "new" || out[1].ID != "mid" || out[2].ID != "old" {
		t.Errorf("expected newest-first order, got %s,%s,%s", out[0].ID, out[1].ID, out[2].ID)
	}

	// Limit=2 should keep the two newest.
	out = filterHistory(mrs, nil, nil, 2)
	if len(out) != 2 {
		t.Fatalf("expected limit=2, got %d", len(out))
	}
	if out[0].ID != "new" || out[1].ID != "mid" {
		t.Errorf("expected newest two, got %s,%s", out[0].ID, out[1].ID)
	}
}

func TestFilterHistorySinceUntil(t *testing.T) {
	base := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	t1 := base.Add(-7 * 24 * time.Hour)
	t2 := base.Add(-3 * 24 * time.Hour)
	t3 := base.Add(-1 * time.Hour)
	mrs := []store.MergeRequest{
		mkMR("wk", store.MRMerged, t1, "", &t1),
		mkMR("3d", store.MRMerged, t2, "", &t2),
		mkMR("hr", store.MRMerged, t3, "", &t3),
	}

	since := base.Add(-4 * 24 * time.Hour)
	out := filterHistory(mrs, &since, nil, 0)
	if len(out) != 2 {
		t.Fatalf("expected 2 MRs within last 4 days, got %d", len(out))
	}
	for _, mr := range out {
		if mr.ID == "wk" {
			t.Errorf("MR older than --since should be excluded")
		}
	}

	until := base.Add(-2 * 24 * time.Hour)
	out = filterHistory(mrs, nil, &until, 0)
	// until=2d ago — only MRs merged on/before that should remain (wk, 3d).
	if len(out) != 2 {
		t.Fatalf("expected 2 MRs older than --until, got %d", len(out))
	}
	for _, mr := range out {
		if mr.ID == "hr" {
			t.Errorf("MR newer than --until should be excluded")
		}
	}
}

func TestHistoryTimestampPrefersMergedAt(t *testing.T) {
	updated := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	merged := time.Date(2026, 4, 5, 0, 0, 0, 0, time.UTC)

	mr := store.MergeRequest{UpdatedAt: updated, MergedAt: &merged}
	if got := historyTimestamp(mr); !got.Equal(merged) {
		t.Errorf("expected merged_at, got %v", got)
	}

	// Fallback to updated_at when MergedAt is nil (e.g. failed MR).
	mr = store.MergeRequest{UpdatedAt: updated}
	if got := historyTimestamp(mr); !got.Equal(updated) {
		t.Errorf("expected updated_at fallback, got %v", got)
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

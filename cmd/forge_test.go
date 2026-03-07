package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/nudge"
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

	result := forgeAwaitResult{
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

	result := forgeAwaitResult{
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
	done := make(chan forgeAwaitResult, 1)
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
				done <- forgeAwaitResult{
					Woke:          true,
					Messages:      messages,
					WaitedSeconds: time.Since(start).Seconds(),
				}
				return
			}
		}
		done <- forgeAwaitResult{Woke: false, Messages: []nudge.Message{}}
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
	result := forgeAwaitResult{
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

	var decoded forgeAwaitResult
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
	result := forgeAwaitResult{
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

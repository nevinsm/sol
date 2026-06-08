package integration

// failure_modes_extra_test.go — Integration tests for documented failure modes
// that previously lacked test coverage:
//
//   T20: Ledger crash recovery — PID file detection, cache rebuild on restart
//   T21: Envoy memory graceful degradation — missing/corrupt MEMORY.md

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	claude "github.com/nevinsm/sol/internal/adapter/claude"
	"github.com/nevinsm/sol/internal/ledger"
	"github.com/nevinsm/sol/internal/processutil"
	"github.com/nevinsm/sol/internal/store"
)

// ============================================================
// T20: Ledger Crash Recovery
// ============================================================

// TestLedgerCrashRecovery exercises the failure mode documented in
// docs/failure-modes.md (lines 124-143): when the ledger crashes, token
// tracking pauses but no agent work is affected. On restart, the ledger starts
// with empty caches and the first OTLP event per session creates a new
// agent_history record.
//
// This test validates:
// 1. PID file detection: dead PID is detectable after crash
// 2. Cache rebuild: post-restart OTLP events create new history records
//    (not reusing cached IDs from before the crash)
// 3. Pre-crash token data survives in the database (WAL durability)
func TestLedgerCrashRecovery(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Initialize a world so the ledger has a store to write to.
	initWorld(t, gtHome, "crashworld")

	// Allocate a dynamic port (avoids conflicts with production collector).
	port := allocatePort(t)

	// === Phase 1: Start ledger and ingest data ===

	cfg := ledger.Config{
		Port:    port,
		SOLHome: gtHome,
	}
	l1 := ledger.New(cfg)

	ctx1, cancel1 := context.WithCancel(context.Background())
	runErr1 := make(chan error, 1)
	go func() {
		runErr1 <- l1.Run(ctx1)
	}()

	// Wait for the ledger to accept connections.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if !pollUntil(3*time.Second, 50*time.Millisecond, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}) {
		cancel1()
		t.Fatalf("ledger did not start within 3s on %s", addr)
	}

	// Verify PID file was created with a valid PID.
	pidPath := filepath.Join(gtHome, ".runtime", "ledger.pid")
	pid1, err := processutil.ReadPID(pidPath)
	if err != nil {
		cancel1()
		t.Fatalf("read PID file: %v", err)
	}
	if pid1 == 0 {
		cancel1()
		t.Fatal("expected non-zero PID in ledger PID file")
	}

	// Send OTLP data (simulating an agent session producing token usage).
	body := makeTestOTLPBody("Toast", "crashworld", "sol-crash000000001", "claude_code.api_request",
		"claude-sonnet-4-6", 1000, 500, 200, 100)
	sendOTLP(t, addr, body)

	// Verify the token data was written to the world store.
	worldStore, err := store.OpenWorld("crashworld")
	if err != nil {
		cancel1()
		t.Fatalf("open world store: %v", err)
	}

	entries1, err := worldStore.ListHistory("Toast")
	if err != nil {
		cancel1()
		worldStore.Close()
		t.Fatalf("list history: %v", err)
	}
	if len(entries1) != 1 {
		cancel1()
		worldStore.Close()
		t.Fatalf("expected 1 history entry before crash, got %d", len(entries1))
	}
	preCrashHistoryID := entries1[0].ID

	// Verify token data was durably written.
	summaries, err := worldStore.AggregateTokens("Toast")
	if err != nil {
		cancel1()
		worldStore.Close()
		t.Fatalf("aggregate tokens: %v", err)
	}
	if len(summaries) == 0 {
		cancel1()
		worldStore.Close()
		t.Fatal("expected token data before crash, got none")
	}
	preCrashInputTokens := summaries[0].InputTokens
	worldStore.Close()

	// === Phase 2: Crash the ledger ===

	cancel1()
	select {
	case err := <-runErr1:
		if err != nil {
			t.Logf("ledger phase 1 returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ledger did not shut down within 5s")
	}

	// After clean shutdown, PID file is cleared by defer. To simulate a crash
	// scenario where the PID file still has a dead PID (the process was killed
	// without running cleanup), write a known-dead PID to the file.
	// PID 99999999 is astronomically unlikely to exist.
	deadPID := 99999999
	if err := processutil.WritePID(pidPath, deadPID); err != nil {
		t.Fatalf("write stale PID: %v", err)
	}

	// Verify PID file detection: the PID is present but the process is dead.
	// This is the detection path the prefect uses (checkSphereDaemons reads
	// the PID file and checks IsRunning).
	stalePID, err := processutil.ReadPID(pidPath)
	if err != nil {
		t.Fatalf("read stale PID: %v", err)
	}
	if stalePID != deadPID {
		t.Fatalf("expected PID %d in file, got %d", deadPID, stalePID)
	}
	if processutil.IsRunning(stalePID) {
		t.Fatalf("expected stale PID %d to be dead, but it appears running", stalePID)
	}

	// === Phase 3: Restart ledger and verify cache recovery ===

	// Allocate a fresh port (the old one may still be in TIME_WAIT).
	port2 := allocatePort(t)

	cfg2 := ledger.Config{
		Port:    port2,
		SOLHome: gtHome,
	}
	l2 := ledger.New(cfg2)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	runErr2 := make(chan error, 1)
	go func() {
		runErr2 <- l2.Run(ctx2)
	}()

	addr2 := fmt.Sprintf("127.0.0.1:%d", port2)
	if !pollUntil(3*time.Second, 50*time.Millisecond, func() bool {
		conn, err := net.DialTimeout("tcp", addr2, 100*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}) {
		t.Fatalf("restarted ledger did not start within 3s on %s", addr2)
	}

	// Verify PID file was updated with a new PID.
	pid2, err := processutil.ReadPID(pidPath)
	if err != nil {
		t.Fatalf("read PID after restart: %v", err)
	}
	if pid2 == 0 {
		t.Fatal("expected non-zero PID after restart")
	}

	// Send the same session's OTLP data again (same agent, world, writ).
	// With fresh caches, the ledger should create a new history record
	// (ensureHistory sees empty cache and creates a new row).
	body2 := makeTestOTLPBody("Toast", "crashworld", "sol-crash000000001", "claude_code.api_request",
		"claude-sonnet-4-6", 800, 400, 150, 50)
	sendOTLP(t, addr2, body2)

	// Verify: new history record was created (cache was empty after restart).
	worldStore2, err := store.OpenWorld("crashworld")
	if err != nil {
		t.Fatalf("reopen world store: %v", err)
	}
	defer worldStore2.Close()

	entries2, err := worldStore2.ListHistory("Toast")
	if err != nil {
		t.Fatalf("list history after restart: %v", err)
	}
	if len(entries2) < 2 {
		t.Fatalf("expected at least 2 history entries after restart (cache rebuild creates new record), got %d", len(entries2))
	}

	// Verify: the new history entry has a different ID from the pre-crash one.
	// This proves the cache was rebuilt from scratch (not reusing stale IDs).
	foundNew := false
	for _, e := range entries2 {
		if e.ID != preCrashHistoryID {
			foundNew = true
			break
		}
	}
	if !foundNew {
		t.Error("expected a new history ID after restart, but all entries match the pre-crash ID")
	}

	// Verify: pre-crash token data survived (WAL durability).
	allSummaries, err := worldStore2.AggregateTokens("Toast")
	if err != nil {
		t.Fatalf("aggregate tokens after restart: %v", err)
	}
	if len(allSummaries) == 0 {
		t.Fatal("expected token data to survive crash, got none")
	}
	// Total input tokens should be at least the pre-crash amount (data survived).
	totalInput := int64(0)
	for _, s := range allSummaries {
		totalInput += s.InputTokens
	}
	if totalInput < preCrashInputTokens {
		t.Errorf("total input tokens %d < pre-crash %d — data lost", totalInput, preCrashInputTokens)
	}

	// Clean shutdown.
	cancel2()
	select {
	case <-runErr2:
	case <-time.After(5 * time.Second):
		t.Log("restarted ledger did not shut down within 5s")
	}
}

// ============================================================
// T21: Envoy Memory Graceful Degradation
// ============================================================

// TestEnvoyMemoryGracefulDegradation exercises the failure mode documented in
// docs/failure-modes.md (lines 145-166): when envoy MEMORY.md is corrupted
// or missing, the system degrades gracefully.
//
// Key invariants validated:
// 1. Missing MEMORY.md = clean start (envoy operations unaffected)
// 2. Corrupt MEMORY.md = reduced context (envoy operations unaffected)
// 3. Memory directory deletion is recoverable (EnsureConfigDir recreates it)
// 4. Sol never crashes or errors due to MEMORY.md state
func TestEnvoyMemoryGracefulDegradation(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, sourceRepo := setupTestEnvWithRepo(t)
	setupWorld(t, gtHome, "memtest", sourceRepo)

	// Create an envoy via CLI.
	out, err := runGT(t, gtHome, "envoy", "create", "Polaris", "--world=memtest")
	if err != nil {
		t.Fatalf("envoy create: %v: %s", err, out)
	}

	// Determine the memory directory path for this envoy.
	// Per the claude adapter: <worldDir>/envoys/<name>/memory/
	worldDir := filepath.Join(gtHome, "memtest")
	memoryDir := filepath.Join(worldDir, "envoys", "Polaris", "memory")

	// --- Scenario 1: Missing MEMORY.md (clean start) ---
	// After envoy create, the memory directory may or may not exist yet
	// (it's created by EnsureConfigDir during session start, not by create).
	// Sol commands should work regardless.

	out, err = runGT(t, gtHome, "envoy", "list", "--world=memtest")
	if err != nil {
		t.Fatalf("envoy list (no memory): %v: %s", err, out)
	}
	if !strings.Contains(out, "Polaris") {
		t.Errorf("expected Polaris in envoy list, got: %s", out)
	}

	// Agent list should also work.
	out, err = runGT(t, gtHome, "agent", "list", "--world=memtest")
	if err != nil {
		t.Fatalf("agent list (no memory): %v: %s", err, out)
	}
	if !strings.Contains(out, "Polaris") {
		t.Errorf("expected Polaris in agent list, got: %s", out)
	}

	// --- Scenario 2: Create memory, verify, then corrupt it ---

	// Create the memory directory and write a valid MEMORY.md.
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	validContent := "# Agent Memory\n\nKey context: running integration tests.\n"
	if err := os.WriteFile(memoryFile, []byte(validContent), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	// Verify the file exists and is readable.
	data, err := os.ReadFile(memoryFile)
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if string(data) != validContent {
		t.Errorf("MEMORY.md content mismatch: got %q", string(data))
	}

	// Corrupt the memory file with binary garbage.
	corruptContent := []byte{0x00, 0xFF, 0xFE, 0x89, 0x50, 0x4E, 0x47}
	if err := os.WriteFile(memoryFile, corruptContent, 0o644); err != nil {
		t.Fatalf("corrupt MEMORY.md: %v", err)
	}

	// Sol commands must still work — sol never parses MEMORY.md.
	out, err = runGT(t, gtHome, "envoy", "list", "--world=memtest")
	if err != nil {
		t.Fatalf("envoy list (corrupt memory): %v: %s", err, out)
	}
	if !strings.Contains(out, "Polaris") {
		t.Errorf("expected Polaris in envoy list after corruption, got: %s", out)
	}

	// Status command should also work — exit code 2 (degraded) is expected
	// when no daemons are running, but it must not crash due to corrupt memory.
	out, err = runGT(t, gtHome, "status", "memtest")
	if err != nil {
		// Exit code 2 = degraded status, which is expected when daemons aren't running.
		// Just verify it didn't panic or produce an unexpected error.
		if !strings.Contains(out, "Polaris") {
			t.Errorf("status should still show Polaris envoy despite corrupt memory, got: %s", out)
		}
	}

	// --- Scenario 3: Delete MEMORY.md entirely ---

	if err := os.Remove(memoryFile); err != nil {
		t.Fatalf("delete MEMORY.md: %v", err)
	}

	// Sol commands still work — missing memory is not an error.
	out, err = runGT(t, gtHome, "envoy", "list", "--world=memtest")
	if err != nil {
		t.Fatalf("envoy list (deleted memory): %v: %s", err, out)
	}
	if !strings.Contains(out, "Polaris") {
		t.Errorf("expected Polaris in envoy list after MEMORY.md deletion, got: %s", out)
	}

	// --- Scenario 4: Delete entire memory directory ---

	if err := os.RemoveAll(memoryDir); err != nil {
		t.Fatalf("delete memory dir: %v", err)
	}

	// Verify that the adapter would recreate the memory directory on
	// the next EnsureConfigDir call (simulating session startup).
	adapter := claude.New()
	recreatedDir := adapter.MemoryDir(worldDir, "envoy", "Polaris")
	if recreatedDir == "" {
		t.Fatal("MemoryDir returned empty for envoy role")
	}
	if !filepath.IsAbs(recreatedDir) {
		t.Errorf("MemoryDir should return absolute path, got: %s", recreatedDir)
	}

	// EnsureConfigDir should create the memory dir (among other things).
	envoyWorktree := filepath.Join(worldDir, "envoys", "Polaris", "worktree")
	_, ensureErr := adapter.EnsureConfigDir(worldDir, "envoy", "Polaris", envoyWorktree)
	if ensureErr != nil {
		t.Fatalf("EnsureConfigDir after memory dir deletion: %v", ensureErr)
	}

	// The memory directory should be recreated.
	if _, err := os.Stat(recreatedDir); os.IsNotExist(err) {
		t.Errorf("expected memory directory to be recreated at %s", recreatedDir)
	}

	// Sol commands still work after memory directory recreation.
	out, err = runGT(t, gtHome, "envoy", "list", "--world=memtest")
	if err != nil {
		t.Fatalf("envoy list (after recreation): %v: %s", err, out)
	}
	if !strings.Contains(out, "Polaris") {
		t.Errorf("expected Polaris in envoy list after dir recreation, got: %s", out)
	}
}

// TestEnvoyMemoryDirForNonEnvoyRoles verifies that the memory directory is
// only created for envoy roles — outposts and forge-merge are ephemeral.
func TestEnvoyMemoryDirForNonEnvoyRoles(t *testing.T) {
	skipUnlessIntegration(t)

	adapter := claude.New()

	for _, role := range []string{"outpost", "forge-merge"} {
		dir := adapter.MemoryDir("/tmp/solhome/world", role, "Agent")
		if dir != "" {
			t.Errorf("MemoryDir(role=%q) = %q, want empty", role, dir)
		}
	}
}

// ============================================================
// Helpers
// ============================================================

// allocatePort dynamically allocates a free TCP port.
func allocatePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// makeTestOTLPBody builds an OTLP JSON body for testing token ingestion.
func makeTestOTLPBody(agentName, world, writID, eventName, model string, input, output, cacheRead, cacheCreation int64) []byte {
	req := map[string]any{
		"resourceLogs": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "service.name", "value": map[string]any{"stringValue": "claude-code"}},
						map[string]any{"key": "agent.name", "value": map[string]any{"stringValue": agentName}},
						map[string]any{"key": "world", "value": map[string]any{"stringValue": world}},
						map[string]any{"key": "writ_id", "value": map[string]any{"stringValue": writID}},
					},
				},
				"scopeLogs": []any{
					map[string]any{
						"logRecords": []any{
							map[string]any{
								"timeUnixNano": "1709740800000000000",
								"body":         map[string]any{"stringValue": eventName},
								"attributes": []any{
									map[string]any{"key": "model", "value": map[string]any{"stringValue": model}},
									map[string]any{"key": "input_tokens", "value": map[string]any{"intValue": fmt.Sprintf("%d", input)}},
									map[string]any{"key": "output_tokens", "value": map[string]any{"intValue": fmt.Sprintf("%d", output)}},
									map[string]any{"key": "cache_read_tokens", "value": map[string]any{"intValue": fmt.Sprintf("%d", cacheRead)}},
									map[string]any{"key": "cache_creation_tokens", "value": map[string]any{"intValue": fmt.Sprintf("%d", cacheCreation)}},
								},
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(req)
	return b
}

// sendOTLP sends an OTLP log export request to the given address.
func sendOTLP(t *testing.T, addr string, body []byte) {
	t.Helper()
	url := fmt.Sprintf("http://%s/v1/logs", addr)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("send OTLP: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("OTLP response: %d", resp.StatusCode)
	}
}

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/ledger"
)

// TestBrokerLifecycle tests the start -> verify running -> stop -> verify
// stopped lifecycle for the broker background daemon.
func TestBrokerLifecycle(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Ensure runtime dir exists (required by daemon start commands).
	if err := os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	// Start the broker.
	out, err := runGT(t, gtHome, "broker", "start")
	if err != nil {
		t.Fatalf("broker start failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Broker started") {
		t.Errorf("expected 'Broker started' in output, got: %s", out)
	}

	// Verify PID file was created.
	pidPath := filepath.Join(gtHome, ".runtime", "broker.pid")
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Errorf("broker PID file not created at %s", pidPath)
	}

	// Stop the broker.
	out, err = runGT(t, gtHome, "broker", "stop")
	if err != nil {
		t.Fatalf("broker stop failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Broker stopped") && !strings.Contains(out, "Broker not running") {
		t.Errorf("expected stop confirmation in broker stop output, got: %s", out)
	}

	// Verify PID file was cleared (truncated to empty, not deleted).
	if data, err := os.ReadFile(pidPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		t.Errorf("expected broker PID file to be cleared after stop, but it still has content %q at %s", string(data), pidPath)
	}
}

// TestBrokerStatusNotRunning verifies that broker status exits non-zero when
// the broker is not running.
func TestBrokerStatusNotRunning(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Status with no broker running — should exit 1.
	out, err := runGT(t, gtHome, "broker", "status")
	if err == nil {
		t.Fatalf("expected broker status to exit non-zero when not running, got success: %s", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("expected 'not running' in broker status output, got: %s", out)
	}
}

// TestLedgerLifecycle tests the start -> verify running -> stop lifecycle for
// the ledger OTLP receiver.
//
// Instead of depending on the hardcoded port 4318, this test allocates a
// dynamic port (listen on :0) and runs the ledger directly via the internal
// API. This ensures the test works regardless of whether port 4318 is already
// in use (e.g. by a production OTLP collector in CI).
func TestLedgerLifecycle(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Allocate a free port dynamically.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate dynamic port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// Create a ledger instance with the dynamic port.
	cfg := ledger.Config{
		Port:    port,
		SOLHome: gtHome,
	}
	l := ledger.New(cfg)

	// Run the ledger in a background goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- l.Run(ctx)
	}()

	// Poll until the ledger is accepting connections on the dynamic port.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	if !pollUntil(3*time.Second, 50*time.Millisecond, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}) {
		cancel()
		t.Fatalf("ledger did not start accepting connections on %s within 3s", addr)
	}

	// Verify PID file was created.
	pidPath := filepath.Join(gtHome, ".runtime", "ledger.pid")
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Errorf("ledger PID file not created at %s", pidPath)
	}

	// Stop the ledger by cancelling the context.
	cancel()

	// Wait for Run to return.
	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("ledger.Run returned unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ledger.Run did not return within 5s after context cancellation")
	}

	// Verify PID file was cleared after shutdown.
	if data, err := os.ReadFile(pidPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		t.Errorf("expected ledger PID file to be cleared after stop, but it still has content %q at %s", string(data), pidPath)
	}
}

// TestLedgerStatusNotRunning verifies that ledger status exits non-zero when
// the ledger is not running.
func TestLedgerStatusNotRunning(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Status with no ledger running — should exit 1.
	out, err := runGT(t, gtHome, "ledger", "status")
	if err == nil {
		t.Fatalf("expected ledger status to exit non-zero when not running, got success: %s", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("expected 'not running' in ledger status output, got: %s", out)
	}
}

// TestSolUpDown tests the sol up / sol down lifecycle for sphere daemons.
// Uses a world-only up/down to avoid starting sphere daemons (which require
// longer startup time and internet connectivity for broker probes).
func TestSolUpDown(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, sourceRepo := setupTestEnvWithRepo(t)

	// Initialize a world with a source repo so sentinel/forge can start.
	initWorldWithRepo(t, gtHome, "testworld", sourceRepo)

	// sol up --world=testworld starts sentinel and forge for the world.
	out, err := runGT(t, gtHome, "up", "--world=testworld")
	if err != nil {
		t.Fatalf("sol up --world=testworld failed: %v: %s", err, out)
	}

	// Verify the up output contains service start indicators.
	// Sentinel PID is at $SOL_HOME/testworld/sentinel.pid.
	if !strings.Contains(out, "started") && !strings.Contains(out, "running") {
		t.Logf("sol up output: %s", out)
		// Not fatal — the output format uses styled characters which may not render in tests.
	}

	// sol down --world=testworld stops sentinel and forge.
	out, err = runGT(t, gtHome, "down", "--world=testworld")
	if err != nil {
		t.Fatalf("sol down --world=testworld failed: %v: %s", err, out)
	}
	// Output should mention testworld.
	if !strings.Contains(out, "testworld") {
		t.Errorf("expected 'testworld' in sol down output, got: %s", out)
	}
}

// TestSolUpDownSphere tests sol up (sphere daemons) followed by sol down.
// Verifies that the primary production lifecycle commands work without error.
//
// The ledger binds to the hardcoded port 4318. If that port is already in use
// (e.g. by a production OTLP collector), the ledger daemon will fail to start.
// The test tolerates this by using --json output to check individual daemon
// results, verifying that at least the non-ledger daemons started successfully.
func TestSolUpDownSphere(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome, _ := setupTestEnv(t)

	// Check if port 4318 is available — used to set expectations below.
	ln, listenErr := net.Listen("tcp", "127.0.0.1:4318")
	ledgerPortFree := listenErr == nil
	if ln != nil {
		ln.Close()
	}

	// sol up --json without --world starts sphere daemons and returns structured results.
	out, err := runGT(t, gtHome, "up", "--json")

	// Parse the JSON output regardless of exit code — sol up reports individual
	// daemon results even when some fail. printJSON (cmd/helpers.go) always
	// indents its output across multiple lines, and the combined output may
	// include an error message after the JSON value (from cobra). Decode with
	// json.Decoder instead of Unmarshal so we read exactly one JSON value from
	// the start of the stream and ignore any trailing non-JSON text, regardless
	// of how many lines the value itself spans.
	var upResult struct {
		SphereDaemons []struct {
			Name           string `json:"name"`
			Started        bool   `json:"started"`
			AlreadyRunning bool   `json:"already_running"`
			Error          string `json:"error"`
		} `json:"sphere_daemons"`
	}
	if jsonErr := json.NewDecoder(strings.NewReader(out)).Decode(&upResult); jsonErr != nil {
		t.Fatalf("sol up --json output not valid JSON: %v\noutput: %s\nerr: %v", jsonErr, out, err)
	}

	// Verify non-ledger daemons started. The ledger may fail if port 4318 is busy.
	for _, d := range upResult.SphereDaemons {
		if d.Name == "ledger" {
			if !ledgerPortFree && d.Error != "" {
				t.Logf("ledger failed to start (port 4318 in use): %s", d.Error)
			} else if ledgerPortFree && !d.Started && !d.AlreadyRunning {
				t.Errorf("ledger should have started (port 4318 was free): error=%s", d.Error)
			}
			continue
		}
		if !d.Started && !d.AlreadyRunning {
			t.Errorf("sphere daemon %q failed to start: %s", d.Name, d.Error)
		}
	}

	// If sol up returned an error and it wasn't just the ledger, fail.
	if err != nil && ledgerPortFree {
		t.Fatalf("sol up failed with port 4318 available: %v: %s", err, out)
	}

	// sol down should stop all sphere daemons.
	out, err = runGT(t, gtHome, "down")
	if err != nil {
		t.Fatalf("sol down failed: %v: %s", err, out)
	}

	// After sol down, all PID files should be cleared (truncated to empty, not deleted).
	// Only check daemons that were actually started.
	for _, daemon := range []string{"prefect", "consul", "chronicle", "broker"} {
		pidPath := filepath.Join(gtHome, ".runtime", daemon+".pid")
		if data, err := os.ReadFile(pidPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			t.Errorf("expected %s PID file to be cleared after sol down, still has content %q at %s", daemon, string(data), pidPath)
		}
	}
	// Check ledger PID cleanup only if it actually started.
	if ledgerPortFree {
		pidPath := filepath.Join(gtHome, ".runtime", "ledger.pid")
		if data, err := os.ReadFile(pidPath); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			t.Errorf("expected ledger PID file to be cleared after sol down, still has content %q at %s", string(data), pidPath)
		}
	}
}


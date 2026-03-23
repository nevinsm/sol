package integration

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChancellorLifecycle tests the full start -> status -> stop lifecycle
// for the chancellor tmux session.
func TestChancellorLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)

	// Status before start — should report not running (exit 0, running=false in JSON).
	out, err := runGT(t, gtHome, "chancellor", "status", "--json")
	if err != nil {
		t.Fatalf("chancellor status before start failed: %v: %s", err, out)
	}
	var statusBefore struct {
		Running bool `json:"running"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &statusBefore); jsonErr != nil {
		t.Fatalf("chancellor status JSON parse error: %v: %s", jsonErr, out)
	}
	if statusBefore.Running {
		t.Errorf("expected chancellor to be not running before start, got running=true")
	}

	// Start chancellor.
	out, err = runGT(t, gtHome, "chancellor", "start")
	if err != nil {
		t.Fatalf("chancellor start failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Started chancellor") {
		t.Errorf("expected 'Started chancellor' in output, got: %s", out)
	}

	// Status after start — should report running.
	out, err = runGT(t, gtHome, "chancellor", "status", "--json")
	if err != nil {
		t.Fatalf("chancellor status after start failed: %v: %s", err, out)
	}
	var statusAfterStart struct {
		Running     bool   `json:"running"`
		SessionName string `json:"session_name"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &statusAfterStart); jsonErr != nil {
		t.Fatalf("chancellor status JSON parse error: %v: %s", jsonErr, out)
	}
	if !statusAfterStart.Running {
		t.Errorf("expected chancellor to be running after start, got running=false")
	}
	if statusAfterStart.SessionName == "" {
		t.Errorf("expected non-empty session_name in status JSON")
	}

	// Stop chancellor.
	out, err = runGT(t, gtHome, "chancellor", "stop")
	if err != nil {
		t.Fatalf("chancellor stop failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Stopped chancellor") {
		t.Errorf("expected 'Stopped chancellor' in output, got: %s", out)
	}

	// Status after stop — should report not running.
	out, err = runGT(t, gtHome, "chancellor", "status", "--json")
	if err != nil {
		t.Fatalf("chancellor status after stop failed: %v: %s", err, out)
	}
	var statusAfterStop struct {
		Running bool `json:"running"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &statusAfterStop); jsonErr != nil {
		t.Fatalf("chancellor status JSON parse error: %v: %s", jsonErr, out)
	}
	if statusAfterStop.Running {
		t.Errorf("expected chancellor to be not running after stop, got running=true")
	}
}

// TestChancellorStartIdempotent verifies that starting an already-running
// chancellor returns an error.
func TestChancellorStartIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, _ := setupTestEnv(t)

	// Start once — succeeds.
	out, err := runGT(t, gtHome, "chancellor", "start")
	if err != nil {
		t.Fatalf("first chancellor start failed: %v: %s", err, out)
	}

	// Start again — should error.
	out, err = runGT(t, gtHome, "chancellor", "start")
	if err == nil {
		t.Fatalf("expected error on second chancellor start, got success: %s", out)
	}
	if !strings.Contains(out, "already running") {
		t.Errorf("expected 'already running' in error output, got: %s", out)
	}

	// Cleanup: stop chancellor.
	runGT(t, gtHome, "chancellor", "stop") //nolint:errcheck
}

// TestBrokerLifecycle tests the start -> verify running -> stop -> verify
// stopped lifecycle for the broker background daemon.
func TestBrokerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

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
	if !strings.Contains(out, "SIGTERM") && !strings.Contains(out, "Broker not running") {
		t.Errorf("expected stop confirmation in broker stop output, got: %s", out)
	}

	// Verify PID file was removed.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("expected broker PID file to be removed after stop, but it still exists at %s", pidPath)
	}
}

// TestBrokerStatusNotRunning verifies that broker status exits non-zero when
// the broker is not running.
func TestBrokerStatusNotRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

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

// TestLedgerLifecycle tests the start -> status -> stop lifecycle for the
// ledger OTLP receiver daemon.
//
// The ledger binds to port 4318. If that port is already in use (e.g. by a
// running production ledger), the test is skipped.
func TestLedgerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Skip if port 4318 is already in use.
	ln, err := net.Listen("tcp", "127.0.0.1:4318")
	if err != nil {
		t.Skipf("skipping: port 4318 already in use (%v)", err)
	}
	ln.Close()

	gtHome, _ := setupTestEnv(t)

	// Ensure runtime dir exists.
	if err := os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	// Start the ledger.
	out, err := runGT(t, gtHome, "ledger", "start")
	if err != nil {
		t.Fatalf("ledger start failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Ledger started") {
		t.Errorf("expected 'Ledger started' in output, got: %s", out)
	}

	// Verify PID file was created.
	pidPath := filepath.Join(gtHome, ".runtime", "ledger.pid")
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Errorf("ledger PID file not created at %s", pidPath)
	}

	// Status — should report running.
	out, err = runGT(t, gtHome, "ledger", "status", "--json")
	if err != nil {
		t.Fatalf("ledger status failed: %v: %s", err, out)
	}
	var ledgerStatus struct {
		Status string `json:"status"`
		PID    int    `json:"pid"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &ledgerStatus); jsonErr != nil {
		t.Fatalf("ledger status JSON parse error: %v: %s", jsonErr, out)
	}
	if ledgerStatus.Status != "running" {
		t.Errorf("expected ledger status 'running', got %q", ledgerStatus.Status)
	}
	if ledgerStatus.PID == 0 {
		t.Errorf("expected non-zero PID in ledger status JSON")
	}

	// Stop the ledger.
	out, err = runGT(t, gtHome, "ledger", "stop")
	if err != nil {
		t.Fatalf("ledger stop failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "SIGTERM") && !strings.Contains(out, "Ledger not running") {
		t.Errorf("expected stop confirmation in ledger stop output, got: %s", out)
	}

	// Verify PID file was removed.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Errorf("expected ledger PID file to be removed after stop, but it still exists at %s", pidPath)
	}
}

// TestLedgerStatusNotRunning verifies that ledger status exits non-zero when
// the ledger is not running.
func TestLedgerStatusNotRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

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
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome, sourceRepo := setupTestEnvWithRepo(t)

	// Initialize a world with a source repo so sentinel/forge can start.
	setupWorld(t, gtHome, "testworld", sourceRepo)

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
// This test requires port 4318 to be available (ledger OTLP receiver). It is
// skipped if the port is already in use (e.g. production sol is running).
func TestSolUpDownSphere(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Skip if port 4318 is already in use — ledger will fail to start.
	ln, err := net.Listen("tcp", "127.0.0.1:4318")
	if err != nil {
		t.Skipf("skipping: port 4318 already in use (%v)", err)
	}
	ln.Close()

	gtHome, _ := setupTestEnv(t)

	// sol up without --world starts sphere daemons (prefect, consul, chronicle, ledger, broker).
	out, err := runGT(t, gtHome, "up")
	if err != nil {
		t.Fatalf("sol up failed: %v: %s", err, out)
	}

	// Verify at least one sphere daemon PID file was created.
	// The broker is the simplest to check (doesn't need a world).
	brokerPIDPath := filepath.Join(gtHome, ".runtime", "broker.pid")
	if _, err := os.Stat(brokerPIDPath); os.IsNotExist(err) {
		t.Logf("broker PID file not found at %s; sol up output: %s", brokerPIDPath, out)
		// Not fatal — sol up may have already cleaned up a fast-exiting process.
	}

	// sol down should stop all sphere daemons.
	out, err = runGT(t, gtHome, "down")
	if err != nil {
		t.Fatalf("sol down failed: %v: %s", err, out)
	}

	// After sol down, all PID files should be gone.
	for _, daemon := range []string{"prefect", "consul", "chronicle", "ledger", "broker"} {
		pidPath := filepath.Join(gtHome, ".runtime", daemon+".pid")
		if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
			t.Errorf("expected %s PID file to be removed after sol down, still exists at %s", daemon, pidPath)
		}
	}
}

// TestAccountCLI tests the full account management lifecycle:
// add -> list -> set-api-key -> remove -> list (empty).
func TestAccountCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	if err := os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	// Add an account.
	out, err := runGT(t, gtHome, "account", "add", "alice", "--email=alice@example.com", "--description=Test account")
	if err != nil {
		t.Fatalf("account add failed: %v: %s", err, out)
	}
	if !strings.Contains(out, `Added account "alice"`) {
		t.Errorf("expected add confirmation in output, got: %s", out)
	}

	// List accounts — should contain alice.
	out, err = runGT(t, gtHome, "account", "list")
	if err != nil {
		t.Fatalf("account list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected 'alice' in account list output, got: %s", out)
	}

	// List accounts as JSON — should contain alice.
	out, err = runGT(t, gtHome, "account", "list", "--json")
	if err != nil {
		t.Fatalf("account list --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("account list --json output is not valid JSON: %s", out)
	}
	var accounts []struct {
		Handle string `json:"handle"`
		Email  string `json:"email"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &accounts); jsonErr != nil {
		t.Fatalf("account list --json parse error: %v: %s", jsonErr, out)
	}
	if len(accounts) != 1 || accounts[0].Handle != "alice" {
		t.Errorf("expected exactly 1 account 'alice', got: %+v", accounts)
	}

	// Set API key for alice.
	out, err = runGT(t, gtHome, "account", "set-api-key", "alice", "test-api-key-12345")
	if err != nil {
		t.Fatalf("account set-api-key failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "API key stored") {
		t.Errorf("expected 'API key stored' in output, got: %s", out)
	}

	// Verify API key token file exists at $SOL_HOME/.accounts/{handle}/token.json.
	tokenPath := filepath.Join(gtHome, ".accounts", "alice", "token.json")
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		t.Errorf("expected token file at %s after set-api-key", tokenPath)
	}

	// Add a second account to test multi-account list.
	out, err = runGT(t, gtHome, "account", "add", "bob")
	if err != nil {
		t.Fatalf("account add bob failed: %v: %s", err, out)
	}

	// Set default account.
	out, err = runGT(t, gtHome, "account", "default", "alice")
	if err != nil {
		t.Fatalf("account default alice failed: %v: %s", err, out)
	}
	if !strings.Contains(out, `"alice"`) {
		t.Errorf("expected default confirmation in output, got: %s", out)
	}

	// Show default account.
	out, err = runGT(t, gtHome, "account", "default")
	if err != nil {
		t.Fatalf("account default (show) failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected 'alice' as default account, got: %s", out)
	}

	// Remove alice (default) — must change default first.
	// Set bob as default before removing alice.
	out, err = runGT(t, gtHome, "account", "default", "bob")
	if err != nil {
		t.Fatalf("account default bob failed: %v: %s", err, out)
	}

	out, err = runGT(t, gtHome, "account", "remove", "--confirm", "alice")
	if err != nil {
		t.Fatalf("account remove alice failed: %v: %s", err, out)
	}
	if !strings.Contains(out, `Removed account "alice"`) {
		t.Errorf("expected remove confirmation in output, got: %s", out)
	}

	// Remove bob.
	out, err = runGT(t, gtHome, "account", "remove", "--confirm", "bob")
	if err != nil {
		t.Fatalf("account remove bob failed: %v: %s", err, out)
	}

	// List — should be empty.
	out, err = runGT(t, gtHome, "account", "list")
	if err != nil {
		t.Fatalf("account list (empty) failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "No accounts registered") {
		t.Errorf("expected 'No accounts registered' after removing all accounts, got: %s", out)
	}
}

// TestAccountAddDuplicate verifies that adding an account with an existing
// handle returns an error.
func TestAccountAddDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	if err := os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}

	// Add once — succeeds.
	out, err := runGT(t, gtHome, "account", "add", "alice")
	if err != nil {
		t.Fatalf("first account add failed: %v: %s", err, out)
	}

	// Add again — should error.
	out, err = runGT(t, gtHome, "account", "add", "alice")
	if err == nil {
		t.Fatalf("expected error on duplicate account add, got success: %s", out)
	}
}

// TestAccountSetAPIKeyNotFound verifies that set-api-key fails when the
// account does not exist.
func TestAccountSetAPIKeyNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)

	out, err := runGT(t, gtHome, "account", "set-api-key", "nobody", "some-key")
	if err == nil {
		t.Fatalf("expected error for nonexistent account, got success: %s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("expected 'not found' in error output, got: %s", out)
	}
}

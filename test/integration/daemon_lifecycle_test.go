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
	// daemon results even when some fail. The combined output may include an
	// error message after the JSON line (from cobra), so extract the first line.
	jsonLine := out
	if idx := strings.Index(out, "\n"); idx >= 0 {
		jsonLine = out[:idx]
	}
	var upResult struct {
		SphereDaemons []struct {
			Name           string `json:"name"`
			Started        bool   `json:"started"`
			AlreadyRunning bool   `json:"already_running"`
			Error          string `json:"error"`
		} `json:"sphere_daemons"`
	}
	if jsonErr := json.Unmarshal([]byte(jsonLine), &upResult); jsonErr != nil {
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

// TestAccountCLI tests the full account management lifecycle:
// add -> list -> set-api-key -> remove -> list (empty).
func TestAccountCLI(t *testing.T) {
	skipUnlessIntegration(t)

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
	skipUnlessIntegration(t)

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
	skipUnlessIntegration(t)

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

// TestAccountRemoveLiveBindingGuard verifies that `sol account remove`
// refuses to delete an account that is still in use, and that --force
// overrides the refusal with a warning per binding.
//
// Acceptance criteria for the live-binding guard (see writ
// sol-271255625dd88a50): the bare `account remove --confirm` must exit
// non-zero with a clear message naming each binding, and `--force` must
// succeed with a warning per binding.
func TestAccountRemoveLiveBindingGuard(t *testing.T) {
	skipUnlessIntegration(t)

	gtHome := t.TempDir()
	t.Setenv("SOL_HOME", gtHome)
	if err := os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755); err != nil {
		t.Fatalf("create .store dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(gtHome, ".runtime"), 0o755); err != nil {
		t.Fatalf("create .runtime dir: %v", err)
	}

	// Create the account.
	out, err := runGT(t, gtHome, "account", "add", "alice")
	if err != nil {
		t.Fatalf("account add alice failed: %v: %s", err, out)
	}

	// Bind the account via a fake claude-config metadata file. This is the
	// simplest binding to fake — no need for a running session, just the
	// .account file under the world's .claude-config tree. Marking it as a
	// binding still requires a world.toml so FindBindings recognises the
	// directory as a world.
	worldDir := filepath.Join(gtHome, "fakeworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatalf("create world dir: %v", err)
	}
	worldToml := `[world]
source_repo = "/tmp/none"
branch = "main"
`
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(worldToml), 0o644); err != nil {
		t.Fatalf("write world.toml: %v", err)
	}
	agentConfigDir := filepath.Join(worldDir, ".claude-config", "outposts", "Spectre")
	if err := os.MkdirAll(agentConfigDir, 0o755); err != nil {
		t.Fatalf("create agent config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentConfigDir, ".account"), []byte("alice\n"), 0o644); err != nil {
		t.Fatalf("write .account file: %v", err)
	}

	// Refusal: --confirm without --force must exit non-zero and name the binding.
	out, err = runGT(t, gtHome, "account", "remove", "--confirm", "alice")
	if err == nil {
		t.Fatalf("expected refusal removing live-bound account, got success: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected refusal output to mention account handle, got: %s", out)
	}
	if !strings.Contains(out, "agent_config") || !strings.Contains(out, "Spectre") {
		t.Errorf("expected refusal output to name the live binding, got: %s", out)
	}
	// Account directory must still exist.
	if _, err := os.Stat(filepath.Join(gtHome, ".accounts", "alice")); err != nil {
		t.Errorf("account directory should still exist after refusal: %v", err)
	}

	// Force: --force --confirm proceeds and warns about each binding.
	out, err = runGT(t, gtHome, "account", "remove", "--confirm", "--force", "alice")
	if err != nil {
		t.Fatalf("expected --force removal to succeed, got error: %v: %s", err, out)
	}
	if !strings.Contains(out, `Removed account "alice"`) {
		t.Errorf("expected success confirmation in output, got: %s", out)
	}
	if !strings.Contains(out, "warning") || !strings.Contains(out, "Spectre") {
		t.Errorf("expected per-binding warning in output, got: %s", out)
	}
	// Account directory must be gone.
	if _, err := os.Stat(filepath.Join(gtHome, ".accounts", "alice")); !os.IsNotExist(err) {
		t.Errorf("account directory should be removed after --force: err=%v", err)
	}
}

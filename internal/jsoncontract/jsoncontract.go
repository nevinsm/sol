// Package jsoncontract provides reusable test helpers for asserting that
// 'sol <cmd> --json' output structurally matches the documented cliapi types.
//
// It composes the same isolation rules used by test/integration/helpers_test.go:
// TMUX_TMPDIR, TMUX="", and SOL_SESSION_COMMAND="sleep 300". Because the
// integration helpers live in a test-only package and cannot be imported,
// this package replicates the minimal setup pieces.
//
// TODO: If test/integration/helpers_test.go is ever refactored into an
// importable package, switch to composing those helpers directly.
package jsoncontract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/processutil"
)

// ---------------------------------------------------------------------------
// Binary build (one-time per test process)
// ---------------------------------------------------------------------------

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// solBin returns the path to the built sol binary, building it once if needed.
func solBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		builtBin = filepath.Join(projectRoot(t), "bin", "sol")
		cmd := exec.Command("go", "build", "-o", builtBin, ".")
		cmd.Dir = projectRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build sol binary: %s: %v", out, err)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBin
}

func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (no go.mod)")
		}
		dir = parent
	}
}

// ---------------------------------------------------------------------------
// Test environment setup
// ---------------------------------------------------------------------------

// Env holds the isolated test environment paths.
type Env struct {
	SOLHome    string
	SourceRepo string
}

// SetupEnv creates an isolated test environment with a temp SOL_HOME, a real
// git repo with one commit, and an isolated tmux server. Cleanup is automatic.
//
// Replicates the isolation rules from test/integration/helpers_test.go:
//   - TMUX_TMPDIR  → isolated socket directory (new tmux server)
//   - TMUX=""      → unset inherited tmux var (forces socket-based discovery)
//   - SOL_SESSION_COMMAND="sleep 300" → stub process instead of real claude
func SetupEnv(t *testing.T) Env {
	t.Helper()

	// 1. SOL_HOME
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	for _, sub := range []string{".store", ".runtime"} {
		if err := os.MkdirAll(filepath.Join(solHome, sub), 0o755); err != nil {
			t.Fatalf("create %s dir: %v", sub, err)
		}
	}

	// Write a fake token so startup.Launch can inject credentials.
	writeTestToken(t, solHome)

	// 2. Source repo with one commit.
	sourceRepo := t.TempDir()
	gitRun(t, sourceRepo, "init")
	gitRun(t, sourceRepo, "config", "user.email", "test@test.com")
	gitRun(t, sourceRepo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(sourceRepo, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	gitRun(t, sourceRepo, "add", ".")
	gitRun(t, sourceRepo, "commit", "-m", "initial")

	// 3. Tmux isolation + daemon cleanup.
	isolateTmux(t)
	t.Cleanup(func() { cleanupAllDaemons(t, solHome) })

	return Env{SOLHome: solHome, SourceRepo: sourceRepo}
}

// writeTestToken writes a minimal api_key token to $SOL_HOME/.accounts/token.json
// so startup.Launch can inject credentials in tests.
func writeTestToken(t *testing.T, solHome string) {
	t.Helper()
	accountsDir := filepath.Join(solHome, ".accounts")
	if err := os.MkdirAll(accountsDir, 0o755); err != nil {
		t.Fatalf("create .accounts dir: %v", err)
	}
	tokenJSON := `{"type":"api_key","token":"test-key","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(accountsDir, "token.json"), []byte(tokenJSON), 0o600); err != nil {
		t.Fatalf("write test token: %v", err)
	}
}

// isolateTmux sets up tmux isolation for tests. Must be called before any
// tmux sessions are created.
func isolateTmux(t *testing.T) {
	t.Helper()
	tmuxDir := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmuxDir)
	t.Setenv("TMUX", "")
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tmuxEnv := append(os.Environ(), "TMUX_TMPDIR="+tmuxDir, "TMUX=")
		killCmd := exec.CommandContext(ctx, "tmux", "kill-server")
		killCmd.Env = tmuxEnv
		_ = killCmd.Run()
	})
}

// gitRun runs a git command in the specified directory.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
	}
}

// cleanupAllDaemons kills all sol daemon processes spawned by a test.
func cleanupAllDaemons(t *testing.T, solHome string) {
	t.Helper()

	// Sphere-level daemons.
	runtimeDir := filepath.Join(solHome, ".runtime")
	for _, name := range []string{"prefect", "consul", "chronicle", "ledger", "broker"} {
		killDaemonByPIDFile(t, filepath.Join(runtimeDir, name+".pid"))
	}

	// Per-world daemons.
	entries, err := os.ReadDir(solHome)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		killDaemonByPIDFile(t, filepath.Join(solHome, entry.Name(), "sentinel.pid"))
		killDaemonByPIDFile(t, filepath.Join(solHome, entry.Name(), "forge", "forge.pid"))
	}
}

// killDaemonByPIDFile reads a PID from the given file and gracefully kills the process.
func killDaemonByPIDFile(t *testing.T, pidFile string) {
	t.Helper()
	pid, err := processutil.ReadPID(pidFile)
	if err != nil || pid == 0 {
		return
	}
	if !processutil.IsRunning(pid) {
		return
	}
	t.Logf("cleanupDaemon: killing pid %d from %s", pid, pidFile)
	if err := processutil.GracefulKill(pid, 2*time.Second); err != nil {
		t.Logf("cleanupDaemon: killing pid %d: %v", pid, err)
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// RunCommand runs the sol binary with the given args using an isolated
// test environment. Returns the raw stdout. Fails via t.Fatal on non-zero exit.
func RunCommand(t *testing.T, args ...string) []byte {
	t.Helper()
	bin := solBin(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "SOL_HOME="+os.Getenv("SOL_HOME"))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("RunCommand(%v) failed: %v\nstderr: %s\nstdout: %s",
			args, err, stderr.String(), stdout.String())
	}
	return stdout.Bytes()
}

// RunCommandJSON runs the sol binary with --json appended to the args,
// parses stdout into the given target, and fails via t.Fatal on parse
// failure or non-zero exit.
func RunCommandJSON(t *testing.T, target any, args ...string) {
	t.Helper()
	args = append(args, "--json")
	raw := RunCommand(t, args...)

	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("RunCommandJSON: unmarshal into %T failed: %v\nraw: %s",
			target, err, string(raw))
	}
}

// AssertJSONShape parses raw JSON into the expected struct using strict
// decoding — it fails if unknown fields are present (extra fields) or if
// the JSON is malformed. Accepts testing.TB so it can be used from both
// *testing.T and *testing.B contexts.
func AssertJSONShape(t testing.TB, raw []byte, expected any) {
	t.Helper()

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	if err := dec.Decode(expected); err != nil {
		t.Fatalf("AssertJSONShape: strict decode into %T failed: %v\nraw: %s",
			expected, err, string(raw))
	}
}

// RequireFields asserts that specific JSON paths are present and non-null
// in the given raw JSON. Paths use dotted notation (e.g. "status",
// "phase_progress.0.name"). Fails via t.Error for each missing/null path.
// Accepts testing.TB so it can be used from both *testing.T and *testing.B.
func RequireFields(t testing.TB, raw []byte, paths ...string) {
	t.Helper()

	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("RequireFields: unmarshal failed: %v\nraw: %s", err, string(raw))
	}

	for _, path := range paths {
		val, ok := resolvePath(data, path)
		if !ok {
			t.Errorf("RequireFields: path %q not found in JSON", path)
			continue
		}
		if val == nil {
			t.Errorf("RequireFields: path %q is null", path)
		}
	}
}

// resolvePath traverses a parsed JSON value using a dotted path.
// Returns the value and true if found, or (nil, false) if any segment
// is missing or the path is invalid.
func resolvePath(data any, path string) (any, bool) {
	segments := strings.Split(path, ".")
	current := data

	for _, seg := range segments {
		switch v := current.(type) {
		case map[string]any:
			val, ok := v[seg]
			if !ok {
				return nil, false
			}
			current = val
		case []any:
			// Support numeric indices for array access.
			idx := 0
			for _, ch := range seg {
				if ch < '0' || ch > '9' {
					return nil, false
				}
				idx = idx*10 + int(ch-'0')
			}
			if idx < 0 || idx >= len(v) {
				return nil, false
			}
			current = v[idx]
		default:
			return nil, false
		}
	}
	return current, true
}

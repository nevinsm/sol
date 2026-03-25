package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	buildOnce sync.Once
	builtBin  string
	buildErr  error
)

// gtBin returns the path to the built sol binary, building it if needed.
func gtBin(t *testing.T) string {
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
	// Walk up from test file to find go.mod.
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
			t.Fatal("could not find project root")
		}
		dir = parent
	}
}

// runGT runs the sol binary with the given args and SOL_HOME set.
func runGT(t *testing.T, gtHome string, args ...string) (string, error) {
	t.Helper()
	bin := gtBin(t)
	cmd := exec.Command(bin, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(), "SOL_HOME="+gtHome)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func TestCLIHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "--help")
	if err != nil {
		t.Fatalf("sol --help failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "sol") {
		t.Errorf("sol --help output missing 'sol': %s", out)
	}
}

func TestCLIWritHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "writ", "--help")
	if err != nil {
		t.Fatalf("sol writ --help failed: %v: %s", err, out)
	}
}

func TestCLISessionHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "session", "--help")
	if err != nil {
		t.Fatalf("sol session --help failed: %v: %s", err, out)
	}
}

func TestCLICastHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "cast", "--help")
	if err != nil {
		t.Fatalf("sol cast --help failed: %v: %s", err, out)
	}
}

func TestCLIWritCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=test")
	if err != nil {
		t.Fatalf("sol writ create failed: %v: %s", err, out)
	}
	if !strings.HasPrefix(out, "sol-") {
		t.Errorf("writ create output not an ID: %q", out)
	}
}

func TestCLIWritListJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "ember")

	// Create an item first.
	runGT(t, gtHome, "writ", "create", "--world=ember", "--title=json test")

	out, err := runGT(t, gtHome, "writ", "list", "--world=ember", "--json")
	if err != nil {
		t.Fatalf("sol writ list --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("writ list --json output is not valid JSON: %s", out)
	}
}

func TestCLIWritListDefaultFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "ember")

	// Create two writs: one will stay open, one will be closed.
	openOut, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=open writ")
	if err != nil {
		t.Fatalf("create open writ: %v: %s", err, openOut)
	}
	openID := strings.TrimSpace(openOut)

	closedOut, err := runGT(t, gtHome, "writ", "create", "--world=ember", "--title=closed writ")
	if err != nil {
		t.Fatalf("create closed writ: %v: %s", err, closedOut)
	}
	closedID := strings.TrimSpace(closedOut)

	// Close the second writ.
	_, err = runGT(t, gtHome, "writ", "close", "--world=ember", "--confirm", closedID)
	if err != nil {
		t.Fatalf("close writ: %v", err)
	}

	// Default list should show the open writ but not the closed one.
	out, err := runGT(t, gtHome, "writ", "list", "--world=ember")
	if err != nil {
		t.Fatalf("writ list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, openID) {
		t.Errorf("default list should contain open writ %s, got: %s", openID, out)
	}
	if strings.Contains(out, closedID) {
		t.Errorf("default list should not contain closed writ %s, got: %s", closedID, out)
	}

	// --all should show both writs.
	out, err = runGT(t, gtHome, "writ", "list", "--world=ember", "--all")
	if err != nil {
		t.Fatalf("writ list --all failed: %v: %s", err, out)
	}
	if !strings.Contains(out, openID) {
		t.Errorf("--all list should contain open writ %s, got: %s", openID, out)
	}
	if !strings.Contains(out, closedID) {
		t.Errorf("--all list should contain closed writ %s, got: %s", closedID, out)
	}

	// --status=closed should show only the closed writ.
	out, err = runGT(t, gtHome, "writ", "list", "--world=ember", "--status=closed")
	if err != nil {
		t.Fatalf("writ list --status=closed failed: %v: %s", err, out)
	}
	if strings.Contains(out, openID) {
		t.Errorf("--status=closed should not contain open writ %s, got: %s", openID, out)
	}
	if !strings.Contains(out, closedID) {
		t.Errorf("--status=closed should contain closed writ %s, got: %s", closedID, out)
	}

	// --all --status=open should error.
	out, err = runGT(t, gtHome, "writ", "list", "--world=ember", "--all", "--status=open")
	if err == nil {
		t.Errorf("--all --status=open should error, got: %s", out)
	}
	if !strings.Contains(out, "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive, got: %s", out)
	}

	// Default list with no results should hint at --all.
	// Close the open writ too so default filter returns empty.
	_, err = runGT(t, gtHome, "writ", "close", "--world=ember", "--confirm", openID)
	if err != nil {
		t.Fatalf("close writ: %v", err)
	}
	out, err = runGT(t, gtHome, "writ", "list", "--world=ember")
	if err != nil {
		t.Fatalf("writ list (empty) failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Use --all to include closed writs") {
		t.Errorf("empty default list should hint at --all, got: %s", out)
	}
}

func TestCLIAgentCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "ember")

	out, err := runGT(t, gtHome, "agent", "create", "Smoke", "--world=ember")
	if err != nil {
		t.Fatalf("sol agent create failed: %v: %s", err, out)
	}
}

func TestCLIAgentList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()
	os.MkdirAll(filepath.Join(gtHome, ".store"), 0o755)
	initWorld(t, gtHome, "ember")

	// Create first.
	runGT(t, gtHome, "agent", "create", "Smoke", "--world=ember")

	out, err := runGT(t, gtHome, "agent", "list", "--world=ember")
	if err != nil {
		t.Fatalf("sol agent list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Smoke") {
		t.Errorf("agent list missing 'Smoke': %s", out)
	}
}

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gtBin returns the path to the built sol binary, building it if needed.
func gtBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(projectRoot(t), "bin", "sol")
	if _, err := os.Stat(bin); os.IsNotExist(err) {
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = projectRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build sol binary: %s: %v", out, err)
		}
	}
	return bin
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

func TestCLIStoreHelp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "store", "--help")
	if err != nil {
		t.Fatalf("sol store --help failed: %v: %s", err, out)
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

func TestCLIStoreCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "store", "create", "--world=testrig", "--title=test")
	if err != nil {
		t.Fatalf("sol store create failed: %v: %s", err, out)
	}
	if !strings.HasPrefix(out, "sol-") {
		t.Errorf("store create output not an ID: %q", out)
	}
}

func TestCLIStoreListJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// Create an item first.
	runGT(t, gtHome, "store", "create", "--world=testrig", "--title=json test")

	out, err := runGT(t, gtHome, "store", "list", "--world=testrig", "--json")
	if err != nil {
		t.Fatalf("sol store list --json failed: %v: %s", err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("store list --json output is not valid JSON: %s", out)
	}
}

func TestCLIAgentCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	out, err := runGT(t, gtHome, "agent", "create", "Smoke", "--world=testrig")
	if err != nil {
		t.Fatalf("sol agent create failed: %v: %s", err, out)
	}
}

func TestCLIAgentList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	gtHome := t.TempDir()

	// Create first.
	runGT(t, gtHome, "agent", "create", "Smoke", "--world=testrig")

	out, err := runGT(t, gtHome, "agent", "list", "--world=testrig")
	if err != nil {
		t.Fatalf("sol agent list failed: %v: %s", err, out)
	}
	if !strings.Contains(out, "Smoke") {
		t.Errorf("agent list missing 'Smoke': %s", out)
	}
}

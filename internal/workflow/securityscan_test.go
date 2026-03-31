package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSecurityScanManifest(t *testing.T) {
	// Find the project root by walking up from this test file.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	dir := filepath.Join(root, ".sol", "workflows", "security-scan")
	manifestPath := filepath.Join(dir, "manifest.toml")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal("failed to read manifest:", err)
	}

	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		t.Fatal("TOML parse error:", err)
	}

	if m.Name != "security-scan" {
		t.Errorf("expected name 'security-scan', got %q", m.Name)
	}
	if m.Mode != "manifest" {
		t.Errorf("expected mode 'manifest', got %q", m.Mode)
	}

	// Validate step references and instruction files.
	if err := Validate(&m, dir); err != nil {
		t.Fatal("validation error:", err)
	}

	// Verify phases compute correctly.
	phases := ComputePhases(m.Steps)

	// Phase 0: 7 analysis steps (no dependencies)
	phase0Count := 0
	for _, p := range phases {
		if p == 0 {
			phase0Count++
		}
	}
	if phase0Count != 7 {
		t.Errorf("expected 7 phase-0 steps, got %d", phase0Count)
	}

	// triage should be phase 1
	if p, ok := phases["triage"]; !ok || p != 1 {
		t.Errorf("expected triage at phase 1, got %d (exists=%v)", p, ok)
	}

	// commission should be phase 2
	if p, ok := phases["commission"]; !ok || p != 2 {
		t.Errorf("expected commission at phase 2, got %d (exists=%v)", p, ok)
	}

	t.Logf("OK — %d steps, 3 phases", len(m.Steps))
}

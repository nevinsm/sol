package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cliwrits "github.com/nevinsm/sol/internal/cliapi/writs"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// setupWritStatusTest creates a temporary SOL_HOME with a world and a writ,
// resetting the writ-status package flags to avoid cross-test pollution
// (Cobra flag state persists across Execute() calls within a test binary).
// Returns the world store, world name, and the created writ ID.
func setupWritStatusTest(t *testing.T) (*store.WorldStore, string, string) {
	t.Helper()

	writStatusWorld = ""
	writStatusJSON = false

	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world := "statustest"
	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(dir, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.OpenWorld(world)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	writID, err := s.CreateWrit("Status test writ", "a writ for status tests", "autarch", 2, nil)
	if err != nil {
		t.Fatal(err)
	}

	return s, world, writID
}

// writeResolutionReport writes content to writ-outputs/{writID}/resolution.md
// under the given SOL_HOME, mirroring where captureResolutionReport stores it.
func writeResolutionReport(t *testing.T, world, writID, content string) string {
	t.Helper()
	outDir := config.WritOutputDir(world, writID)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outDir, "resolution.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const shortResolutionReport = `# Resolution Report

## Summary
Did the thing.

## Deviations from spec
None.

## Assumptions
None.

## Surprises
None.

## Durable lessons
None.
`

func TestWritStatus_NoResolutionReport(t *testing.T) {
	_, world, writID := setupWritStatusTest(t)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"writ", "status", writID, "--world", world})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("writ status failed: %v", err)
		}
	})

	if strings.Contains(out, "Resolution report:") {
		t.Errorf("expected no Resolution report section when none captured, got:\n%s", out)
	}
}

func TestWritStatus_WithResolutionReport(t *testing.T) {
	_, world, writID := setupWritStatusTest(t)
	path := writeResolutionReport(t, world, writID, shortResolutionReport)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"writ", "status", writID, "--world", world})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("writ status failed: %v", err)
		}
	})

	if !strings.Contains(out, "Resolution report:") {
		t.Errorf("expected Resolution report section, got:\n%s", out)
	}
	if !strings.Contains(out, path) {
		t.Errorf("expected report path %q in output, got:\n%s", path, out)
	}
	if !strings.Contains(out, "Did the thing.") {
		t.Errorf("expected full report body in output, got:\n%s", out)
	}
	if !strings.Contains(out, "## Durable lessons") {
		t.Errorf("expected the full report (including Durable lessons) for a short report, got:\n%s", out)
	}
}

func TestWritStatus_LongResolutionReportTruncates(t *testing.T) {
	_, world, writID := setupWritStatusTest(t)

	var b strings.Builder
	b.WriteString("# Resolution Report\n\n## Summary\nShort summary line.\n\n## Deviations from spec\nOne deviation line.\n\n## Assumptions\n")
	for range 60 {
		b.WriteString("assumption filler line\n")
	}
	b.WriteString("\n## Surprises\nNone.\n\n## Durable lessons\nNone.\n")
	path := writeResolutionReport(t, world, writID, b.String())

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"writ", "status", writID, "--world", world})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("writ status failed: %v", err)
		}
	})

	if !strings.Contains(out, path) {
		t.Errorf("expected report path %q always shown, got:\n%s", path, out)
	}
	if !strings.Contains(out, "Short summary line.") {
		t.Errorf("expected Summary section content, got:\n%s", out)
	}
	if !strings.Contains(out, "One deviation line.") {
		t.Errorf("expected Deviations section content, got:\n%s", out)
	}
	if strings.Contains(out, "assumption filler line") {
		t.Errorf("expected Assumptions section to be omitted for a long report, got:\n%s", out)
	}
}

func TestWritStatus_JSON_IncludesResolutionReport(t *testing.T) {
	_, world, writID := setupWritStatusTest(t)
	path := writeResolutionReport(t, world, writID, shortResolutionReport)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"writ", "status", writID, "--world", world, "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("writ status --json failed: %v", err)
		}
	})

	var resp cliwrits.Writ
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\noutput: %s", err, out)
	}
	if resp.ResolutionReport == nil {
		t.Fatalf("expected resolution_report in JSON output, got nil. output: %s", out)
	}
	if resp.ResolutionReport.Path != path {
		t.Errorf("resolution_report.path = %q, want %q", resp.ResolutionReport.Path, path)
	}
	if resp.ResolutionReport.Content != shortResolutionReport {
		t.Errorf("resolution_report.content mismatch:\ngot:  %q\nwant: %q", resp.ResolutionReport.Content, shortResolutionReport)
	}
}

func TestWritStatus_JSON_NoResolutionReport(t *testing.T) {
	_, world, writID := setupWritStatusTest(t)

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"writ", "status", writID, "--world", world, "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("writ status --json failed: %v", err)
		}
	})

	if strings.Contains(out, "resolution_report") {
		t.Errorf("expected resolution_report to be omitted when no report captured, got:\n%s", out)
	}

	var resp cliwrits.Writ
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v\noutput: %s", err, out)
	}
	if resp.ResolutionReport != nil {
		t.Errorf("expected nil ResolutionReport, got %+v", resp.ResolutionReport)
	}
}

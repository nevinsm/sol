package docvalidate

import (
	"strings"
	"testing"
)

const codebaseScanManifest = `name = "codebase-scan"
mode = "manifest"

[[steps]]
id = "a"
title = "Step A"

[[steps]]
id = "b"
title = "Step B"

[[steps]]
id = "c"
title = "Step C"
`

const reviewManifest = `name = "review"
mode = "manifest"

[[steps]]
id = "x"
title = "Only"
`

func TestCountStepsInManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "manifest.toml", codebaseScanManifest)
	n, err := countStepsInManifest(root + "/manifest.toml")
	if err != nil {
		t.Fatalf("countStepsInManifest: %v", err)
	}
	if n != 3 {
		t.Errorf("got %d want 3", n)
	}
}

func TestCheckWorkflowSteps_FlagsMismatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sol/workflows/codebase-scan/manifest.toml", codebaseScanManifest)
	writeFile(t, root, ".sol/workflows/review/manifest.toml", reviewManifest)

	// Doc claims wrong count for codebase-scan, correct for review.
	writeFile(t, root, "docs/workflows.md", strings.Join([]string{
		"# Workflows",
		"",
		"### 1. codebase-scan",
		"",
		"**Mode:** manifest (12 steps)",
		"",
		"### 2. review",
		"",
		"**Mode:** manifest (1 step)",
		"",
	}, "\n"))

	findings, err := CheckWorkflowSteps(root)
	if err != nil {
		t.Fatalf("CheckWorkflowSteps: %v", err)
	}
	flagged := findingsByCheck(findings, "workflow-steps")
	if len(flagged) != 1 {
		t.Fatalf("want 1 finding, got %d: %+v", len(flagged), flagged)
	}
	if !strings.Contains(flagged[0].Message, "codebase-scan") ||
		!strings.Contains(flagged[0].Message, "12") ||
		!strings.Contains(flagged[0].Message, "3") {
		t.Errorf("message missing expected fields: %q", flagged[0].Message)
	}
}

func TestCheckWorkflowSteps_OrphanClaim(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sol/workflows/foo/manifest.toml", reviewManifest)
	// Step-count claim with no preceding workflow heading.
	writeFile(t, root, "docs/workflows.md", "**Mode:** manifest (5 steps)\n")

	findings, err := CheckWorkflowSteps(root)
	if err != nil {
		t.Fatalf("CheckWorkflowSteps: %v", err)
	}
	if !containsMessage(findings, "no preceding workflow heading") {
		t.Errorf("expected orphan-claim finding, got %+v", findings)
	}
}

func TestCheckWorkflowSteps_PassesWhenAligned(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sol/workflows/codebase-scan/manifest.toml", codebaseScanManifest)
	writeFile(t, root, "docs/workflows.md", strings.Join([]string{
		"### codebase-scan",
		"",
		"**Mode:** manifest (3 steps)",
		"",
	}, "\n"))

	findings, err := CheckWorkflowSteps(root)
	if err != nil {
		t.Fatalf("CheckWorkflowSteps: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %+v", findings)
	}
}

func TestLoadWorkflowManifests_PrefersProjectTier(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".sol/workflows/dup/manifest.toml", codebaseScanManifest) // 3 steps
	writeFile(t, root, "internal/workflow/defaults/dup/manifest.toml", reviewManifest) // 1 step

	got, err := loadWorkflowManifests(root)
	if err != nil {
		t.Fatalf("loadWorkflowManifests: %v", err)
	}
	if got["dup"] != 3 {
		t.Errorf("project-tier should win: got %d want 3", got["dup"])
	}
}

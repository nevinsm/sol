package docvalidate

import (
	"testing"
)

// TestRun_AggregatesFromAllChecks confirms that Run delegates to every
// registered check and aggregates findings into one report. We do this by
// building a synthetic repo that triggers two distinct checks
// simultaneously and asserting both findings are present.
func TestRun_AggregatesFromAllChecks(t *testing.T) {
	root := t.TempDir()

	// Trigger adr-refs:
	writeFile(t, root, "docs/decisions/README.md", adrIndexFixture)
	writeFile(t, root, "CLAUDE.md", "see ADR-0027.\n")

	// Trigger persona-archetypes:
	writeFile(t, root, personaDefaultsPath, personaDefaultsFixture)
	writeFile(t, root, "docs/personas.md", personasDocFixture)

	// Trigger recovery-matrix (broker missing):
	writeFile(t, root, servicePackagePath, servicePackageFixture)
	writeFile(t, root, recoveryMatrixDoc, failureModesFixture)

	// Avoid spurious findings from the other three checks by giving them
	// no input files. test/integration/ doesn't exist → walk error.
	writeFile(t, root, "test/integration/.keep", "")
	writeFile(t, root, "internal/workflow/defaults/.keep", "")
	writeFile(t, root, ".sol/workflows/.keep", "")
	// Empty operations.md so heartbeat-paths reports nothing.
	writeFile(t, root, "docs/operations.md", "# ops\n")

	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.HasFailures() {
		t.Fatal("expected failures from triggered checks")
	}

	// Verify all three triggered checks fired.
	checks := map[string]int{}
	for _, f := range report.Findings {
		checks[f.Check]++
	}
	for _, want := range []string{"adr-refs", "persona-archetypes", "recovery-matrix"} {
		if checks[want] == 0 {
			t.Errorf("expected at least one %q finding, got: %v", want, checks)
		}
	}
}

func TestAllChecks_NamesAreUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, c := range AllChecks() {
		if seen[c.Name] {
			t.Errorf("duplicate check name %q", c.Name)
		}
		seen[c.Name] = true
		if c.Run == nil {
			t.Errorf("check %q has nil Run", c.Name)
		}
	}
}

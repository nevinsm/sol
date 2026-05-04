package docvalidate

import (
	"fmt"
)

// Check is one drift check. Returns a slice of findings (possibly empty) and
// an error only if the check itself could not run (missing fixture file,
// malformed Go syntax, etc.). A "drift detected" outcome is reported via
// findings, not via error — that lets callers distinguish "the validator is
// broken" from "the docs are wrong".
type Check struct {
	Name string
	Run  func(repoRoot string) ([]Finding, error)
}

// AllChecks returns the canonical list of drift checks shipped with the
// validator, in deterministic order.
func AllChecks() []Check {
	return []Check{
		{Name: "adr-refs", Run: CheckADRReferences},
		{Name: "workflow-steps", Run: CheckWorkflowSteps},
		{Name: "recovery-matrix", Run: CheckRecoveryMatrix},
		{Name: "heartbeat-paths", Run: CheckHeartbeatPaths},
		{Name: "persona-archetypes", Run: CheckPersonaArchetypes},
		{Name: "acceptance-tests", Run: CheckAcceptanceTests},
	}
}

// Run executes every check in AllChecks against the tree rooted at repoRoot
// and returns the aggregated report.
//
// Per-check errors (e.g., a missing manifest) are wrapped and returned;
// drift is recorded in Report.Findings. The two are not exclusive: a check
// that fails halfway may still have produced partial findings, which the
// returned report will contain.
func Run(repoRoot string) (Report, error) {
	var report Report
	var firstErr error
	for _, c := range AllChecks() {
		findings, err := c.Run(repoRoot)
		report.Findings = append(report.Findings, findings...)
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("check %q: %w", c.Name, err)
		}
	}
	return report, firstErr
}

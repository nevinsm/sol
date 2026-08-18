package doctor

import (
	"fmt"

	"github.com/nevinsm/sol/internal/workflow"
)

// CheckWorkflowsStale warns about files inside auto-extracted workflow
// directories ($SOL_HOME/workflows/{name}/) that diverge from their current
// embedded template and cannot be confirmed safe to auto-refresh — either
// the operator hand-edited them in place (without going through Eject), or
// they predate per-file hash-stamping and can't be verified as untouched.
//
// This mirrors guidelines:stale (CheckGuidelinesStale) and is advisory
// only: content divergence may be deliberate operator customization, which
// is operator data. There is no --fix here — the remediation is manual
// (diff, then Eject to take explicit ownership of a copy, or hand-merge).
//
// Files that are up to date, or untouched (and so self-heal transparently
// the next time the workflow is resolved), are not surfaced — there is
// nothing for an operator to act on.
func CheckWorkflowsStale() []CheckResult {
	const name = "workflows:stale"

	stale, err := workflow.CheckStaleFiles()
	if err != nil {
		return []CheckResult{{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("workflows: failed to check auto-extracted workflow files: %v", err),
			Fix:     "check permissions on $SOL_HOME/workflows/",
		}}
	}

	if len(stale) == 0 {
		return []CheckResult{{
			Name:    name,
			Passed:  true,
			Message: "no stale auto-extracted workflow files",
		}}
	}

	results := make([]CheckResult, 0, len(stale))
	for _, s := range stale {
		reason := "hand-edited — its content differs from the embedded template it was extracted from"
		if !s.Verifiable {
			reason = "unverifiable — it predates per-file hash-stamping and its content differs from the current " +
				"embedded template, so sol can't tell whether this is a hand edit or simply a pre-stamping extract"
		}
		results = append(results, CheckResult{
			Name:    fmt.Sprintf("%s:%s:%s", name, s.WorkflowName, s.RelPath),
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"workflows: %q in workflow %q at %s is %s.\n"+
					"      It will NOT be auto-updated, so embedded template changes won't reach it.",
				s.RelPath, s.WorkflowName, s.Path, reason),
			Fix: fmt.Sprintf(
				"Diff %s against the current embedded default, then either run\n"+
					"      `sol workflow eject %s --confirm` to take explicit ownership, or manually merge in\n"+
					"      the changes. No auto-fix — hand-edited workflow content is operator data.",
				s.Path, s.WorkflowName),
		})
	}
	return results
}

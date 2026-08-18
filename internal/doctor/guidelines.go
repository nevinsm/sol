package doctor

import (
	"fmt"

	"github.com/nevinsm/sol/internal/guidelines"
)

// CheckGuidelinesStale warns about user-tier guideline extracts
// ($SOL_HOME/guidelines/*.md) that diverge from their current embedded
// template and cannot be confirmed safe to auto-refresh — either the
// operator customized them, or they predate hash-stamping and can't be
// verified as untouched.
//
// This is advisory only: content divergence may be deliberate operator
// customization, which is operator data. There is no --fix here — the
// remediation is manual (diff, then delete-to-re-extract or hand-merge).
//
// Extracts that are up to date, or untouched since extraction (in which case
// guidelines.Resolve refreshes them transparently the next time the template
// is used), are not surfaced — there is nothing for an operator to act on.
func CheckGuidelinesStale() []CheckResult {
	const name = "guidelines:stale"

	stale, err := guidelines.CheckStaleExtracts()
	if err != nil {
		return []CheckResult{{
			Name:    name,
			Passed:  false,
			Message: fmt.Sprintf("guidelines: failed to check user-tier extracts: %v", err),
			Fix:     "check permissions on $SOL_HOME/guidelines/",
		}}
	}

	if len(stale) == 0 {
		return []CheckResult{{
			Name:    name,
			Passed:  true,
			Message: "no stale user-tier guideline extracts",
		}}
	}

	results := make([]CheckResult, 0, len(stale))
	for _, s := range stale {
		reason := "customized — its content differs from the embedded template it was extracted from"
		if !s.Verifiable {
			reason = "unverifiable — it predates hash-stamping and its content differs from the current " +
				"embedded template, so sol can't tell whether this is operator customization or simply an old extract"
		}
		results = append(results, CheckResult{
			Name:    fmt.Sprintf("%s:%s", name, s.Name),
			Passed:  true,
			Warning: true,
			Message: fmt.Sprintf(
				"guidelines: %q extract at %s is %s.\n"+
					"      It will NOT be auto-updated, so embedded template changes won't reach it.",
				s.Name, s.Path, reason),
			Fix: fmt.Sprintf(
				"Diff %s against the current embedded default, then either delete it to\n"+
					"      re-extract the current default, or manually merge in the changes. No auto-fix —\n"+
					"      customized guideline content is operator data.",
				s.Path),
		})
	}
	return results
}

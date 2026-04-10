// Package doctor provides the CLI API types for doctor command output.
package doctor

import (
	"github.com/nevinsm/sol/internal/doctor"
)

// CheckResult is the CLI API representation of a single doctor check.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Warning bool   `json:"warning,omitempty"`
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

// DoctorResponse is the CLI API representation of sol doctor --json output.
type DoctorResponse struct {
	Checks []CheckResult `json:"checks"`
}

// FromReport converts an internal doctor.Report to a DoctorResponse.
func FromReport(r *doctor.Report) DoctorResponse {
	checks := make([]CheckResult, len(r.Checks))
	for i, c := range r.Checks {
		checks[i] = CheckResult{
			Name:    c.Name,
			Passed:  c.Passed,
			Warning: c.Warning,
			Message: c.Message,
			Fix:     c.Fix,
		}
	}
	return DoctorResponse{Checks: checks}
}

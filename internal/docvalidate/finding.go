// Package docvalidate runs documentation drift checks against the source tree.
//
// Each check looks at one piece of "ground truth" in the code (a workflow
// manifest, a Go slice, an ADR index, …) and one or more documents that
// describe it. When the document disagrees with the code, the check emits a
// Finding with file:line context and an expected-vs-actual message.
//
// The checks are independent — running any subset works. They are intended to
// be cheap (regex + Go AST walks, no shelling out) so they can run on every
// build via `make docs-validate`.
package docvalidate

import (
	"fmt"
	"sort"
	"strings"
)

// Finding describes one documentation-drift problem.
//
// File is repo-relative. Line is 1-based; 0 means "no specific line"
// (the message itself describes the location).
type Finding struct {
	Check   string // short name of the check that produced this finding (e.g., "adr-refs")
	File    string // repo-relative path to the offending file
	Line    int    // 1-based line number, or 0 if not applicable
	Message string // human-readable description ending with the expected vs actual
}

// String returns "file:line: [check] message" or "file: [check] message" when
// Line is zero. Used for error reports and golden-file tests.
func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: [%s] %s", f.File, f.Line, f.Check, f.Message)
	}
	return fmt.Sprintf("%s: [%s] %s", f.File, f.Check, f.Message)
}

// Report is the aggregated output of one or more checks.
type Report struct {
	Findings []Finding
}

// HasFailures reports whether any finding was recorded.
func (r Report) HasFailures() bool {
	return len(r.Findings) > 0
}

// Sorted returns the findings ordered by check, then file, then line. The
// returned slice is a copy; the receiver is not mutated.
func (r Report) Sorted() []Finding {
	out := make([]Finding, len(r.Findings))
	copy(out, r.Findings)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Check != out[j].Check {
			return out[i].Check < out[j].Check
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// Format renders the report as a human-readable error block. Empty if there
// are no findings.
func (r Report) Format() string {
	if !r.HasFailures() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "docvalidate: %d documentation drift finding(s)\n", len(r.Findings))
	for _, f := range r.Sorted() {
		fmt.Fprintf(&b, "  %s\n", f.String())
	}
	return b.String()
}

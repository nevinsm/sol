package docvalidate

import (
	"strings"
	"testing"
)

func TestFindingString(t *testing.T) {
	cases := []struct {
		name string
		in   Finding
		want string
	}{
		{
			name: "with line",
			in:   Finding{Check: "adr-refs", File: "CLAUDE.md", Line: 34, Message: "cites ADR-0027"},
			want: "CLAUDE.md:34: [adr-refs] cites ADR-0027",
		},
		{
			name: "no line",
			in:   Finding{Check: "recovery-matrix", File: "docs/failure-modes.md", Line: 0, Message: "missing row for Broker"},
			want: "docs/failure-modes.md: [recovery-matrix] missing row for Broker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.String(); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestReportSortedAndFormat(t *testing.T) {
	r := Report{Findings: []Finding{
		{Check: "workflow-steps", File: "docs/workflows.md", Line: 50, Message: "z"},
		{Check: "adr-refs", File: "docs/b.md", Line: 1, Message: "a"},
		{Check: "adr-refs", File: "docs/a.md", Line: 5, Message: "x"},
		{Check: "adr-refs", File: "docs/a.md", Line: 1, Message: "y"},
	}}
	if !r.HasFailures() {
		t.Fatal("HasFailures should be true")
	}
	sorted := r.Sorted()
	wantOrder := []string{"docs/a.md", "docs/a.md", "docs/b.md", "docs/workflows.md"}
	for i, want := range wantOrder {
		if sorted[i].File != want {
			t.Errorf("sorted[%d].File = %q want %q", i, sorted[i].File, want)
		}
	}
	// Check (a.md) line ordering inside the same check+file.
	if sorted[0].Line != 1 || sorted[1].Line != 5 {
		t.Errorf("expected line ordering 1,5, got %d,%d", sorted[0].Line, sorted[1].Line)
	}

	out := r.Format()
	if !strings.Contains(out, "4 documentation drift finding(s)") {
		t.Errorf("Format missing finding count: %q", out)
	}
	if !strings.Contains(out, "[adr-refs]") {
		t.Errorf("Format missing check name: %q", out)
	}
}

func TestReportEmpty(t *testing.T) {
	r := Report{}
	if r.HasFailures() {
		t.Error("empty report should not have failures")
	}
	if r.Format() != "" {
		t.Errorf("empty report should format to empty string, got %q", r.Format())
	}
}

package resolutionreport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleReport = `# Resolution Report

## Summary
Did the thing.

## Deviations from spec
None.

## Assumptions
Assumed X.

## Surprises
Nothing surprising.

## Durable lessons
Watch out for Y next time.
`

func TestPath(t *testing.T) {
	t.Setenv("SOL_HOME", "/tmp/sol-home-test")
	got := Path("ember", "sol-abc123")
	want := filepath.Join("/tmp/sol-home-test", "ember", "writ-outputs", "sol-abc123", "resolution.md")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLoad_Absent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	report, err := Load("ember", "sol-missing")
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for absent report", err)
	}
	if report != nil {
		t.Errorf("Load() = %+v, want nil for absent report", report)
	}
}

func TestLoad_Present(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	outDir := filepath.Join(dir, "ember", "writ-outputs", "sol-abc123")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, Filename), []byte(sampleReport), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Load("ember", "sol-abc123")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if report == nil {
		t.Fatal("Load() = nil, want a report")
	}
	if report.Content != sampleReport {
		t.Errorf("Load() content mismatch:\ngot:  %q\nwant: %q", report.Content, sampleReport)
	}
	if report.Path != filepath.Join(outDir, Filename) {
		t.Errorf("Load() path = %q, want %q", report.Path, filepath.Join(outDir, Filename))
	}
}

func TestLoad_UnreadableIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Block the report path with a directory instead of a regular file so
	// os.ReadFile fails with something other than "not exist".
	outDir := filepath.Join(dir, "ember", "writ-outputs", "sol-abc123")
	if err := os.MkdirAll(filepath.Join(outDir, Filename), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := Load("ember", "sol-abc123")
	if err == nil {
		t.Fatal("Load() error = nil, want error when report path is a directory")
	}
	if report != nil {
		t.Errorf("Load() = %+v, want nil report on error", report)
	}
}

func TestSection(t *testing.T) {
	tests := []struct {
		heading string
		want    string
	}{
		{SectionSummary, "Did the thing."},
		{SectionDeviations, "None."},
		{SectionAssumptions, "Assumed X."},
		{SectionSurprises, "Nothing surprising."},
		{SectionDurableLessons, "Watch out for Y next time."},
		{"Nonexistent", ""},
	}
	for _, tc := range tests {
		got := Section(sampleReport, tc.heading)
		if got != tc.want {
			t.Errorf("Section(%q) = %q, want %q", tc.heading, got, tc.want)
		}
	}
}

func TestSection_LastSectionRunsToEnd(t *testing.T) {
	content := "## Durable lessons\nline one\nline two\n"
	got := Section(content, SectionDurableLessons)
	want := "line one\nline two"
	if got != want {
		t.Errorf("Section() = %q, want %q", got, want)
	}
}

func TestHasContent(t *testing.T) {
	cases := map[string]bool{
		"":                  false,
		"   \n\t\n  ":       false,
		"-":                 false,
		"- ":                false,
		"-\n-\n-":           false,
		"* ":                false,
		"+  ":               false,
		"None.":             true, // literal prose is meaningful per spec
		"- Watch out for Y": true,
		"  some text  ":     true,
	}
	for section, want := range cases {
		got := HasContent(section)
		if got != want {
			t.Errorf("HasContent(%q) = %v, want %v", section, got, want)
		}
	}
}

func TestRenderLines_ShortReportShowsFullContent(t *testing.T) {
	r := &Report{Path: "/tmp/resolution.md", Content: sampleReport}
	lines := r.RenderLines()
	want := strings.Split(strings.TrimRight(sampleReport, "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("RenderLines() returned %d lines, want %d:\n%v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("RenderLines()[%d] = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestRenderLines_LongReportTruncatesToSummaryAndDeviations(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Resolution Report\n\n## Summary\nShort summary.\n\n## Deviations from spec\nOne deviation.\n\n## Assumptions\n")
	for range 60 {
		b.WriteString("assumption line\n")
	}
	b.WriteString("\n## Surprises\nNone.\n\n## Durable lessons\nNone.\n")
	content := b.String()

	r := &Report{Path: "/tmp/resolution.md", Content: content}
	lines := r.RenderLines()

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Short summary.") {
		t.Errorf("RenderLines() missing Summary content:\n%s", joined)
	}
	if !strings.Contains(joined, "One deviation.") {
		t.Errorf("RenderLines() missing Deviations content:\n%s", joined)
	}
	if strings.Contains(joined, "assumption line") {
		t.Errorf("RenderLines() should not include Assumptions section for a long report:\n%s", joined)
	}
	if !strings.Contains(joined, "/tmp/resolution.md") {
		t.Errorf("RenderLines() should point to the report path when truncated:\n%s", joined)
	}
}

func TestRenderLines_EmptyContent(t *testing.T) {
	r := &Report{Path: "/tmp/resolution.md", Content: ""}
	if lines := r.RenderLines(); lines != nil {
		t.Errorf("RenderLines() = %v, want nil for empty content", lines)
	}
}

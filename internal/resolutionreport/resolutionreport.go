// Package resolutionreport provides shared read/parse helpers for the
// resolution report an agent may write at the worktree root before
// resolving (.resolution.md), captured by dispatch's resolve step into
// the writ's persistent output directory as resolution.md (see
// internal/dispatch/resolve.go's captureResolutionReport).
//
// This package does not write or capture the report — that logic lives in
// internal/dispatch and is intentionally left untouched (see the writ that
// added this package: consumers surface and route the already-captured
// report, they don't change how it's produced). It only knows how to find,
// read, and parse the captured file so cmd/writ.go (status), internal/trace
// (trace rendering), and internal/dispatch (durable-lessons mail routing)
// share one definition of the report's shape instead of three.
package resolutionreport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/config"
)

// Filename is the name of the captured resolution report file within a
// writ's persistent output directory.
const Filename = "resolution.md"

// The five section headings the resolve-and-submit guidelines instruct
// agents to use, in order, as "## <Heading>" markdown lines. Any section
// may be empty, but all five headers are expected to be present.
const (
	SectionSummary        = "Summary"
	SectionDeviations     = "Deviations from spec"
	SectionAssumptions    = "Assumptions"
	SectionSurprises      = "Surprises"
	SectionDurableLessons = "Durable lessons"
)

// MaxInlineLines is the line-count threshold above which callers should
// render only the Summary and Deviations sections instead of the full
// report body (see [Report.RenderLines]) — the path is always shown
// regardless, so the reader can reach the full report either way.
const MaxInlineLines = 40

// Report holds a captured resolution report's path and raw content.
type Report struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Path returns the path where a writ's captured resolution report would
// live, whether or not it currently exists.
func Path(world, writID string) string {
	return filepath.Join(config.WritOutputDir(world, writID), Filename)
}

// Load reads the captured resolution report for a writ. Returns (nil, nil)
// if no report was captured — most writs won't have one, and that is not
// an error condition for callers.
func Load(world, writID string) (*Report, error) {
	path := Path(world, writID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read resolution report %q: %w", path, err)
	}
	return &Report{Path: path, Content: string(data)}, nil
}

// Section extracts the body text of a "## <heading>" markdown section from
// report content. Returns "" if the heading is not present. The body runs
// from the line after the heading until the next "## " heading (or end of
// content), trimmed of surrounding whitespace.
func Section(content, heading string) string {
	lines := strings.Split(content, "\n")
	marker := "## " + heading
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == marker {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

// HasContent reports whether a section body has meaningful text: non-empty
// once whitespace-only lines and bare bullet-marker placeholders (e.g. "-",
// "*", "+" with nothing after them) are ignored. It does not attempt to
// interpret prose like "None" or "N/A" as empty — an agent that wrote that
// literally is asserting content, however terse.
func HasContent(section string) bool {
	for line := range strings.SplitSeq(section, "\n") {
		l := strings.TrimSpace(line)
		l = strings.TrimLeft(l, "-*+")
		l = strings.TrimSpace(l)
		if l != "" {
			return true
		}
	}
	return false
}

// RenderLines returns the report body as display lines for a text-mode
// consumer: the full content when it's at most MaxInlineLines lines, or
// just the Summary and Deviations sections (each preceded by its "##
// <heading>" line) plus a note pointing to Path for the rest when the
// report is longer. Callers should also always display r.Path themselves
// (RenderLines does not repeat it in the un-truncated case).
func (r *Report) RenderLines() []string {
	trimmed := strings.TrimRight(r.Content, "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= MaxInlineLines {
		return lines
	}

	var out []string
	if s := Section(r.Content, SectionSummary); s != "" {
		out = append(out, "## "+SectionSummary)
		out = append(out, strings.Split(s, "\n")...)
	}
	if d := Section(r.Content, SectionDeviations); d != "" {
		out = append(out, "## "+SectionDeviations)
		out = append(out, strings.Split(d, "\n")...)
	}
	out = append(out, fmt.Sprintf("(truncated — %d lines total; see %s for the full report)", len(lines), r.Path))
	return out
}

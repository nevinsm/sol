package docgen

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// ValidationResult holds the outcome of comparing generated docs against
// an existing file.
type ValidationResult struct {
	// Match is true when the generated output matches the existing file exactly.
	Match bool
	// Diff describes the differences found, if any.
	Diff string
}

// Validate compares the generated CLI reference against existing content.
// It returns a ValidationResult indicating whether they match and, if not,
// a human-readable description of the differences.
func Validate(root *cobra.Command, existing string) ValidationResult {
	generated := Generate(root)

	if generated == existing {
		return ValidationResult{Match: true}
	}

	diff := buildDiff(generated, existing)
	return ValidationResult{Match: false, Diff: diff}
}

// buildDiff produces a human-readable report of differences between
// the generated and existing content.
func buildDiff(generated, existing string) string {
	var b strings.Builder

	genLines := strings.Split(generated, "\n")
	existLines := strings.Split(existing, "\n")

	// Extract command references from both.
	genCmds := extractCommands(genLines)
	existCmds := extractCommands(existLines)

	// Find missing commands (in generated but not in existing).
	var missing []string
	for cmd := range genCmds {
		if _, ok := existCmds[cmd]; !ok {
			missing = append(missing, cmd)
		}
	}
	sortStrings(missing)

	// Find extra commands (in existing but not in generated).
	var extra []string
	for cmd := range existCmds {
		if _, ok := genCmds[cmd]; !ok {
			extra = append(extra, cmd)
		}
	}
	sortStrings(extra)

	if len(missing) > 0 {
		b.WriteString("Missing commands (present in command tree but not in docs):\n")
		for _, cmd := range missing {
			b.WriteString(fmt.Sprintf("  + %s\n", cmd))
		}
		b.WriteString("\n")
	}

	if len(extra) > 0 {
		b.WriteString("Extra commands (present in docs but not in command tree):\n")
		for _, cmd := range extra {
			b.WriteString(fmt.Sprintf("  - %s\n", cmd))
		}
		b.WriteString("\n")
	}

	// Line-level diff summary.
	genLen := len(genLines)
	existLen := len(existLines)
	if genLen != existLen {
		b.WriteString(fmt.Sprintf("Line count: generated=%d, existing=%d\n", genLen, existLen))
	}

	// Find first divergence point.
	minLen := genLen
	if existLen < minLen {
		minLen = existLen
	}
	for i := 0; i < minLen; i++ {
		if genLines[i] != existLines[i] {
			b.WriteString(fmt.Sprintf("\nFirst difference at line %d:\n", i+1))
			b.WriteString(fmt.Sprintf("  generated: %s\n", truncate(genLines[i], 120)))
			b.WriteString(fmt.Sprintf("  existing:  %s\n", truncate(existLines[i], 120)))
			break
		}
	}

	if b.Len() == 0 {
		b.WriteString("Content differs (whitespace or formatting changes)\n")
	}

	b.WriteString("\nRun `sol docs generate` to update docs/cli.md\n")
	return b.String()
}

// extractCommands finds command references in backtick-quoted form from
// markdown heading lines (### `cmd subcmd`). Returns a set of command strings.
func extractCommands(lines []string) map[string]bool {
	cmds := make(map[string]bool)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Match heading patterns like ### `sol foo` or ### `testcli bar`
		if strings.HasPrefix(trimmed, "#") {
			cmd := extractHeadingCmd(trimmed)
			if cmd != "" {
				cmds[cmd] = true
			}
		}

		// Match table row patterns like | `sol foo bar` | desc |
		if strings.HasPrefix(trimmed, "|") {
			cmd := extractTableCmd(trimmed)
			if cmd != "" {
				cmds[cmd] = true
			}
		}
	}
	return cmds
}

// extractHeadingCmd extracts a backtick-quoted command from a heading line.
// Example: "### `sol agent list`" → "sol agent list"
func extractHeadingCmd(line string) string {
	idx := strings.Index(line, "`")
	if idx < 0 {
		return ""
	}
	rest := line[idx+1:]
	end := strings.Index(rest, "`")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// extractTableCmd extracts a backtick-quoted command from a table row.
// Example: "| `sol cost --json` | desc |" → "sol cost"
// Only extracts the command path (words before flags starting with --).
func extractTableCmd(line string) string {
	idx := strings.Index(line, "`")
	if idx < 0 {
		return ""
	}
	rest := line[idx+1:]
	end := strings.Index(rest, "`")
	if end < 0 {
		return ""
	}
	full := rest[:end]
	// Strip flags and arguments to get just the command path.
	parts := strings.Fields(full)
	var cmdParts []string
	for _, p := range parts {
		if strings.HasPrefix(p, "-") || strings.HasPrefix(p, "<") || strings.HasPrefix(p, "[") {
			break
		}
		cmdParts = append(cmdParts, p)
	}
	if len(cmdParts) == 0 {
		return ""
	}
	return strings.Join(cmdParts, " ")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

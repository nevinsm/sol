// Package docgen generates and validates CLI reference documentation from
// the Cobra command tree. It produces deterministic markdown output grouped
// by command category and supports validation against an existing file.
package docgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Generate produces CLI reference markdown from the given root command.
// The output is deterministic: commands are grouped by their GroupID and
// sorted alphabetically within each group. Subcommands are listed under
// their parent command's section.
func Generate(root *cobra.Command) string {
	var b strings.Builder

	b.WriteString("# CLI Reference\n\n")
	b.WriteString("Auto-generated from the Cobra command tree. Do not edit manually.\n\n")
	b.WriteString("Run `sol docs generate` to regenerate this file.\n\n")
	b.WriteString("---\n\n")

	// Collect groups in display order.
	groups := collectGroups(root)

	// Collect top-level commands grouped by GroupID.
	grouped := make(map[string][]*cobra.Command)
	for _, cmd := range root.Commands() {
		if cmd.Hidden || !cmd.IsAvailableCommand() {
			continue
		}
		gid := cmd.GroupID
		if gid == "" {
			gid = "_ungrouped"
		}
		grouped[gid] = append(grouped[gid], cmd)
	}

	// Sort commands within each group alphabetically.
	for _, cmds := range grouped {
		sort.Slice(cmds, func(i, j int) bool {
			return cmds[i].Name() < cmds[j].Name()
		})
	}

	for i, g := range groups {
		cmds, ok := grouped[g.ID]
		if !ok || len(cmds) == 0 {
			continue
		}

		if i > 0 {
			b.WriteString("---\n\n")
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", g.Title))

		for _, cmd := range cmds {
			writeCommand(&b, cmd, 3)
		}
	}

	// Handle any ungrouped commands.
	if cmds, ok := grouped["_ungrouped"]; ok && len(cmds) > 0 {
		b.WriteString("---\n\n")
		b.WriteString("## Other Commands\n\n")
		for _, cmd := range cmds {
			writeCommand(&b, cmd, 3)
		}
	}

	return b.String()
}

// collectGroups returns the groups registered on the root command in their
// original order.
func collectGroups(root *cobra.Command) []*cobra.Group {
	return root.Groups()
}

// writeCommand writes a single command and its subcommands to the builder.
func writeCommand(b *strings.Builder, cmd *cobra.Command, headingLevel int) {
	heading := strings.Repeat("#", headingLevel)
	fullPath := cmd.CommandPath()

	b.WriteString(fmt.Sprintf("%s `%s`\n\n", heading, fullPath))

	// Short description.
	if cmd.Short != "" {
		b.WriteString(cmd.Short + "\n\n")
	}

	// Long description if different from short.
	if cmd.Long != "" && cmd.Long != cmd.Short {
		b.WriteString(cmd.Long + "\n\n")
	}

	// Aliases.
	if len(cmd.Aliases) > 0 {
		aliases := make([]string, len(cmd.Aliases))
		for i, a := range cmd.Aliases {
			aliases[i] = fmt.Sprintf("`%s`", a)
		}
		b.WriteString(fmt.Sprintf("**Aliases:** %s\n\n", strings.Join(aliases, ", ")))
	}

	// Usage.
	if cmd.Use != "" && strings.Contains(cmd.Use, " ") {
		// Extract the args portion from Use (everything after the command name).
		parts := strings.SplitN(cmd.Use, " ", 2)
		if len(parts) > 1 {
			b.WriteString(fmt.Sprintf("**Usage:** `%s %s`\n\n", fullPath, parts[1]))
		}
	}

	// Flags table.
	flags := collectFlags(cmd)
	if len(flags) > 0 {
		b.WriteString("| Flag | Type | Default | Description |\n")
		b.WriteString("|------|------|---------|-------------|\n")
		for _, f := range flags {
			b.WriteString(fmt.Sprintf("| `--%s` | %s | %s | %s |\n",
				f.name, f.typ, f.defVal, escapeMarkdown(f.usage)))
		}
		b.WriteString("\n")
	}

	// Subcommands.
	subs := collectSubcommands(cmd)
	if len(subs) > 0 {
		b.WriteString("**Subcommands:**\n\n")
		b.WriteString("| Command | Description |\n")
		b.WriteString("|---------|-------------|\n")
		for _, sub := range subs {
			b.WriteString(fmt.Sprintf("| `%s` | %s |\n", sub.CommandPath(), sub.Short))
		}
		b.WriteString("\n")

		// Write details for each subcommand.
		nextLevel := headingLevel + 1
		if nextLevel > 6 {
			nextLevel = 6
		}
		for _, sub := range subs {
			writeSubcommandDetail(b, sub, nextLevel)
		}
	}
}

// writeSubcommandDetail writes details for a subcommand (flags, usage).
func writeSubcommandDetail(b *strings.Builder, cmd *cobra.Command, headingLevel int) {
	heading := strings.Repeat("#", headingLevel)
	fullPath := cmd.CommandPath()

	// Only write details if the subcommand has flags, aliases, or its own subcommands.
	flags := collectFlags(cmd)
	subs := collectSubcommands(cmd)
	hasDetail := len(flags) > 0 || len(cmd.Aliases) > 0 || len(subs) > 0

	if !hasDetail {
		return
	}

	b.WriteString(fmt.Sprintf("%s `%s`\n\n", heading, fullPath))

	if cmd.Long != "" && cmd.Long != cmd.Short {
		b.WriteString(cmd.Long + "\n\n")
	}

	if len(cmd.Aliases) > 0 {
		aliases := make([]string, len(cmd.Aliases))
		for i, a := range cmd.Aliases {
			aliases[i] = fmt.Sprintf("`%s`", a)
		}
		b.WriteString(fmt.Sprintf("**Aliases:** %s\n\n", strings.Join(aliases, ", ")))
	}

	if cmd.Use != "" && strings.Contains(cmd.Use, " ") {
		parts := strings.SplitN(cmd.Use, " ", 2)
		if len(parts) > 1 {
			b.WriteString(fmt.Sprintf("**Usage:** `%s %s`\n\n", fullPath, parts[1]))
		}
	}

	if len(flags) > 0 {
		b.WriteString("| Flag | Type | Default | Description |\n")
		b.WriteString("|------|------|---------|-------------|\n")
		for _, f := range flags {
			b.WriteString(fmt.Sprintf("| `--%s` | %s | %s | %s |\n",
				f.name, f.typ, f.defVal, escapeMarkdown(f.usage)))
		}
		b.WriteString("\n")
	}

	if len(subs) > 0 {
		b.WriteString("**Subcommands:**\n\n")
		b.WriteString("| Command | Description |\n")
		b.WriteString("|---------|-------------|\n")
		for _, sub := range subs {
			b.WriteString(fmt.Sprintf("| `%s` | %s |\n", sub.CommandPath(), sub.Short))
		}
		b.WriteString("\n")

		nextLevel := headingLevel + 1
		if nextLevel > 6 {
			nextLevel = 6
		}
		for _, sub := range subs {
			writeSubcommandDetail(b, sub, nextLevel)
		}
	}
}

// flagInfo holds extracted flag metadata for rendering.
type flagInfo struct {
	name   string
	typ    string
	defVal string
	usage  string
}

// collectFlags returns non-hidden local flags for a command, sorted by name.
// The --help flag is excluded since Cobra adds it automatically and its
// presence depends on command initialization state (non-deterministic).
func collectFlags(cmd *cobra.Command) []flagInfo {
	var flags []flagInfo
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		defVal := f.DefValue
		if defVal == "" {
			defVal = `""`
		}
		flags = append(flags, flagInfo{
			name:   f.Name,
			typ:    f.Value.Type(),
			defVal: defVal,
			usage:  f.Usage,
		})
	})
	sort.Slice(flags, func(i, j int) bool {
		return flags[i].name < flags[j].name
	})
	return flags
}

// collectSubcommands returns visible subcommands sorted alphabetically.
func collectSubcommands(cmd *cobra.Command) []*cobra.Command {
	var subs []*cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Hidden || !sub.IsAvailableCommand() {
			continue
		}
		subs = append(subs, sub)
	}
	sort.Slice(subs, func(i, j int) bool {
		return subs[i].Name() < subs[j].Name()
	})
	return subs
}

// escapeMarkdown escapes pipe characters in markdown table cells.
func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

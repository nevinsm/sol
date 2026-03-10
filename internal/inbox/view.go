package inbox

import (
	"fmt"
	"strings"
)

// renderHeader returns the header line: "Inbox — N items".
func renderHeader(count int) string {
	label := "items"
	if count == 1 {
		label = "item"
	}
	return headerStyle.Render(fmt.Sprintf("Inbox — %d %s", count, label))
}

// renderFooter returns the action hint bar.
func renderFooter() string {
	return dimStyle.Render("[a]ck  [r]esolve  [d]ismiss  [enter] detail  [q]uit")
}

// renderDetailFooter returns the footer for detail view.
func renderDetailFooter() string {
	return dimStyle.Render("[esc] back  [a]ck  [r]esolve  [d]ismiss  [q]uit")
}

// renderListView renders the item list with cursor and scrolling.
func renderListView(items []InboxItem, cursor int, scrollOffset int, width int, height int, highlights map[string]int) string {
	var b strings.Builder

	// Header.
	b.WriteString(renderHeader(len(items)))
	b.WriteString("\n\n")

	if len(items) == 0 {
		b.WriteString(dimStyle.Render("  No items need attention."))
		b.WriteString("\n")
		b.WriteString("\n")
		b.WriteString(renderFooter())
		return b.String()
	}

	// Column header.
	priCol := 4
	typeCol := 13
	sourceCol := 12
	ageCol := 6
	// Description takes the remaining width.
	descCol := width - 2 - priCol - typeCol - sourceCol - ageCol - 2 // 2 for cursor + spaces
	if descCol < 10 {
		descCol = 10
	}

	hdr := fmt.Sprintf("  %s%s%s%s%s",
		padRight("PRI", priCol),
		padRight("TYPE", typeCol),
		padRight("SOURCE", sourceCol),
		padRight("DESCRIPTION", descCol),
		padRight("AGE", ageCol),
	)
	b.WriteString(dimStyle.Render(hdr))
	b.WriteString("\n")

	// Available lines for items (header=2 lines, col header=1 line, footer=2 lines).
	viewportHeight := height - 5
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	// Adjust scroll offset to keep cursor visible.
	if cursor < scrollOffset {
		scrollOffset = cursor
	}
	if cursor >= scrollOffset+viewportHeight {
		scrollOffset = cursor - viewportHeight + 1
	}

	// Render visible items.
	end := scrollOffset + viewportHeight
	if end > len(items) {
		end = len(items)
	}

	for i := scrollOffset; i < end; i++ {
		item := items[i]
		selected := i == cursor

		// Cursor indicator.
		prefix := "  "
		if selected {
			prefix = focusIndicator + " "
		}

		// Priority with color.
		priStr := fmt.Sprintf("%d", item.Priority)
		switch item.Priority {
		case 1:
			priStr = errorStyle.Render(priStr)
		case 2:
			priStr = warnStyle.Render(priStr)
		default:
			priStr = dimStyle.Render(priStr)
		}

		typeStr := item.TypeString()
		sourceStr := truncateStr(item.Source, sourceCol-1)
		descStr := truncateStr(item.Description, descCol-1)
		ageStr := item.Age()

		row := fmt.Sprintf("%s%s%s%s%s%s",
			prefix,
			padRight(priStr, priCol),
			padRight(typeStr, typeCol),
			padRight(sourceStr, sourceCol),
			padRight(descStr, descCol),
			ageStr,
		)

		// Apply highlight or selection style.
		if level, ok := highlights[item.ID]; ok && level > 0 {
			row = highlightAtLevel(level).Render(row)
		} else if selected {
			row = selectStyle.Render(row)
		}

		b.WriteString(row)
		b.WriteString("\n")
	}

	// Pad remaining viewport lines.
	rendered := end - scrollOffset
	for i := rendered; i < viewportHeight; i++ {
		b.WriteString("\n")
	}

	// Scroll indicator.
	if len(items) > viewportHeight {
		indicator := dimStyle.Render(fmt.Sprintf("  [%d-%d of %d]", scrollOffset+1, end, len(items)))
		b.WriteString(indicator)
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}

	b.WriteString(renderFooter())

	return b.String()
}

// renderDetailView renders the expanded detail for a selected item.
func renderDetailView(item InboxItem, width int, height int) string {
	var b strings.Builder

	b.WriteString(renderHeader(1))
	b.WriteString("\n\n")

	switch item.Type {
	case ItemEscalation:
		if esc := item.Escalation; esc != nil {
			b.WriteString(headerStyle.Render("Escalation"))
			b.WriteString("\n\n")
			b.WriteString(fmt.Sprintf("  ID:          %s\n", esc.ID))
			b.WriteString(fmt.Sprintf("  Severity:    %s\n", severityStyled(esc.Severity)))
			b.WriteString(fmt.Sprintf("  Source:      %s\n", esc.Source))
			if esc.SourceRef != "" {
				b.WriteString(fmt.Sprintf("  Source Ref:  %s\n", esc.SourceRef))
			}
			b.WriteString(fmt.Sprintf("  Status:      %s\n", esc.Status))
			b.WriteString(fmt.Sprintf("  Created:     %s\n", esc.CreatedAt.Format("2006-01-02 15:04:05 UTC")))
			b.WriteString(fmt.Sprintf("  Updated:     %s\n", esc.UpdatedAt.Format("2006-01-02 15:04:05 UTC")))
			b.WriteString("\n")
			b.WriteString("  " + headerStyle.Render("Description"))
			b.WriteString("\n")
			b.WriteString(wrapIndent(esc.Description, 4, width))
			b.WriteString("\n")
		}

	case ItemMail:
		if msg := item.Message; msg != nil {
			b.WriteString(headerStyle.Render("Message"))
			b.WriteString("\n\n")
			b.WriteString(fmt.Sprintf("  ID:        %s\n", msg.ID))
			b.WriteString(fmt.Sprintf("  Sender:    %s\n", msg.Sender))
			b.WriteString(fmt.Sprintf("  Priority:  %d\n", msg.Priority))
			b.WriteString(fmt.Sprintf("  Type:      %s\n", msg.Type))
			b.WriteString(fmt.Sprintf("  Created:   %s\n", msg.CreatedAt.Format("2006-01-02 15:04:05 UTC")))
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf("  %s  %s\n", headerStyle.Render("Subject:"), msg.Subject))
			b.WriteString("\n")
			if msg.Body != "" {
				b.WriteString("  " + headerStyle.Render("Body"))
				b.WriteString("\n")
				b.WriteString(wrapIndent(msg.Body, 4, width))
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(renderDetailFooter())

	return b.String()
}

// severityStyled renders a severity string with appropriate color.
func severityStyled(severity string) string {
	switch severity {
	case "critical":
		return errorStyle.Render(severity)
	case "high":
		return warnStyle.Render(severity)
	default:
		return severity
	}
}

// wrapIndent wraps text at the given width with an indent prefix.
func wrapIndent(text string, indent int, maxWidth int) string {
	prefix := strings.Repeat(" ", indent)
	available := maxWidth - indent
	if available < 20 {
		available = 20
	}

	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) == 0 {
			b.WriteString(prefix)
			b.WriteString("\n")
			continue
		}
		// Simple word wrap.
		words := strings.Fields(line)
		current := prefix
		for _, word := range words {
			if len(current)+len(word)+1 > available+indent && current != prefix {
				b.WriteString(current)
				b.WriteString("\n")
				current = prefix
			}
			if current == prefix {
				current += word
			} else {
				current += " " + word
			}
		}
		if current != "" {
			b.WriteString(current)
			b.WriteString("\n")
		}
	}
	return b.String()
}

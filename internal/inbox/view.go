package inbox

import (
	"fmt"
	"strings"

	"github.com/nevinsm/sol/internal/style"
)

// renderHeader returns the top banner line: "Inbox — {identity} — N items".
// identity is omitted when empty (e.g. a caller that never resolved one).
func renderHeader(identity string, count int) string {
	label := "items"
	if count == 1 {
		label = "item"
	}
	if identity == "" {
		return headerStyle.Render(fmt.Sprintf("Inbox — %d %s", count, label))
	}
	return headerStyle.Render(fmt.Sprintf("Inbox — %s — %d %s", identity, count, label))
}

// renderFooter returns the list-view action hint bar. [a]ck applies to any
// selection (escalation or mail); [r]esolve/[d]ismiss are context-sensitive
// to the selected item's type, since resolve only makes sense for
// escalations and dismiss only for mail.
func renderFooter(item InboxItem, haveSelection bool) string {
	var parts []string
	if haveSelection {
		parts = append(parts, "[a]ck")
		if item.Type == ItemEscalation {
			parts = append(parts, "[r]esolve")
		} else {
			parts = append(parts, "[d]ismiss")
		}
	}
	parts = append(parts, "[enter] detail", "[q]uit")
	return dimStyle.Render(strings.Join(parts, "  "))
}

// renderDetailFooter returns the detail-view action hint bar, context
// sensitive to the pinned item's type in the same way as renderFooter.
func renderDetailFooter(item InboxItem) string {
	parts := []string{"[esc] back", "[a]ck"}
	if item.Type == ItemEscalation {
		parts = append(parts, "[r]esolve")
	} else {
		parts = append(parts, "[d]ismiss")
	}
	parts = append(parts, "[q]uit")
	return dimStyle.Render(strings.Join(parts, "  "))
}

// listRow is one renderable line in the sectioned list view: either a
// section header (Escalations / Mail) or an item row. index is the row's
// position in the items slice the cursor addresses; it is meaningful only
// when header == "".
type listRow struct {
	header string
	item   InboxItem
	index  int
}

// buildListRows partitions items — already ordered escalations-then-mail
// by FetchItems/groupThreads — into section-header and item rows for
// display. Empty sections are omitted entirely, so an identity with no
// escalations sees only the Mail section.
func buildListRows(items []InboxItem) []listRow {
	escCount := 0
	for _, it := range items {
		if it.Type != ItemEscalation {
			break
		}
		escCount++
	}
	mailCount := len(items) - escCount

	var rows []listRow
	if escCount > 0 {
		rows = append(rows, listRow{header: sectionHeaderText("Escalations", escCount)})
		for i := 0; i < escCount; i++ {
			rows = append(rows, listRow{item: items[i], index: i})
		}
	}
	if mailCount > 0 {
		rows = append(rows, listRow{header: sectionHeaderText("Mail", mailCount)})
		for i := escCount; i < len(items); i++ {
			rows = append(rows, listRow{item: items[i], index: i})
		}
	}
	return rows
}

func sectionHeaderText(name string, count int) string {
	label := "items"
	if count == 1 {
		label = "item"
	}
	return fmt.Sprintf("%s (%d %s)", name, count, label)
}

// rowIndexForItem returns the visual row index (in the slice buildListRows
// returns) of the item at position itemIndex in the underlying items
// slice, so the model can keep that row scrolled into view. Returns 0 if
// not found (e.g. itemIndex out of range).
func rowIndexForItem(rows []listRow, itemIndex int) int {
	for i, r := range rows {
		if r.header == "" && r.index == itemIndex {
			return i
		}
	}
	return 0
}

// sourceColWidth sizes the SOURCE column to the longest visible source
// among items, floored at the header text's own width and capped at 24 so
// a single long identity can't blow out the whole layout.
func sourceColWidth(items []InboxItem) int {
	width := len("SOURCE")
	for _, it := range items {
		if l := len([]rune(it.Source)); l > width {
			width = l
		}
	}
	if width > 24 {
		width = 24
	}
	return width
}

// renderListView renders the sectioned item list with cursor and scrolling.
func renderListView(items []InboxItem, cursor int, scrollOffset int, width int, height int, highlights map[string]int, fetchErr string, actionErr string, notice string, identity string) string {
	var b strings.Builder

	b.WriteString(renderHeader(identity, len(items)))
	b.WriteString("\n\n")

	if fetchErr != "" {
		b.WriteString(errorStyle.Render("  ⚠ fetch error: " + fetchErr))
		b.WriteString("\n\n")
	}
	if actionErr != "" {
		b.WriteString(errorStyle.Render("  ⚠ " + actionErr))
		b.WriteString("\n\n")
	}
	if notice != "" {
		b.WriteString(dimStyle.Render("  " + notice))
		b.WriteString("\n\n")
	}

	if len(items) == 0 {
		if fetchErr == "" {
			b.WriteString(dimStyle.Render("  No items need attention."))
		}
		b.WriteString("\n\n")
		b.WriteString(renderFooter(InboxItem{}, false))
		return b.String()
	}

	rows := buildListRows(items)
	priCol := 4
	sourceCol := sourceColWidth(items)
	ageCol := 6
	// Description takes the remaining width.
	descCol := width - 2 - priCol - sourceCol - ageCol - 2 // 2 for cursor + spaces
	if descCol < 10 {
		descCol = 10
	}

	hdr := fmt.Sprintf("  %s%s%s%s",
		padRight("PRI", priCol),
		padRight("SOURCE", sourceCol),
		padRight("DESCRIPTION", descCol),
		padRight("AGE", ageCol),
	)
	b.WriteString(dimStyle.Render(hdr))
	b.WriteString("\n")

	// Available lines for rows (header=2 lines, col header=1 line, footer=2 lines).
	viewportHeight := height - 5
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	if scrollOffset > len(rows) {
		scrollOffset = len(rows)
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	end := scrollOffset + viewportHeight
	if end > len(rows) {
		end = len(rows)
	}

	for i := scrollOffset; i < end; i++ {
		row := rows[i]
		if row.header != "" {
			b.WriteString(sectionHeaderStyle.Render("  " + row.header))
			b.WriteString("\n")
			continue
		}

		item := row.item
		selected := row.index == cursor

		prefix := "  "
		if selected {
			prefix = focusIndicator + " "
		}

		priStr := fmt.Sprintf("%d", item.Priority)
		switch item.Priority {
		case 1:
			priStr = errorStyle.Render(priStr)
		case 2:
			priStr = warnStyle.Render(priStr)
		default:
			priStr = dimStyle.Render(priStr)
		}

		sourceStr := style.TruncateWidth(item.Source, sourceCol-1)
		descStr := item.Description
		if len(item.ThreadMessages) > 1 {
			descStr = fmt.Sprintf("%s (%d)", descStr, len(item.ThreadMessages))
		}
		descStr = style.TruncateWidth(descStr, descCol-1)
		ageStr := item.Age()

		rowStr := fmt.Sprintf("%s%s%s%s%s",
			prefix,
			padRight(priStr, priCol),
			padRight(sourceStr, sourceCol),
			padRight(descStr, descCol),
			ageStr,
		)

		// Apply highlight or selection style.
		if level, ok := highlights[item.ID]; ok && level > 0 {
			rowStr = highlightAtLevel(level).Render(rowStr)
		} else if selected {
			rowStr = selectStyle.Render(rowStr)
		}

		b.WriteString(rowStr)
		b.WriteString("\n")
	}

	// Pad remaining viewport lines.
	rendered := end - scrollOffset
	for i := rendered; i < viewportHeight; i++ {
		b.WriteString("\n")
	}

	// Scroll indicator.
	if len(rows) > viewportHeight {
		indicator := dimStyle.Render(fmt.Sprintf("  [%d-%d of %d]", scrollOffset+1, end, len(rows)))
		b.WriteString(indicator)
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}

	var footerItem InboxItem
	haveSelection := cursor >= 0 && cursor < len(items)
	if haveSelection {
		footerItem = items[cursor]
	}
	b.WriteString(renderFooter(footerItem, haveSelection))

	return b.String()
}

// detailViewportHeight is the number of content lines visible at once in
// the detail view (top header=2 lines, footer=2 lines, blank separator=1).
func detailViewportHeight(height int) int {
	v := height - 5
	if v < 1 {
		v = 1
	}
	return v
}

// detailContentLines renders item's full detail content — metadata fields
// plus body/description — as plain lines (ANSI styling included but no
// trailing blank padding), for the scrollable viewport in renderDetailView.
func detailContentLines(item InboxItem, width int) []string {
	var b strings.Builder

	switch item.Type {
	case ItemEscalation:
		if esc := item.Escalation; esc != nil {
			b.WriteString(headerStyle.Render("Escalation"))
			b.WriteString("\n\n")
			fmt.Fprintf(&b, "  ID:          %s\n", esc.ID)
			fmt.Fprintf(&b, "  Severity:    %s\n", severityStyled(esc.Severity))
			fmt.Fprintf(&b, "  Source:      %s\n", esc.Source)
			if esc.SourceRef != "" {
				fmt.Fprintf(&b, "  Source Ref:  %s\n", esc.SourceRef)
			}
			fmt.Fprintf(&b, "  Status:      %s\n", esc.Status)
			fmt.Fprintf(&b, "  Created:     %s\n", esc.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
			fmt.Fprintf(&b, "  Updated:     %s\n", esc.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
			b.WriteString("\n")
			fmt.Fprintf(&b, "  %s\n", headerStyle.Render("Description"))
			b.WriteString(wrapIndent(esc.Description, 4, width))
		}

	case ItemMail:
		if item.ThreadID != "" {
			fmt.Fprintf(&b, "%s\n\n", headerStyle.Render(fmt.Sprintf("Thread — %d pending message(s)", len(item.ThreadMessages))))
			fmt.Fprintf(&b, "  Thread ID:  %s\n", item.ThreadID)
			b.WriteString("\n")
			for i, m := range item.ThreadMessages {
				if i > 0 {
					b.WriteString("  " + strings.Repeat("-", 40) + "\n\n")
				}
				fmt.Fprintf(&b, "  Sender:    %s\n", m.Sender)
				fmt.Fprintf(&b, "  Priority:  %d\n", m.Priority)
				fmt.Fprintf(&b, "  Created:   %s\n", m.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
				b.WriteString("\n")
				fmt.Fprintf(&b, "  %s  %s\n", headerStyle.Render("Subject:"), m.Subject)
				if m.Body != "" {
					b.WriteString("\n")
					b.WriteString(wrapIndent(m.Body, 4, width))
				}
			}
		} else if msg := item.Message; msg != nil {
			b.WriteString(headerStyle.Render("Message"))
			b.WriteString("\n\n")
			fmt.Fprintf(&b, "  ID:        %s\n", msg.ID)
			fmt.Fprintf(&b, "  Sender:    %s\n", msg.Sender)
			fmt.Fprintf(&b, "  Priority:  %d\n", msg.Priority)
			fmt.Fprintf(&b, "  Type:      %s\n", msg.Type)
			fmt.Fprintf(&b, "  Created:   %s\n", msg.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
			b.WriteString("\n")
			fmt.Fprintf(&b, "  %s  %s\n", headerStyle.Render("Subject:"), msg.Subject)
			if msg.Body != "" {
				b.WriteString("\n")
				b.WriteString(headerStyle.Render("  Body"))
				b.WriteString("\n")
				b.WriteString(wrapIndent(msg.Body, 4, width))
			}
		}
	}

	content := strings.TrimRight(b.String(), "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

// renderDetailView renders the pinned item's detail, scrolled to
// scrollOffset lines, with a clipped-content indicator when the content
// overflows the viewport.
func renderDetailView(item InboxItem, width int, height int, actionErr string, scrollOffset int, identity string) string {
	var b strings.Builder

	b.WriteString(renderHeader(identity, 1))
	b.WriteString("\n\n")

	if actionErr != "" {
		b.WriteString(errorStyle.Render("  ⚠ " + actionErr))
		b.WriteString("\n\n")
	}

	lines := detailContentLines(item, width)
	viewportHeight := detailViewportHeight(height)

	if scrollOffset > len(lines) {
		scrollOffset = len(lines)
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	end := scrollOffset + viewportHeight
	if end > len(lines) {
		end = len(lines)
	}

	for i := scrollOffset; i < end; i++ {
		b.WriteString(lines[i])
		b.WriteString("\n")
	}

	if len(lines) > viewportHeight {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  [%d-%d of %d lines, clipped — ↑/↓ pgup/pgdn to scroll]", scrollOffset+1, end, len(lines))))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(renderDetailFooter(item))

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

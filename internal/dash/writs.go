package dash

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/dispatch"
	"github.com/nevinsm/sol/internal/events"
	"github.com/nevinsm/sol/internal/status"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/style"
)

// --- Writs backlog section (world view) ---

// renderWritsSection renders the Writs section in summary or expanded mode —
// same collapse/expand pattern as renderMergeQueueSection. Callers only
// invoke this when len(data.Writs) > 0 (the section is hidden entirely when
// the backlog is empty, same as Caravans).
func (wm worldModel) renderWritsSection(b *strings.Builder, data *status.WorldStatus) {
	isFocused := wm.hasFocus && wm.focusedSection == sectionWrits
	sectionHeader := fmt.Sprintf("Writs (%d open)", len(data.Writs))

	if !isFocused {
		b.WriteString("  " + headerStyle.Render(sectionHeader))
		b.WriteString("\n")
		return
	}

	vpHeight := wm.sectionViewportHeight(sectionWrits)
	scrollInfo := scrollIndicator(wm.writsScroll, vpHeight, len(data.Writs))
	header := "  " + focusIndicator + " " + focusStyle.Render(sectionHeader)
	if scrollInfo != "" {
		header += "  " + dimStyle.Render(scrollInfo)
	}
	b.WriteString(header + "\n")
	wm.renderWritsTable(b, data.Writs)
	b.WriteString("\n")
}

// writIDWidth, writPriorityWidth, writKindWidth, and writAgeWidth are the
// fixed column widths for the Writs table; TITLE takes the remaining width.
const (
	writIDWidth       = 20
	writPriorityWidth = 8
	writKindWidth     = 10
	writAgeWidth      = 6
)

// writTitleWidth computes the TITLE column width at the current terminal
// width, leaving room for the fixed ID/PRIORITY/KIND/AGE columns.
func (wm worldModel) writTitleWidth() int {
	// 2 (indent) + id + 1 + priority + 1 + kind + 1 + age + 1 (sep before title).
	fixed := 2 + writIDWidth + 1 + writPriorityWidth + 1 + writKindWidth + 1 + writAgeWidth + 1
	maxTitle := wm.width - fixed
	if maxTitle < 15 {
		maxTitle = 15
	}
	return maxTitle
}

func (wm worldModel) renderWritsTable(b *strings.Builder, writs []status.WritSummary) {
	titleWidth := wm.writTitleWidth()
	b.WriteString("  " +
		padRight(dimStyle.Render("ID"), writIDWidth) + " " +
		padRight(dimStyle.Render("PRIORITY"), writPriorityWidth) + " " +
		padRight(dimStyle.Render("KIND"), writKindWidth) + " " +
		padRight(dimStyle.Render("AGE"), writAgeWidth) + " " +
		dimStyle.Render("TITLE") + "\n")

	vpHeight := wm.sectionViewportHeight(sectionWrits)
	start := wm.writsScroll
	end := start + vpHeight
	if end > len(writs) {
		end = len(writs)
	}
	if start > len(writs) {
		start = len(writs)
	}

	for i := start; i < end; i++ {
		line := wm.renderWritRow(writs[i], titleWidth)
		if i == wm.writsCursor {
			b.WriteString(selectStyle.Render(padRight(line, wm.width)))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (wm worldModel) renderWritRow(w status.WritSummary, titleWidth int) string {
	age := status.FormatDuration(time.Since(w.CreatedAt))
	title := style.TruncateWidth(w.Title, titleWidth)
	return "  " +
		padRight(w.ID, writIDWidth) + " " +
		padRight(fmt.Sprintf("%d", w.Priority), writPriorityWidth) + " " +
		padRight(w.Kind, writKindWidth) + " " +
		padRight(age, writAgeWidth) + " " +
		title
}

// handleWritPeek builds writ peek items (description detail panel) and
// emits a peekMsg. No-op unless the Writs section has rows.
func (wm worldModel) handleWritPeek(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if len(data.Writs) == 0 {
		return wm, nil
	}
	items := buildWritPeekItems(data.Writs)
	if len(items) == 0 {
		return wm, nil
	}
	msg := peekMsg{
		items:         items,
		initialCursor: wm.writsCursor,
		fromView:      viewWorld,
		world:         data.World,
	}
	return wm, func() tea.Msg { return msg }
}

// buildWritPeekItems creates peek items for the open-writ backlog. Each
// item carries its writ description so the peek right panel can render it
// as a scrollable text detail — see peekModel.renderWritDetail.
func buildWritPeekItems(writs []status.WritSummary) []peekItem {
	var items []peekItem
	for _, w := range writs {
		items = append(items, peekItem{
			name:        w.Title,
			category:    "Writs",
			state:       fmt.Sprintf("p%d", w.Priority),
			alive:       false,
			peekable:    false,
			isWrit:      true,
			writID:      w.ID,
			description: w.Description,
		})
	}
	return items
}

// --- Cast action (world view, Writs section) ---

// requestCastMsg is emitted by the world view when 'c' is pressed on a
// focused open-writ row, requesting a cast confirmation.
type requestCastMsg struct {
	world         string
	writID        string
	confirmTitle  string
	confirmDetail string
}

// castDoneMsg carries the result of dispatching a writ via castCmd.
type castDoneMsg struct {
	writID    string
	agentName string
	err       error
}

// handleCast builds a cast confirmation request for the currently focused
// open-writ row. No-op unless the Writs section is focused.
func (wm worldModel) handleCast(data *status.WorldStatus) (worldModel, tea.Cmd) {
	if data == nil || wm.focusedSection != sectionWrits {
		return wm, nil
	}
	if wm.writsCursor >= len(data.Writs) {
		return wm, nil
	}
	w := data.Writs[wm.writsCursor]
	msg := requestCastMsg{
		world:        data.World,
		writID:       w.ID,
		confirmTitle: fmt.Sprintf("Cast %s?", w.ID),
		confirmDetail: fmt.Sprintf(
			"%s — dispatches to an auto-selected idle outpost agent (creates one if none idle). "+
				"Writ dependencies are advisory only: sol cast does not enforce them.",
			w.Title),
	}
	return wm, func() tea.Msg { return msg }
}

// castCmd dispatches writID in world to an auto-selected idle outpost agent
// via the same dispatch.Cast entry point `sol cast` uses (internal/dispatch)
// — no selection/lock logic is reimplemented here. mgr is the dashboard's
// shared session manager (Config.SessionMgr).
func castCmd(world, writID string, mgr dispatch.SessionManager) tea.Cmd {
	return func() tea.Msg {
		worldCfg, err := config.LoadWorldConfig(world)
		if err != nil {
			return castDoneMsg{writID: writID, err: fmt.Errorf("load world config: %w", err)}
		}
		if worldCfg.World.Sleeping {
			return castDoneMsg{writID: writID, err: fmt.Errorf("world %q is sleeping", world)}
		}

		sourceRepo, err := dispatch.ResolveSourceRepo(world, worldCfg)
		if err != nil {
			return castDoneMsg{writID: writID, err: fmt.Errorf("resolve source repo: %w", err)}
		}

		worldStore, err := store.OpenWorld(world)
		if err != nil {
			return castDoneMsg{writID: writID, err: fmt.Errorf("open world store: %w", err)}
		}
		defer worldStore.Close()

		sphereStore, err := store.OpenSphere()
		if err != nil {
			return castDoneMsg{writID: writID, err: fmt.Errorf("open sphere store: %w", err)}
		}
		defer sphereStore.Close()

		logger := events.NewLogger(config.Home())

		result, err := dispatch.Cast(context.Background(), dispatch.CastOpts{
			WritID:      writID,
			World:       world,
			SourceRepo:  sourceRepo,
			WorldConfig: &worldCfg,
		}, worldStore, sphereStore, mgr, logger)
		if err != nil {
			return castDoneMsg{writID: writID, err: fmt.Errorf("cast writ: %w", err)}
		}

		return castDoneMsg{writID: writID, agentName: result.AgentName}
	}
}

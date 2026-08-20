// Package statusformat is the canonical home for sol's process-detail
// formatters. Both `sol status` (internal/status/render.go) and `sol dash`
// (internal/dash) consume these so the two views always show the same field
// set in the same order.
//
// Why a separate package? Historically the canonical formatters lived in
// internal/status/render.go and dash maintained its own parallel copies that
// drifted (missing (stale) styling, missing EventsProcessed/PatrolCount/
// MergesTotal). The bug class kept reappearing.
//
// Why DTO types instead of importing internal/status?
// Because internal/status/render.go is the primary caller of these
// formatters, and that file lives in package status. If statusformat
// imported status (for ChronicleInfo, ForgeInfo, etc.) then status could
// not import statusformat back — Go forbids import cycles. The DTO types
// here have underlying types identical to their counterparts in
// internal/status, so callers may convert via plain Go struct conversion:
//
//	statusformat.FormatChronicleDetail(statusformat.ChronicleDetail(c))
//
// Field order and types must be kept in sync with internal/status/status.go.
// If status adds a field, the corresponding DTO here must add it too — the
// compiler will surface the mismatch at every conversion site.
package statusformat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nevinsm/sol/internal/broker"
	"github.com/nevinsm/sol/internal/style"
)

// PrefectDetail mirrors status.PrefectInfo for formatter input.
type PrefectDetail struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// ConsulDetail mirrors status.ConsulInfo for formatter input.
type ConsulDetail struct {
	Running      bool   `json:"running"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	Stale        bool   `json:"stale"`
}

// ChronicleDetail mirrors status.ChronicleInfo for formatter input.
type ChronicleDetail struct {
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	EventsProcessed int64  `json:"events_processed,omitempty"`
	HeartbeatAge    string `json:"heartbeat_age,omitempty"`
	Stale           bool   `json:"stale,omitempty"`
}

// LedgerDetail mirrors status.LedgerInfo for formatter input.
type LedgerDetail struct {
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Port         int    `json:"port,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
	// DroppedRecords and DroppedByService mirror
	// ledger.Heartbeat.DroppedRecords/DroppedByService: records received for
	// a service.name with no registered extractor.
	DroppedRecords   int64            `json:"dropped_records,omitempty"`
	DroppedByService map[string]int64 `json:"dropped_by_service,omitempty"`
}

// BrokerDetail mirrors status.BrokerInfo for formatter input.
type BrokerDetail struct {
	Running      bool                     `json:"running"`
	HeartbeatAge string                   `json:"heartbeat_age,omitempty"`
	PatrolCount  int                      `json:"patrol_count,omitempty"`
	Stale        bool                     `json:"stale"`
	Runtimes     []broker.RuntimeLiveness `json:"runtimes,omitempty"`
}

// ForgeRemoteFailureThreshold is the number of consecutive remote-git
// (fetch/ls-remote against origin) failures at which the forge line and the
// world health rollup render as degraded. Below this, transient network
// blips don't need operator attention; at or above it, the failure is
// persistent and GLASS requires it be visible in `sol status`, not just the
// forge log.
const ForgeRemoteFailureThreshold = 3

// ForgeDetail mirrors status.ForgeInfo for formatter input.
type ForgeDetail struct {
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Merging      bool   `json:"merging,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	QueueDepth   int    `json:"queue_depth,omitempty"`
	MergesTotal  int    `json:"merges_total,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
	Paused       bool   `json:"paused,omitempty"`

	Status      string `json:"status,omitempty"`
	LastMerge   string `json:"last_merge,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	CurrentMR   string `json:"current_mr,omitempty"`
	CurrentWrit string `json:"current_writ,omitempty"`

	// ConsecutiveRemoteFailures and LastRemoteError mirror the forge
	// heartbeat's remote-git failure tracking (see internal/forge.Heartbeat).
	ConsecutiveRemoteFailures int    `json:"consecutive_remote_failures,omitempty"`
	LastRemoteError           string `json:"last_remote_error,omitempty"`
}

// SentinelDetail mirrors status.SentinelInfo for formatter input.
type SentinelDetail struct {
	Running       bool   `json:"running"`
	PID           int    `json:"pid,omitempty"`
	PatrolCount   int    `json:"patrol_count,omitempty"`
	AgentsChecked int    `json:"agents_checked,omitempty"`
	StalledCount  int    `json:"stalled_count,omitempty"`
	ReapedCount   int    `json:"reaped_count,omitempty"`
	HeartbeatAge  string `json:"heartbeat_age,omitempty"`
	Status        string `json:"status,omitempty"`
	Stale         bool   `json:"stale,omitempty"`
}

// FormatPrefectDetail renders a one-line detail for the prefect process.
func FormatPrefectDetail(p PrefectDetail) string {
	if p.Running {
		return fmt.Sprintf("pid %d", p.PID)
	}
	return ""
}

// FormatConsulDetail renders a one-line detail for the consul process.
func FormatConsulDetail(c ConsulDetail) string {
	if !c.Running {
		return ""
	}
	parts := fmt.Sprintf("%d patrols", c.PatrolCount)
	if c.HeartbeatAge != "" {
		parts += fmt.Sprintf(", last %s ago", c.HeartbeatAge)
	}
	if c.Stale {
		parts += style.Warn.Render(" (stale)")
	}
	return parts
}

// FormatChronicleDetail renders a one-line detail for the chronicle process.
func FormatChronicleDetail(c ChronicleDetail) string {
	if !c.Running {
		return ""
	}
	var parts string
	if c.PID > 0 {
		parts = fmt.Sprintf("pid %d", c.PID)
	}
	if c.HeartbeatAge != "" {
		if parts != "" {
			parts += " "
		}
		parts += style.Dim.Render(fmt.Sprintf("hb %s", c.HeartbeatAge))
	}
	if c.EventsProcessed > 0 {
		if parts != "" {
			parts += " "
		}
		parts += style.Dim.Render(fmt.Sprintf("ev %d", c.EventsProcessed))
	}
	if c.Stale {
		parts += style.Warn.Render(" (stale)")
	}
	return parts
}

// FormatLedgerDetail renders a one-line detail for the ledger process.
func FormatLedgerDetail(l LedgerDetail) string {
	if !l.Running {
		return ""
	}
	detail := ""
	if l.PID > 0 {
		detail = fmt.Sprintf("pid %d", l.PID)
	}
	if l.HeartbeatAge != "" {
		if detail != "" {
			detail += "  "
		}
		detail += fmt.Sprintf("hb %s", l.HeartbeatAge)
	}
	if l.Stale {
		detail += style.Warn.Render(" (stale)")
	}
	if l.DroppedRecords > 0 {
		if detail != "" {
			detail += "  "
		}
		detail += style.Warn.Render(formatLedgerDropWarning(l))
	}
	if detail == "" {
		return "running"
	}
	return detail
}

// formatLedgerDropWarning renders the "dropped N records (unknown service:
// X)" warning fragment for records the ledger received but could not route
// because their service.name has no registered extractor. Without this,
// the drop is invisible: the heartbeat still says "running" and the request
// counter only increments on successfully routed records, so an unroutable
// stream looks identical to no traffic at all (see sol-3e88d749a6b88dd5,
// where exactly that hid a two-month outage). Service names are sorted for
// deterministic rendering.
func formatLedgerDropWarning(l LedgerDetail) string {
	names := make([]string, 0, len(l.DroppedByService))
	for name := range l.DroppedByService {
		names = append(names, name)
	}
	sort.Strings(names)

	label := "unknown service"
	if len(names) > 1 {
		label = "unknown services"
	}
	suffix := ""
	if len(names) > 0 {
		suffix = fmt.Sprintf(" (%s: %s)", label, strings.Join(names, ", "))
	}

	plural := "records"
	if l.DroppedRecords == 1 {
		plural = "record"
	}
	return fmt.Sprintf("dropped %d %s%s", l.DroppedRecords, plural, suffix)
}

// FormatBrokerDetail renders a one-line detail for the broker process.
func FormatBrokerDetail(b BrokerDetail) string {
	if !b.Running {
		return ""
	}
	parts := fmt.Sprintf("%d patrols", b.PatrolCount)
	if b.HeartbeatAge != "" {
		parts += fmt.Sprintf(", last %s ago", b.HeartbeatAge)
	}
	if b.Stale {
		parts += style.Warn.Render(" (stale)")
	}
	// Show inline liveness summary: "claude: ok, codex: unreachable".
	for _, r := range b.Runtimes {
		if !r.OK {
			parts += style.Error.Render(fmt.Sprintf(" [%s: unreachable]", r.Runtime))
		}
	}
	return parts
}

// FormatForgeDetail renders a one-line detail for the forge process.
func FormatForgeDetail(f ForgeDetail) string {
	if !f.Running {
		return ""
	}
	if f.Paused {
		return style.Warn.Render("paused") + fmt.Sprintf(" (pid %d)", f.PID)
	}
	// Treat either the explicit Merging flag or a heartbeat status of
	// "merging" as the active-merge signal. status.Gather sets Merging from
	// the merge tmux session, but accepting Status="merging" too keeps the
	// formatter robust against callers that populate one field but not both.
	merging := f.Merging || f.Status == "merging"
	if f.PatrolCount > 0 || f.MergesTotal > 0 {
		parts := fmt.Sprintf("pid %d, %d patrols, %d merged", f.PID, f.PatrolCount, f.MergesTotal)
		if f.HeartbeatAge != "" {
			parts += fmt.Sprintf(", last %s ago", f.HeartbeatAge)
		}
		if f.QueueDepth > 0 {
			parts += fmt.Sprintf(", %d queued", f.QueueDepth)
		}
		if f.Stale {
			parts += style.Warn.Render(" (stale)")
		}
		if merging {
			parts += style.OK.Render(" [merging]")
		}
		parts += formatForgeRemoteDegraded(f)
		return parts
	}
	if f.PID > 0 {
		detail := fmt.Sprintf("pid %d", f.PID)
		if merging {
			detail += style.OK.Render(" [merging]")
		}
		detail += formatForgeRemoteDegraded(f)
		return detail
	}
	return ""
}

// formatForgeRemoteDegraded renders the degraded marker for persistent
// remote-git failures (Task B: sol-0ec6b898c083264f), or "" when the
// consecutive-failure count is below ForgeRemoteFailureThreshold.
func formatForgeRemoteDegraded(f ForgeDetail) string {
	if f.ConsecutiveRemoteFailures < ForgeRemoteFailureThreshold {
		return ""
	}
	reason := fmt.Sprintf(" [degraded: %d consecutive remote-git failures", f.ConsecutiveRemoteFailures)
	if f.LastRemoteError != "" {
		reason += ": " + f.LastRemoteError
	}
	reason += "]"
	return style.Error.Render(reason)
}

// FormatCompactTokens formats a token count as a compact human-readable string.
// < 1,000: show as-is (e.g., "842")
// 1,000–999,999: "1.2K", "340K"
// 1,000,000+: "1.2M", "14.3M"
func FormatCompactTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		v := float64(n) / 1000
		if v < 9.95 {
			return fmt.Sprintf("%.1fK", v)
		}
		return fmt.Sprintf("%.0fK", v)
	}
	v := float64(n) / 1_000_000
	if v < 9.95 {
		return fmt.Sprintf("%.1fM", v)
	}
	return fmt.Sprintf("%.0fM", v)
}

// FormatCost formats a USD cost value for display.
func FormatCost(cost float64) string {
	if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// FormatSentinelDetail renders a one-line detail for the sentinel process.
func FormatSentinelDetail(s SentinelDetail) string {
	if !s.Running {
		return ""
	}
	if s.PatrolCount > 0 {
		parts := fmt.Sprintf("%d patrols, %d checked", s.PatrolCount, s.AgentsChecked)
		if s.HeartbeatAge != "" {
			parts += fmt.Sprintf(", last %s ago", s.HeartbeatAge)
		}
		if s.Stale {
			parts += style.Warn.Render(" (stale)")
		}
		return parts
	}
	if s.PID > 0 {
		return fmt.Sprintf("pid %d", s.PID)
	}
	return ""
}

// SessionSeverity classifies the styling weight of a rendered session label.
// Callers that need pulsing/blinking treatment for attention-worthy states
// (dash's dead-session indicator) key off SeverityError; everything else is
// a plain color.
type SessionSeverity int

const (
	// SessionNeutral renders dim — no session information is meaningful for
	// this row (outpost at rest) or the session is stopped but expected to be
	// woken on demand (idle envoy, no wake needed yet).
	SessionNeutral SessionSeverity = iota
	// SessionOK renders green — the session is alive and responsive.
	SessionOK
	// SessionError renders red — the session should be alive but is not.
	SessionError
)

// SessionLabel returns the unstyled session-column text and its severity for
// an agent/envoy row, given work state, session liveness, and whether the
// row is an envoy. Callers apply their own styling per severity (status uses
// style.OK/Error/Dim directly; dash additionally pulses SessionError).
//
// working/stalled rows are identical for outposts and envoys: "alive"
// (green) when the session is up, "dead" (red/pulsed) when it isn't — an
// agent that should be actively working but has no session needs attention.
//
// idle rows diverge by role. Outposts: a dim "—" regardless of session
// state — an idle outpost with no session is the normal resting state of an
// empty ephemeral slot, and printing "stopped" there would be noise on every
// unused slot. Envoys: idle is a persistent role, not a transient slot, so
// the session state distinguishes an envoy that would answer mail
// immediately ("alive", green) from one that needs wake-on-mail first
// ("stopped", dim). See sol-58849f0f446b8aab.
func SessionLabel(state string, sessionAlive bool, isEnvoy bool) (string, SessionSeverity) {
	switch state {
	case "working", "stalled":
		if sessionAlive {
			return "alive", SessionOK
		}
		return "dead", SessionError
	case "idle":
		if isEnvoy {
			if sessionAlive {
				return "alive", SessionOK
			}
			return "stopped", SessionNeutral
		}
		return "—", SessionNeutral
	default:
		return "—", SessionNeutral
	}
}

// EscalationSummaryDetail mirrors status.EscalationSummary for formatter
// input. Field order and types must be kept in sync with
// status.EscalationSummary so callers may convert via plain Go pointer
// conversion: (*statusformat.EscalationSummaryDetail)(s.Escalations).
type EscalationSummaryDetail struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
}

// severityOrder is the display order for known escalation severities within
// the inbox breakdown, most urgent first. Mirrors the priority scale in
// internal/escalation.SeverityToPriority.
var severityOrder = []string{"critical", "high", "medium", "low"}

// FormatInboxLine returns a formatted inbox summary line for the given mail
// count and escalation summary, or an empty string when the combined total
// is zero. The returned string (when non-empty) ends with a newline
// character. When breakdown data is available, the line includes a
// parenthetical severity/mail breakdown, e.g.
// "Inbox: 4 items (1 critical, 1 high, 2 mail)\n".
//
// This is the canonical formatter shared by sol status and sol dash so both
// surfaces always show the same wording.
func FormatInboxLine(mailCount int, escalations *EscalationSummaryDetail) string {
	total := mailCount
	if escalations != nil {
		total += escalations.Total
	}
	if total <= 0 {
		return ""
	}

	breakdown := formatInboxBreakdown(mailCount, escalations)
	if breakdown == "" {
		label := "items need attention"
		if total == 1 {
			label = "item needs attention"
		}
		return fmt.Sprintf("Inbox: %d %s\n", total, label)
	}

	label := "items"
	if total == 1 {
		label = "item"
	}
	return fmt.Sprintf("Inbox: %d %s (%s)\n", total, label, breakdown)
}

// formatInboxBreakdown renders the parenthetical severity/mail breakdown for
// FormatInboxLine, e.g. "1 critical, 1 high, 2 mail". Known severities are
// listed in severityOrder; any unrecognized severity strings are appended
// after, sorted for deterministic output. Returns "" when there is nothing
// to break down (e.g. counts present but BySeverity empty and no mail).
func formatInboxBreakdown(mailCount int, escalations *EscalationSummaryDetail) string {
	var parts []string
	if escalations != nil {
		seen := make(map[string]bool, len(severityOrder))
		for _, sev := range severityOrder {
			if n := escalations.BySeverity[sev]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, sev))
				seen[sev] = true
			}
		}
		var extra []string
		for sev, n := range escalations.BySeverity {
			if !seen[sev] && n > 0 {
				extra = append(extra, sev)
			}
		}
		sort.Strings(extra)
		for _, sev := range extra {
			parts = append(parts, fmt.Sprintf("%d %s", escalations.BySeverity[sev], sev))
		}
	}
	if mailCount > 0 {
		parts = append(parts, fmt.Sprintf("%d mail", mailCount))
	}
	return strings.Join(parts, ", ")
}

// FormatMaxActive formats an agent count with optional capacity limit.
// When maxActive is zero (unlimited), only the active count is shown.
// When maxActive is positive, the result is "active/maxActive".
func FormatMaxActive(maxActive, active int) string {
	if maxActive <= 0 {
		return fmt.Sprintf("%d", active)
	}
	return fmt.Sprintf("%d/%d", active, maxActive)
}

// RuntimeTokenDetail mirrors status.RuntimeTokenInfo for formatter input.
// Field order and types must be kept in sync with status.RuntimeTokenInfo.
type RuntimeTokenDetail struct {
	Runtime      string
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
}

// TokenDetail mirrors status.TokenInfo for formatter input.
// Field order and types must be kept in sync with status.TokenInfo.
type TokenDetail struct {
	InputTokens      int64
	OutputTokens     int64
	CacheTokens      int64
	AgentCount       int
	CostUSD          float64
	RuntimeBreakdown []RuntimeTokenDetail
}

// FormatTokenSection writes the "Tokens (24h)" display block to b.
// Absent when all token counts are zero. Includes per-runtime breakdown when
// multiple runtimes are present. Output ends with a blank line.
//
// This is the canonical token renderer shared by sol status and sol dash.
func FormatTokenSection(b *strings.Builder, t TokenDetail) {
	if t.InputTokens == 0 && t.OutputTokens == 0 && t.CacheTokens == 0 {
		return
	}

	b.WriteString(style.Header.Render("Tokens (24h)"))
	b.WriteString("\n")

	line := fmt.Sprintf("  %s in / %s out",
		FormatCompactTokens(t.InputTokens),
		FormatCompactTokens(t.OutputTokens))

	if t.CostUSD > 0 {
		line += fmt.Sprintf(", %s", FormatCost(t.CostUSD))
	}

	if t.AgentCount > 0 {
		line += fmt.Sprintf("  %s  %d agents", style.Dim.Render("•"), t.AgentCount)
	}

	b.WriteString(line)
	b.WriteString("\n")

	// Per-runtime breakdown (only shown when multiple runtimes present).
	if len(t.RuntimeBreakdown) > 0 {
		for _, rt := range t.RuntimeBreakdown {
			rtLine := fmt.Sprintf("    %s: %s in / %s out",
				rt.Runtime,
				FormatCompactTokens(rt.InputTokens),
				FormatCompactTokens(rt.OutputTokens))
			if rt.CostUSD > 0 {
				rtLine += fmt.Sprintf(", %s", FormatCost(rt.CostUSD))
			}
			b.WriteString(style.Dim.Render(rtLine))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
}

// PhaseProgressDetail mirrors status.PhaseProgress for formatter input.
// Only the fields required for caravan display are included.
type PhaseProgressDetail struct {
	Phase  int
	Total  int
	Closed int
}

// CaravanDetail mirrors the display-relevant fields of status.CaravanInfo.
// Callers convert from status.CaravanInfo before passing to RenderCaravanRows.
type CaravanDetail struct {
	ID          string
	Name        string
	Status      string // "drydock" or any active status
	TotalItems  int
	ClosedItems int
	Phases      []PhaseProgressDetail
}

// CaravanRenderOpts configures the visual presentation of caravan rows.
type CaravanRenderOpts struct {
	// ProgressFn returns a rendered progress bar for the given caravan ID and
	// completion fraction. If nil, no progress bar is included in the row.
	ProgressFn func(id string, fraction float64, maxWidth int) string
	// MaxProgressWidth caps the rendered progress bar width in characters.
	MaxProgressWidth int
	// ShowCursor enables cursor-selection highlighting.
	ShowCursor bool
	// Cursor is the currently-selected row index in the rendered list.
	Cursor int
	// SelectFn renders a row string with a selection highlight. It receives the
	// raw (unstyled) line text; the callee handles padding to terminal width.
	// If nil, the selected row is rendered without a highlight.
	SelectFn func(line string) string
}

// RenderCaravanRows writes caravan rows to b, split into Active and Drydocked
// sub-groups. Selection highlighting and progress bars are supplied by the
// caller via opts so this package does not depend on bubbletea.
//
// This is the canonical caravan row renderer shared by sol dash sphere and
// world views.
func RenderCaravanRows(b *strings.Builder, caravans []CaravanDetail, opts CaravanRenderOpts) {
	maxProgressWidth := opts.MaxProgressWidth
	if maxProgressWidth < 20 {
		maxProgressWidth = 20
	}
	if maxProgressWidth > 40 {
		maxProgressWidth = 40
	}

	// Split into active and drydocked groups.
	var active, drydocked []CaravanDetail
	for _, c := range caravans {
		if c.Status == "drydock" {
			drydocked = append(drydocked, c)
		} else {
			active = append(active, c)
		}
	}

	idx := 0

	// Render active caravans.
	if len(active) > 0 {
		if len(drydocked) > 0 {
			// Only show sub-header when both groups are present.
			b.WriteString("  " + style.Dim.Render("Active") + "\n")
		}
		for _, c := range active {
			fraction := float64(0)
			if c.TotalItems > 0 {
				fraction = float64(c.ClosedItems) / float64(c.TotalItems)
			}
			progressStr := ""
			if opts.ProgressFn != nil {
				progressStr = opts.ProgressFn(c.ID, fraction, maxProgressWidth)
			}
			line := formatCaravanRow(c, progressStr)
			if opts.ShowCursor && idx == opts.Cursor && opts.SelectFn != nil {
				b.WriteString(opts.SelectFn(line))
			} else {
				b.WriteString(line)
			}
			b.WriteString("\n")
			idx++
		}
	}

	// Render drydocked caravans.
	if len(drydocked) > 0 {
		b.WriteString("  " + style.Dim.Render("Drydocked") + "\n")
		for _, c := range drydocked {
			fraction := float64(0)
			if c.TotalItems > 0 {
				fraction = float64(c.ClosedItems) / float64(c.TotalItems)
			}
			progressStr := ""
			if opts.ProgressFn != nil {
				progressStr = opts.ProgressFn(c.ID, fraction, maxProgressWidth)
			}
			line := formatCaravanRow(c, progressStr)
			if opts.ShowCursor && idx == opts.Cursor && opts.SelectFn != nil {
				b.WriteString(opts.SelectFn(line))
			} else {
				b.WriteString(style.Dim.Render(line))
			}
			b.WriteString("\n")
			idx++
		}
	}
}

// formatCaravanRow formats a single caravan row given a pre-rendered progress string.
func formatCaravanRow(c CaravanDetail, progressStr string) string {
	phaseSummary := caravanPhaseSummary(c)
	mergeCount := style.Dim.Render(fmt.Sprintf("%d/%d merged", c.ClosedItems, c.TotalItems))
	if progressStr != "" {
		if phaseSummary != "" {
			return fmt.Sprintf("  %s  %s  %s  %s",
				c.Name, progressStr, mergeCount,
				style.Dim.Render(phaseSummary))
		}
		return fmt.Sprintf("  %s  %s  %s", c.Name, progressStr, mergeCount)
	}
	if phaseSummary != "" {
		return fmt.Sprintf("  %s  %s  %s", c.Name, mergeCount, style.Dim.Render(phaseSummary))
	}
	return fmt.Sprintf("  %s  %s", c.Name, mergeCount)
}

// caravanPhaseSummary builds a compact phase description for a caravan.
func caravanPhaseSummary(c CaravanDetail) string {
	if len(c.Phases) == 0 {
		return ""
	}
	var parts []string
	for _, p := range c.Phases {
		parts = append(parts, fmt.Sprintf("p%d: %d/%d", p.Phase, p.Closed, p.Total))
	}
	return strings.Join(parts, " ")
}

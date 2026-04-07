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
}

// BrokerDetail mirrors status.BrokerInfo for formatter input.
type BrokerDetail struct {
	Running        bool                         `json:"running"`
	HeartbeatAge   string                       `json:"heartbeat_age,omitempty"`
	PatrolCount    int                          `json:"patrol_count,omitempty"`
	Stale          bool                         `json:"stale"`
	ProviderHealth string                       `json:"provider_health,omitempty"`
	Providers      []broker.ProviderHealthEntry `json:"providers,omitempty"`
	TokenHealth    []broker.AccountTokenHealth  `json:"token_health,omitempty"`
}

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
	if detail == "" {
		return "running"
	}
	return detail
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
	// When single provider (no per-provider entries), show inline.
	if len(b.Providers) == 0 {
		switch b.ProviderHealth {
		case "degraded":
			parts += style.Warn.Render(" [provider: degraded]")
		case "down":
			parts += style.Error.Render(" [provider: down]")
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
		if f.Merging {
			parts += style.OK.Render(" [merging]")
		}
		return parts
	}
	if f.PID > 0 {
		detail := fmt.Sprintf("pid %d", f.PID)
		if f.Merging {
			detail += style.OK.Render(" [merging]")
		}
		return detail
	}
	return ""
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

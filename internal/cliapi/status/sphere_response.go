// Package status provides the CLI API response types for the status command.
package status

import (
	"time"

	"github.com/nevinsm/sol/internal/broker"
	internstatus "github.com/nevinsm/sol/internal/status"
)

// SphereStatusResponse is the CLI API response for `sol status` (sphere mode).
type SphereStatusResponse struct {
	SOLHome     string             `json:"sol_home"`
	Prefect     PrefectInfo        `json:"prefect"`
	Consul      ConsulInfo         `json:"consul"`
	Chronicle   ChronicleInfo      `json:"chronicle"`
	Ledger      LedgerInfo         `json:"ledger"`
	Broker      BrokerInfo         `json:"broker"`
	Worlds      []WorldSummary     `json:"worlds"`
	Tokens      TokenInfo          `json:"tokens"`
	Caravans    []CaravanInfo      `json:"caravans,omitempty"`
	Escalations *EscalationSummary `json:"escalations,omitempty"`
	MailCount   int                `json:"mail_count,omitempty"`
	Health      string             `json:"health"`
}

// PrefectInfo holds prefect process state.
type PrefectInfo struct {
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
}

// ConsulInfo holds consul process state.
type ConsulInfo struct {
	Running      bool   `json:"running"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	PatrolCount  int    `json:"patrol_count,omitempty"`
	Stale        bool   `json:"stale"`
}

// ChronicleInfo holds chronicle process state.
type ChronicleInfo struct {
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	EventsProcessed int64  `json:"events_processed,omitempty"`
	HeartbeatAge    string `json:"heartbeat_age,omitempty"`
	Stale           bool   `json:"stale,omitempty"`
}

// LedgerInfo holds ledger process state.
type LedgerInfo struct {
	Running      bool   `json:"running"`
	PID          int    `json:"pid,omitempty"`
	Port         int    `json:"port,omitempty"`
	HeartbeatAge string `json:"heartbeat_age,omitempty"`
	Stale        bool   `json:"stale,omitempty"`
}

// BrokerInfo holds broker process state.
type BrokerInfo struct {
	Running        bool                  `json:"running"`
	HeartbeatAge   string                `json:"heartbeat_age,omitempty"`
	PatrolCount    int                   `json:"patrol_count,omitempty"`
	Stale          bool                  `json:"stale"`
	ProviderHealth string                `json:"provider_health,omitempty"`
	Providers      []ProviderHealthEntry `json:"providers,omitempty"`
	TokenHealth    []AccountTokenHealth  `json:"token_health,omitempty"`
}

// ProviderHealthEntry holds per-provider health state.
type ProviderHealthEntry struct {
	Provider            string    `json:"provider"`
	Health              string    `json:"health"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastProbe           time.Time `json:"last_probe,omitzero"`
	LastHealthy         time.Time `json:"last_healthy,omitzero"`
}

// AccountTokenHealth holds per-account token health state.
type AccountTokenHealth struct {
	Handle    string     `json:"handle"`
	Type      string     `json:"type"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Status    string     `json:"status"`
}

// RuntimeTokenInfo holds per-runtime token usage for display.
type RuntimeTokenInfo struct {
	Runtime      string  `json:"runtime"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// TokenInfo holds aggregated token usage for a 24-hour rolling window.
type TokenInfo struct {
	InputTokens      int64              `json:"input_tokens"`
	OutputTokens     int64              `json:"output_tokens"`
	CacheTokens      int64              `json:"cache_tokens"`
	AgentCount       int                `json:"agent_count"`
	CostUSD          float64            `json:"cost_usd,omitempty"`
	RuntimeBreakdown []RuntimeTokenInfo `json:"runtime_breakdown,omitempty"`
}

// PhaseProgress holds progress info for a single phase within a caravan.
type PhaseProgress struct {
	Phase      int `json:"phase"`
	Total      int `json:"total"`
	Done       int `json:"done"`
	Closed     int `json:"closed"`
	Ready      int `json:"ready"`
	Dispatched int `json:"dispatched"`
}

// CaravanItemDetail holds per-item detail within a caravan.
type CaravanItemDetail struct {
	WritID   string `json:"writ_id"`
	World    string `json:"world"`
	Phase    int    `json:"phase"`
	Status   string `json:"status"`
	Ready    bool   `json:"ready"`
	Assignee string `json:"assignee,omitempty"`
	Title    string `json:"title"`
}

// CaravanInfo holds summary information about a caravan relevant to a world.
type CaravanInfo struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Status          string              `json:"status"`
	TotalItems      int                 `json:"total_items"`
	ReadyItems      int                 `json:"ready_items"`
	DoneItems       int                 `json:"done_items"`
	ClosedItems     int                 `json:"closed_items"`
	DispatchedItems int                 `json:"dispatched_items"`
	Phases          []PhaseProgress     `json:"phases,omitempty"`
	Items           []CaravanItemDetail `json:"items,omitempty"`
}

// EscalationSummary holds aggregated escalation counts.
type EscalationSummary struct {
	Total      int            `json:"total"`
	BySeverity map[string]int `json:"by_severity"`
}

// WorldSummary holds a condensed view of one world for the sphere overview.
type WorldSummary struct {
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo,omitempty"`
	Sleeping   bool   `json:"sleeping,omitempty"`
	Agents     int    `json:"agents"`
	MaxActive  int    `json:"max_active"`
	Envoys     int    `json:"envoys"`
	Working    int    `json:"working"`
	Idle       int    `json:"idle"`
	Stalled    int    `json:"stalled"`
	Dead       int    `json:"dead"`
	Forge      bool   `json:"forge"`
	Sentinel   bool   `json:"sentinel"`
	MRReady    int    `json:"mr_ready"`
	MRFailed   int    `json:"mr_failed"`
	Health     string `json:"health"`
}

// FromSphereStatus converts an internal status.SphereStatus to the CLI API response type.
func FromSphereStatus(s *internstatus.SphereStatus) SphereStatusResponse {
	resp := SphereStatusResponse{
		SOLHome:   s.SOLHome,
		Prefect:   convertPrefectInfo(s.Prefect),
		Consul:    convertConsulInfo(s.Consul),
		Chronicle: convertChronicleInfo(s.Chronicle),
		Ledger:    convertLedgerInfo(s.Ledger),
		Broker:    convertBrokerInfo(s.Broker),
		Tokens:    convertTokenInfo(s.Tokens),
		MailCount: s.MailCount,
		Health:    s.Health,
	}

	for _, w := range s.Worlds {
		resp.Worlds = append(resp.Worlds, convertWorldSummary(w))
	}

	for _, c := range s.Caravans {
		resp.Caravans = append(resp.Caravans, convertCaravanInfo(c))
	}

	if s.Escalations != nil {
		resp.Escalations = convertEscalationSummary(s.Escalations)
	}

	return resp
}

func convertPrefectInfo(p internstatus.PrefectInfo) PrefectInfo {
	return PrefectInfo{
		Running: p.Running,
		PID:     p.PID,
	}
}

func convertConsulInfo(c internstatus.ConsulInfo) ConsulInfo {
	return ConsulInfo{
		Running:      c.Running,
		HeartbeatAge: c.HeartbeatAge,
		PatrolCount:  c.PatrolCount,
		Stale:        c.Stale,
	}
}

func convertChronicleInfo(c internstatus.ChronicleInfo) ChronicleInfo {
	return ChronicleInfo{
		Running:         c.Running,
		PID:             c.PID,
		EventsProcessed: c.EventsProcessed,
		HeartbeatAge:    c.HeartbeatAge,
		Stale:           c.Stale,
	}
}

func convertLedgerInfo(l internstatus.LedgerInfo) LedgerInfo {
	return LedgerInfo{
		Running:      l.Running,
		PID:          l.PID,
		Port:         l.Port,
		HeartbeatAge: l.HeartbeatAge,
		Stale:        l.Stale,
	}
}

func convertBrokerInfo(b internstatus.BrokerInfo) BrokerInfo {
	info := BrokerInfo{
		Running:        b.Running,
		HeartbeatAge:   b.HeartbeatAge,
		PatrolCount:    b.PatrolCount,
		Stale:          b.Stale,
		ProviderHealth: b.ProviderHealth,
	}

	for _, p := range b.Providers {
		info.Providers = append(info.Providers, convertProviderHealthEntry(p))
	}

	for _, t := range b.TokenHealth {
		info.TokenHealth = append(info.TokenHealth, convertAccountTokenHealth(t))
	}

	return info
}

func convertProviderHealthEntry(p broker.ProviderHealthEntry) ProviderHealthEntry {
	return ProviderHealthEntry{
		Provider:            p.Provider,
		Health:              string(p.Health),
		ConsecutiveFailures: p.ConsecutiveFailures,
		LastProbe:           p.LastProbe,
		LastHealthy:         p.LastHealthy,
	}
}

func convertAccountTokenHealth(t broker.AccountTokenHealth) AccountTokenHealth {
	return AccountTokenHealth{
		Handle:    t.Handle,
		Type:      t.Type,
		ExpiresAt: t.ExpiresAt,
		Status:    t.Status,
	}
}

func convertTokenInfo(t internstatus.TokenInfo) TokenInfo {
	info := TokenInfo{
		InputTokens:  t.InputTokens,
		OutputTokens: t.OutputTokens,
		CacheTokens:  t.CacheTokens,
		AgentCount:   t.AgentCount,
		CostUSD:      t.CostUSD,
	}

	for _, rt := range t.RuntimeBreakdown {
		info.RuntimeBreakdown = append(info.RuntimeBreakdown, RuntimeTokenInfo{
			Runtime:      rt.Runtime,
			InputTokens:  rt.InputTokens,
			OutputTokens: rt.OutputTokens,
			CacheTokens:  rt.CacheTokens,
			CostUSD:      rt.CostUSD,
		})
	}

	return info
}

func convertWorldSummary(w internstatus.WorldSummary) WorldSummary {
	return WorldSummary{
		Name:       w.Name,
		SourceRepo: w.SourceRepo,
		Sleeping:   w.Sleeping,
		Agents:     w.Agents,
		MaxActive:  w.MaxActive,
		Envoys:     w.Envoys,
		Working:    w.Working,
		Idle:       w.Idle,
		Stalled:    w.Stalled,
		Dead:       w.Dead,
		Forge:      w.Forge,
		Sentinel:   w.Sentinel,
		MRReady:    w.MRReady,
		MRFailed:   w.MRFailed,
		Health:     w.Health,
	}
}

func convertCaravanInfo(c internstatus.CaravanInfo) CaravanInfo {
	info := CaravanInfo{
		ID:              c.ID,
		Name:            c.Name,
		Status:          c.Status,
		TotalItems:      c.TotalItems,
		ReadyItems:      c.ReadyItems,
		DoneItems:       c.DoneItems,
		ClosedItems:     c.ClosedItems,
		DispatchedItems: c.DispatchedItems,
	}

	for _, p := range c.Phases {
		info.Phases = append(info.Phases, PhaseProgress{
			Phase:      p.Phase,
			Total:      p.Total,
			Done:       p.Done,
			Closed:     p.Closed,
			Ready:      p.Ready,
			Dispatched: p.Dispatched,
		})
	}

	for _, item := range c.Items {
		info.Items = append(info.Items, CaravanItemDetail{
			WritID:   item.WritID,
			World:    item.World,
			Phase:    item.Phase,
			Status:   item.Status,
			Ready:    item.Ready,
			Assignee: item.Assignee,
			Title:    item.Title,
		})
	}

	return info
}

func convertEscalationSummary(e *internstatus.EscalationSummary) *EscalationSummary {
	if e == nil {
		return nil
	}
	return &EscalationSummary{
		Total:      e.Total,
		BySeverity: e.BySeverity,
	}
}

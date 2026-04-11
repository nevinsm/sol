package writs

import (
	"time"

	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/trace"
)

// TraceResponse is the CLI API response for `sol writ trace --json`.
// Field names and casing match the pre-migration JSON shape exactly.
type TraceResponse struct {
	World         string                        `json:"world"`
	Writ          *TraceWrit                    `json:"writ"`
	History       []TraceHistoryEntry           `json:"history"`
	Tokens        []TraceTokenSummary           `json:"tokens"`
	MergeRequests []TraceMergeRequest           `json:"merge_requests"`
	Dependencies  []string                      `json:"dependencies"`
	Dependents    []string                      `json:"dependents"`
	Labels        []string                      `json:"labels"`
	Escalations   []TraceEscalation             `json:"escalations"`
	CaravanItems  []TraceCaravanItem            `json:"caravan_items"`
	Caravans      map[string]*TraceCaravan      `json:"caravans,omitempty"`
	ActiveAgents  []TraceAgent                  `json:"active_agents"`
	Tethers       []TraceTetherInfo             `json:"tethers"`
	Timeline      []TraceTimelineEvent          `json:"timeline"`
	Cost          *TraceCostSummary             `json:"cost,omitempty"`
	Degradations  []string                      `json:"degradations,omitempty"`
}

// TraceWrit mirrors store.Writ's default JSON marshaling (PascalCase, no json tags).
type TraceWrit struct {
	ID          string         `json:"ID"`
	Title       string         `json:"Title"`
	Description string         `json:"Description"`
	Status      string         `json:"Status"`
	Priority    int            `json:"Priority"`
	Assignee    string         `json:"Assignee"`
	ParentID    string         `json:"ParentID"`
	Kind        string         `json:"Kind"`
	CreatedBy   string         `json:"CreatedBy"`
	CreatedAt   time.Time      `json:"CreatedAt"`
	UpdatedAt   time.Time      `json:"UpdatedAt"`
	ClosedAt    *time.Time     `json:"ClosedAt"`
	CloseReason string         `json:"CloseReason"`
	Labels      []string       `json:"Labels"`
	Metadata    map[string]any `json:"Metadata"`
}

// TraceHistoryEntry mirrors store.HistoryEntry's default JSON marshaling (PascalCase).
type TraceHistoryEntry struct {
	ID        string     `json:"ID"`
	AgentName string     `json:"AgentName"`
	WritID    string     `json:"WritID"`
	Action    string     `json:"Action"`
	StartedAt time.Time  `json:"StartedAt"`
	EndedAt   *time.Time `json:"EndedAt"`
	Summary   string     `json:"Summary"`
}

// TraceTokenSummary mirrors store.TokenSummary's default JSON marshaling (PascalCase).
type TraceTokenSummary struct {
	Model               string   `json:"Model"`
	InputTokens         int64    `json:"InputTokens"`
	OutputTokens        int64    `json:"OutputTokens"`
	CacheReadTokens     int64    `json:"CacheReadTokens"`
	CacheCreationTokens int64    `json:"CacheCreationTokens"`
	ReasoningTokens     int64    `json:"ReasoningTokens"`
	CostUSD             *float64 `json:"CostUSD"`
	DurationMS          *int64   `json:"DurationMS"`
}

// TraceMergeRequest mirrors store.MergeRequest's default JSON marshaling (PascalCase).
type TraceMergeRequest struct {
	ID              string     `json:"ID"`
	WritID          string     `json:"WritID"`
	Branch          string     `json:"Branch"`
	Phase           string     `json:"Phase"`
	ClaimedBy       string     `json:"ClaimedBy"`
	ClaimedAt       *time.Time `json:"ClaimedAt"`
	Attempts        int        `json:"Attempts"`
	Priority        int        `json:"Priority"`
	BlockedBy       string     `json:"BlockedBy"`
	ResolutionCount int        `json:"ResolutionCount"`
	CreatedAt       time.Time  `json:"CreatedAt"`
	UpdatedAt       time.Time  `json:"UpdatedAt"`
	MergedAt        *time.Time `json:"MergedAt"`
}

// TraceEscalation mirrors store.Escalation's default JSON marshaling (PascalCase).
type TraceEscalation struct {
	ID             string     `json:"ID"`
	Severity       string     `json:"Severity"`
	Source         string     `json:"Source"`
	Description    string     `json:"Description"`
	SourceRef      string     `json:"SourceRef"`
	Status         string     `json:"Status"`
	Acknowledged   bool       `json:"Acknowledged"`
	LastNotifiedAt *time.Time `json:"LastNotifiedAt"`
	CreatedAt      time.Time  `json:"CreatedAt"`
	UpdatedAt      time.Time  `json:"UpdatedAt"`
}

// TraceCaravanItem mirrors store.CaravanItem's JSON marshaling (has snake_case json tags).
type TraceCaravanItem struct {
	CaravanID string `json:"caravan_id"`
	WritID    string `json:"writ_id"`
	World     string `json:"world"`
	Phase     int    `json:"phase"`
}

// TraceCaravan mirrors store.Caravan's JSON marshaling (has snake_case json tags).
type TraceCaravan struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Owner     string     `json:"owner"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// TraceAgent mirrors store.Agent's default JSON marshaling (PascalCase).
type TraceAgent struct {
	ID         string    `json:"ID"`
	Name       string    `json:"Name"`
	World      string    `json:"World"`
	Role       string    `json:"Role"`
	State      string    `json:"State"`
	ActiveWrit string    `json:"ActiveWrit"`
	CreatedAt  time.Time `json:"CreatedAt"`
	UpdatedAt  time.Time `json:"UpdatedAt"`
}

// TraceTetherInfo mirrors trace.TetherInfo's JSON marshaling (snake_case json tags).
type TraceTetherInfo struct {
	Agent string `json:"agent"`
	Role  string `json:"role"`
}

// TraceTimelineEvent mirrors trace.TimelineEvent's JSON marshaling (snake_case json tags).
type TraceTimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
}

// TraceCostSummary mirrors trace.CostSummary's JSON marshaling (snake_case json tags).
type TraceCostSummary struct {
	Models    []TraceModelCost `json:"models"`
	Total     float64          `json:"total"`
	CycleTime string           `json:"cycle_time,omitempty"`
}

// TraceModelCost mirrors trace.ModelCost's JSON marshaling (snake_case json tags).
type TraceModelCost struct {
	Model               string  `json:"model"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	ReasoningTokens     int64   `json:"reasoning_tokens"`
	Cost                float64 `json:"cost"`
}

// FromTraceData converts a trace.TraceData to the CLI API TraceResponse.
func FromTraceData(td *trace.TraceData) TraceResponse {
	resp := TraceResponse{
		World:        td.World,
		Dependencies: td.Dependencies,
		Dependents:   td.Dependents,
		Labels:       td.Labels,
		Degradations: td.Degradations,
	}

	// Writ.
	if td.Writ != nil {
		resp.Writ = traceWritFromStore(td.Writ)
	}

	// History.
	resp.History = make([]TraceHistoryEntry, len(td.History))
	for i, h := range td.History {
		resp.History[i] = traceHistoryEntryFromStore(h)
	}

	// Tokens.
	resp.Tokens = make([]TraceTokenSummary, len(td.Tokens))
	for i, t := range td.Tokens {
		resp.Tokens[i] = traceTokenSummaryFromStore(t)
	}

	// Merge requests.
	resp.MergeRequests = make([]TraceMergeRequest, len(td.MergeRequests))
	for i, mr := range td.MergeRequests {
		resp.MergeRequests[i] = traceMergeRequestFromStore(mr)
	}

	// Escalations.
	resp.Escalations = make([]TraceEscalation, len(td.Escalations))
	for i, e := range td.Escalations {
		resp.Escalations[i] = traceEscalationFromStore(e)
	}

	// Caravan items.
	resp.CaravanItems = make([]TraceCaravanItem, len(td.CaravanItems))
	for i, ci := range td.CaravanItems {
		resp.CaravanItems[i] = TraceCaravanItem{
			CaravanID: ci.CaravanID,
			WritID:    ci.WritID,
			World:     ci.World,
			Phase:     ci.Phase,
		}
	}

	// Caravans.
	if td.Caravans != nil {
		resp.Caravans = make(map[string]*TraceCaravan, len(td.Caravans))
		for k, c := range td.Caravans {
			resp.Caravans[k] = traceCaravanFromStore(c)
		}
	}

	// Active agents.
	resp.ActiveAgents = make([]TraceAgent, len(td.ActiveAgents))
	for i, a := range td.ActiveAgents {
		resp.ActiveAgents[i] = traceAgentFromStore(a)
	}

	// Tethers.
	resp.Tethers = make([]TraceTetherInfo, len(td.Tethers))
	for i, t := range td.Tethers {
		resp.Tethers[i] = TraceTetherInfo{
			Agent: t.Agent,
			Role:  t.Role,
		}
	}

	// Timeline.
	resp.Timeline = make([]TraceTimelineEvent, len(td.Timeline))
	for i, te := range td.Timeline {
		resp.Timeline[i] = TraceTimelineEvent{
			Timestamp: te.Timestamp,
			Action:    te.Action,
			Detail:    te.Detail,
		}
	}

	// Cost.
	if td.Cost != nil {
		resp.Cost = traceCostSummaryFromTrace(td.Cost)
	}

	return resp
}

func traceWritFromStore(w *store.Writ) *TraceWrit {
	return &TraceWrit{
		ID:          w.ID,
		Title:       w.Title,
		Description: w.Description,
		Status:      w.Status,
		Priority:    w.Priority,
		Assignee:    w.Assignee,
		ParentID:    w.ParentID,
		Kind:        w.Kind,
		CreatedBy:   w.CreatedBy,
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
		ClosedAt:    w.ClosedAt,
		CloseReason: w.CloseReason,
		Labels:      w.Labels,
		Metadata:    w.Metadata,
	}
}

func traceHistoryEntryFromStore(h store.HistoryEntry) TraceHistoryEntry {
	return TraceHistoryEntry{
		ID:        h.ID,
		AgentName: h.AgentName,
		WritID:    h.WritID,
		Action:    h.Action,
		StartedAt: h.StartedAt,
		EndedAt:   h.EndedAt,
		Summary:   h.Summary,
	}
}

func traceTokenSummaryFromStore(t store.TokenSummary) TraceTokenSummary {
	return TraceTokenSummary{
		Model:               t.Model,
		InputTokens:         t.InputTokens,
		OutputTokens:        t.OutputTokens,
		CacheReadTokens:     t.CacheReadTokens,
		CacheCreationTokens: t.CacheCreationTokens,
		ReasoningTokens:     t.ReasoningTokens,
		CostUSD:             t.CostUSD,
		DurationMS:          t.DurationMS,
	}
}

func traceMergeRequestFromStore(mr store.MergeRequest) TraceMergeRequest {
	return TraceMergeRequest{
		ID:              mr.ID,
		WritID:          mr.WritID,
		Branch:          mr.Branch,
		Phase:           mr.Phase,
		ClaimedBy:       mr.ClaimedBy,
		ClaimedAt:       mr.ClaimedAt,
		Attempts:        mr.Attempts,
		Priority:        mr.Priority,
		BlockedBy:       mr.BlockedBy,
		ResolutionCount: mr.ResolutionCount,
		CreatedAt:       mr.CreatedAt,
		UpdatedAt:       mr.UpdatedAt,
		MergedAt:        mr.MergedAt,
	}
}

func traceEscalationFromStore(e store.Escalation) TraceEscalation {
	return TraceEscalation{
		ID:             e.ID,
		Severity:       e.Severity,
		Source:         e.Source,
		Description:    e.Description,
		SourceRef:      e.SourceRef,
		Status:         e.Status,
		Acknowledged:   e.Acknowledged,
		LastNotifiedAt: e.LastNotifiedAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
	}
}

func traceCaravanFromStore(c *store.Caravan) *TraceCaravan {
	if c == nil {
		return nil
	}
	return &TraceCaravan{
		ID:        c.ID,
		Name:      c.Name,
		Status:    c.Status,
		Owner:     c.Owner,
		CreatedAt: c.CreatedAt,
		ClosedAt:  c.ClosedAt,
	}
}

func traceAgentFromStore(a store.Agent) TraceAgent {
	return TraceAgent{
		ID:         a.ID,
		Name:       a.Name,
		World:      a.World,
		Role:       a.Role,
		State:      a.State,
		ActiveWrit: a.ActiveWrit,
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

func traceCostSummaryFromTrace(cs *trace.CostSummary) *TraceCostSummary {
	models := make([]TraceModelCost, len(cs.Models))
	for i, m := range cs.Models {
		models[i] = TraceModelCost{
			Model:               m.Model,
			InputTokens:         m.InputTokens,
			OutputTokens:        m.OutputTokens,
			CacheReadTokens:     m.CacheReadTokens,
			CacheCreationTokens: m.CacheCreationTokens,
			ReasoningTokens:     m.ReasoningTokens,
			Cost:                m.Cost,
		}
	}
	return &TraceCostSummary{
		Models:    models,
		Total:     cs.Total,
		CycleTime: cs.CycleTime,
	}
}

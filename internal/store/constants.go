package store

// AgentState represents the lifecycle state of an agent.
type AgentState = string

const (
	AgentIdle    AgentState = "idle"
	AgentWorking AgentState = "working"
	AgentStalled AgentState = "stalled"
)

// WritStatus represents the lifecycle status of a writ.
type WritStatus = string

const (
	WritOpen     WritStatus = "open"
	WritTethered WritStatus = "tethered"
	WritWorking  WritStatus = "working"
	WritResolve  WritStatus = "resolve"
	WritDone     WritStatus = "done"
	WritClosed   WritStatus = "closed"
)

// MRPhase represents the lifecycle phase of a merge request.
type MRPhase = string

const (
	MRReady      MRPhase = "ready"
	MRClaimed    MRPhase = "claimed"
	MRMerged     MRPhase = "merged"
	MRFailed     MRPhase = "failed"
	MRSuperseded MRPhase = "superseded"
)

// CaravanStatus represents the lifecycle status of a caravan.
type CaravanStatus = string

const (
	CaravanDrydock CaravanStatus = "drydock"
	CaravanOpen    CaravanStatus = "open"
	CaravanReady   CaravanStatus = "ready"
	CaravanClosed  CaravanStatus = "closed"
)

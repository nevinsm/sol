package store

import "time"

// interfaces.go defines canonical composable interfaces for the store package.
//
// Consumer packages should depend on the narrowest interface(s) that satisfy
// their needs rather than the concrete *WorldStore or *SphereStore types.
// This file also contains compile-time assertions that both concrete types
// satisfy their respective interfaces.
//
// World-scoped interfaces (implemented by *WorldStore):
//   WritReader, WritWriter, MRReader, MRWriter, DepReader, DepWriter,
//   LedgerReader, LedgerWriter, HistoryStore, AgentMemoryStore
//
// Sphere-scoped interfaces (implemented by *SphereStore):
//   AgentReader, AgentWriter, CaravanReader, CaravanWriter,
//   CaravanDepReader, CaravanDepWriter, MessageStore, EscalationStore,
//   WorldRegistry
//
// Transitional note: until consumers fully migrate, *Store satisfies all
// interfaces through embedding promotion. New code should target the
// specific WorldStore/SphereStore interfaces below.

// ——— World-scoped interfaces ———

// WritReader provides read access to writs in a world database.
type WritReader interface {
	GetWrit(id string) (*Writ, error)
	ListWrits(filters ListFilters) ([]Writ, error)
	ListChildWrits(parentID string) ([]Writ, error)
	ReadyWrits() ([]Writ, error)
}

// WritWriter provides write access to writs and labels in a world database.
type WritWriter interface {
	CreateWrit(title, description, createdBy string, priority int, labels []string) (string, error)
	CreateWritWithOpts(opts CreateWritOpts) (string, error)
	UpdateWrit(id string, updates WritUpdates) error
	CloseWrit(id string, closeReason ...string) ([]string, error)
	GetWritMetadata(id string) (map[string]any, error)
	SetWritMetadata(id string, metadata map[string]any) error
	AddLabel(itemID, label string) error
	RemoveLabel(itemID, label string) error
}

// MRReader provides read access to merge requests in a world database.
type MRReader interface {
	GetMergeRequest(id string) (*MergeRequest, error)
	ListMergeRequests(phase MRPhase) ([]MergeRequest, error)
	ListMergeRequestsByWrit(writID string, phase MRPhase) ([]MergeRequest, error)
	ListBlockedMergeRequests() ([]MergeRequest, error)
	FindMergeRequestByBlocker(blockerID string) (*MergeRequest, error)
}

// MergeRequestReader provides narrow read access to merge requests (subset of MRReader).
type MergeRequestReader interface {
	ListMergeRequests(phase MRPhase) ([]MergeRequest, error)
}

// MRWriter provides write access to merge requests in a world database.
type MRWriter interface {
	CreateMergeRequest(writID, branch string, priority int) (string, error)
	ClaimMergeRequest(claimerID string) (*MergeRequest, error)
	UpdateMergeRequestPhase(id string, phase MRPhase) error
	BlockMergeRequest(mrID, blockerWritID string) error
	UnblockMergeRequest(mrID string) error
	ReleaseStaleClaims(ttl time.Duration) (int, error)
	ResetMergeRequestForRetry(mrID string) error
	SupersedeFailedMRsForWrit(writID string) ([]string, error)
}

// DepReader provides read access to writ dependency data in a world database.
type DepReader interface {
	GetDependencies(itemID string) ([]string, error)
	GetDependents(itemID string) ([]string, error)
	IsReady(itemID string) (bool, error)
	HasOpenTransitiveDependents(writID string) (bool, error)
}

// DepWriter provides write access to writ dependencies in a world database.
type DepWriter interface {
	AddDependency(fromID, toID string) error
	RemoveDependency(fromID, toID string) error
}

// LedgerReader provides read access to token usage history in a world database.
type LedgerReader interface {
	AggregateTokens(agentName string) ([]TokenSummary, error)
	TokensSince(since time.Time) ([]TokenSummary, error)
	TokensForWrit(writID string) ([]TokenSummary, error)
	TokensForWorld() ([]TokenSummary, error)
	TokensByWritForAgent(agentName string) (map[string][]TokenSummary, error)
	TokensByAgentForWorld() ([]AgentTokenSummary, error)
	TokensByAgentSince(since time.Time) ([]AgentTokenSummary, error)
	TokensByWritForAgentSince(agentName string, since time.Time) (map[string][]TokenSummary, error)
	TokensForWritSince(writID string, since time.Time) ([]TokenSummary, error)
	WorldTokenMeta() (agents int, writs int, err error)
	WorldTokenMetaSince(since time.Time) (agents int, writs int, err error)
	MergeStatsForAgent(agentName string) (AgentMergeRequestSummary, error)
}

// LedgerWriter provides write access to token usage records in a world database.
type LedgerWriter interface {
	WriteTokenUsage(historyID, model string, input, output, cacheRead, cacheCreation int64) (string, error)
}

// HistoryStore provides access to agent history records in a world database.
type HistoryStore interface {
	WriteHistory(agentName, writID, action, summary string, startedAt time.Time, endedAt *time.Time) (string, error)
	GetHistory(id string) (*HistoryEntry, error)
	ListHistory(agentName string) ([]HistoryEntry, error)
	EndHistory(writID string) (string, error)
	HistoryForWrit(writID string) ([]HistoryEntry, error)
	TokensForHistory(historyID string) (*TokenSummary, error)
}

// AgentMemoryStore provides access to per-agent key/value memories in a world database.
type AgentMemoryStore interface {
	SetAgentMemory(agentName, key, value string) error
	ListAgentMemories(agentName string) ([]AgentMemory, error)
	DeleteAgentMemory(agentName, key string) error
	CountAgentMemories(agentName string) (int, error)
	DeleteAllAgentMemories(agentName string) (int64, error)
}

// ——— Sphere-scoped interfaces ———

// AgentReader provides read access to agent records in the sphere database.
type AgentReader interface {
	GetAgent(id string) (*Agent, error)
	ListAgents(world string, state AgentState) ([]Agent, error)
	FindIdleAgent(world string) (*Agent, error)
}

// AgentWriter provides write access to agent records in the sphere database.
type AgentWriter interface {
	CreateAgent(name, world, role string) (string, error)
	EnsureAgent(name, world, role string) error
	UpdateAgentState(id string, state AgentState, activeWrit string) error
	DeleteAgent(id string) error
	DeleteAgentsForWorld(world string) error
}

// CaravanReader provides read access to caravan records in the sphere database.
type CaravanReader interface {
	GetCaravan(id string) (*Caravan, error)
	ListCaravans(status CaravanStatus) ([]Caravan, error)
	ListCaravanItems(caravanID string) ([]CaravanItem, error)
	GetCaravanItemsForWrit(writID string) ([]CaravanItem, error)
	CheckCaravanReadiness(caravanID string, worldOpener func(world string) (*Store, error)) ([]CaravanItemStatus, error)
}

// CaravanWriter provides write access to caravan records in the sphere database.
type CaravanWriter interface {
	CreateCaravan(name, owner string) (string, error)
	UpdateCaravanStatus(id string, status CaravanStatus) error
	CreateCaravanItem(caravanID, writID, world string, phase int) error
	RemoveCaravanItem(caravanID, writID string) error
	UpdateCaravanItemPhase(caravanID, writID string, phase int) error
	DeleteCaravan(id string) error
	TryCloseCaravan(caravanID string, worldOpener func(world string) (*Store, error)) (bool, error)
}

// CaravanDepReader provides read access to caravan dependency data in the sphere database.
type CaravanDepReader interface {
	GetCaravanDependencies(caravanID string) ([]string, error)
	GetCaravanDependents(caravanID string) ([]string, error)
	AreCaravanDependenciesSatisfied(caravanID string) (bool, error)
	UnsatisfiedCaravanDependencies(caravanID string) ([]string, error)
	IsWritBlockedByCaravanDeps(writID string) (bool, []string, error)
}

// CaravanDepWriter provides write access to caravan dependencies in the sphere database.
type CaravanDepWriter interface {
	AddCaravanDependency(fromID, toID string) error
	RemoveCaravanDependency(fromID, toID string) error
	DeleteCaravanDependencies(caravanID string) error
}

// MessageStore provides access to messages and protocol messages in the sphere database.
type MessageStore interface {
	SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error)
	SendMessageWithThread(sender, recipient, subject, body string, priority int, msgType, threadID string) (string, error)
	Inbox(recipient string) ([]Message, error)
	ReadMessage(id string) (*Message, error)
	AckMessage(id string) error
	ListMessages(filters MessageFilters) ([]Message, error)
	CountPending(recipient string) (int, error)
	HasPendingThreadMessage(threadID string) (bool, error)
	SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error)
	PendingProtocol(recipient, protoType string) ([]Message, error)
}

// EscalationStore provides access to escalation records in the sphere database.
type EscalationStore interface {
	CreateEscalation(severity, source, description string, sourceRef ...string) (string, error)
	GetEscalation(id string) (*Escalation, error)
	ListEscalations(status string) ([]Escalation, error)
	ListOpenEscalations() ([]Escalation, error)
	ListEscalationsBySourceRef(sourceRef string) ([]Escalation, error)
	AckEscalation(id string) error
	ResolveEscalation(id string) error
	UpdateEscalationLastNotified(id string) error
	CountOpen() (int, error)
}

// EscalationReader provides narrow read access to open escalations (subset of EscalationStore).
type EscalationReader interface {
	ListOpenEscalations() ([]Escalation, error)
}

// WorldRegistry provides access to the world registry in the sphere database.
type WorldRegistry interface {
	RegisterWorld(name, sourceRepo string) error
	GetWorld(name string) (*World, error)
	ListWorlds() ([]World, error)
	UpdateWorldRepo(name, sourceRepo string) error
	DeleteWorldData(world string) error
}

// WorldReader provides narrow read access to the world registry (subset of WorldRegistry).
type WorldReader interface {
	ListWorlds() ([]World, error)
}

// ——— Compile-time interface satisfaction checks ———

// WorldStore must satisfy all world-scoped interfaces.
var (
	_ WritReader        = (*WorldStore)(nil)
	_ WritWriter        = (*WorldStore)(nil)
	_ MRReader          = (*WorldStore)(nil)
	_ MergeRequestReader = (*WorldStore)(nil)
	_ MRWriter          = (*WorldStore)(nil)
	_ DepReader         = (*WorldStore)(nil)
	_ DepWriter         = (*WorldStore)(nil)
	_ LedgerReader      = (*WorldStore)(nil)
	_ LedgerWriter      = (*WorldStore)(nil)
	_ HistoryStore      = (*WorldStore)(nil)
	_ AgentMemoryStore  = (*WorldStore)(nil)
)

// SphereStore must satisfy all sphere-scoped interfaces.
var (
	_ AgentReader      = (*SphereStore)(nil)
	_ AgentWriter      = (*SphereStore)(nil)
	_ CaravanReader    = (*SphereStore)(nil)
	_ CaravanWriter    = (*SphereStore)(nil)
	_ CaravanDepReader = (*SphereStore)(nil)
	_ CaravanDepWriter = (*SphereStore)(nil)
	_ MessageStore     = (*SphereStore)(nil)
	_ EscalationStore  = (*SphereStore)(nil)
	_ EscalationReader = (*SphereStore)(nil)
	_ WorldRegistry    = (*SphereStore)(nil)
	_ WorldReader      = (*SphereStore)(nil)
)

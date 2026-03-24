package store

import (
	"fmt"
	"time"
)

// Compile-time interface satisfaction checks for UnimplementedWorldStore.
var (
	_ WritReader       = UnimplementedWorldStore{}
	_ WritWriter       = UnimplementedWorldStore{}
	_ MRReader         = UnimplementedWorldStore{}
	_ MRWriter         = UnimplementedWorldStore{}
	_ DepReader        = UnimplementedWorldStore{}
	_ DepWriter        = UnimplementedWorldStore{}
	_ HistoryStore     = UnimplementedWorldStore{}
	_ LedgerReader     = UnimplementedWorldStore{}
	_ LedgerWriter     = UnimplementedWorldStore{}
	_ AgentMemoryStore = UnimplementedWorldStore{}
)

// Compile-time interface satisfaction checks for UnimplementedSphereStore.
var (
	_ AgentReader      = UnimplementedSphereStore{}
	_ AgentWriter      = UnimplementedSphereStore{}
	_ CaravanReader    = UnimplementedSphereStore{}
	_ CaravanWriter    = UnimplementedSphereStore{}
	_ CaravanDepReader = UnimplementedSphereStore{}
	_ CaravanDepWriter = UnimplementedSphereStore{}
	_ MessageStore     = UnimplementedSphereStore{}
	_ EscalationStore  = UnimplementedSphereStore{}
	_ WorldRegistry    = UnimplementedSphereStore{}
)

// UnimplementedWorldStore implements every world-scoped store interface by
// returning an "unimplemented" error. Embed it in test mocks to avoid
// hand-writing stubs for methods the package under test never calls.
//
// Pattern:
//
//	type mockWorldStore struct {
//	    store.UnimplementedWorldStore
//	    // only override methods your mock actually uses
//	}
type UnimplementedWorldStore struct{}

// --- WritReader ---

func (UnimplementedWorldStore) GetWrit(id string) (*Writ, error) {
	return nil, fmt.Errorf("unimplemented: GetWrit")
}

func (UnimplementedWorldStore) ListWrits(filters ListFilters) ([]Writ, error) {
	return nil, fmt.Errorf("unimplemented: ListWrits")
}

func (UnimplementedWorldStore) ListChildWrits(parentID string) ([]Writ, error) {
	return nil, fmt.Errorf("unimplemented: ListChildWrits")
}

func (UnimplementedWorldStore) GetWritMetadata(id string) (map[string]any, error) {
	return nil, fmt.Errorf("unimplemented: GetWritMetadata")
}

func (UnimplementedWorldStore) ReadyWrits() ([]Writ, error) {
	return nil, fmt.Errorf("unimplemented: ReadyWrits")
}

// --- WritWriter ---

func (UnimplementedWorldStore) CreateWrit(title, description, createdBy string, priority int, labels []string) (string, error) {
	return "", fmt.Errorf("unimplemented: CreateWrit")
}

func (UnimplementedWorldStore) CreateWritWithOpts(opts CreateWritOpts) (string, error) {
	return "", fmt.Errorf("unimplemented: CreateWritWithOpts")
}

func (UnimplementedWorldStore) UpdateWrit(id string, updates WritUpdates) error {
	return fmt.Errorf("unimplemented: UpdateWrit")
}

func (UnimplementedWorldStore) CloseWrit(id string, closeReason ...string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: CloseWrit")
}

func (UnimplementedWorldStore) SetWritMetadata(id string, metadata map[string]any) error {
	return fmt.Errorf("unimplemented: SetWritMetadata")
}

func (UnimplementedWorldStore) AddLabel(itemID, label string) error {
	return fmt.Errorf("unimplemented: AddLabel")
}

func (UnimplementedWorldStore) RemoveLabel(itemID, label string) error {
	return fmt.Errorf("unimplemented: RemoveLabel")
}

// --- MRReader ---

func (UnimplementedWorldStore) GetMergeRequest(id string) (*MergeRequest, error) {
	return nil, fmt.Errorf("unimplemented: GetMergeRequest")
}

func (UnimplementedWorldStore) ListMergeRequests(phase MRPhase) ([]MergeRequest, error) {
	return nil, fmt.Errorf("unimplemented: ListMergeRequests")
}

func (UnimplementedWorldStore) ListMergeRequestsByWrit(writID string, phase MRPhase) ([]MergeRequest, error) {
	return nil, fmt.Errorf("unimplemented: ListMergeRequestsByWrit")
}

func (UnimplementedWorldStore) FindMergeRequestByBlocker(blockerID string) (*MergeRequest, error) {
	return nil, fmt.Errorf("unimplemented: FindMergeRequestByBlocker")
}

func (UnimplementedWorldStore) ListBlockedMergeRequests() ([]MergeRequest, error) {
	return nil, fmt.Errorf("unimplemented: ListBlockedMergeRequests")
}

// --- MRWriter ---

func (UnimplementedWorldStore) CreateMergeRequest(writID, branch string, priority int) (string, error) {
	return "", fmt.Errorf("unimplemented: CreateMergeRequest")
}

func (UnimplementedWorldStore) ClaimMergeRequest(claimerID string) (*MergeRequest, error) {
	return nil, fmt.Errorf("unimplemented: ClaimMergeRequest")
}

func (UnimplementedWorldStore) UpdateMergeRequestPhase(id string, phase MRPhase) error {
	return fmt.Errorf("unimplemented: UpdateMergeRequestPhase")
}

func (UnimplementedWorldStore) BlockMergeRequest(mrID, blockerWritID string) error {
	return fmt.Errorf("unimplemented: BlockMergeRequest")
}

func (UnimplementedWorldStore) UnblockMergeRequest(mrID string) error {
	return fmt.Errorf("unimplemented: UnblockMergeRequest")
}

func (UnimplementedWorldStore) ReleaseStaleClaims(ttl time.Duration) (int, error) {
	return 0, fmt.Errorf("unimplemented: ReleaseStaleClaims")
}

func (UnimplementedWorldStore) ResetMergeRequestForRetry(mrID string) error {
	return fmt.Errorf("unimplemented: ResetMergeRequestForRetry")
}

func (UnimplementedWorldStore) SupersedeFailedMRsForWrit(writID string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: SupersedeFailedMRsForWrit")
}

// --- DepReader ---

func (UnimplementedWorldStore) GetDependencies(itemID string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: GetDependencies")
}

func (UnimplementedWorldStore) GetDependents(itemID string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: GetDependents")
}

func (UnimplementedWorldStore) IsReady(itemID string) (bool, error) {
	return false, fmt.Errorf("unimplemented: IsReady")
}

func (UnimplementedWorldStore) HasOpenTransitiveDependents(writID string) (bool, error) {
	return false, fmt.Errorf("unimplemented: HasOpenTransitiveDependents")
}

// --- DepWriter ---

func (UnimplementedWorldStore) AddDependency(fromID, toID string) error {
	return fmt.Errorf("unimplemented: AddDependency")
}

func (UnimplementedWorldStore) RemoveDependency(fromID, toID string) error {
	return fmt.Errorf("unimplemented: RemoveDependency")
}

// --- HistoryStore ---

func (UnimplementedWorldStore) WriteHistory(agentName, writID, action, summary string, startedAt time.Time, endedAt *time.Time) (string, error) {
	return "", fmt.Errorf("unimplemented: WriteHistory")
}

func (UnimplementedWorldStore) GetHistory(id string) (*HistoryEntry, error) {
	return nil, fmt.Errorf("unimplemented: GetHistory")
}

func (UnimplementedWorldStore) ListHistory(agentName string) ([]HistoryEntry, error) {
	return nil, fmt.Errorf("unimplemented: ListHistory")
}

func (UnimplementedWorldStore) EndHistory(writID string) (string, error) {
	return "", fmt.Errorf("unimplemented: EndHistory")
}

func (UnimplementedWorldStore) HistoryForWrit(writID string) ([]HistoryEntry, error) {
	return nil, fmt.Errorf("unimplemented: HistoryForWrit")
}

// --- LedgerReader ---

func (UnimplementedWorldStore) TokensForHistory(historyID string) (*TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensForHistory")
}

func (UnimplementedWorldStore) AggregateTokens(agentName string) ([]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: AggregateTokens")
}

func (UnimplementedWorldStore) TokensForWrit(writID string) ([]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensForWrit")
}

func (UnimplementedWorldStore) TokensForWorld() ([]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensForWorld")
}

func (UnimplementedWorldStore) TokensByWritForAgent(agentName string) (map[string][]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensByWritForAgent")
}

func (UnimplementedWorldStore) TokensSince(since time.Time) ([]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensSince")
}

func (UnimplementedWorldStore) TokensByAgentForWorld() ([]AgentTokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensByAgentForWorld")
}

func (UnimplementedWorldStore) TokensByAgentSince(since time.Time) ([]AgentTokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensByAgentSince")
}

func (UnimplementedWorldStore) TokensByWritForAgentSince(agentName string, since time.Time) (map[string][]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensByWritForAgentSince")
}

func (UnimplementedWorldStore) WorldTokenMeta() (agents int, writs int, err error) {
	return 0, 0, fmt.Errorf("unimplemented: WorldTokenMeta")
}

func (UnimplementedWorldStore) WorldTokenMetaSince(since time.Time) (agents int, writs int, err error) {
	return 0, 0, fmt.Errorf("unimplemented: WorldTokenMetaSince")
}

func (UnimplementedWorldStore) TokensForWritSince(writID string, since time.Time) ([]TokenSummary, error) {
	return nil, fmt.Errorf("unimplemented: TokensForWritSince")
}

func (UnimplementedWorldStore) MergeStatsForAgent(agentName string) (AgentMergeRequestSummary, error) {
	return AgentMergeRequestSummary{}, fmt.Errorf("unimplemented: MergeStatsForAgent")
}

// --- LedgerWriter ---

func (UnimplementedWorldStore) WriteTokenUsage(historyID, model string, input, output, cacheRead, cacheCreation int64, costUSD *float64, durationMS *int64, runtime string) (string, error) {
	return "", fmt.Errorf("unimplemented: WriteTokenUsage")
}

// --- AgentMemoryStore ---

func (UnimplementedWorldStore) SetAgentMemory(agentName, key, value string) error {
	return fmt.Errorf("unimplemented: SetAgentMemory")
}

func (UnimplementedWorldStore) ListAgentMemories(agentName string) ([]AgentMemory, error) {
	return nil, fmt.Errorf("unimplemented: ListAgentMemories")
}

func (UnimplementedWorldStore) DeleteAgentMemory(agentName, key string) error {
	return fmt.Errorf("unimplemented: DeleteAgentMemory")
}

func (UnimplementedWorldStore) CountAgentMemories(agentName string) (int, error) {
	return 0, fmt.Errorf("unimplemented: CountAgentMemories")
}

func (UnimplementedWorldStore) DeleteAllAgentMemories(agentName string) (int64, error) {
	return 0, fmt.Errorf("unimplemented: DeleteAllAgentMemories")
}

func (UnimplementedWorldStore) Close() error {
	return fmt.Errorf("unimplemented: Close")
}

// UnimplementedSphereStore implements every sphere-scoped store interface by
// returning an "unimplemented" error. Embed it in test mocks to avoid
// hand-writing stubs for methods the package under test never calls.
//
// Pattern:
//
//	type mockSphereStore struct {
//	    store.UnimplementedSphereStore
//	    // only override methods your mock actually uses
//	}
type UnimplementedSphereStore struct{}

// --- AgentReader ---

func (UnimplementedSphereStore) GetAgent(id string) (*Agent, error) {
	return nil, fmt.Errorf("unimplemented: GetAgent")
}

func (UnimplementedSphereStore) ListAgents(world string, state AgentState) ([]Agent, error) {
	return nil, fmt.Errorf("unimplemented: ListAgents")
}

func (UnimplementedSphereStore) FindIdleAgent(world string) (*Agent, error) {
	return nil, fmt.Errorf("unimplemented: FindIdleAgent")
}

// --- AgentWriter ---

func (UnimplementedSphereStore) CreateAgent(name, world, role string) (string, error) {
	return "", fmt.Errorf("unimplemented: CreateAgent")
}

func (UnimplementedSphereStore) EnsureAgent(name, world, role string) error {
	return fmt.Errorf("unimplemented: EnsureAgent")
}

func (UnimplementedSphereStore) UpdateAgentState(id string, state AgentState, activeWrit string) error {
	return fmt.Errorf("unimplemented: UpdateAgentState")
}

func (UnimplementedSphereStore) DeleteAgent(id string) error {
	return fmt.Errorf("unimplemented: DeleteAgent")
}

func (UnimplementedSphereStore) DeleteAgentsForWorld(world string) error {
	return fmt.Errorf("unimplemented: DeleteAgentsForWorld")
}

// --- CaravanReader ---

func (UnimplementedSphereStore) GetCaravan(id string) (*Caravan, error) {
	return nil, fmt.Errorf("unimplemented: GetCaravan")
}

func (UnimplementedSphereStore) ListCaravans(status CaravanStatus) ([]Caravan, error) {
	return nil, fmt.Errorf("unimplemented: ListCaravans")
}

func (UnimplementedSphereStore) ListCaravanItems(caravanID string) ([]CaravanItem, error) {
	return nil, fmt.Errorf("unimplemented: ListCaravanItems")
}

func (UnimplementedSphereStore) GetCaravanItemsForWrit(writID string) ([]CaravanItem, error) {
	return nil, fmt.Errorf("unimplemented: GetCaravanItemsForWrit")
}

func (UnimplementedSphereStore) CheckCaravanReadiness(caravanID string, worldOpener func(world string) (*WorldStore, error)) ([]CaravanItemStatus, error) {
	return nil, fmt.Errorf("unimplemented: CheckCaravanReadiness")
}

// --- CaravanWriter ---

func (UnimplementedSphereStore) CreateCaravan(name, owner string) (string, error) {
	return "", fmt.Errorf("unimplemented: CreateCaravan")
}

func (UnimplementedSphereStore) UpdateCaravanStatus(id string, status CaravanStatus) error {
	return fmt.Errorf("unimplemented: UpdateCaravanStatus")
}

func (UnimplementedSphereStore) CreateCaravanItem(caravanID, writID, world string, phase int) error {
	return fmt.Errorf("unimplemented: CreateCaravanItem")
}

func (UnimplementedSphereStore) DeleteCaravanItemsForWorld(world string) error {
	return fmt.Errorf("unimplemented: DeleteCaravanItemsForWorld")
}

func (UnimplementedSphereStore) RemoveCaravanItem(caravanID, writID string) error {
	return fmt.Errorf("unimplemented: RemoveCaravanItem")
}

func (UnimplementedSphereStore) UpdateCaravanItemPhase(caravanID, writID string, phase int) error {
	return fmt.Errorf("unimplemented: UpdateCaravanItemPhase")
}

func (UnimplementedSphereStore) UpdateAllCaravanItemPhases(caravanID string, phase int) (int64, error) {
	return 0, fmt.Errorf("unimplemented: UpdateAllCaravanItemPhases")
}

func (UnimplementedSphereStore) DeleteCaravan(id string) error {
	return fmt.Errorf("unimplemented: DeleteCaravan")
}

func (UnimplementedSphereStore) TryCloseCaravan(caravanID string, worldOpener func(world string) (*WorldStore, error)) (bool, error) {
	return false, fmt.Errorf("unimplemented: TryCloseCaravan")
}

// --- CaravanDepReader ---

func (UnimplementedSphereStore) GetCaravanDependencies(caravanID string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: GetCaravanDependencies")
}

func (UnimplementedSphereStore) GetCaravanDependents(caravanID string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: GetCaravanDependents")
}

func (UnimplementedSphereStore) AreCaravanDependenciesSatisfied(caravanID string) (bool, error) {
	return false, fmt.Errorf("unimplemented: AreCaravanDependenciesSatisfied")
}

func (UnimplementedSphereStore) UnsatisfiedCaravanDependencies(caravanID string) ([]string, error) {
	return nil, fmt.Errorf("unimplemented: UnsatisfiedCaravanDependencies")
}

func (UnimplementedSphereStore) IsWritBlockedByCaravanDeps(writID string) (bool, []string, error) {
	return false, nil, fmt.Errorf("unimplemented: IsWritBlockedByCaravanDeps")
}

func (UnimplementedSphereStore) IsWritBlockedByCaravan(writID, world string, worldOpener func(world string) (*WorldStore, error)) (bool, error) {
	return false, fmt.Errorf("unimplemented: IsWritBlockedByCaravan")
}

// --- CaravanDepWriter ---

func (UnimplementedSphereStore) AddCaravanDependency(fromID, toID string) error {
	return fmt.Errorf("unimplemented: AddCaravanDependency")
}

func (UnimplementedSphereStore) RemoveCaravanDependency(fromID, toID string) error {
	return fmt.Errorf("unimplemented: RemoveCaravanDependency")
}

func (UnimplementedSphereStore) DeleteCaravanDependencies(caravanID string) error {
	return fmt.Errorf("unimplemented: DeleteCaravanDependencies")
}

// --- MessageStore ---

func (UnimplementedSphereStore) SendMessage(sender, recipient, subject, body string, priority int, msgType string) (string, error) {
	return "", fmt.Errorf("unimplemented: SendMessage")
}

func (UnimplementedSphereStore) SendMessageWithThread(sender, recipient, subject, body string, priority int, msgType, threadID string) (string, error) {
	return "", fmt.Errorf("unimplemented: SendMessageWithThread")
}

func (UnimplementedSphereStore) HasPendingThreadMessage(threadID string) (bool, error) {
	return false, fmt.Errorf("unimplemented: HasPendingThreadMessage")
}

func (UnimplementedSphereStore) Inbox(recipient string) ([]Message, error) {
	return nil, fmt.Errorf("unimplemented: Inbox")
}

func (UnimplementedSphereStore) ReadMessage(id string) (*Message, error) {
	return nil, fmt.Errorf("unimplemented: ReadMessage")
}

func (UnimplementedSphereStore) AckMessage(id string) error {
	return fmt.Errorf("unimplemented: AckMessage")
}

func (UnimplementedSphereStore) CountPending(recipient string) (int, error) {
	return 0, fmt.Errorf("unimplemented: CountPending")
}

func (UnimplementedSphereStore) ListMessages(filters MessageFilters) ([]Message, error) {
	return nil, fmt.Errorf("unimplemented: ListMessages")
}

func (UnimplementedSphereStore) CountAcked() (int, error) {
	return 0, fmt.Errorf("unimplemented: CountAcked")
}

func (UnimplementedSphereStore) CountAckedBefore(before time.Time) (int, error) {
	return 0, fmt.Errorf("unimplemented: CountAckedBefore")
}

func (UnimplementedSphereStore) PurgeAckedMessages(before time.Time) (int64, error) {
	return 0, fmt.Errorf("unimplemented: PurgeAckedMessages")
}

func (UnimplementedSphereStore) PurgeAllAcked() (int64, error) {
	return 0, fmt.Errorf("unimplemented: PurgeAllAcked")
}

func (UnimplementedSphereStore) SendProtocolMessage(sender, recipient, protoType string, payload any) (string, error) {
	return "", fmt.Errorf("unimplemented: SendProtocolMessage")
}

func (UnimplementedSphereStore) PendingProtocol(recipient, protoType string) ([]Message, error) {
	return nil, fmt.Errorf("unimplemented: PendingProtocol")
}

// --- EscalationStore ---

func (UnimplementedSphereStore) CreateEscalation(severity, source, description string, sourceRef ...string) (string, error) {
	return "", fmt.Errorf("unimplemented: CreateEscalation")
}

func (UnimplementedSphereStore) GetEscalation(id string) (*Escalation, error) {
	return nil, fmt.Errorf("unimplemented: GetEscalation")
}

func (UnimplementedSphereStore) ListEscalations(status EscalationStatus) ([]Escalation, error) {
	return nil, fmt.Errorf("unimplemented: ListEscalations")
}

func (UnimplementedSphereStore) ListOpenEscalations() ([]Escalation, error) {
	return nil, fmt.Errorf("unimplemented: ListOpenEscalations")
}

func (UnimplementedSphereStore) ListEscalationsBySourceRef(sourceRef string) ([]Escalation, error) {
	return nil, fmt.Errorf("unimplemented: ListEscalationsBySourceRef")
}

func (UnimplementedSphereStore) AckEscalation(id string) error {
	return fmt.Errorf("unimplemented: AckEscalation")
}

func (UnimplementedSphereStore) ResolveEscalation(id string) error {
	return fmt.Errorf("unimplemented: ResolveEscalation")
}

func (UnimplementedSphereStore) UpdateEscalationLastNotified(id string) error {
	return fmt.Errorf("unimplemented: UpdateEscalationLastNotified")
}

func (UnimplementedSphereStore) CountOpen() (int, error) {
	return 0, fmt.Errorf("unimplemented: CountOpen")
}

// --- WorldRegistry ---

func (UnimplementedSphereStore) RegisterWorld(name, sourceRepo string) error {
	return fmt.Errorf("unimplemented: RegisterWorld")
}

func (UnimplementedSphereStore) GetWorld(name string) (*World, error) {
	return nil, fmt.Errorf("unimplemented: GetWorld")
}

func (UnimplementedSphereStore) ListWorlds() ([]World, error) {
	return nil, fmt.Errorf("unimplemented: ListWorlds")
}

func (UnimplementedSphereStore) UpdateWorldRepo(name, sourceRepo string) error {
	return fmt.Errorf("unimplemented: UpdateWorldRepo")
}

func (UnimplementedSphereStore) DeleteWorldData(world string) error {
	return fmt.Errorf("unimplemented: DeleteWorldData")
}

func (UnimplementedSphereStore) Close() error {
	return fmt.Errorf("unimplemented: Close")
}

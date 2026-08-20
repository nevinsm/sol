package inbox

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// --- mock DataSource ---

type mockDataSource struct {
	escalations   []store.Escalation
	messages      []store.Message
	escErr        error
	msgErr        error
	ackedEsc      []string
	resolvedEsc   []string
	ackedMsg      []string
	readMsg       []string
	dismissedMsg  []string
	ackEscErr     error
	resolveEscErr error
	ackMsgErr     error
	readMsgErr    error
	dismissMsgErr error
}

func (m *mockDataSource) ListOpenEscalations() ([]store.Escalation, error) {
	return m.escalations, m.escErr
}

// Inbox mirrors the real store contract (internal/store.SphereStore.Inbox):
// an empty recipient returns everything, otherwise only messages addressed
// to that exact recipient. A fake that ignored recipient (returning
// m.messages unconditionally regardless of the argument) previously masked
// identity-scoping bugs in FetchItems — see writ-outputs for
// sol-68f7455088c6b04c.
func (m *mockDataSource) Inbox(recipient string) ([]store.Message, error) {
	if m.msgErr != nil {
		return nil, m.msgErr
	}
	if recipient == "" {
		return m.messages, nil
	}
	var out []store.Message
	for _, msg := range m.messages {
		if msg.Recipient == recipient {
			out = append(out, msg)
		}
	}
	return out, nil
}
func (m *mockDataSource) AckEscalation(id string) error {
	m.ackedEsc = append(m.ackedEsc, id)
	return m.ackEscErr
}
func (m *mockDataSource) ResolveEscalation(id string) error {
	m.resolvedEsc = append(m.resolvedEsc, id)
	return m.resolveEscErr
}
func (m *mockDataSource) AckMessage(id string) error {
	m.ackedMsg = append(m.ackedMsg, id)
	return m.ackMsgErr
}
func (m *mockDataSource) ReadMessage(id string) (*store.Message, error) {
	m.readMsg = append(m.readMsg, id)
	if m.readMsgErr != nil {
		return nil, m.readMsgErr
	}
	return &store.Message{ID: id}, nil
}
func (m *mockDataSource) DismissMessage(id string) error {
	m.dismissedMsg = append(m.dismissedMsg, id)
	return m.dismissMsgErr
}

// --- InboxItem tests ---

func TestItemTypeString(t *testing.T) {
	tests := []struct {
		name     string
		itemType ItemType
		want     string
	}{
		{"escalation", ItemEscalation, "escalation"},
		{"mail", ItemMail, "mail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := InboxItem{Type: tt.itemType}
			if got := item.TypeString(); got != tt.want {
				t.Errorf("TypeString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemAge(t *testing.T) {
	// Age returns a human-readable string from status.FormatDuration.
	// Just verify it returns a non-empty string for a known age.
	item := InboxItem{
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	age := item.Age()
	if age == "" {
		t.Error("expected non-empty age string")
	}
}

func TestEscalationPriority(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{"critical", 1},
		{"high", 2},
		{"medium", 3},
		{"low", 4},
		{"unknown", 3},
		{"", 3},
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			if got := escalationPriority(tt.severity); got != tt.want {
				t.Errorf("escalationPriority(%q) = %d, want %d", tt.severity, got, tt.want)
			}
		})
	}
}

// --- FetchItems tests ---

func TestFetchItemsSortsByPriorityThenDate(t *testing.T) {
	now := time.Now()

	src := &mockDataSource{
		escalations: []store.Escalation{
			{ID: "esc-low", Severity: "low", Source: "agent-a", Description: "low sev", CreatedAt: now.Add(-1 * time.Hour)},
			{ID: "esc-critical", Severity: "critical", Source: "agent-b", Description: "critical sev", CreatedAt: now.Add(-2 * time.Hour)},
		},
		messages: []store.Message{
			{ID: "msg-p2-old", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "hello", CreatedAt: now.Add(-3 * time.Hour)},
			{ID: "msg-p1-new", Priority: 1, Sender: "bob", Recipient: config.Autarch, Subject: "urgent", CreatedAt: now.Add(-30 * time.Minute)},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	// Verify sorted by priority ASC, then created_at ASC.
	if !sort.SliceIsSorted(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	}) {
		t.Errorf("items not sorted correctly by priority then date")
		for i, item := range items {
			t.Logf("  [%d] ID=%s Priority=%d Created=%s", i, item.ID, item.Priority, item.CreatedAt.Format(time.RFC3339))
		}
	}

	// P1 items should come first.
	if items[0].Priority != 1 {
		t.Errorf("first item should be priority 1, got %d (ID=%s)", items[0].Priority, items[0].ID)
	}
}

func TestFetchItemsDeduplicatesEscalationThreads(t *testing.T) {
	now := time.Now()

	src := &mockDataSource{
		escalations: []store.Escalation{
			{ID: "esc-001", Severity: "high", Source: "sentinel", Description: "agent stalled", CreatedAt: now},
		},
		messages: []store.Message{
			// This message is a notification duplicate of an escalation (ThreadID starts with "esc:").
			{ID: "msg-dup", Priority: 1, Sender: "sentinel", Recipient: config.Autarch, Subject: "[ESCALATION-high]", ThreadID: "esc:esc-001", CreatedAt: now},
			// This message is a regular mail.
			{ID: "msg-real", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "hello", ThreadID: "", CreatedAt: now},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 items: the escalation + the non-duplicate message.
	if len(items) != 2 {
		t.Fatalf("expected 2 items (dedup should remove esc-threaded message), got %d", len(items))
	}

	// Verify the duplicate message was filtered out.
	for _, item := range items {
		if item.ID == "msg-dup" {
			t.Error("expected msg-dup to be filtered out as escalation thread duplicate")
		}
	}
}

func TestFetchItemsDedupOnlyListedEscalations(t *testing.T) {
	// V7: dedup of esc:-prefix mail must be scoped to escalations that
	// are currently listed in the same FetchItems call. An orphan esc:-
	// prefix mail (e.g. notification for an escalation that was resolved
	// out from under us, or any future esc:-prefix usage) is preserved.
	now := time.Now()

	src := &mockDataSource{
		escalations: []store.Escalation{
			// Only esc-listed appears in the open escalations list.
			{ID: "esc-listed", Severity: "high", Source: "sentinel", Description: "listed escalation", CreatedAt: now},
		},
		messages: []store.Message{
			// Notification thread for the listed escalation — duplicate, drop.
			{ID: "msg-listed-dup", Priority: 1, Sender: "sentinel", Recipient: config.Autarch, Subject: "[ESCALATION-high]", ThreadID: "esc:esc-listed", CreatedAt: now},
			// Notification thread for an escalation NOT in the listed set
			// (orphan / unrelated future use of the esc: prefix). Must be preserved.
			{ID: "msg-orphan", Priority: 1, Sender: "sentinel", Recipient: config.Autarch, Subject: "[ESCALATION-high]", ThreadID: "esc:esc-orphan", CreatedAt: now},
			// Regular mail with no thread — unaffected.
			{ID: "msg-real", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "hello", ThreadID: "", CreatedAt: now},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected: escalation (esc-listed) + msg-orphan + msg-real = 3 items.
	// msg-listed-dup is the only one elided.
	if len(items) != 3 {
		ids := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		t.Fatalf("expected 3 items, got %d (%v)", len(items), ids)
	}

	var sawListedDup, sawOrphan, sawReal, sawEscListed bool
	for _, item := range items {
		switch item.ID {
		case "msg-listed-dup":
			sawListedDup = true
		case "msg-orphan":
			sawOrphan = true
		case "msg-real":
			sawReal = true
		case "esc-listed":
			sawEscListed = true
		}
	}
	if sawListedDup {
		t.Error("expected msg-listed-dup to be deduplicated (matches a listed escalation)")
	}
	if !sawOrphan {
		t.Error("expected msg-orphan (esc:-prefix not matching any listed escalation) to be preserved")
	}
	if !sawReal {
		t.Error("expected msg-real (no thread) to be preserved")
	}
	if !sawEscListed {
		t.Error("expected esc-listed to appear as an escalation item")
	}
}

func TestFetchItemsEscFallthroughWhenEscFetchFails(t *testing.T) {
	// When the escalation list cannot be fetched, the listed-set is empty
	// and esc:-prefix mail must NOT be silently elided. The operator
	// should still see whatever mail is available, including any esc:-
	// threaded notifications, since they cannot be deduplicated against
	// an unknown escalation set.
	now := time.Now()

	src := &mockDataSource{
		escErr: errTestSentinel,
		messages: []store.Message{
			{ID: "msg-esc-threaded", Priority: 1, Sender: "sentinel", Recipient: config.Autarch, Subject: "[ESCALATION-high]", ThreadID: "esc:esc-anything", CreatedAt: now},
			{ID: "msg-real", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "hello", ThreadID: "", CreatedAt: now},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err == nil {
		t.Error("expected error when escalation fetch fails")
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 mail items when escalation fetch fails, got %d", len(items))
	}
}

func TestFetchItemsEmptySources(t *testing.T) {
	src := &mockDataSource{}
	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items from empty sources, got %d", len(items))
	}
}

func TestFetchItemsPartialErrorReturnsItems(t *testing.T) {
	// FetchItems returns available items even when one source errors,
	// but the error is surfaced rather than silently swallowed.
	src := &mockDataSource{
		escErr: errTestSentinel,
		messages: []store.Message{
			{ID: "msg-1", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "hello", CreatedAt: time.Now()},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err == nil {
		t.Error("expected error when escalation fetch fails")
	}

	// Even though escalation fetch failed, messages should still appear.
	if len(items) != 1 {
		t.Fatalf("expected 1 item when escalation fetch fails, got %d", len(items))
	}
	if items[0].ID != "msg-1" {
		t.Errorf("expected item ID 'msg-1', got %q", items[0].ID)
	}
}

func TestFetchItemsBothErrors(t *testing.T) {
	src := &mockDataSource{
		escErr: errTestSentinel,
		msgErr: errTestSentinel,
	}

	items, err := FetchItems(src, config.Autarch)
	if err == nil {
		t.Error("expected error when both fetches fail")
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items when both fetches fail, got %d", len(items))
	}
}

func TestFetchItemsEscalationFields(t *testing.T) {
	now := time.Now()
	esc := store.Escalation{
		ID:          "esc-abc",
		Severity:    "high",
		Source:      "haven/sentinel",
		Description: "agent stalled for 30 min",
		Status:      "open",
		CreatedAt:   now,
	}

	src := &mockDataSource{escalations: []store.Escalation{esc}}
	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	item := items[0]
	if item.Type != ItemEscalation {
		t.Errorf("expected ItemEscalation, got %d", item.Type)
	}
	if item.ID != "esc-abc" {
		t.Errorf("expected ID 'esc-abc', got %q", item.ID)
	}
	if item.Priority != 2 {
		t.Errorf("expected priority 2 for high severity, got %d", item.Priority)
	}
	if item.Source != "haven/sentinel" {
		t.Errorf("expected source 'haven/sentinel', got %q", item.Source)
	}
	if item.Description != "agent stalled for 30 min" {
		t.Errorf("expected description to match, got %q", item.Description)
	}
	if item.Escalation == nil {
		t.Error("expected Escalation to be non-nil")
	}
	if item.Message != nil {
		t.Error("expected Message to be nil for escalation item")
	}
}

func TestFetchItemsMessageFields(t *testing.T) {
	now := time.Now()
	msg := store.Message{
		ID:        "msg-xyz",
		Sender:    "bob",
		Recipient: config.Autarch,
		Subject:   "deployment ready",
		Priority:  1,
		Type:      "notification",
		ThreadID:  "",
		CreatedAt: now,
	}

	src := &mockDataSource{messages: []store.Message{msg}}
	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	item := items[0]
	if item.Type != ItemMail {
		t.Errorf("expected ItemMail, got %d", item.Type)
	}
	if item.ID != "msg-xyz" {
		t.Errorf("expected ID 'msg-xyz', got %q", item.ID)
	}
	if item.Priority != 1 {
		t.Errorf("expected priority 1, got %d", item.Priority)
	}
	if item.Source != "bob" {
		t.Errorf("expected source 'bob', got %q", item.Source)
	}
	if item.Message == nil {
		t.Error("expected Message to be non-nil")
	}
	if item.Escalation != nil {
		t.Error("expected Escalation to be nil for mail item")
	}
}

func TestFetchItemsSamePrioritySortsByDate(t *testing.T) {
	now := time.Now()

	src := &mockDataSource{
		messages: []store.Message{
			{ID: "msg-new", Priority: 2, Sender: "a", Recipient: config.Autarch, Subject: "new", CreatedAt: now},
			{ID: "msg-old", Priority: 2, Sender: "b", Recipient: config.Autarch, Subject: "old", CreatedAt: now.Add(-1 * time.Hour)},
			{ID: "msg-mid", Priority: 2, Sender: "c", Recipient: config.Autarch, Subject: "mid", CreatedAt: now.Add(-30 * time.Minute)},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Same priority: oldest first.
	if items[0].ID != "msg-old" {
		t.Errorf("expected oldest first, got %q", items[0].ID)
	}
	if items[1].ID != "msg-mid" {
		t.Errorf("expected middle second, got %q", items[1].ID)
	}
	if items[2].ID != "msg-new" {
		t.Errorf("expected newest last, got %q", items[2].ID)
	}
}

// --- Identity scoping (non-autarch FetchItems) ---

// TestFetchItemsNonAutarchIdentityOwnMailOnly verifies a non-autarch
// identity sees only its own pending mail, not the autarch's or another
// identity's, and never sees escalations.
func TestFetchItemsNonAutarchIdentityOwnMailOnly(t *testing.T) {
	now := time.Now()

	src := &mockDataSource{
		escalations: []store.Escalation{
			{ID: "esc-1", Severity: "high", Source: "sentinel", Description: "stalled", CreatedAt: now},
		},
		messages: []store.Message{
			{ID: "msg-autarch", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "for the operator", CreatedAt: now},
			{ID: "msg-mine", Priority: 2, Sender: "bob", Recipient: "sol-dev/Nova", Subject: "for me", CreatedAt: now},
			{ID: "msg-other", Priority: 2, Sender: "carol", Recipient: "sol-dev/Polaris", Subject: "for someone else", CreatedAt: now},
		},
	}

	items, err := FetchItems(src, "sol-dev/Nova")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 1 {
		ids := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		t.Fatalf("expected 1 item (own mail only), got %d (%v)", len(items), ids)
	}
	if items[0].ID != "msg-mine" {
		t.Errorf("expected msg-mine, got %q", items[0].ID)
	}
	if items[0].Type != ItemMail {
		t.Errorf("expected ItemMail, got %d", items[0].Type)
	}

	// ListOpenEscalations must not even be consulted for a non-autarch
	// identity — escalations are autarch-directed.
	if len(src.escalations) != 1 {
		t.Fatalf("test setup error: expected 1 escalation in fixture")
	}
	for _, item := range items {
		if item.Type == ItemEscalation {
			t.Error("expected no escalation items for a non-autarch identity")
		}
	}
}

// TestFetchItemsAutarchIdentityUnchanged verifies identity == config.Autarch
// still returns escalations plus autarch mail, and does not leak other
// identities' mail.
func TestFetchItemsAutarchIdentityUnchanged(t *testing.T) {
	now := time.Now()

	src := &mockDataSource{
		escalations: []store.Escalation{
			{ID: "esc-1", Severity: "high", Source: "sentinel", Description: "stalled", CreatedAt: now},
		},
		messages: []store.Message{
			{ID: "msg-autarch", Priority: 2, Sender: "alice", Recipient: config.Autarch, Subject: "for the operator", CreatedAt: now},
			{ID: "msg-other", Priority: 2, Sender: "carol", Recipient: "sol-dev/Polaris", Subject: "for someone else", CreatedAt: now},
		},
	}

	items, err := FetchItems(src, config.Autarch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		ids := make([]string, len(items))
		for i, item := range items {
			ids[i] = item.ID
		}
		t.Fatalf("expected 2 items (escalation + autarch mail), got %d (%v)", len(items), ids)
	}

	var sawEsc, sawAutarchMail bool
	for _, item := range items {
		switch item.ID {
		case "esc-1":
			sawEsc = true
		case "msg-autarch":
			sawAutarchMail = true
		case "msg-other":
			t.Error("expected msg-other (another identity's mail) to be excluded from the autarch view")
		}
	}
	if !sawEsc {
		t.Error("expected the escalation to be present for the autarch identity")
	}
	if !sawAutarchMail {
		t.Error("expected the autarch's own mail to be present")
	}
}

// --- Model state tests ---

func TestNewModelInitialState(t *testing.T) {
	m := NewModel(Config{})

	if m.view != viewList {
		t.Errorf("expected initial view to be viewList, got %d", m.view)
	}
	if m.cursor != 0 {
		t.Errorf("expected initial cursor 0, got %d", m.cursor)
	}
	if m.ready {
		t.Error("expected ready to be false initially")
	}
	if m.highlights == nil {
		t.Error("expected highlights map to be initialized")
	}
	if len(m.items) != 0 {
		t.Errorf("expected 0 initial items, got %d", len(m.items))
	}
}

func TestDecayHighlights(t *testing.T) {
	m := NewModel(Config{})

	m.highlights["item-a"] = 5
	m.highlights["item-b"] = 2
	m.highlights["item-c"] = 1

	m.decayHighlights()

	if m.highlights["item-a"] != 4 {
		t.Errorf("expected item-a level 4, got %d", m.highlights["item-a"])
	}
	if m.highlights["item-b"] != 1 {
		t.Errorf("expected item-b level 1, got %d", m.highlights["item-b"])
	}
	if _, exists := m.highlights["item-c"]; exists {
		t.Error("expected item-c to be removed at level 1")
	}
}

func TestDecayHighlightsFullDecay(t *testing.T) {
	m := NewModel(Config{})
	m.highlights["item-a"] = 3

	// Decay 3 times: 3 → 2 → 1 → removed.
	for i := 0; i < 3; i++ {
		m.decayHighlights()
	}

	if len(m.highlights) != 0 {
		t.Errorf("expected all highlights removed after full decay, got %d remaining", len(m.highlights))
	}
}

func TestDecayHighlightsEmpty(t *testing.T) {
	m := NewModel(Config{})
	// Should not panic on empty map.
	m.decayHighlights()
	if len(m.highlights) != 0 {
		t.Errorf("expected 0 highlights, got %d", len(m.highlights))
	}
}

func TestListKeysCursorBounds(t *testing.T) {
	m := NewModel(Config{})
	m.items = makeTestItems(3)

	// Start at 0 — up should not go negative.
	m.updateListKeys(keyMsg("up"))
	if m.cursor != 0 {
		t.Errorf("expected cursor to stay at 0 on up, got %d", m.cursor)
	}

	// Move down to 1, 2.
	m.updateListKeys(keyMsg("down"))
	if m.cursor != 1 {
		t.Errorf("expected cursor 1 after down, got %d", m.cursor)
	}

	m.updateListKeys(keyMsg("down"))
	if m.cursor != 2 {
		t.Errorf("expected cursor 2 after second down, got %d", m.cursor)
	}

	// At last item — down should not go past end.
	m.updateListKeys(keyMsg("down"))
	if m.cursor != 2 {
		t.Errorf("expected cursor to stay at 2 on down past end, got %d", m.cursor)
	}
}

func TestListKeysVimBindings(t *testing.T) {
	m := NewModel(Config{})
	m.items = makeTestItems(3)

	m.updateListKeys(keyMsg("j"))
	if m.cursor != 1 {
		t.Errorf("expected cursor 1 after j, got %d", m.cursor)
	}

	m.updateListKeys(keyMsg("k"))
	if m.cursor != 0 {
		t.Errorf("expected cursor 0 after k, got %d", m.cursor)
	}
}

func TestViewModeTransition(t *testing.T) {
	m := NewModel(Config{})
	m.items = makeTestItems(2)

	// Initial state: list view.
	if m.view != viewList {
		t.Fatalf("expected viewList, got %d", m.view)
	}

	// Enter -> detail view.
	m.updateListKeys(keyMsg("enter"))
	if m.view != viewDetail {
		t.Errorf("expected viewDetail after enter, got %d", m.view)
	}

	// Esc -> back to list.
	m.updateDetailKeys(keyMsg("esc"))
	if m.view != viewList {
		t.Errorf("expected viewList after esc, got %d", m.view)
	}

	// Enter -> detail.
	m.updateListKeys(keyMsg("enter"))
	if m.view != viewDetail {
		t.Errorf("expected viewDetail after enter, got %d", m.view)
	}

	// Backspace -> back to list.
	m.updateDetailKeys(keyMsg("backspace"))
	if m.view != viewList {
		t.Errorf("expected viewList after backspace, got %d", m.view)
	}
}

func TestViewModeEnterWithEmptyItems(t *testing.T) {
	m := NewModel(Config{})
	// No items — enter should NOT switch to detail view.
	m.updateListKeys(keyMsg("enter"))
	if m.view != viewList {
		t.Errorf("expected viewList with no items, got %d", m.view)
	}
}

func TestRefreshMsgTransitionsToListViewWhenItemsRemoved(t *testing.T) {
	m := NewModel(Config{})
	m.items = makeTestItems(1)
	m.cursor = 0
	m.view = viewDetail
	m.ready = true

	// Simulate a refresh that removes all items (e.g. last escalation resolved).
	raw, _ := m.Update(refreshMsg{items: []InboxItem{}})
	updated := raw.(Model)

	if updated.view != viewList {
		t.Errorf("expected view to transition to viewList after all items removed, got %d", updated.view)
	}
	if updated.cursor != 0 {
		t.Errorf("expected cursor clamped to 0, got %d", updated.cursor)
	}
}

func TestRefreshMsgKeepsDetailViewWhenItemsRemain(t *testing.T) {
	m := NewModel(Config{})
	m.items = makeTestItems(3)
	m.cursor = 1
	m.view = viewDetail
	m.pinnedID = "item-1" // pinned by ID, as updateListKeys' "enter" would set it
	m.ready = true

	// Simulate a refresh that still has items at cursor position.
	raw, _ := m.Update(refreshMsg{items: makeTestItems(3)})
	updated := raw.(Model)

	if updated.view != viewDetail {
		t.Errorf("expected view to remain viewDetail when items still present, got %d", updated.view)
	}
	if updated.cursor != 1 {
		t.Errorf("expected cursor to remain at 1, got %d", updated.cursor)
	}
	if updated.pinnedID != "item-1" {
		t.Errorf("expected pinnedID to remain 'item-1', got %q", updated.pinnedID)
	}
}

// TestRefreshMsgPinnedItemSurvivesUnrelatedRemoval is the core acceptance
// criterion for pin-by-ID: with detail view open on a lower-priority item,
// a refresh that removes a DIFFERENT, higher-priority item (e.g. acked
// elsewhere) must not change which item the detail view shows, even
// though cursor-index-based lookups would silently shift.
func TestRefreshMsgPinnedItemSurvivesUnrelatedRemoval(t *testing.T) {
	m := NewModel(Config{})
	before := []InboxItem{
		{ID: "esc-urgent", Type: ItemEscalation, Priority: 1, Description: "urgent"},
		{ID: "msg-mine", Type: ItemMail, Priority: 3, Description: "pinned one"},
	}
	m.items = before
	m.cursor = 1
	m.view = viewDetail
	m.pinnedID = "msg-mine"
	m.ready = true

	// esc-urgent (a HIGHER priority item, earlier in the list) is resolved
	// elsewhere and disappears; msg-mine remains.
	after := []InboxItem{
		{ID: "msg-mine", Type: ItemMail, Priority: 3, Description: "pinned one"},
	}
	raw, _ := m.Update(refreshMsg{items: after})
	updated := raw.(Model)

	if updated.view != viewDetail {
		t.Fatalf("expected view to remain viewDetail, got %d", updated.view)
	}
	if updated.pinnedID != "msg-mine" {
		t.Errorf("expected pinnedID to remain 'msg-mine', got %q", updated.pinnedID)
	}
	shown, ok := findItemByID(updated.items, updated.pinnedID)
	if !ok || shown.ID != "msg-mine" {
		t.Errorf("expected detail view to still show msg-mine, got %+v (ok=%v)", shown, ok)
	}
}

// TestRefreshMsgPinnedItemGoneReturnsToListWithNotice covers the other
// half: when the pinned item itself disappears, the model drops back to
// the list view and surfaces a dim notice rather than silently rendering
// whatever now occupies the old cursor position.
func TestRefreshMsgPinnedItemGoneReturnsToListWithNotice(t *testing.T) {
	m := NewModel(Config{})
	m.items = []InboxItem{{ID: "esc-1", Type: ItemEscalation, Priority: 1}}
	m.cursor = 0
	m.view = viewDetail
	m.pinnedID = "esc-1"
	m.ready = true

	raw, _ := m.Update(refreshMsg{items: []InboxItem{}})
	updated := raw.(Model)

	if updated.view != viewList {
		t.Errorf("expected view to fall back to viewList, got %d", updated.view)
	}
	if updated.pinnedID != "" {
		t.Errorf("expected pinnedID cleared, got %q", updated.pinnedID)
	}
	if updated.detailNotice == "" {
		t.Error("expected a non-empty detailNotice when the pinned item disappears")
	}
}

// --- Style helper tests ---

func TestPadRight(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  int // expected visible width
	}{
		{"shorter than width", "abc", 10, 10},
		{"exact width", "abcde", 5, 5},
		{"longer than width", "abcdefgh", 5, 8}, // no truncation
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := padRight(tt.input, tt.width)
			// For plain strings, len == visible width.
			if len(result) != tt.want {
				t.Errorf("padRight(%q, %d) visible width = %d, want %d", tt.input, tt.width, len(result), tt.want)
			}
		})
	}
}

// truncateStr moved to internal/style.TruncateRunes — see
// internal/style/style_test.go for its coverage.

func TestHighlightAtLevel(t *testing.T) {
	tests := []struct {
		level    int
		hasColor bool
	}{
		{0, false},
		{-1, false},
		{6, false},
		{1, true},
		{5, true},
	}
	for _, tt := range tests {
		style := highlightAtLevel(tt.level)
		// Can't easily inspect lipgloss internals, but verify it doesn't panic.
		_ = style.Render("test")
	}
}

// --- View helper tests ---

func TestRenderHeader(t *testing.T) {
	tests := []struct {
		identity string
		count    int
		want     string // substring to check
	}{
		{"autarch", 0, "0 items"},
		{"autarch", 1, "1 item"},
		{"autarch", 5, "5 items"},
		{"autarch", 5, "autarch"},
		{"", 3, "3 items"},
	}
	for _, tt := range tests {
		result := renderHeader(tt.identity, tt.count)
		if len(result) == 0 {
			t.Errorf("renderHeader(%q, %d) returned empty string", tt.identity, tt.count)
		}
		if !strings.Contains(result, tt.want) {
			t.Errorf("renderHeader(%q, %d) = %q, want substring %q", tt.identity, tt.count, result, tt.want)
		}
	}
}

func TestSeverityStyled(t *testing.T) {
	tests := []struct {
		severity string
	}{
		{"critical"},
		{"high"},
		{"medium"},
		{"low"},
		{"unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := severityStyled(tt.severity)
			if result == "" {
				t.Errorf("severityStyled(%q) returned empty string", tt.severity)
			}
		})
	}
}

func TestWrapIndent(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		indent int
		width  int
	}{
		{"simple line", "hello world", 4, 80},
		{"empty text", "", 4, 80},
		{"multiline", "line one\nline two\nline three", 2, 80},
		{"narrow width forces wrap", "this is a longer line that should wrap at narrow widths", 4, 30},
		{"very narrow defaults to 20", "some text", 4, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := wrapIndent(tt.text, tt.indent, tt.width)
			// Should not panic and should produce output for non-empty input.
			if tt.text != "" && result == "" {
				t.Errorf("wrapIndent produced empty output for non-empty text")
			}
		})
	}
}

// --- Action command tests ---

// TestListKeysResolveNoOpOnMailSelection covers the context-sensitive
// footer change: [r]esolve is only advertised (and only wired) for an
// escalation selection. Since resolveCmd/dismissCmd no longer carry their
// own "only applies to X" error path (that path is now unreachable through
// the UI), the guard lives in updateListKeys — pressing "r" on a mail
// selection must be a silent no-op, not an error banner.
func TestListKeysResolveNoOpOnMailSelection(t *testing.T) {
	src := &mockDataSource{}
	m := NewModel(Config{Store: src})
	m.items = []InboxItem{{ID: "msg-1", Type: ItemMail}}
	m.ready = true

	cmd := m.updateListKeys(keyMsg("r"))
	if cmd != nil {
		t.Fatal("expected nil cmd for resolve on a mail selection")
	}
	if len(src.resolvedEsc) != 0 {
		t.Errorf("expected ResolveEscalation not called, got %v", src.resolvedEsc)
	}
}

// TestListKeysDismissNoOpOnEscalationSelection is the mirror of
// TestListKeysResolveNoOpOnMailSelection for [d]ismiss, which now only
// applies to a mail selection.
func TestListKeysDismissNoOpOnEscalationSelection(t *testing.T) {
	src := &mockDataSource{}
	m := NewModel(Config{Store: src})
	m.items = []InboxItem{{ID: "esc-1", Type: ItemEscalation}}
	m.ready = true

	cmd := m.updateListKeys(keyMsg("d"))
	if cmd != nil {
		t.Fatal("expected nil cmd for dismiss on an escalation selection")
	}
	if len(src.dismissedMsg) != 0 {
		t.Errorf("expected DismissMessage not called, got %v", src.dismissedMsg)
	}
}

// TestDetailKeysResolveNoOpOnMailSelection mirrors the list-view guard for
// the pinned detail view.
func TestDetailKeysResolveNoOpOnMailSelection(t *testing.T) {
	src := &mockDataSource{}
	m := NewModel(Config{Store: src})
	m.items = []InboxItem{{ID: "msg-1", Type: ItemMail}}
	m.pinnedID = "msg-1"
	m.view = viewDetail
	m.ready = true

	cmd := m.updateDetailKeys(keyMsg("r"))
	if cmd != nil {
		t.Fatal("expected nil cmd for resolve on a mail selection in detail view")
	}
	if len(src.resolvedEsc) != 0 {
		t.Errorf("expected ResolveEscalation not called, got %v", src.resolvedEsc)
	}
}

// TestDetailKeysDismissNoOpOnEscalationSelection mirrors the list-view
// guard for the pinned detail view.
func TestDetailKeysDismissNoOpOnEscalationSelection(t *testing.T) {
	src := &mockDataSource{}
	m := NewModel(Config{Store: src})
	m.items = []InboxItem{{ID: "esc-1", Type: ItemEscalation}}
	m.pinnedID = "esc-1"
	m.view = viewDetail
	m.ready = true

	cmd := m.updateDetailKeys(keyMsg("d"))
	if cmd != nil {
		t.Fatal("expected nil cmd for dismiss on an escalation selection in detail view")
	}
	if len(src.dismissedMsg) != 0 {
		t.Errorf("expected DismissMessage not called, got %v", src.dismissedMsg)
	}
}

func TestAckCmdEscalation(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemEscalation, ID: "esc-1"}
	cmd := ackCmd(src, item, nil)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	// Execute the command to trigger the mock.
	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.itemID != "esc-1" {
		t.Errorf("expected itemID 'esc-1', got %q", result.itemID)
	}
	if result.action != "ack" {
		t.Errorf("expected action 'ack', got %q", result.action)
	}
	if result.err != nil {
		t.Errorf("expected no error, got %v", result.err)
	}

	if len(src.ackedEsc) != 1 || src.ackedEsc[0] != "esc-1" {
		t.Errorf("expected AckEscalation called with 'esc-1', got %v", src.ackedEsc)
	}
}

func TestAckCmdMessage(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemMail, ID: "msg-1"}
	cmd := ackCmd(src, item, nil)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()
	result := msg.(actionResultMsg)
	if result.action != "ack" {
		t.Errorf("expected action 'ack', got %q", result.action)
	}

	if len(src.ackedMsg) != 1 || src.ackedMsg[0] != "msg-1" {
		t.Errorf("expected AckMessage called with 'msg-1', got %v", src.ackedMsg)
	}
}

func TestResolveCmdEscalation(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemEscalation, ID: "esc-2"}
	cmd := resolveCmd(src, item, nil)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for resolve on escalation")
	}

	msg := cmd()
	result := msg.(actionResultMsg)
	if result.action != "resolve" {
		t.Errorf("expected action 'resolve', got %q", result.action)
	}

	if len(src.resolvedEsc) != 1 || src.resolvedEsc[0] != "esc-2" {
		t.Errorf("expected ResolveEscalation called with 'esc-2', got %v", src.resolvedEsc)
	}
}

func TestDismissCmdMessage(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemMail, ID: "msg-2"}
	cmd := dismissCmd(src, item)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for dismiss on mail")
	}

	msg := cmd()
	result := msg.(actionResultMsg)
	if result.action != "dismiss" {
		t.Errorf("expected action 'dismiss', got %q", result.action)
	}

	if len(src.dismissedMsg) != 1 || src.dismissedMsg[0] != "msg-2" {
		t.Errorf("expected DismissMessage called with 'msg-2', got %v", src.dismissedMsg)
	}
}

// --- Action error propagation ---

func TestAckCmdEscalationStoreError(t *testing.T) {
	src := &mockDataSource{ackEscErr: errTestSentinel}
	item := InboxItem{Type: ItemEscalation, ID: "esc-err-1"}
	cmd := ackCmd(src, item, nil)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.err == nil {
		t.Fatal("expected error in actionResultMsg when store returns error")
	}
	if result.err != errTestSentinel {
		t.Errorf("expected errTestSentinel, got %v", result.err)
	}
	if result.itemID != "esc-err-1" {
		t.Errorf("expected itemID 'esc-err-1', got %q", result.itemID)
	}
}

func TestResolveCmdEscalationStoreError(t *testing.T) {
	src := &mockDataSource{resolveEscErr: errTestSentinel}
	item := InboxItem{Type: ItemEscalation, ID: "esc-err-2"}
	cmd := resolveCmd(src, item, nil)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.err == nil {
		t.Fatal("expected error in actionResultMsg when store returns error")
	}
	if result.err != errTestSentinel {
		t.Errorf("expected errTestSentinel, got %v", result.err)
	}
	if result.itemID != "esc-err-2" {
		t.Errorf("expected itemID 'esc-err-2', got %q", result.itemID)
	}
}

func TestDismissCmdMessageStoreError(t *testing.T) {
	src := &mockDataSource{dismissMsgErr: errTestSentinel}
	item := InboxItem{Type: ItemMail, ID: "msg-err-1"}
	cmd := dismissCmd(src, item)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.err == nil {
		t.Fatal("expected error in actionResultMsg when store returns error")
	}
	if result.err != errTestSentinel {
		t.Errorf("expected errTestSentinel, got %v", result.err)
	}
	if result.itemID != "msg-err-1" {
		t.Errorf("expected itemID 'msg-err-1', got %q", result.itemID)
	}
}

// --- ReadMessage on view tests (V6) ---

func TestEnterMailItemMarksAsRead(t *testing.T) {
	// V6 acceptance: opening a mail item via the model's view action
	// must call ReadMessage on the underlying store, and the store must
	// record the call.
	src := &mockDataSource{}
	m := NewModel(Config{Store: src})
	m.items = []InboxItem{
		{ID: "msg-1", Type: ItemMail, Source: "alice", Description: "hello"},
	}
	m.ready = true

	cmd := m.updateListKeys(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from enter on mail item")
	}

	// Execute the command — this is what the Bubble Tea runtime would do.
	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.action != "read" {
		t.Errorf("expected action 'read', got %q", result.action)
	}
	if result.itemID != "msg-1" {
		t.Errorf("expected itemID 'msg-1', got %q", result.itemID)
	}
	if result.err != nil {
		t.Errorf("expected no error, got %v", result.err)
	}

	// View transitioned to detail.
	if m.view != viewDetail {
		t.Errorf("expected viewDetail after enter, got %d", m.view)
	}

	// Underlying store recorded the ReadMessage call.
	if len(src.readMsg) != 1 || src.readMsg[0] != "msg-1" {
		t.Errorf("expected ReadMessage called with 'msg-1', got %v", src.readMsg)
	}
}

func TestEnterEscalationItemDoesNotMarkAsRead(t *testing.T) {
	// V6: read state is mail-only. Opening an escalation item must not
	// invoke ReadMessage on the underlying store (escalations have no
	// read flag — they are acked / resolved).
	src := &mockDataSource{}
	m := NewModel(Config{Store: src})
	m.items = []InboxItem{
		{ID: "esc-1", Type: ItemEscalation, Source: "sentinel", Description: "stalled"},
	}
	m.ready = true

	cmd := m.updateListKeys(keyMsg("enter"))

	// readCmd returns nil for escalations; updateListKeys propagates that.
	if cmd != nil {
		// If a cmd was returned, executing it must not invoke ReadMessage.
		_ = cmd()
	}

	if m.view != viewDetail {
		t.Errorf("expected viewDetail after enter, got %d", m.view)
	}
	if len(src.readMsg) != 0 {
		t.Errorf("expected ReadMessage NOT called for escalation, got %v", src.readMsg)
	}
}

func TestReadCmdMailItemPropagatesStoreError(t *testing.T) {
	src := &mockDataSource{readMsgErr: errTestSentinel}
	item := InboxItem{Type: ItemMail, ID: "msg-err"}
	cmd := readCmd(src, item)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}

	msg := cmd()
	result, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("expected actionResultMsg, got %T", msg)
	}
	if result.err == nil {
		t.Fatal("expected error in actionResultMsg when store returns error")
	}
	if result.err != errTestSentinel {
		t.Errorf("expected errTestSentinel, got %v", result.err)
	}
	if result.itemID != "msg-err" {
		t.Errorf("expected itemID 'msg-err', got %q", result.itemID)
	}
	if result.action != "read" {
		t.Errorf("expected action 'read', got %q", result.action)
	}
}

func TestReadCmdEscalationReturnsNil(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemEscalation, ID: "esc-1"}
	cmd := readCmd(src, item)
	if cmd != nil {
		t.Fatal("expected nil cmd for escalation item")
	}
	if len(src.readMsg) != 0 {
		t.Errorf("expected ReadMessage NOT called, got %v", src.readMsg)
	}
}

// --- Refresh edge cases ---

func TestRefreshMsgCursorBeyondNewItemCountClampsToLast(t *testing.T) {
	m := NewModel(Config{})
	m.items = makeTestItems(5)
	m.cursor = 4 // pointing at last item
	m.ready = true

	// Refresh with fewer items — cursor should clamp to last item index.
	newItems := makeTestItems(2)
	raw, _ := m.Update(refreshMsg{items: newItems})
	updated := raw.(Model)

	if updated.cursor != 1 {
		t.Errorf("expected cursor clamped to 1 (last of 2 items), got %d", updated.cursor)
	}
	// View should switch to list since cursor was beyond bounds.
	if updated.view != viewList {
		t.Errorf("expected view to be viewList after cursor clamp, got %d", updated.view)
	}
}

func TestRefreshMsgFetchErrorStoresErrorString(t *testing.T) {
	m := NewModel(Config{})
	m.ready = true

	raw, _ := m.Update(refreshMsg{items: nil, err: errTestSentinel})
	updated := raw.(Model)

	if updated.fetchErr == "" {
		t.Fatal("expected fetchErr to be set when refresh contains error")
	}
	if updated.fetchErr != errTestSentinel.Error() {
		t.Errorf("expected fetchErr %q, got %q", errTestSentinel.Error(), updated.fetchErr)
	}
}

// --- Thread grouping tests ---

func TestGroupThreadsCollapsesMultiMessageThread(t *testing.T) {
	now := time.Now()
	items := []InboxItem{
		{
			ID: "msg-1", Type: ItemMail, Priority: 3, Source: "alice", Description: "first",
			CreatedAt: now.Add(-2 * time.Hour),
			Message:   &store.Message{ID: "msg-1", Sender: "alice", Subject: "first", Priority: 3, ThreadID: "th-1", CreatedAt: now.Add(-2 * time.Hour)},
		},
		{
			ID: "msg-2", Type: ItemMail, Priority: 2, Source: "bob", Description: "second",
			CreatedAt: now.Add(-1 * time.Hour),
			Message:   &store.Message{ID: "msg-2", Sender: "bob", Subject: "second", Priority: 2, ThreadID: "th-1", CreatedAt: now.Add(-1 * time.Hour)},
		},
		{
			ID: "msg-3", Type: ItemMail, Priority: 2, Source: "carol", Description: "third (newest)",
			CreatedAt: now,
			Message:   &store.Message{ID: "msg-3", Sender: "carol", Subject: "third (newest)", Priority: 2, ThreadID: "th-1", CreatedAt: now},
		},
	}

	grouped := groupThreads(items)

	if len(grouped) != 1 {
		t.Fatalf("expected 1 grouped row, got %d: %+v", len(grouped), grouped)
	}
	row := grouped[0]
	if row.ThreadID != "th-1" {
		t.Errorf("expected ThreadID 'th-1', got %q", row.ThreadID)
	}
	if row.ID != "th-1" {
		t.Errorf("expected row ID to be the thread id, got %q", row.ID)
	}
	if len(row.ThreadMessages) != 3 {
		t.Fatalf("expected 3 thread messages, got %d", len(row.ThreadMessages))
	}
	// Oldest-first.
	if row.ThreadMessages[0].ID != "msg-1" || row.ThreadMessages[2].ID != "msg-3" {
		t.Errorf("expected thread messages oldest-first, got order %v", msgIDs(row.ThreadMessages))
	}
	// Representative fields come from the newest message.
	if row.Description != "third (newest)" {
		t.Errorf("expected Description from newest message, got %q", row.Description)
	}
	if row.Source != "carol" {
		t.Errorf("expected Source from newest message, got %q", row.Source)
	}
	// Priority is the most urgent (minimum) across the group.
	if row.Priority != 2 {
		t.Errorf("expected Priority 2 (min across group), got %d", row.Priority)
	}
}

func TestGroupThreadsLeavesStandaloneMailAlone(t *testing.T) {
	items := []InboxItem{
		{ID: "msg-1", Type: ItemMail, Message: &store.Message{ID: "msg-1", ThreadID: ""}},
		{ID: "esc-1", Type: ItemEscalation},
	}
	grouped := groupThreads(items)
	if len(grouped) != 2 {
		t.Fatalf("expected 2 rows (no grouping), got %d", len(grouped))
	}
	if grouped[0].ThreadID != "" || grouped[1].ThreadID != "" {
		t.Error("expected no ThreadID set for standalone items")
	}
}

func TestGroupThreadsPreservesPosition(t *testing.T) {
	// The thread row should appear at the position of the thread's first
	// message, not get pushed to the end.
	now := time.Now()
	items := []InboxItem{
		{ID: "esc-1", Type: ItemEscalation, Priority: 1},
		{ID: "msg-1", Type: ItemMail, Priority: 2, Message: &store.Message{ID: "msg-1", ThreadID: "th-1", CreatedAt: now}},
		{ID: "msg-solo", Type: ItemMail, Priority: 3, Message: &store.Message{ID: "msg-solo", ThreadID: "", CreatedAt: now}},
		{ID: "msg-2", Type: ItemMail, Priority: 2, Message: &store.Message{ID: "msg-2", ThreadID: "th-1", CreatedAt: now.Add(time.Minute)}},
	}
	grouped := groupThreads(items)
	if len(grouped) != 3 {
		t.Fatalf("expected 3 rows, got %d: %+v", len(grouped), grouped)
	}
	if grouped[0].ID != "esc-1" {
		t.Errorf("expected escalation first, got %q", grouped[0].ID)
	}
	if grouped[1].ThreadID != "th-1" {
		t.Errorf("expected thread row second (at msg-1's original position), got %q", grouped[1].ID)
	}
	if grouped[2].ID != "msg-solo" {
		t.Errorf("expected standalone mail third, got %q", grouped[2].ID)
	}
}

func msgIDs(msgs []store.Message) []string {
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	return ids
}

// TestSectionOrderSegregatesInterleavedTypes covers the bug that motivated
// sectionOrder: FetchItems sorts globally by (priority, created_at) across
// BOTH escalations and mail, so a P1 message can sort ahead of a P4
// escalation in FetchItems's own output — the two types are genuinely
// interleaved, not already segregated. Section display (and simple ±1
// cursor movement over item index) requires escalations-block-then-mail-
// block ordering, which sectionOrder must produce regardless of how the
// two types interleave by priority.
func TestSectionOrderSegregatesInterleavedTypes(t *testing.T) {
	interleaved := []InboxItem{
		{ID: "msg-urgent", Type: ItemMail, Priority: 1},
		{ID: "esc-critical", Type: ItemEscalation, Priority: 1},
		{ID: "esc-low", Type: ItemEscalation, Priority: 4},
	}
	got := sectionOrder(interleaved)

	if len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}
	// Both escalations first, in their original relative order...
	if got[0].ID != "esc-critical" || got[1].ID != "esc-low" {
		t.Errorf("expected escalations first (critical, low), got %q, %q", got[0].ID, got[1].ID)
	}
	// ...then mail.
	if got[2].ID != "msg-urgent" {
		t.Errorf("expected mail last, got %q", got[2].ID)
	}
}

func TestFindItemByID(t *testing.T) {
	items := []InboxItem{{ID: "a"}, {ID: "b"}}
	if _, ok := findItemByID(items, "b"); !ok {
		t.Error("expected to find item 'b'")
	}
	if _, ok := findItemByID(items, "missing"); ok {
		t.Error("expected not to find 'missing'")
	}
	if _, ok := findItemByID(items, ""); ok {
		t.Error("expected empty id to never match")
	}
}

// --- Sectioning tests ---

func TestBuildListRowsSectionsEscalationsAndMail(t *testing.T) {
	items := []InboxItem{
		{ID: "esc-1", Type: ItemEscalation},
		{ID: "esc-2", Type: ItemEscalation},
		{ID: "msg-1", Type: ItemMail},
	}
	rows := buildListRows(items)

	if len(rows) != 5 { // 2 headers + 3 items
		t.Fatalf("expected 5 rows, got %d: %+v", len(rows), rows)
	}
	if rows[0].header == "" || !strings.Contains(rows[0].header, "Escalations") {
		t.Errorf("expected first row to be an Escalations header, got %+v", rows[0])
	}
	if rows[3].header == "" || !strings.Contains(rows[3].header, "Mail") {
		t.Errorf("expected 4th row to be a Mail header, got %+v", rows[3])
	}
}

func TestBuildListRowsOmitsEmptyEscalationSection(t *testing.T) {
	items := []InboxItem{{ID: "msg-1", Type: ItemMail}}
	rows := buildListRows(items)

	if len(rows) != 2 { // 1 header + 1 item
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	if !strings.Contains(rows[0].header, "Mail") {
		t.Errorf("expected only a Mail header, got %+v", rows[0])
	}
}

func TestBuildListRowsOmitsEmptyMailSection(t *testing.T) {
	items := []InboxItem{{ID: "esc-1", Type: ItemEscalation}}
	rows := buildListRows(items)

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	if !strings.Contains(rows[0].header, "Escalations") {
		t.Errorf("expected only an Escalations header, got %+v", rows[0])
	}
}

// --- Footer context sensitivity ---

func TestRenderFooterContextSensitive(t *testing.T) {
	escFooter := renderFooter(InboxItem{Type: ItemEscalation}, true)
	if !strings.Contains(escFooter, "[r]esolve") {
		t.Errorf("expected escalation footer to advertise resolve: %q", escFooter)
	}
	if strings.Contains(escFooter, "[d]ismiss") {
		t.Errorf("expected escalation footer to not advertise dismiss: %q", escFooter)
	}

	mailFooter := renderFooter(InboxItem{Type: ItemMail}, true)
	if !strings.Contains(mailFooter, "[d]ismiss") {
		t.Errorf("expected mail footer to advertise dismiss: %q", mailFooter)
	}
	if strings.Contains(mailFooter, "[r]esolve") {
		t.Errorf("expected mail footer to not advertise resolve: %q", mailFooter)
	}

	noSelection := renderFooter(InboxItem{}, false)
	if strings.Contains(noSelection, "[a]ck") {
		t.Errorf("expected no-selection footer to omit ack: %q", noSelection)
	}
}

func TestRenderDetailFooterContextSensitive(t *testing.T) {
	escFooter := renderDetailFooter(InboxItem{Type: ItemEscalation})
	if !strings.Contains(escFooter, "[r]esolve") || strings.Contains(escFooter, "[d]ismiss") {
		t.Errorf("expected escalation detail footer to show resolve only: %q", escFooter)
	}

	mailFooter := renderDetailFooter(InboxItem{Type: ItemMail})
	if !strings.Contains(mailFooter, "[d]ismiss") || strings.Contains(mailFooter, "[r]esolve") {
		t.Errorf("expected mail detail footer to show dismiss only: %q", mailFooter)
	}
}

// --- Source column sizing ---

func TestSourceColWidthSizesToLongestCapped(t *testing.T) {
	items := []InboxItem{{Source: "courier/Nova"}, {Source: "x"}}
	if got := sourceColWidth(items); got != len("courier/Nova") {
		t.Errorf("expected width %d, got %d", len("courier/Nova"), got)
	}

	long := []InboxItem{{Source: strings.Repeat("a", 40)}}
	if got := sourceColWidth(long); got != 24 {
		t.Errorf("expected width capped at 24, got %d", got)
	}

	if got := sourceColWidth(nil); got != len("SOURCE") {
		t.Errorf("expected floor of len(SOURCE), got %d", got)
	}
}

// --- Thread ack/dismiss/read ---

func TestAckCmdThreadAcksAllPendingMessages(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{
		Type:     ItemMail,
		ID:       "th-1",
		ThreadID: "th-1",
		ThreadMessages: []store.Message{
			{ID: "msg-1"}, {ID: "msg-2"}, {ID: "msg-3"},
		},
	}
	cmd := ackCmd(src, item, nil)
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd().(actionResultMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if len(src.ackedMsg) != 3 {
		t.Fatalf("expected all 3 thread messages acked, got %v", src.ackedMsg)
	}
}

func TestDismissCmdThreadDismissesAllPendingMessages(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{
		Type:     ItemMail,
		ID:       "th-1",
		ThreadID: "th-1",
		ThreadMessages: []store.Message{
			{ID: "msg-1"}, {ID: "msg-2"}, {ID: "msg-3"},
		},
	}
	cmd := dismissCmd(src, item)
	msg := cmd().(actionResultMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if len(src.dismissedMsg) != 3 {
		t.Fatalf("expected all 3 thread messages dismissed, got %v", src.dismissedMsg)
	}
}

func TestReadCmdThreadMarksAllPendingMessagesRead(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{
		Type:     ItemMail,
		ID:       "th-1",
		ThreadID: "th-1",
		ThreadMessages: []store.Message{
			{ID: "msg-1"}, {ID: "msg-2"},
		},
	}
	cmd := readCmd(src, item)
	msg := cmd().(actionResultMsg)
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if len(src.readMsg) != 2 {
		t.Fatalf("expected both thread messages marked read, got %v", src.readMsg)
	}
}

// --- Detail scroll ---

func TestDetailScrollUpDownClamped(t *testing.T) {
	m := NewModel(Config{})
	m.items = []InboxItem{
		{ID: "esc-1", Type: ItemEscalation, Escalation: &store.Escalation{ID: "esc-1", Description: strings.Repeat("line\n", 100)}},
	}
	m.pinnedID = "esc-1"
	m.view = viewDetail
	m.width = 80
	m.height = 15
	m.ready = true

	// up at scroll 0 stays at 0.
	m.updateDetailKeys(keyMsg("up"))
	if m.detailScroll != 0 {
		t.Errorf("expected detailScroll to stay 0, got %d", m.detailScroll)
	}

	// pgdown advances by a page.
	m.updateDetailKeys(keyMsg("pgdown"))
	m.clampDetailScroll()
	if m.detailScroll != detailPageSize {
		t.Errorf("expected detailScroll %d after pgdown, got %d", detailPageSize, m.detailScroll)
	}

	// Repeated pgdown clamps to the max scrollable offset, not runaway.
	for i := 0; i < 50; i++ {
		m.updateDetailKeys(keyMsg("pgdown"))
		m.clampDetailScroll()
	}
	lines := detailContentLines(m.items[0], m.width)
	maxScroll := len(lines) - detailViewportHeight(m.height)
	if m.detailScroll != maxScroll {
		t.Errorf("expected detailScroll clamped to max %d, got %d", maxScroll, m.detailScroll)
	}
}

// --- helpers ---

var errTestSentinel = fmt.Errorf("test error")

func makeTestItems(n int) []InboxItem {
	items := make([]InboxItem, n)
	for i := 0; i < n; i++ {
		items[i] = InboxItem{
			ID:          fmt.Sprintf("item-%d", i),
			Type:        ItemMail,
			Priority:    2,
			Source:      "test",
			Description: fmt.Sprintf("test item %d", i),
			CreatedAt:   time.Now().Add(-time.Duration(i) * time.Minute),
		}
	}
	return items
}

// keyMsg creates a tea.KeyMsg for testing key handlers.
func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

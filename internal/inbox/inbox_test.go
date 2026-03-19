package inbox

import (
	"fmt"
	"sort"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nevinsm/sol/internal/store"
)

// --- mock DataSource ---

type mockDataSource struct {
	escalations    []store.Escalation
	messages       []store.Message
	escErr         error
	msgErr         error
	ackedEsc       []string
	resolvedEsc    []string
	ackedMsg       []string
	readMsg        []string
	ackEscErr      error
	resolveEscErr  error
	ackMsgErr      error
	readMsgErr     error
}

func (m *mockDataSource) ListOpenEscalations() ([]store.Escalation, error) {
	return m.escalations, m.escErr
}
func (m *mockDataSource) Inbox(recipient string) ([]store.Message, error) {
	return m.messages, m.msgErr
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
		{"low", 3},
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
			{ID: "msg-p2-old", Priority: 2, Sender: "alice", Subject: "hello", CreatedAt: now.Add(-3 * time.Hour)},
			{ID: "msg-p1-new", Priority: 1, Sender: "bob", Subject: "urgent", CreatedAt: now.Add(-30 * time.Minute)},
		},
	}

	items, err := FetchItems(src)
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
			{ID: "msg-dup", Priority: 1, Sender: "sentinel", Subject: "[ESCALATION-high]", ThreadID: "esc:esc-001", CreatedAt: now},
			// This message is a regular mail.
			{ID: "msg-real", Priority: 2, Sender: "alice", Subject: "hello", ThreadID: "", CreatedAt: now},
		},
	}

	items, err := FetchItems(src)
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

func TestFetchItemsEmptySources(t *testing.T) {
	src := &mockDataSource{}
	items, err := FetchItems(src)
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
			{ID: "msg-1", Priority: 2, Sender: "alice", Subject: "hello", CreatedAt: time.Now()},
		},
	}

	items, err := FetchItems(src)
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

	items, err := FetchItems(src)
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
	items, err := FetchItems(src)
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
		ID:       "msg-xyz",
		Sender:   "bob",
		Subject:  "deployment ready",
		Priority: 1,
		Type:     "notification",
		ThreadID: "",
		CreatedAt: now,
	}

	src := &mockDataSource{messages: []store.Message{msg}}
	items, err := FetchItems(src)
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
			{ID: "msg-new", Priority: 2, Sender: "a", Subject: "new", CreatedAt: now},
			{ID: "msg-old", Priority: 2, Sender: "b", Subject: "old", CreatedAt: now.Add(-1 * time.Hour)},
			{ID: "msg-mid", Priority: 2, Sender: "c", Subject: "mid", CreatedAt: now.Add(-30 * time.Minute)},
		},
	}

	items, err := FetchItems(src)
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

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"exact max", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello", 3, "hel"},
		{"max 0", "hello", 0, ""},
		{"empty string", "", 5, ""},
		{"unicode safe", "こんにちは世界", 5, "こん..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.input, tt.max)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}

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
		count int
		want  string // substring to check
	}{
		{0, "0 items"},
		{1, "1 item"},
		{5, "5 items"},
	}
	for _, tt := range tests {
		result := renderHeader(tt.count)
		// renderHeader applies lipgloss styling, so check the content is present.
		if len(result) == 0 {
			t.Errorf("renderHeader(%d) returned empty string", tt.count)
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

func TestResolveCmdNilForNonEscalation(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemMail, ID: "msg-1"}
	cmd := resolveCmd(src, item, nil)
	if cmd != nil {
		t.Error("expected nil cmd for resolve on mail item")
	}
}

func TestReadCmdNilForNonMail(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemEscalation, ID: "esc-1"}
	cmd := readCmd(src, item)
	if cmd != nil {
		t.Error("expected nil cmd for read on escalation item")
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

func TestReadCmdMessage(t *testing.T) {
	src := &mockDataSource{}
	item := InboxItem{Type: ItemMail, ID: "msg-2"}
	cmd := readCmd(src, item)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for read on mail")
	}

	msg := cmd()
	result := msg.(actionResultMsg)
	if result.action != "read" {
		t.Errorf("expected action 'read', got %q", result.action)
	}

	if len(src.readMsg) != 1 || src.readMsg[0] != "msg-2" {
		t.Errorf("expected ReadMessage called with 'msg-2', got %v", src.readMsg)
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

func TestReadCmdMessageStoreError(t *testing.T) {
	src := &mockDataSource{readMsgErr: errTestSentinel}
	item := InboxItem{Type: ItemMail, ID: "msg-err-1"}
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
	if result.itemID != "msg-err-1" {
		t.Errorf("expected itemID 'msg-err-1', got %q", result.itemID)
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

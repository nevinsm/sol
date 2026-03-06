package ledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nevinsm/sol/internal/store"
)

// setupTestLedger creates a Ledger backed by a temp SOL_HOME with a world store.
func setupTestLedger(t *testing.T, worldName string) (*Ledger, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	ws, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })

	cfg := DefaultConfig(dir)
	l := New(cfg)

	return l, ws
}

// makeOTLPBody builds an OTLP JSON body for testing.
func makeOTLPBody(agentName, world, workItemID, eventName, model string, input, output, cacheRead, cacheCreation int64) []byte {
	req := ExportLogsServiceRequest{
		ResourceLogs: []ResourceLogs{{
			Resource: Resource{
				Attributes: []KeyValue{
					{Key: "agent.name", Value: AnyValue{StringValue: agentName}},
					{Key: "world", Value: AnyValue{StringValue: world}},
					{Key: "work_item_id", Value: AnyValue{StringValue: workItemID}},
				},
			},
			ScopeLogs: []ScopeLogs{{
				LogRecords: []LogRecord{{
					TimeUnixNano: "1709740800000000000",
					Body:         AnyValue{StringValue: eventName},
					Attributes: []KeyValue{
						{Key: "gen_ai.response.model", Value: AnyValue{StringValue: model}},
						{Key: "gen_ai.usage.input_tokens", Value: AnyValue{IntValue: fmt.Sprintf("%d", input)}},
						{Key: "gen_ai.usage.output_tokens", Value: AnyValue{IntValue: fmt.Sprintf("%d", output)}},
						{Key: "gen_ai.usage.cache_read_input_tokens", Value: AnyValue{IntValue: fmt.Sprintf("%d", cacheRead)}},
						{Key: "gen_ai.usage.cache_creation_input_tokens", Value: AnyValue{IntValue: fmt.Sprintf("%d", cacheCreation)}},
					},
				}},
			}},
		}},
	}
	b, _ := json.Marshal(req)
	return b
}

func TestHandleLogs_Success(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	body := makeOTLPBody("Toast", "testworld", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 1000, 500, 200, 100)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()

	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify history was created.
	entries, err := ws.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}
	if entries[0].Action != "session" {
		t.Fatalf("expected action 'session', got %q", entries[0].Action)
	}

	// Verify token usage was written.
	summaries, err := ws.AggregateTokens("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 token summary, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 1000 {
		t.Fatalf("expected input_tokens 1000, got %d", summaries[0].InputTokens)
	}
	if summaries[0].OutputTokens != 500 {
		t.Fatalf("expected output_tokens 500, got %d", summaries[0].OutputTokens)
	}
}

func TestHandleLogs_MultipleEvents(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// Send first request.
	body1 := makeOTLPBody("Toast", "testworld", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 1000, 500, 200, 100)
	req1 := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body1))
	w1 := httptest.NewRecorder()
	l.handleLogs(w1, req1)

	// Send second request — same session, should reuse history.
	body2 := makeOTLPBody("Toast", "testworld", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 2000, 800, 300, 50)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body2))
	w2 := httptest.NewRecorder()
	l.handleLogs(w2, req2)

	// Still just 1 history entry.
	entries, err := ws.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry (reused), got %d", len(entries))
	}

	// Aggregated tokens = sum of both requests.
	summaries, err := ws.AggregateTokens("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if summaries[0].InputTokens != 3000 {
		t.Fatalf("expected aggregated input_tokens 3000, got %d", summaries[0].InputTokens)
	}
}

func TestHandleLogs_IgnoresNonAPIRequest(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	body := makeOTLPBody("Toast", "testworld", "sol-item01", "some.other.event",
		"claude-sonnet-4-6", 1000, 500, 200, 100)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// No history should be created.
	entries, err := ws.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 history entries, got %d", len(entries))
	}
}

func TestHandleLogs_SkipsMissingResource(t *testing.T) {
	l, _ := setupTestLedger(t, "testworld")

	// No resource attributes — should be silently skipped.
	otlpReq := ExportLogsServiceRequest{
		ResourceLogs: []ResourceLogs{{
			Resource: Resource{},
			ScopeLogs: []ScopeLogs{{
				LogRecords: []LogRecord{{
					Body: AnyValue{StringValue: "claude_code.api_request"},
					Attributes: []KeyValue{
						{Key: "gen_ai.response.model", Value: AnyValue{StringValue: "claude-sonnet-4-6"}},
						{Key: "gen_ai.usage.input_tokens", Value: AnyValue{IntValue: "100"}},
					},
				}},
			}},
		}},
	}
	body, _ := json.Marshal(otlpReq)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleLogs_MethodNotAllowed(t *testing.T) {
	l, _ := setupTestLedger(t, "testworld")

	req := httptest.NewRequest(http.MethodGet, "/v1/logs", nil)
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestAttributeMap(t *testing.T) {
	attrs := []KeyValue{
		{Key: "agent.name", Value: AnyValue{StringValue: "Toast"}},
		{Key: "count", Value: AnyValue{IntValue: "42"}},
		{Key: "empty", Value: AnyValue{}},
	}

	m := attributeMap(attrs)
	if m["agent.name"] != "Toast" {
		t.Fatalf("expected 'Toast', got %q", m["agent.name"])
	}
	if m["count"] != "42" {
		t.Fatalf("expected '42', got %q", m["count"])
	}
	if _, ok := m["empty"]; ok {
		t.Fatal("expected 'empty' key to be absent")
	}
}

func TestParseIntAttr(t *testing.T) {
	attrs := map[string]string{
		"count":   "42",
		"invalid": "abc",
	}

	if v := parseIntAttr(attrs, "count"); v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
	if v := parseIntAttr(attrs, "invalid"); v != 0 {
		t.Fatalf("expected 0 for invalid, got %d", v)
	}
	if v := parseIntAttr(attrs, "missing"); v != 0 {
		t.Fatalf("expected 0 for missing, got %d", v)
	}
}

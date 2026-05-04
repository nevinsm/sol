package ledger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// setupTestLedger creates a Ledger backed by a temp SOL_HOME with a world store.
// Returns the ledger and a cachedStore with the correct inode for the DB file.
func setupTestLedger(t *testing.T, worldName string) (*Ledger, cachedStore) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	rawStore, err := store.OpenWorld(worldName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rawStore.Close() })

	// Capture the inode of the DB file so the cache entry is valid.
	var inode uint64
	dbPath := filepath.Join(config.StoreDir(), worldName+".db")
	if info, err := os.Stat(dbPath); err == nil {
		inode = fileInode(info)
	}

	cfg := DefaultConfig(dir)
	l := New(cfg)

	return l, cachedStore{store: rawStore, inode: inode}
}

// makeOTLPBody builds an OTLP JSON body using Claude Code's actual attribute names.
// IntValues are formatted as strings (the Go json.Marshal-compatible path).
func makeOTLPBody(agentName, world, writID, eventName, model string, input, output, cacheRead, cacheCreation int64) []byte {
	return makeOTLPBodyRaw(agentName, world, writID, eventName, model, input, output, cacheRead, cacheCreation, nil, nil)
}

// makeOTLPBodyWithCost builds an OTLP JSON body with cost_usd and duration_ms attributes.
func makeOTLPBodyWithCost(agentName, world, writID, eventName, model string, input, output, cacheRead, cacheCreation int64, costUSD float64, durationMS int64) []byte {
	return makeOTLPBodyRaw(agentName, world, writID, eventName, model, input, output, cacheRead, cacheCreation, &costUSD, &durationMS)
}

func makeOTLPBodyRaw(agentName, world, writID, eventName, model string, input, output, cacheRead, cacheCreation int64, costUSD *float64, durationMS *int64) []byte {
	attrs := []map[string]interface{}{
		{"key": "model", "value": map[string]interface{}{"stringValue": model}},
		{"key": "input_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", input)}},
		{"key": "output_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", output)}},
		{"key": "cache_read_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", cacheRead)}},
		{"key": "cache_creation_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", cacheCreation)}},
	}
	if costUSD != nil {
		attrs = append(attrs, map[string]interface{}{"key": "cost_usd", "value": map[string]interface{}{"doubleValue": *costUSD}})
	}
	if durationMS != nil {
		attrs = append(attrs, map[string]interface{}{"key": "duration_ms", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", *durationMS)}})
	}

	req := map[string]interface{}{
		"resourceLogs": []interface{}{
			map[string]interface{}{
				"resource": map[string]interface{}{
					"attributes": []interface{}{
						map[string]interface{}{"key": "service.name", "value": map[string]interface{}{"stringValue": "claude-code"}},
						map[string]interface{}{"key": "agent.name", "value": map[string]interface{}{"stringValue": agentName}},
						map[string]interface{}{"key": "world", "value": map[string]interface{}{"stringValue": world}},
						map[string]interface{}{"key": "writ_id", "value": map[string]interface{}{"stringValue": writID}},
					},
				},
				"scopeLogs": []interface{}{
					map[string]interface{}{
						"logRecords": []interface{}{
							map[string]interface{}{
								"timeUnixNano": "1709740800000000000",
								"body":         map[string]interface{}{"stringValue": eventName},
								"attributes":   attrs,
							},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(req)
	return b
}

// makeOTLPBodyGenAI builds an OTLP JSON body using OTel gen_ai.* attribute names
// to verify the fallback path still works.
func makeOTLPBodyGenAI(agentName, world, writID, eventName, model string, input, output, cacheRead, cacheCreation int64) []byte {
	req := map[string]interface{}{
		"resourceLogs": []interface{}{
			map[string]interface{}{
				"resource": map[string]interface{}{
					"attributes": []interface{}{
						map[string]interface{}{"key": "service.name", "value": map[string]interface{}{"stringValue": "claude-code"}},
						map[string]interface{}{"key": "agent.name", "value": map[string]interface{}{"stringValue": agentName}},
						map[string]interface{}{"key": "world", "value": map[string]interface{}{"stringValue": world}},
						map[string]interface{}{"key": "writ_id", "value": map[string]interface{}{"stringValue": writID}},
					},
				},
				"scopeLogs": []interface{}{
					map[string]interface{}{
						"logRecords": []interface{}{
							map[string]interface{}{
								"timeUnixNano": "1709740800000000000",
								"body":         map[string]interface{}{"stringValue": eventName},
								"attributes": []interface{}{
									map[string]interface{}{"key": "gen_ai.response.model", "value": map[string]interface{}{"stringValue": model}},
									map[string]interface{}{"key": "gen_ai.usage.input_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", input)}},
									map[string]interface{}{"key": "gen_ai.usage.output_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", output)}},
									map[string]interface{}{"key": "gen_ai.usage.cache_read_input_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", cacheRead)}},
									map[string]interface{}{"key": "gen_ai.usage.cache_creation_input_tokens", "value": map[string]interface{}{"intValue": fmt.Sprintf("%d", cacheCreation)}},
								},
							},
						},
					},
				},
			},
		},
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
	entries, err := ws.store.ListHistory("Toast")
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
	summaries, err := ws.store.AggregateTokens("Toast")
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
	entries, err := ws.store.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry (reused), got %d", len(entries))
	}

	// Aggregated tokens = sum of both requests.
	summaries, err := ws.store.AggregateTokens("Toast")
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
	entries, err := ws.store.ListHistory("Toast")
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
	// Use raw JSON to test deserialization with intValue as string.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {},
			"scopeLogs": [{
				"logRecords": [{
					"body": {"stringValue": "claude_code.api_request"},
					"attributes": [
						{"key": "gen_ai.response.model", "value": {"stringValue": "claude-sonnet-4-6"}},
						{"key": "gen_ai.usage.input_tokens", "value": {"intValue": "100"}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
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
	// Test with raw JSON to exercise UnmarshalJSON.
	rawJSON := `[
		{"key": "agent.name", "value": {"stringValue": "Toast"}},
		{"key": "count", "value": {"intValue": "42"}},
		{"key": "score", "value": {"doubleValue": 3.14}},
		{"key": "active", "value": {"boolValue": true}},
		{"key": "empty", "value": {}}
	]`
	var attrs []KeyValue
	if err := json.Unmarshal([]byte(rawJSON), &attrs); err != nil {
		t.Fatal(err)
	}

	m := attributeMap(attrs)
	if m["agent.name"] != "Toast" {
		t.Fatalf("expected 'Toast', got %q", m["agent.name"])
	}
	if m["count"] != "42" {
		t.Fatalf("expected '42', got %q", m["count"])
	}
	if m["score"] != "3.14" {
		t.Fatalf("expected '3.14', got %q", m["score"])
	}
	if m["active"] != "true" {
		t.Fatalf("expected 'true', got %q", m["active"])
	}
	if _, ok := m["empty"]; ok {
		t.Fatal("expected 'empty' key to be absent")
	}
}


// TestHandleLogs_GenAIFallback verifies that gen_ai.* attribute names are
// still accepted when Claude Code's short names are absent.
func TestHandleLogs_GenAIFallback(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	body := makeOTLPBodyGenAI("Toast", "testworld", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 1000, 500, 200, 100)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	summaries, err := ws.store.AggregateTokens("Toast")
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

// TestHandleLogs_EventNameAttrFallback verifies that events using the
// event.name attribute set to "api_request" (without the prefix) are accepted.
func TestHandleLogs_EventNameAttrFallback(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// Body is empty; event name is conveyed via the event.name attribute.
	// Use raw JSON to test deserialization.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "claude-code"}},
					{"key": "agent.name", "value": {"stringValue": "Toast"}},
					{"key": "world", "value": {"stringValue": "testworld"}},
					{"key": "writ_id", "value": {"stringValue": "sol-item01"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1709740800000000000",
					"body": {},
					"attributes": [
						{"key": "event.name", "value": {"stringValue": "api_request"}},
						{"key": "model", "value": {"stringValue": "claude-sonnet-4-6"}},
						{"key": "input_tokens", "value": {"intValue": "750"}},
						{"key": "output_tokens", "value": {"intValue": "250"}},
						{"key": "cache_read_tokens", "value": {"intValue": "0"}},
						{"key": "cache_creation_tokens", "value": {"intValue": "0"}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	summaries, err := ws.store.AggregateTokens("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 token summary, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 750 {
		t.Fatalf("expected input_tokens 750, got %d", summaries[0].InputTokens)
	}
}

// TestAnyValue_IntValueAsJSONNumber tests the exact failure case that broke all
// ingestion: Claude Code's JS OTel exporter sends intValue as a JSON number
// (e.g. {"intValue": 311}) instead of a JSON string ({"intValue": "311"}).
func TestAnyValue_IntValueAsJSONNumber(t *testing.T) {
	// This is exactly what Claude Code sends.
	rawJSON := `{"intValue": 311}`
	var v AnyValue
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		t.Fatalf("failed to unmarshal intValue as JSON number: %v", err)
	}
	if v.IntValue != "311" {
		t.Fatalf("expected IntValue '311', got %q", v.IntValue)
	}

	// Also verify the string form still works.
	rawJSONStr := `{"intValue": "311"}`
	var v2 AnyValue
	if err := json.Unmarshal([]byte(rawJSONStr), &v2); err != nil {
		t.Fatalf("failed to unmarshal intValue as JSON string: %v", err)
	}
	if v2.IntValue != "311" {
		t.Fatalf("expected IntValue '311', got %q", v2.IntValue)
	}
}

// TestAnyValue_DoubleValue tests parsing of doubleValue as both JSON number and string.
func TestAnyValue_DoubleValue(t *testing.T) {
	rawJSON := `{"doubleValue": 0.0042}`
	var v AnyValue
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		t.Fatalf("failed to unmarshal doubleValue as JSON number: %v", err)
	}
	if v.DoubleValue != 0.0042 {
		t.Fatalf("expected DoubleValue 0.0042, got %f", v.DoubleValue)
	}

	// String form.
	rawJSONStr := `{"doubleValue": "0.0042"}`
	var v2 AnyValue
	if err := json.Unmarshal([]byte(rawJSONStr), &v2); err != nil {
		t.Fatalf("failed to unmarshal doubleValue as JSON string: %v", err)
	}
	if v2.DoubleValue != 0.0042 {
		t.Fatalf("expected DoubleValue 0.0042, got %f", v2.DoubleValue)
	}
}

// TestAnyValue_BoolValue tests parsing of boolValue.
func TestAnyValue_BoolValue(t *testing.T) {
	rawJSON := `{"boolValue": true}`
	var v AnyValue
	if err := json.Unmarshal([]byte(rawJSON), &v); err != nil {
		t.Fatalf("failed to unmarshal boolValue: %v", err)
	}
	if v.BoolValue == nil || *v.BoolValue != true {
		t.Fatalf("expected BoolValue true, got %v", v.BoolValue)
	}

	// String form.
	rawJSONStr := `{"boolValue": "false"}`
	var v2 AnyValue
	if err := json.Unmarshal([]byte(rawJSONStr), &v2); err != nil {
		t.Fatalf("failed to unmarshal boolValue as string: %v", err)
	}
	if v2.BoolValue == nil || *v2.BoolValue != false {
		t.Fatalf("expected BoolValue false, got %v", v2.BoolValue)
	}
}

// TestHandleLogs_CostAndDuration tests that cost_usd and duration_ms are
// extracted and persisted.
func TestHandleLogs_CostAndDuration(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	body := makeOTLPBodyWithCost("Toast", "testworld", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 1000, 500, 200, 100, 0.0042, 1500)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify token usage with cost and duration.
	summaries, err := ws.store.AggregateTokens("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 token summary, got %d", len(summaries))
	}
	if summaries[0].CostUSD == nil {
		t.Fatal("expected CostUSD to be set")
	}
	if *summaries[0].CostUSD != 0.0042 {
		t.Fatalf("expected CostUSD 0.0042, got %f", *summaries[0].CostUSD)
	}
	if summaries[0].DurationMS == nil {
		t.Fatal("expected DurationMS to be set")
	}
	if *summaries[0].DurationMS != 1500 {
		t.Fatalf("expected DurationMS 1500, got %d", *summaries[0].DurationMS)
	}
}

// TestHandleLogs_RealClaudeCodePayload tests with a payload format matching
// what Claude Code's OTel-OTLP-Exporter-JavaScript/0.208.0 actually sends.
// Key difference: intValue is a JSON number, not a string.
func TestHandleLogs_RealClaudeCodePayload(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// This is the exact format Claude Code sends — intValue as JSON numbers.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "claude-code"}},
					{"key": "agent.name", "value": {"stringValue": "Nova"}},
					{"key": "world", "value": {"stringValue": "testworld"}},
					{"key": "writ_id", "value": {"stringValue": "sol-abc123"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1709740800000000000",
					"body": {"stringValue": "claude_code.api_request"},
					"attributes": [
						{"key": "model", "value": {"stringValue": "claude-sonnet-4-20250514"}},
						{"key": "input_tokens", "value": {"intValue": 15234}},
						{"key": "output_tokens", "value": {"intValue": 3891}},
						{"key": "cache_read_tokens", "value": {"intValue": 48210}},
						{"key": "cache_creation_tokens", "value": {"intValue": 0}},
						{"key": "cost_usd", "value": {"doubleValue": 0.0312}},
						{"key": "duration_ms", "value": {"intValue": 4521}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	summaries, err := ws.store.AggregateTokens("Nova")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 token summary, got %d", len(summaries))
	}
	ts := summaries[0]
	if ts.InputTokens != 15234 {
		t.Fatalf("expected input_tokens 15234, got %d", ts.InputTokens)
	}
	if ts.OutputTokens != 3891 {
		t.Fatalf("expected output_tokens 3891, got %d", ts.OutputTokens)
	}
	if ts.CacheReadTokens != 48210 {
		t.Fatalf("expected cache_read_tokens 48210, got %d", ts.CacheReadTokens)
	}
	if ts.CacheCreationTokens != 0 {
		t.Fatalf("expected cache_creation_tokens 0, got %d", ts.CacheCreationTokens)
	}
	if ts.CostUSD == nil {
		t.Fatal("expected CostUSD to be set")
	}
	if *ts.CostUSD != 0.0312 {
		t.Fatalf("expected CostUSD 0.0312, got %f", *ts.CostUSD)
	}
	if ts.DurationMS == nil {
		t.Fatal("expected DurationMS to be set")
	}
	if *ts.DurationMS != 4521 {
		t.Fatalf("expected DurationMS 4521, got %d", *ts.DurationMS)
	}
}

// TestExtractorRegistry_UnknownServiceName verifies that unknown service.name
// values are silently skipped — no crash, no error, no history created.
func TestExtractorRegistry_UnknownServiceName(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// Use a service.name that has no registered extractor.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "unknown-runtime"}},
					{"key": "agent.name", "value": {"stringValue": "Toast"}},
					{"key": "world", "value": {"stringValue": "testworld"}},
					{"key": "writ_id", "value": {"stringValue": "sol-item01"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1709740800000000000",
					"body": {"stringValue": "claude_code.api_request"},
					"attributes": [
						{"key": "model", "value": {"stringValue": "some-model"}},
						{"key": "input_tokens", "value": {"intValue": "1000"}},
						{"key": "output_tokens", "value": {"intValue": "500"}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// No history should be created for unknown runtime.
	entries, err := ws.store.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 history entries for unknown runtime, got %d", len(entries))
	}
}

// TestExtractorRegistry_MissingServiceName verifies that events without
// service.name are silently skipped.
func TestExtractorRegistry_MissingServiceName(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// No service.name in resource attributes.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "agent.name", "value": {"stringValue": "Toast"}},
					{"key": "world", "value": {"stringValue": "testworld"}},
					{"key": "writ_id", "value": {"stringValue": "sol-item01"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1709740800000000000",
					"body": {"stringValue": "claude_code.api_request"},
					"attributes": [
						{"key": "model", "value": {"stringValue": "claude-sonnet-4-6"}},
						{"key": "input_tokens", "value": {"intValue": "1000"}},
						{"key": "output_tokens", "value": {"intValue": "500"}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// No history should be created without service.name.
	entries, err := ws.store.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 history entries without service.name, got %d", len(entries))
	}
}

// TestExtractorRegistry_ClaudeCodeDispatch verifies that the claude-code
// extractor is correctly registered and dispatches events.
func TestExtractorRegistry_ClaudeCodeDispatch(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// Verify the extractor is registered.
	if _, ok := l.extractors["claude-code"]; !ok {
		t.Fatal("expected claude-code extractor to be registered")
	}

	// Send a valid event with service.name=claude-code.
	body := makeOTLPBody("Toast", "testworld", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 1000, 500, 200, 100)

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	summaries, err := ws.store.AggregateTokens("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 token summary, got %d", len(summaries))
	}
	if summaries[0].InputTokens != 1000 {
		t.Fatalf("expected input_tokens 1000, got %d", summaries[0].InputTokens)
	}
}

// TestClaudeExtractTelemetry_Direct tests the Claude adapter's ExtractTelemetry
// method directly with various inputs.
func TestClaudeExtractTelemetry_Direct(t *testing.T) {
	l := New(DefaultConfig(t.TempDir()))
	extract := l.extractors["claude-code"]

	t.Run("accepts claude_code.api_request", func(t *testing.T) {
		attrs := map[string]string{
			"model":                 "claude-sonnet-4-6",
			"input_tokens":         "1000",
			"output_tokens":        "500",
			"cache_read_tokens":    "200",
			"cache_creation_tokens": "100",
		}
		tr := extract("claude_code.api_request", attrs)
		if tr == nil {
			t.Fatal("expected non-nil TelemetryRecord")
		}
		if tr.Model != "claude-sonnet-4-6" {
			t.Fatalf("expected model 'claude-sonnet-4-6', got %q", tr.Model)
		}
		if tr.InputTokens != 1000 {
			t.Fatalf("expected input 1000, got %d", tr.InputTokens)
		}
		if tr.OutputTokens != 500 {
			t.Fatalf("expected output 500, got %d", tr.OutputTokens)
		}
	})

	t.Run("accepts api_request", func(t *testing.T) {
		attrs := map[string]string{
			"model":          "claude-sonnet-4-6",
			"input_tokens":  "100",
			"output_tokens": "50",
		}
		tr := extract("api_request", attrs)
		if tr == nil {
			t.Fatal("expected non-nil TelemetryRecord")
		}
	})

	t.Run("rejects unknown event", func(t *testing.T) {
		attrs := map[string]string{
			"model":          "claude-sonnet-4-6",
			"input_tokens":  "100",
			"output_tokens": "50",
		}
		tr := extract("some.other.event", attrs)
		if tr != nil {
			t.Fatal("expected nil for unknown event")
		}
	})

	t.Run("rejects missing model", func(t *testing.T) {
		attrs := map[string]string{
			"input_tokens":  "100",
			"output_tokens": "50",
		}
		tr := extract("claude_code.api_request", attrs)
		if tr != nil {
			t.Fatal("expected nil for missing model")
		}
	})

	t.Run("gen_ai fallback", func(t *testing.T) {
		attrs := map[string]string{
			"gen_ai.response.model":                   "claude-sonnet-4-6",
			"gen_ai.usage.input_tokens":               "1000",
			"gen_ai.usage.output_tokens":              "500",
			"gen_ai.usage.cache_read_input_tokens":    "200",
			"gen_ai.usage.cache_creation_input_tokens": "100",
		}
		tr := extract("claude_code.api_request", attrs)
		if tr == nil {
			t.Fatal("expected non-nil TelemetryRecord")
		}
		if tr.Model != "claude-sonnet-4-6" {
			t.Fatalf("expected model 'claude-sonnet-4-6', got %q", tr.Model)
		}
		if tr.InputTokens != 1000 {
			t.Fatalf("expected input 1000, got %d", tr.InputTokens)
		}
		if tr.CacheReadTokens != 200 {
			t.Fatalf("expected cache_read 200, got %d", tr.CacheReadTokens)
		}
	})

	t.Run("cost and duration", func(t *testing.T) {
		attrs := map[string]string{
			"model":          "claude-sonnet-4-6",
			"input_tokens":  "100",
			"output_tokens": "50",
			"cost_usd":      "0.0042",
			"duration_ms":   "1500",
		}
		tr := extract("claude_code.api_request", attrs)
		if tr == nil {
			t.Fatal("expected non-nil TelemetryRecord")
		}
		if tr.CostUSD == nil || *tr.CostUSD != 0.0042 {
			t.Fatalf("expected CostUSD 0.0042, got %v", tr.CostUSD)
		}
		if tr.DurationMS == nil || *tr.DurationMS != 1500 {
			t.Fatalf("expected DurationMS 1500, got %v", tr.DurationMS)
		}
	})
}

// TestExtractorRegistry_CodexRegistered verifies that the codex extractor is
// registered under all known Codex service names.
func TestExtractorRegistry_CodexRegistered(t *testing.T) {
	l := New(DefaultConfig(t.TempDir()))

	for _, svc := range []string{"codex", "codex-cli", "codex_cli_rs"} {
		extract, ok := l.extractors[svc]
		if !ok {
			t.Errorf("expected extractor registered for %q", svc)
			continue
		}
		if extract == nil {
			t.Errorf("expected non-nil ExtractFunc for %q", svc)
		}
	}
}

// TestHandleLogs_HeaderFallback verifies that X-Sol-* headers fill in missing
// resource attributes so that Codex telemetry is not silently dropped.
func TestHandleLogs_HeaderFallback(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// OTLP body with NO resource attributes — all context comes from headers.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {
				"attributes": []
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1709740800000000000",
					"body": {"stringValue": "codex.api_request_initiated"},
					"attributes": [
						{"key": "model", "value": {"stringValue": "o3"}},
						{"key": "input_tokens", "value": {"intValue": "2000"}},
						{"key": "output_tokens", "value": {"intValue": "800"}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
	req.Header.Set("X-Sol-Agent", "Nova")
	req.Header.Set("X-Sol-World", "testworld")
	req.Header.Set("X-Sol-Service", "codex")
	req.Header.Set("X-Sol-Writ", "sol-abc123")
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// History entry should have been created via header context.
	entries, err := ws.store.ListHistory("Nova")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least 1 history entry from header-based context, got 0")
	}
}

// TestHandleLogs_ResourceAttrsPrecedeHeaders verifies that resource attributes
// take precedence over X-Sol-* headers when both are present.
func TestHandleLogs_ResourceAttrsPrecedeHeaders(t *testing.T) {
	l, ws := setupTestLedger(t, "realworld")
	l.stores["realworld"] = ws

	// Resource attributes say "realworld" / "Toast", headers say different values.
	rawJSON := `{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "service.name", "value": {"stringValue": "codex"}},
					{"key": "agent.name", "value": {"stringValue": "Toast"}},
					{"key": "world", "value": {"stringValue": "realworld"}},
					{"key": "writ_id", "value": {"stringValue": "sol-real123"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1709740800000000000",
					"body": {"stringValue": "codex.api_request_initiated"},
					"attributes": [
						{"key": "model", "value": {"stringValue": "o3"}},
						{"key": "input_tokens", "value": {"intValue": "500"}},
						{"key": "output_tokens", "value": {"intValue": "200"}}
					]
				}]
			}]
		}]
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte(rawJSON)))
	// Headers provide different values — should be ignored.
	req.Header.Set("X-Sol-Agent", "WrongAgent")
	req.Header.Set("X-Sol-World", "wrongworld")
	req.Header.Set("X-Sol-Service", "codex-cli")
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// History should be created under the resource-attribute agent "Toast", not "WrongAgent".
	entries, err := ws.store.ListHistory("Toast")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected history entry for agent Toast (from resource attrs), got 0")
	}

	// No history should exist for "WrongAgent" (header value was overridden).
	wrongEntries, err := ws.store.ListHistory("WrongAgent")
	if err != nil {
		t.Fatal(err)
	}
	if len(wrongEntries) != 0 {
		t.Fatalf("expected 0 history entries for WrongAgent, got %d", len(wrongEntries))
	}
}

// TestStoreEviction_ClearsSessionsCache verifies that when a world store is
// evicted from l.stores (due to inode change), the corresponding entries in
// l.sessions are also cleared. This ensures ensureHistory creates a fresh
// history record in the new database instead of returning a stale cached ID.
func TestStoreEviction_ClearsSessionsCache(t *testing.T) {
	l, ws := setupTestLedger(t, "testworld")
	l.stores["testworld"] = ws

	// Step 1: Create a history entry via ensureHistory — this caches the
	// session key -> history ID mapping in l.sessions.
	oldHistoryID, err := l.ensureHistory("testworld", "Toast", "sol-item01")
	if err != nil {
		t.Fatalf("step 1: ensureHistory failed: %v", err)
	}
	if oldHistoryID == "" {
		t.Fatal("expected non-empty history ID")
	}

	// Also cache a second agent so we can verify selective clearing.
	otherHistoryID, err := l.ensureHistory("testworld", "Nova", "sol-item02")
	if err != nil {
		t.Fatalf("step 1b: ensureHistory failed: %v", err)
	}

	// Verify session cache is populated.
	key := sessionKey{World: "testworld", AgentName: "Toast", WritID: "sol-item01"}
	otherKey := sessionKey{World: "testworld", AgentName: "Nova", WritID: "sol-item02"}
	l.mu.Lock()
	if _, ok := l.sessions[key]; !ok {
		l.mu.Unlock()
		t.Fatal("expected session cache entry for Toast")
	}
	if _, ok := l.sessions[otherKey]; !ok {
		l.mu.Unlock()
		t.Fatal("expected session cache entry for Nova")
	}
	l.mu.Unlock()

	// Step 2: Simulate inode change by setting a bogus inode in the cached store.
	// This triggers eviction on the next worldStoreLocked call.
	l.mu.Lock()
	cs := l.stores["testworld"]
	cs.inode = cs.inode + 9999 // Force inode mismatch.
	l.stores["testworld"] = cs
	l.mu.Unlock()

	// Step 3: Trigger eviction via worldStore — this calls worldStoreLocked,
	// detects inode mismatch, evicts the store, and clears sessions for the world.
	_, err = l.worldStore("testworld")
	if err != nil {
		t.Fatalf("step 3: worldStore failed: %v", err)
	}

	// Step 4: Verify all session entries for this world were cleared.
	l.mu.Lock()
	_, toastCached := l.sessions[key]
	_, novaCached := l.sessions[otherKey]
	l.mu.Unlock()
	if toastCached {
		t.Fatal("expected Toast session cache to be cleared after store eviction")
	}
	if novaCached {
		t.Fatal("expected Nova session cache to be cleared after store eviction")
	}

	// Step 5: Call ensureHistory — should create a fresh history record since
	// the session cache was cleared.
	newHistoryID, err := l.ensureHistory("testworld", "Toast", "sol-item01")
	if err != nil {
		t.Fatalf("step 5: ensureHistory failed: %v", err)
	}
	if newHistoryID == oldHistoryID {
		t.Fatalf("expected fresh history ID after store eviction, but got same stale ID %q", newHistoryID)
	}

	// Nova should also get a fresh history.
	newOtherID, err := l.ensureHistory("testworld", "Nova", "sol-item02")
	if err != nil {
		t.Fatalf("step 5b: ensureHistory failed: %v", err)
	}
	if newOtherID == otherHistoryID {
		t.Fatalf("expected fresh history ID for Nova after eviction, but got same stale ID %q", newOtherID)
	}
}

// TestStoreEviction_ClearsOnlyMatchingWorld verifies that evicting one world's
// store only clears session entries for that world, leaving other worlds intact.
func TestStoreEviction_ClearsOnlyMatchingWorld(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".store"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig(dir)
	l := New(cfg)

	// Set up two worlds.
	for _, world := range []string{"world-a", "world-b"} {
		rawStore, err := store.OpenWorld(world)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { rawStore.Close() })

		var inode uint64
		dbPath := filepath.Join(config.StoreDir(), world+".db")
		if info, err := os.Stat(dbPath); err == nil {
			inode = fileInode(info)
		}
		l.stores[world] = cachedStore{store: rawStore, inode: inode}
	}

	// Record tokens in both worlds.
	for _, world := range []string{"world-a", "world-b"} {
		body := makeOTLPBody("Toast", world, "sol-item01", "claude_code.api_request",
			"claude-sonnet-4-6", 1000, 500, 200, 100)
		req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
		w := httptest.NewRecorder()
		l.handleLogs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", world, w.Code)
		}
	}

	// Verify both session entries exist.
	keyA := sessionKey{World: "world-a", AgentName: "Toast", WritID: "sol-item01"}
	keyB := sessionKey{World: "world-b", AgentName: "Toast", WritID: "sol-item01"}

	l.mu.Lock()
	_, okA := l.sessions[keyA]
	_, okB := l.sessions[keyB]
	l.mu.Unlock()
	if !okA || !okB {
		t.Fatalf("expected both session entries, got world-a=%v world-b=%v", okA, okB)
	}

	// Evict only world-a by setting a bogus inode.
	l.mu.Lock()
	cs := l.stores["world-a"]
	cs.inode = cs.inode + 9999
	l.stores["world-a"] = cs
	l.mu.Unlock()

	// Trigger eviction by recording to world-a.
	body := makeOTLPBody("Toast", "world-a", "sol-item01", "claude_code.api_request",
		"claude-sonnet-4-6", 500, 200, 0, 0)
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	l.handleLogs(w, req)

	// world-b session should be untouched.
	l.mu.Lock()
	_, okB = l.sessions[keyB]
	l.mu.Unlock()
	if !okB {
		t.Fatal("world-b session entry was incorrectly cleared during world-a eviction")
	}
}

// runLedgerForShutdownTest starts ledger.Run in a goroutine on an
// OS-assigned port, waits until the server is listening, then returns
// the cancel func and a wait func. Tests use it to exercise the
// Run-shutdown sequence (V9/V12).
func runLedgerForShutdownTest(t *testing.T) (l *Ledger, cancel context.CancelFunc, wait func() error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Port: 0, SOLHome: dir} // port 0 -> OS assigns a free port
	l = New(cfg)

	ctx, cancelFn := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- l.Run(ctx)
	}()

	// Wait briefly for Run to write its initial heartbeat (writeHeartbeat
	// is called synchronously before the heartbeat goroutine spawns).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hb, _ := ReadHeartbeat()
		if hb != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	wait = func() error {
		select {
		case err := <-errCh:
			return err
		case <-time.After(10 * time.Second):
			return fmt.Errorf("ledger Run did not return within 10s")
		}
	}
	return l, cancelFn, wait
}

// TestLedgerRunWritesStoppingHeartbeat verifies V12: ledger writes a
// final "stopping" heartbeat before Run returns.
func TestLedgerRunWritesStoppingHeartbeat(t *testing.T) {
	_, cancel, wait := runLedgerForShutdownTest(t)

	cancel()
	if err := wait(); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if hb == nil {
		t.Fatal("expected heartbeat to be present after shutdown, got nil")
	}
	if hb.Status != "stopping" {
		t.Fatalf("expected final heartbeat status %q, got %q", "stopping", hb.Status)
	}
}

// TestLedgerRunHeartbeatStopsAfterReturn verifies V9: no heartbeat is
// written more than ~50ms after Run returns. The heartbeat goroutine
// must be joined under wg before Run's final heartbeat write so that
// no further writes can race past Run's return.
func TestLedgerRunHeartbeatStopsAfterReturn(t *testing.T) {
	_, cancel, wait := runLedgerForShutdownTest(t)

	cancel()
	if err := wait(); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Capture the heartbeat written immediately at shutdown.
	hbAtReturn, err := ReadHeartbeat()
	if err != nil {
		t.Fatalf("read heartbeat at return: %v", err)
	}
	if hbAtReturn == nil {
		t.Fatal("expected heartbeat at return, got nil")
	}
	tsAtReturn := hbAtReturn.Timestamp

	// Allow background work a generous window — much longer than the
	// 50ms in the writ — and re-read. The timestamp must NOT have moved.
	time.Sleep(200 * time.Millisecond)

	hbAfter, err := ReadHeartbeat()
	if err != nil {
		t.Fatalf("read heartbeat after sleep: %v", err)
	}
	if hbAfter == nil {
		t.Fatal("heartbeat disappeared after Run returned")
	}
	if !hbAfter.Timestamp.Equal(tsAtReturn) {
		t.Fatalf("heartbeat advanced after Run returned: was %v, now %v (delta %v)",
			tsAtReturn, hbAfter.Timestamp, hbAfter.Timestamp.Sub(tsAtReturn))
	}
	if hbAfter.Status != "stopping" {
		t.Fatalf("post-return heartbeat status changed: %q", hbAfter.Status)
	}
}

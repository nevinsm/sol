// Package ledger provides an OTLP HTTP receiver for agent token tracking.
package ledger

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// OTLP JSON types for parsing ExportLogsServiceRequest.
// Defined locally to avoid pulling in the full OTel proto dependency.
// See: https://opentelemetry.io/docs/specs/otlp/#otlphttp

// ExportLogsServiceRequest is the top-level OTLP log export request.
type ExportLogsServiceRequest struct {
	ResourceLogs []ResourceLogs `json:"resourceLogs"`
}

// ResourceLogs groups log records by resource.
type ResourceLogs struct {
	Resource  Resource    `json:"resource"`
	ScopeLogs []ScopeLogs `json:"scopeLogs"`
}

// Resource describes the entity producing telemetry.
type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

// ScopeLogs groups log records by instrumentation scope.
type ScopeLogs struct {
	LogRecords []LogRecord `json:"logRecords"`
}

// LogRecord is a single OTLP log record.
type LogRecord struct {
	TimeUnixNano string     `json:"timeUnixNano"`
	Body         AnyValue   `json:"body"`
	Attributes   []KeyValue `json:"attributes"`
}

// KeyValue is an OTLP attribute key-value pair.
type KeyValue struct {
	Key   string   `json:"key"`
	Value AnyValue `json:"value"`
}

// AnyValue represents an OTLP value (string, int, double, bool).
// Claude Code's JS OTel exporter sends intValue as a JSON number
// (e.g. {"intValue": 311}) but the OTLP spec allows it as a string.
// Custom UnmarshalJSON handles both forms for all numeric/bool types.
type AnyValue struct {
	StringValue string  `json:"stringValue,omitempty"`
	IntValue    string  `json:"-"` // populated by UnmarshalJSON
	DoubleValue float64 `json:"-"` // populated by UnmarshalJSON
	DoubleSet   bool    `json:"-"` // true when doubleValue was present in JSON
	BoolValue   *bool   `json:"-"` // populated by UnmarshalJSON (nil = not set)
}

// UnmarshalJSON implements custom JSON unmarshaling for AnyValue.
// It accepts intValue, doubleValue, and boolValue as either their native
// JSON types (number, boolean) or as JSON strings.
func (v *AnyValue) UnmarshalJSON(data []byte) error {
	// Use a raw intermediate to avoid infinite recursion.
	var raw struct {
		StringValue string           `json:"stringValue,omitempty"`
		IntValue    json.RawMessage  `json:"intValue,omitempty"`
		DoubleValue json.RawMessage  `json:"doubleValue,omitempty"`
		BoolValue   json.RawMessage  `json:"boolValue,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	v.StringValue = raw.StringValue

	// Parse intValue: accept JSON number or JSON string.
	if len(raw.IntValue) > 0 {
		v.IntValue = parseJSONNumberOrString(raw.IntValue)
	}

	// Parse doubleValue: accept JSON number or JSON string.
	if len(raw.DoubleValue) > 0 {
		s := parseJSONNumberOrString(raw.DoubleValue)
		if s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				v.DoubleValue = f
				v.DoubleSet = true
			}
		}
	}

	// Parse boolValue: accept JSON boolean or JSON string.
	if len(raw.BoolValue) > 0 {
		s := parseJSONNumberOrString(raw.BoolValue)
		if s == "true" {
			b := true
			v.BoolValue = &b
		} else if s == "false" {
			b := false
			v.BoolValue = &b
		}
	}

	return nil
}

// parseJSONNumberOrString extracts a string from a json.RawMessage that is
// either a JSON string (quoted) or a JSON number/boolean (unquoted).
func parseJSONNumberOrString(raw json.RawMessage) string {
	// Try as quoted string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Fall back to raw literal (number or boolean).
	return string(raw)
}

// attributeMap converts a slice of KeyValue to a map for easy lookup.
func attributeMap(attrs []KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv.Value.StringValue != "" {
			m[kv.Key] = kv.Value.StringValue
		} else if kv.Value.IntValue != "" {
			m[kv.Key] = kv.Value.IntValue
		} else if kv.Value.DoubleSet {
			m[kv.Key] = fmt.Sprintf("%g", kv.Value.DoubleValue)
		} else if kv.Value.BoolValue != nil {
			m[kv.Key] = fmt.Sprintf("%t", *kv.Value.BoolValue)
		}
	}
	return m
}

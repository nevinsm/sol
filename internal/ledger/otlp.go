// Package ledger provides an OTLP HTTP receiver for agent token tracking.
package ledger

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

// AnyValue represents an OTLP value (string, int, bool, etc.).
type AnyValue struct {
	StringValue string `json:"stringValue,omitempty"`
	IntValue    string `json:"intValue,omitempty"`
}

// attributeMap converts a slice of KeyValue to a map for easy lookup.
func attributeMap(attrs []KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, kv := range attrs {
		if kv.Value.StringValue != "" {
			m[kv.Key] = kv.Value.StringValue
		} else if kv.Value.IntValue != "" {
			m[kv.Key] = kv.Value.IntValue
		}
	}
	return m
}

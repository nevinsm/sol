package escalation

import "testing"

func TestSeverityToPriority(t *testing.T) {
	tests := []struct {
		severity string
		want     int
	}{
		{"critical", 1},
		{"high", 2},
		{"medium", 3},
		{"low", 4},
		// Unknown severities default to medium (3).
		{"unknown", 3},
		{"", 3},
		{"CRITICAL", 3}, // case-sensitive; no match → default
	}
	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			got := SeverityToPriority(tt.severity)
			if got != tt.want {
				t.Errorf("SeverityToPriority(%q) = %d, want %d", tt.severity, got, tt.want)
			}
		})
	}
}

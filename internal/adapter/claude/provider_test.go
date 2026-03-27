package claude

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/broker"
)

func TestClaudeProviderName(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}

func TestClaudeProviderDetectRateLimit(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		name      string
		output    string
		wantMatch bool
		wantReset bool
	}{
		{
			name:      "no rate limit",
			output:    "Working on task...\nCode updated successfully.",
			wantMatch: false,
		},
		{
			name:      "hit limit pattern",
			output:    "Error: You've hit your daily limit\nPlease wait.",
			wantMatch: true,
		},
		{
			name:      "limit resets pattern",
			output:    "Usage limit · resets 3:45pm\nStop and wait.",
			wantMatch: true,
			wantReset: true,
		},
		{
			name:      "stop and wait pattern",
			output:    "Stop and wait for limit to reset",
			wantMatch: true,
		},
		{
			name:      "API rate limit",
			output:    "API Error: Rate limit reached\nRetrying...",
			wantMatch: true,
		},
		{
			name:      "OAuth token revoked",
			output:    "OAuth token revoked\nPlease re-authenticate.",
			wantMatch: true,
		},
		{
			name:      "OAuth token expired",
			output:    "OAuth token has expired\nPlease log in again.",
			wantMatch: true,
		},
		{
			name:      "limit resets with hour only",
			output:    "limit · resets 4pm",
			wantMatch: true,
			wantReset: true,
		},
		{
			name:      "empty output",
			output:    "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := p.DetectRateLimit(tt.output)
			if tt.wantMatch && signal == nil {
				t.Fatal("expected non-nil signal")
			}
			if !tt.wantMatch && signal != nil {
				t.Fatalf("expected nil signal, got %+v", signal)
			}
			if tt.wantReset && signal != nil && signal.ResetsAt.IsZero() {
				t.Error("expected non-zero ResetsAt")
			}
			if !tt.wantReset && signal != nil && !signal.ResetsAt.IsZero() {
				t.Errorf("expected zero ResetsAt, got %v", signal.ResetsAt)
			}
		})
	}
}

func TestClaudeProviderCredentialExpires(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		credType string
		want     bool
	}{
		{"oauth_token", true},
		{"api_key", false},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.credType, func(t *testing.T) {
			got := p.CredentialExpires(tt.credType)
			if got != tt.want {
				t.Errorf("CredentialExpires(%q) = %v, want %v", tt.credType, got, tt.want)
			}
		})
	}
}

func TestClaudeProviderRegistered(t *testing.T) {
	// The init() function should have registered the Claude provider.
	p, ok := broker.GetProvider("claude")
	if !ok {
		t.Fatal("expected Claude provider to be registered")
	}
	if p.Name() != "claude" {
		t.Errorf("registered provider name = %q, want %q", p.Name(), "claude")
	}
}

func TestClaudeProviderInterfaceSatisfaction(t *testing.T) {
	// Compile-time check is in the main file, but verify at runtime too.
	var _ broker.Provider = (*Provider)(nil)
}

func TestParseResetTime(t *testing.T) {
	tests := []struct {
		input    string
		wantHour int
		wantMin  int
		wantErr  bool
	}{
		{"3:45pm", 15, 45, false},
		{"4pm", 16, 0, false},
		{"12pm", 12, 0, false},
		{"12am", 0, 0, false},
		{"11:30am", 11, 30, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseResetTime(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// Result must be in UTC.
			if result.IsZero() {
				t.Error("expected non-zero time")
			}
			if result.Location() != time.UTC {
				t.Errorf("expected UTC location, got %v", result.Location())
			}
		})
	}
}

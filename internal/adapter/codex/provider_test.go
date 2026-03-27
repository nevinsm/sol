package codex

import (
	"testing"

	"github.com/nevinsm/sol/internal/broker"
)

func TestCodexProviderName(t *testing.T) {
	p := &Provider{}
	if got := p.Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q", got, "codex")
	}
}

func TestCodexProviderDetectRateLimit(t *testing.T) {
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
			name:      "rate limit lowercase",
			output:    "Error: rate limit exceeded for this API key",
			wantMatch: true,
		},
		{
			name:      "Rate limit reached",
			output:    "Rate limit reached for default-model on tokens per min.",
			wantMatch: true,
		},
		{
			name:      "too many requests",
			output:    "Error 429: Too many requests. Please slow down.",
			wantMatch: true,
		},
		{
			name:      "retry after with seconds",
			output:    "Rate limit hit. Retry after 30s to continue.",
			wantMatch: true,
			wantReset: true,
		},
		{
			name:      "retry after without seconds unit",
			output:    "Please retry after some time.",
			wantMatch: true,
			wantReset: false,
		},
		{
			name:      "HTTP 429 in output",
			output:    "HTTP 429 — rate limited",
			wantMatch: true,
		},
		{
			name:      "empty output",
			output:    "",
			wantMatch: false,
		},
		{
			name:      "normal error no rate limit",
			output:    "Error: invalid API key provided",
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
			if tt.wantReset && signal != nil && signal.ResetsIn == 0 {
				t.Error("expected non-zero ResetsIn")
			}
			if !tt.wantReset && signal != nil && signal.ResetsIn != 0 {
				t.Errorf("expected zero ResetsIn, got %v", signal.ResetsIn)
			}
		})
	}
}

func TestCodexProviderCredentialExpires(t *testing.T) {
	p := &Provider{}

	tests := []struct {
		credType string
		want     bool
	}{
		{"api_key", false},
		{"oauth_token", false},
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

func TestCodexProviderRegistered(t *testing.T) {
	// The init() function should have registered the Codex provider.
	p, ok := broker.GetProvider("codex")
	if !ok {
		t.Fatal("expected Codex provider to be registered")
	}
	if p.Name() != "codex" {
		t.Errorf("registered provider name = %q, want %q", p.Name(), "codex")
	}
}

func TestCodexProviderInterfaceSatisfaction(t *testing.T) {
	var _ broker.Provider = (*Provider)(nil)
}

package codex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/nevinsm/sol/internal/broker"
)

func init() {
	broker.RegisterProvider("codex", &Provider{})
}

// ProviderHealthEndpoint is the endpoint used to probe OpenAI API liveness.
const ProviderHealthEndpoint = "https://api.openai.com/v1/models"

// Provider implements broker.Provider for the Codex runtime.
type Provider struct{}

// Compile-time interface satisfaction check.
var _ broker.Provider = (*Provider)(nil)

// Name returns "codex".
func (p *Provider) Name() string { return "codex" }

// ProbeHealth performs a lightweight HTTP request to the OpenAI API
// to check provider liveness. Any HTTP response (including 4xx) means
// the provider is reachable. Only network errors or 5xx responses
// indicate a health problem.
func (p *Provider) ProbeHealth(_ context.Context) error {
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(ProviderHealthEndpoint)
	if err != nil {
		return fmt.Errorf("provider unreachable: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()
	}()

	// 5xx = server-side problem.
	if resp.StatusCode >= 500 {
		return fmt.Errorf("provider returned %d", resp.StatusCode)
	}

	// Any other response (2xx, 3xx, 4xx) means the provider is up.
	return nil
}

// TODO: Refine rate limit patterns based on actual Codex CLI error output
// (needs runtime verification). These patterns are based on documented
// OpenAI HTTP 429 error messages.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
	regexp.MustCompile(`(?i)retry after`),
	regexp.MustCompile(`(?i)429`),
}

// retryAfterPattern extracts a "retry after N seconds" value from output.
var retryAfterPattern = regexp.MustCompile(`(?i)retry\s+after\s+(\d+)\s*s`)

// DetectRateLimit scans session output for OpenAI-specific rate limit patterns.
// Returns a *RateLimitSignal with parsed reset time info, or nil if no match.
func (p *Provider) DetectRateLimit(output string) *broker.RateLimitSignal {
	var matched bool
	for _, pat := range rateLimitPatterns {
		if pat.MatchString(output) {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}

	signal := &broker.RateLimitSignal{}

	// Try to parse "retry after N seconds" into a reset duration.
	if m := retryAfterPattern.FindStringSubmatch(output); len(m) > 1 {
		if secs, err := strconv.Atoi(m[1]); err == nil {
			signal.ResetsIn = time.Duration(secs) * time.Second
			signal.ResetsAt = time.Now().Add(signal.ResetsIn).UTC()
		}
	}

	return signal
}

// CredentialExpires reports whether the given credential type expires.
// OpenAI API keys do not expire. If Codex adds OAuth-based auth in the
// future, that credential type would need updating here.
func (p *Provider) CredentialExpires(_ string) bool {
	return false
}

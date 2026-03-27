package claude

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/broker"
)

func init() {
	broker.RegisterProvider("claude", &Provider{})
}

// ProviderHealthEndpoint is the endpoint used to probe Claude API liveness.
const ProviderHealthEndpoint = "https://api.anthropic.com/v1/models"

// Provider implements broker.Provider for the Claude runtime.
type Provider struct{}

// Compile-time interface satisfaction check.
var _ broker.Provider = (*Provider)(nil)

// Name returns "claude".
func (p *Provider) Name() string { return "claude" }

// ProbeHealth performs a lightweight HTTP request to the Anthropic API
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

// rateLimitPatterns match Claude rate limit error messages in pane output.
var rateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`You've hit your .*limit`),
	regexp.MustCompile(`limit\s*·\s*resets\s+\d+[:\d]*(am|pm)`),
	regexp.MustCompile(`Stop and wait for limit to reset`),
	regexp.MustCompile(`API Error:\s*Rate limit reached`),
	regexp.MustCompile(`OAuth token revoked`),
	regexp.MustCompile(`OAuth token has expired`),
}

// resetTimePattern extracts a reset time from rate limit messages.
var resetTimePattern = regexp.MustCompile(`resets\s+(\d{1,2}(?::\d{2})?\s*(?:am|pm))`)

// DetectRateLimit scans session output for Claude-specific rate limit patterns.
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

	// Try to parse reset time.
	if m := resetTimePattern.FindStringSubmatch(output); len(m) > 1 {
		if t, err := parseResetTime(m[1]); err == nil {
			signal.ResetsAt = t
			signal.ResetsIn = time.Until(t)
		}
	}

	return signal
}

// CredentialExpires reports whether the given credential type expires.
// OAuth tokens expire; API keys do not.
func (p *Provider) CredentialExpires(credType string) bool {
	return credType == "oauth_token"
}

// parseResetTime parses a time string like "3:45pm" or "4am" into a time.Time
// on the current day (or next day if the time has already passed).
func parseResetTime(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))

	var hour, minute int
	var ampm string

	// Try "3:45pm" format first, then "4pm" format.
	if n, _ := fmt.Sscanf(s, "%d:%d%s", &hour, &minute, &ampm); n == 3 {
		// parsed
	} else if n, _ := fmt.Sscanf(s, "%d%s", &hour, &ampm); n == 2 {
		minute = 0
	} else {
		return time.Time{}, fmt.Errorf("cannot parse reset time %q", s)
	}

	ampm = strings.TrimSpace(ampm)
	if ampm == "pm" && hour != 12 {
		hour += 12
	} else if ampm == "am" && hour == 12 {
		hour = 0
	}

	now := time.Now()
	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
	// Convert to UTC for consistent storage.
	reset = reset.UTC()
	if reset.Before(now) {
		reset = reset.Add(24 * time.Hour)
	}

	return reset, nil
}

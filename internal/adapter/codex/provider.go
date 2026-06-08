package codex

import (
	"regexp"
	"strconv"
	"time"

	"github.com/nevinsm/sol/internal/broker"
)

func init() {
	broker.RegisterProvider("codex", &Provider{})
}

// Provider implements broker.Provider for the Codex runtime.
type Provider struct{}

// Compile-time interface satisfaction check.
var _ broker.Provider = (*Provider)(nil)

// Name returns "codex".
func (p *Provider) Name() string { return "codex" }

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

package escalation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/nevinsm/gt/internal/store"
)

// WebhookNotifier POSTs escalation data to an HTTP endpoint.
type WebhookNotifier struct {
	URL     string
	Client  *http.Client
	Timeout time.Duration
}

// NewWebhookNotifier creates a WebhookNotifier with default timeout of 10 seconds.
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{
		URL:     url,
		Client:  &http.Client{},
		Timeout: 10 * time.Second,
	}
}

// webhookPayload is the JSON body sent to the webhook endpoint.
type webhookPayload struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Source      string `json:"source"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// Notify sends a POST request with JSON body containing escalation details.
func (n *WebhookNotifier) Notify(ctx context.Context, esc store.Escalation) error {
	payload := webhookPayload{
		ID:          esc.ID,
		Severity:    esc.Severity,
		Source:      esc.Source,
		Description: esc.Description,
		Status:      esc.Status,
		CreatedAt:   esc.CreatedAt.Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	timeout := n.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gt-escalation/1.0")

	resp, err := n.Client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// Name returns "webhook".
func (n *WebhookNotifier) Name() string { return "webhook" }

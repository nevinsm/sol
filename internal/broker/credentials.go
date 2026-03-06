package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// OAuthTokenEndpoint is the Anthropic OAuth token endpoint.
const OAuthTokenEndpoint = "https://console.anthropic.com/v1/oauth/token"

// OAuthClientID is the Claude Code OAuth client ID.
const OAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"

// Credentials represents the contents of a .credentials.json file.
type Credentials struct {
	ClaudeAIOAuth *OAuthCredentials `json:"claudeAiOauth,omitempty"`
}

// OAuthCredentials holds OAuth token data.
type OAuthCredentials struct {
	AccessToken      string   `json:"accessToken"`
	RefreshToken     string   `json:"refreshToken,omitempty"`
	ExpiresAt        int64    `json:"expiresAt"`
	Scopes           []string `json:"scopes"`
	SubscriptionType string   `json:"subscriptionType,omitempty"`
	RateLimitTier    string   `json:"rateLimitTier,omitempty"`
}

// ExpiresAtTime returns the expiration time.
func (c *OAuthCredentials) ExpiresAtTime() time.Time {
	return time.UnixMilli(c.ExpiresAt)
}

// TimeUntilExpiry returns the duration until the token expires.
func (c *OAuthCredentials) TimeUntilExpiry() time.Duration {
	return time.Until(c.ExpiresAtTime())
}

// ReadCredentials reads and parses a .credentials.json file.
func ReadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials %q: %w", path, err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials %q: %w", path, err)
	}

	return &creds, nil
}

// AccessTokenOnly returns a copy of the credentials with the refresh token
// removed. This is what gets written to agent config dirs.
func (c *Credentials) AccessTokenOnly() *Credentials {
	if c.ClaudeAIOAuth == nil {
		return &Credentials{}
	}
	return &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken:      c.ClaudeAIOAuth.AccessToken,
			ExpiresAt:        c.ClaudeAIOAuth.ExpiresAt,
			Scopes:           c.ClaudeAIOAuth.Scopes,
			SubscriptionType: c.ClaudeAIOAuth.SubscriptionType,
			RateLimitTier:    c.ClaudeAIOAuth.RateLimitTier,
		},
	}
}

// WriteCredentials writes credentials to a file atomically (write tmp + rename).
func WriteCredentials(path string, creds *Credentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to commit credentials: %w", err)
	}
	return nil
}

// refreshRequest is the POST body for the OAuth token refresh endpoint.
type refreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
}

// refreshResponse is the response from the OAuth token refresh endpoint.
type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// RefreshFn is the signature for a function that refreshes an OAuth token.
// Abstracted for testing.
type RefreshFn func(refreshToken string) (*refreshResponse, error)

// RefreshOAuthToken exchanges a refresh token for a new access token
// via the Anthropic OAuth endpoint. Returns the raw response containing
// new access and refresh tokens.
func RefreshOAuthToken(refreshToken string) (*refreshResponse, error) {
	reqBody := refreshRequest{
		GrantType:    "refresh_token",
		RefreshToken: refreshToken,
		ClientID:     OAuthClientID,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	resp, err := http.Post(OAuthTokenEndpoint, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to send refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read refresh response: %w", err)
	}

	var result refreshResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := result.Error
		if result.ErrorDesc != "" {
			errMsg += ": " + result.ErrorDesc
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("token refresh failed: %s", errMsg)
	}

	return &result, nil
}

// ApplyRefreshResponse updates an OAuthCredentials with the response from
// a token refresh. Updates the source credentials (with new refresh token)
// and returns a new access-token-only copy for agents.
func ApplyRefreshResponse(src *Credentials, resp *refreshResponse) *Credentials {
	now := time.Now()
	expiresAt := now.Add(time.Duration(resp.ExpiresIn) * time.Second).UnixMilli()

	src.ClaudeAIOAuth.AccessToken = resp.AccessToken
	src.ClaudeAIOAuth.RefreshToken = resp.RefreshToken
	src.ClaudeAIOAuth.ExpiresAt = expiresAt

	return src
}

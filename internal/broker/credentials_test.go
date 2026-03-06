package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken:      "sk-ant-oat01-test",
			RefreshToken:     "sk-ant-ort01-test",
			ExpiresAt:        time.Now().Add(8 * time.Hour).UnixMilli(),
			Scopes:           []string{"user:inference"},
			SubscriptionType: "max",
			RateLimitTier:    "default_claude_max_20x",
		},
	}

	data, _ := json.MarshalIndent(creds, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}

	if got.ClaudeAIOAuth.AccessToken != "sk-ant-oat01-test" {
		t.Errorf("got access token %q, want %q", got.ClaudeAIOAuth.AccessToken, "sk-ant-oat01-test")
	}
	if got.ClaudeAIOAuth.RefreshToken != "sk-ant-ort01-test" {
		t.Errorf("got refresh token %q, want %q", got.ClaudeAIOAuth.RefreshToken, "sk-ant-ort01-test")
	}
	if len(got.ClaudeAIOAuth.Scopes) != 1 || got.ClaudeAIOAuth.Scopes[0] != "user:inference" {
		t.Errorf("got scopes %v, want [user:inference]", got.ClaudeAIOAuth.Scopes)
	}
}

func TestAccessTokenOnly(t *testing.T) {
	creds := &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken:      "sk-ant-oat01-test",
			RefreshToken:     "sk-ant-ort01-test",
			ExpiresAt:        time.Now().Add(8 * time.Hour).UnixMilli(),
			Scopes:           []string{"user:inference"},
			SubscriptionType: "max",
			RateLimitTier:    "default_claude_max_20x",
		},
	}

	accessOnly := creds.AccessTokenOnly()

	if accessOnly.ClaudeAIOAuth.AccessToken != "sk-ant-oat01-test" {
		t.Error("access token should be preserved")
	}
	if accessOnly.ClaudeAIOAuth.RefreshToken != "" {
		t.Errorf("refresh token should be empty, got %q", accessOnly.ClaudeAIOAuth.RefreshToken)
	}
	if accessOnly.ClaudeAIOAuth.SubscriptionType != "max" {
		t.Error("subscription type should be preserved")
	}
	if accessOnly.ClaudeAIOAuth.RateLimitTier != "default_claude_max_20x" {
		t.Error("rate limit tier should be preserved")
	}

	// Verify original is unchanged.
	if creds.ClaudeAIOAuth.RefreshToken != "sk-ant-ort01-test" {
		t.Error("original refresh token should be unchanged")
	}
}

func TestWriteCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken: "sk-ant-oat01-test",
			ExpiresAt:   time.Now().Add(8 * time.Hour).UnixMilli(),
			Scopes:      []string{"user:inference"},
		},
	}

	if err := WriteCredentials(path, creds); err != nil {
		t.Fatal(err)
	}

	// Read back and verify.
	got, err := ReadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudeAIOAuth.AccessToken != "sk-ant-oat01-test" {
		t.Errorf("got access token %q, want %q", got.ClaudeAIOAuth.AccessToken, "sk-ant-oat01-test")
	}

	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("got permissions %o, want 0600", info.Mode().Perm())
	}
}

func TestApplyRefreshResponse(t *testing.T) {
	src := &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken:      "old-access",
			RefreshToken:     "old-refresh",
			ExpiresAt:        time.Now().Add(-1 * time.Hour).UnixMilli(),
			Scopes:           []string{"user:inference"},
			SubscriptionType: "max",
		},
	}

	resp := &refreshResponse{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresIn:    28800, // 8 hours
	}

	updated := ApplyRefreshResponse(src, resp)

	if updated.ClaudeAIOAuth.AccessToken != "new-access" {
		t.Errorf("got access token %q, want %q", updated.ClaudeAIOAuth.AccessToken, "new-access")
	}
	if updated.ClaudeAIOAuth.RefreshToken != "new-refresh" {
		t.Errorf("got refresh token %q, want %q", updated.ClaudeAIOAuth.RefreshToken, "new-refresh")
	}

	// Verify expiry is ~8 hours from now.
	ttl := updated.ClaudeAIOAuth.TimeUntilExpiry()
	if ttl < 7*time.Hour || ttl > 9*time.Hour {
		t.Errorf("expected expiry ~8h from now, got %s", ttl)
	}

	// Verify metadata is preserved.
	if updated.ClaudeAIOAuth.SubscriptionType != "max" {
		t.Errorf("subscription type should be preserved, got %q", updated.ClaudeAIOAuth.SubscriptionType)
	}
}

func TestTimeUntilExpiry(t *testing.T) {
	future := &OAuthCredentials{
		ExpiresAt: time.Now().Add(2 * time.Hour).UnixMilli(),
	}
	if ttl := future.TimeUntilExpiry(); ttl < 1*time.Hour || ttl > 3*time.Hour {
		t.Errorf("expected ~2h TTL, got %s", ttl)
	}

	past := &OAuthCredentials{
		ExpiresAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
	}
	if ttl := past.TimeUntilExpiry(); ttl > 0 {
		t.Errorf("expected negative TTL for expired token, got %s", ttl)
	}
}

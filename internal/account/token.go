package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/fileutil"
)

// Token represents stored credentials for a Claude account.
type Token struct {
	Type      string     `json:"type"`
	Token     string     `json:"token"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// TokenPath returns the path to the token.json file for the given account handle.
func TokenPath(handle string) string {
	return filepath.Join(config.AccountDir(handle), "token.json")
}

// validateToken checks that a Token has valid type and non-empty token.
func validateToken(tok *Token) error {
	if tok.Type != "oauth_token" && tok.Type != "api_key" {
		return fmt.Errorf("invalid token type %q: must be \"oauth_token\" or \"api_key\"", tok.Type)
	}
	if tok.Token == "" {
		return fmt.Errorf("token must not be empty")
	}
	return nil
}

// ReadToken reads token.json for the given account handle.
func ReadToken(handle string) (*Token, error) {
	path := TokenPath(handle)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read token for account %q: %w", handle, err)
	}

	var tok Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse token for account %q: %w", handle, err)
	}

	if err := validateToken(&tok); err != nil {
		return nil, fmt.Errorf("invalid token for account %q: %w", handle, err)
	}

	return &tok, nil
}

// WriteToken atomically writes token.json for the given account handle.
func WriteToken(handle string, tok *Token) error {
	if err := validateToken(tok); err != nil {
		return err
	}

	dir := config.AccountDir(handle)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create account directory: %w", err)
	}

	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := fileutil.AtomicWrite(TokenPath(handle), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write token: %w", err)
	}
	return nil
}

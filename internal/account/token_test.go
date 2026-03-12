package account

import (
	"testing"
	"time"
)

func TestReadWriteTokenRoundTrip(t *testing.T) {
	setupTestHome(t)

	// Write an oauth_token.
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(365 * 24 * time.Hour)
	tok := &Token{
		Type:      "oauth_token",
		Token:     "sk-ant-oat01-xxx",
		CreatedAt: now,
		ExpiresAt: &expires,
	}

	if err := WriteToken("personal", tok); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	got, err := ReadToken("personal")
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}

	if got.Type != tok.Type {
		t.Errorf("Type = %q, want %q", got.Type, tok.Type)
	}
	if got.Token != tok.Token {
		t.Errorf("Token = %q, want %q", got.Token, tok.Token)
	}
	if !got.CreatedAt.Equal(tok.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, tok.CreatedAt)
	}
	if got.ExpiresAt == nil {
		t.Fatal("ExpiresAt = nil, want non-nil")
	}
	if !got.ExpiresAt.Equal(*tok.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, tok.ExpiresAt)
	}
}

func TestReadWriteAPIKeyRoundTrip(t *testing.T) {
	setupTestHome(t)

	now := time.Now().UTC().Truncate(time.Second)
	tok := &Token{
		Type:      "api_key",
		Token:     "sk-ant-xxx",
		CreatedAt: now,
		ExpiresAt: nil,
	}

	if err := WriteToken("work", tok); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	got, err := ReadToken("work")
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}

	if got.Type != tok.Type {
		t.Errorf("Type = %q, want %q", got.Type, tok.Type)
	}
	if got.Token != tok.Token {
		t.Errorf("Token = %q, want %q", got.Token, tok.Token)
	}
	if got.ExpiresAt != nil {
		t.Errorf("ExpiresAt = %v, want nil", got.ExpiresAt)
	}
}

func TestWriteTokenValidation(t *testing.T) {
	setupTestHome(t)

	tests := []struct {
		name string
		tok  Token
	}{
		{
			name: "empty token",
			tok:  Token{Type: "oauth_token", Token: ""},
		},
		{
			name: "invalid type",
			tok:  Token{Type: "bad_type", Token: "sk-ant-xxx"},
		},
		{
			name: "empty type",
			tok:  Token{Type: "", Token: "sk-ant-xxx"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WriteToken("personal", &tt.tok)
			if err == nil {
				t.Errorf("WriteToken(%v) = nil, want error", tt.tok)
			}
		})
	}
}

func TestTokenPath(t *testing.T) {
	setupTestHome(t)

	path := TokenPath("personal")
	if path == "" {
		t.Error("TokenPath returned empty string")
	}
}

func TestReadTokenNotFound(t *testing.T) {
	setupTestHome(t)

	_, err := ReadToken("nonexistent")
	if err == nil {
		t.Error("ReadToken on missing account: expected error, got nil")
	}
}

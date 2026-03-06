package broker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverAgentDirs(t *testing.T) {
	// Set up a temporary SOL_HOME.
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Create a world with agent config dirs.
	world := "testworld"
	worldDir := filepath.Join(solHome, world)

	// Create agent config dirs with .account files.
	agents := []struct {
		role    string
		name    string
		account string
	}{
		{"outposts", "Toast", "alice"},
		{"outposts", "Ember", "alice"},
		{"outposts", "Nova", "bob"},
		{"forge", "forge", "alice"},
	}

	for _, a := range agents {
		configDir := filepath.Join(worldDir, ".claude-config", a.role, a.name)
		os.MkdirAll(configDir, 0o755)
		os.WriteFile(filepath.Join(configDir, ".account"), []byte(a.account+"\n"), 0o644)
	}

	b := New(Config{}, nil)

	// Find dirs for "alice".
	dirs, err := b.discoverAgentDirs("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 3 {
		t.Errorf("expected 3 dirs for alice, got %d: %v", len(dirs), dirs)
	}

	// Find dirs for "bob".
	dirs, err = b.discoverAgentDirs("bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 {
		t.Errorf("expected 1 dir for bob, got %d: %v", len(dirs), dirs)
	}

	// Find dirs for nonexistent account.
	dirs, err = b.discoverAgentDirs("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 0 {
		t.Errorf("expected 0 dirs for nobody, got %d", len(dirs))
	}
}

func TestReadWriteAccountFile(t *testing.T) {
	dir := t.TempDir()

	// Write account file.
	if err := WriteAccountFile(dir, "alice"); err != nil {
		t.Fatal(err)
	}

	// Read it back.
	got := ReadAccountFile(dir)
	if got != "alice" {
		t.Errorf("got %q, want %q", got, "alice")
	}

	// Non-existent returns empty.
	got = ReadAccountFile(filepath.Join(dir, "nope"))
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestBrokerPatrolRefresh(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Set up account registry.
	accountsDir := filepath.Join(solHome, ".accounts")
	os.MkdirAll(filepath.Join(accountsDir, "alice"), 0o755)

	// Write account credentials (almost expired).
	creds := &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken:      "old-access",
			RefreshToken:     "old-refresh",
			ExpiresAt:        time.Now().Add(5 * time.Minute).UnixMilli(), // expires in 5 min
			Scopes:           []string{"user:inference"},
			SubscriptionType: "max",
		},
	}
	srcPath := filepath.Join(accountsDir, "alice", ".credentials.json")
	if err := WriteCredentials(srcPath, creds); err != nil {
		t.Fatal(err)
	}

	// Write account registry.
	registry := map[string]any{
		"accounts": map[string]any{
			"alice": map[string]string{
				"config_dir": filepath.Join(accountsDir, "alice"),
			},
		},
		"default": "alice",
	}
	regData, _ := json.Marshal(registry)
	os.WriteFile(filepath.Join(accountsDir, "accounts.json"), regData, 0o644)

	// Create an agent config dir that uses alice.
	agentDir := filepath.Join(solHome, "testworld", ".claude-config", "outposts", "Toast")
	os.MkdirAll(agentDir, 0o755)
	os.WriteFile(filepath.Join(agentDir, ".account"), []byte("alice\n"), 0o644)

	// Create broker with a mock refresh function.
	b := New(Config{RefreshMargin: 30 * time.Minute}, nil)
	b.SetRefreshFn(func(refreshToken string) (*refreshResponse, error) {
		if refreshToken != "old-refresh" {
			t.Errorf("unexpected refresh token: %q", refreshToken)
		}
		return &refreshResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    28800,
		}, nil
	})

	// Run one patrol.
	b.patrol()

	// Verify source credentials were updated with new refresh token.
	srcCreds, err := ReadCredentials(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if srcCreds.ClaudeAIOAuth.AccessToken != "new-access" {
		t.Errorf("source access token not updated: %q", srcCreds.ClaudeAIOAuth.AccessToken)
	}
	if srcCreds.ClaudeAIOAuth.RefreshToken != "new-refresh" {
		t.Errorf("source refresh token not updated: %q", srcCreds.ClaudeAIOAuth.RefreshToken)
	}

	// Verify agent credentials have access token but NO refresh token.
	agentCreds, err := ReadCredentials(filepath.Join(agentDir, ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if agentCreds.ClaudeAIOAuth.AccessToken != "new-access" {
		t.Errorf("agent access token not updated: %q", agentCreds.ClaudeAIOAuth.AccessToken)
	}
	if agentCreds.ClaudeAIOAuth.RefreshToken != "" {
		t.Errorf("agent should have no refresh token, got %q", agentCreds.ClaudeAIOAuth.RefreshToken)
	}

	// Verify heartbeat was written.
	hb, err := ReadHeartbeat()
	if err != nil {
		t.Fatal(err)
	}
	if hb == nil {
		t.Fatal("heartbeat should exist")
	}
	if hb.Refreshed != 1 {
		t.Errorf("expected 1 refresh, got %d", hb.Refreshed)
	}
	if hb.AgentDirs != 1 {
		t.Errorf("expected 1 agent dir, got %d", hb.AgentDirs)
	}
}

func TestBrokerPatrolNoRefreshNeeded(t *testing.T) {
	solHome := t.TempDir()
	t.Setenv("SOL_HOME", solHome)

	// Set up account with token far from expiry.
	accountsDir := filepath.Join(solHome, ".accounts")
	os.MkdirAll(filepath.Join(accountsDir, "alice"), 0o755)

	creds := &Credentials{
		ClaudeAIOAuth: &OAuthCredentials{
			AccessToken:  "good-access",
			RefreshToken: "good-refresh",
			ExpiresAt:    time.Now().Add(7 * time.Hour).UnixMilli(), // 7h left
			Scopes:       []string{"user:inference"},
		},
	}
	srcPath := filepath.Join(accountsDir, "alice", ".credentials.json")
	WriteCredentials(srcPath, creds)

	registry := map[string]any{
		"accounts": map[string]any{
			"alice": map[string]string{"config_dir": filepath.Join(accountsDir, "alice")},
		},
		"default": "alice",
	}
	regData, _ := json.Marshal(registry)
	os.WriteFile(filepath.Join(accountsDir, "accounts.json"), regData, 0o644)

	// Agent config dir.
	agentDir := filepath.Join(solHome, "testworld", ".claude-config", "outposts", "Toast")
	os.MkdirAll(agentDir, 0o755)
	os.WriteFile(filepath.Join(agentDir, ".account"), []byte("alice\n"), 0o644)

	refreshCalled := false
	b := New(Config{RefreshMargin: 30 * time.Minute}, nil)
	b.SetRefreshFn(func(refreshToken string) (*refreshResponse, error) {
		refreshCalled = true
		return nil, nil
	})

	b.patrol()

	if refreshCalled {
		t.Error("refresh should NOT have been called, token is far from expiry")
	}

	// Agent should still get access-token-only credentials.
	agentCreds, err := ReadCredentials(filepath.Join(agentDir, ".credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if agentCreds.ClaudeAIOAuth.AccessToken != "good-access" {
		t.Errorf("agent should have current access token, got %q", agentCreds.ClaudeAIOAuth.AccessToken)
	}
	if agentCreds.ClaudeAIOAuth.RefreshToken != "" {
		t.Errorf("agent should have no refresh token, got %q", agentCreds.ClaudeAIOAuth.RefreshToken)
	}
}

func TestHeartbeatStale(t *testing.T) {
	hb := &Heartbeat{
		Timestamp: time.Now().Add(-15 * time.Minute),
	}
	if !hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 15m old should be stale with 10m threshold")
	}

	hb.Timestamp = time.Now().Add(-5 * time.Minute)
	if hb.IsStale(10 * time.Minute) {
		t.Error("heartbeat 5m old should not be stale with 10m threshold")
	}
}

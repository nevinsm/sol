package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/config"
)

// writeWorldConfig creates a minimal world.toml for a world under solHome.
func writeWorldConfig(t *testing.T, solHome, world, runtime string) {
	t.Helper()
	worldDir := filepath.Join(solHome, world)
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	if runtime != "" {
		content = "[agents]\ndefault_runtime = \"" + runtime + "\"\n"
	}
	cfg := filepath.Join(solHome, world, "world.toml")
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Point SOL_HOME so config.LoadWorldConfig finds it.
	t.Setenv("SOL_HOME", solHome)
}

// --- CheckRuntimeCredentials tests ---

func TestCheckRuntimeCredentialsMissingAll(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Remove credential env vars from the process environment.
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	// Unset them fully so the check doesn't see stale values.
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	os.Unsetenv("ANTHROPIC_API_KEY")

	writeWorldConfig(t, dir, "myworld", "claude")

	results := CheckRuntimeCredentials(dir, []string{"myworld"})

	// Expect exactly one result for (myworld, claude).
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]

	// Should be passed=true (advisory), warning=true.
	if !r.Passed {
		t.Errorf("expected Passed=true (warning, not failure), got Passed=false")
	}
	if !r.Warning {
		t.Errorf("expected Warning=true, got Warning=false")
	}
	// Message should mention the world and runtime.
	if !strings.Contains(r.Message, "myworld") {
		t.Errorf("expected Message to contain 'myworld', got: %s", r.Message)
	}
	if !strings.Contains(r.Message, "claude") {
		t.Errorf("expected Message to contain 'claude', got: %s", r.Message)
	}
	// Message should list the env var names.
	if !strings.Contains(r.Message, "ANTHROPIC_API_KEY") && !strings.Contains(r.Message, "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Errorf("expected Message to mention credential env var names, got: %s", r.Message)
	}
	// Message should NOT mention runtime-specific tooling names.
	if strings.Contains(r.Message, "claude login") || strings.Contains(r.Message, "claude setup-token") {
		t.Errorf("expected Message to be runtime-generic, but found tooling reference: %s", r.Message)
	}
	// Fix should be present.
	if r.Fix == "" {
		t.Error("expected non-empty Fix, got empty string")
	}
	// Remediate should be non-nil (can create the .env template).
	if r.Remediate == nil {
		t.Error("expected non-nil Remediate, got nil")
	}
	// Name should be structured as "credentials:<world>:<runtime>".
	if r.Name != "credentials:myworld:claude" {
		t.Errorf("expected Name='credentials:myworld:claude', got %q", r.Name)
	}
	// Message should reference docs/credentials.md.
	if !strings.Contains(r.Message, "docs/credentials.md") {
		t.Errorf("expected Message to reference docs/credentials.md, got: %s", r.Message)
	}
}

func TestCheckRuntimeCredentialsOAuthPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "tok_test_value")
	os.Unsetenv("ANTHROPIC_API_KEY")

	writeWorldConfig(t, dir, "myworld", "claude")

	results := CheckRuntimeCredentials(dir, []string{"myworld"})

	// Expect exactly one result for (myworld, claude).
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed {
		t.Errorf("expected Passed=true, got false: %s", r.Message)
	}
	if r.Warning {
		t.Errorf("expected Warning=false (credential present), got Warning=true")
	}
}

func TestCheckRuntimeCredentialsAPIKeyPresent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	writeWorldConfig(t, dir, "myworld", "claude")

	results := CheckRuntimeCredentials(dir, []string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected Passed=true Warning=false, got Passed=%v Warning=%v: %s", r.Passed, r.Warning, r.Message)
	}
}

func TestCheckRuntimeCredentialsFromEnvFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	os.Unsetenv("ANTHROPIC_API_KEY")

	// Write the credential to $SOL_HOME/.env (sphere-level).
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=sk-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	writeWorldConfig(t, dir, "myworld", "claude")

	results := CheckRuntimeCredentials(dir, []string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Passed || r.Warning {
		t.Errorf("expected Passed=true Warning=false (cred in .env file), got Passed=%v Warning=%v: %s", r.Passed, r.Warning, r.Message)
	}
}

func TestCheckRuntimeCredentialsCodexMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.Unsetenv("OPENAI_API_KEY")

	writeWorldConfig(t, dir, "codexworld", "codex")

	results := CheckRuntimeCredentials(dir, []string{"codexworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if !r.Passed || !r.Warning {
		t.Errorf("expected Passed=true Warning=true, got Passed=%v Warning=%v", r.Passed, r.Warning)
	}
	if !strings.Contains(r.Message, "OPENAI_API_KEY") {
		t.Errorf("expected Message to mention OPENAI_API_KEY, got: %s", r.Message)
	}
}

func TestCheckRuntimeCredentialsTwoWorldsDeduplicated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	os.Unsetenv("ANTHROPIC_API_KEY")

	writeWorldConfig(t, dir, "world1", "claude")
	writeWorldConfig(t, dir, "world2", "claude")

	results := CheckRuntimeCredentials(dir, []string{"world1", "world2"})
	// Both worlds use claude with no credentials — should get one warning per world.
	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per world), got %d: %+v", len(results), results)
	}
	for _, r := range results {
		if !r.Warning {
			t.Errorf("expected Warning=true for all results, got: %+v", r)
		}
	}
}

func TestCheckRuntimeCredentialsNoWorlds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	results := CheckRuntimeCredentials(dir, nil)
	if len(results) != 0 {
		t.Errorf("expected no results for empty worlds list, got %d", len(results))
	}
}

// --- createSphereEnvTemplate tests ---

func TestCreateSphereEnvTemplateCreatesFile(t *testing.T) {
	dir := t.TempDir()

	if err := createSphereEnvTemplate(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(dir, ".env")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected .env to exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %04o", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	// Must be generic — no runtime-specific tooling names.
	for _, forbidden := range []string{"claude", "codex", "setup-token", "login"} {
		if strings.Contains(strings.ToLower(content), forbidden) {
			t.Errorf("template contains forbidden runtime-specific term %q: %s", forbidden, content)
		}
	}
	// Must mention docs/credentials.md.
	if !strings.Contains(content, "docs/credentials.md") {
		t.Errorf("expected template to mention docs/credentials.md, got: %s", content)
	}
}

func TestCreateSphereEnvTemplateNoopIfExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	original := "EXISTING_KEY=value\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := createSphereEnvTemplate(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("expected file unchanged, got: %q", string(data))
	}
}

func TestCheckRuntimeCredentialsRemediateCreatesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)
	os.Unsetenv("CLAUDE_CODE_OAUTH_TOKEN")
	os.Unsetenv("ANTHROPIC_API_KEY")

	writeWorldConfig(t, dir, "myworld", "claude")

	results := CheckRuntimeCredentials(dir, []string{"myworld"})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Remediate == nil {
		t.Fatal("expected non-nil Remediate")
	}

	// Run the remediation.
	if err := r.Remediate(); err != nil {
		t.Fatalf("Remediate() returned error: %v", err)
	}

	// Verify the .env file was created.
	envPath := filepath.Join(dir, ".env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("expected .env to exist after remediation: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected mode 0600, got %04o", info.Mode().Perm())
	}

	// Point SOL_HOME to dir so config.LoadWorldConfig finds world.toml.
	_ = config.Home() // warm up
}

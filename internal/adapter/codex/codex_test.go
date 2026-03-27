package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/adapter"
)

func newAdapter() *Adapter {
	return New()
}

// ---- Name ----

func TestName(t *testing.T) {
	a := newAdapter()
	if a.Name() != "codex" {
		t.Errorf("expected Name()=codex, got %q", a.Name())
	}
}

// ---- CalloutCommand ----

func TestCalloutCommand(t *testing.T) {
	a := newAdapter()
	if got := a.CalloutCommand(); got != "codex exec --json" {
		t.Errorf("CalloutCommand() = %q, want %q", got, "codex exec --json")
	}
}

// ---- BuildCommand ----

func TestBuildCommandBasic(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Prompt:      "Hello agent",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.HasPrefix(cmd, "codex --full-auto") {
		t.Errorf("expected codex --full-auto prefix, got: %q", cmd)
	}
	if !strings.Contains(cmd, "Hello agent") {
		t.Errorf("expected prompt in command, got: %q", cmd)
	}
}

func TestBuildCommandWithModel(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Model:       "o3",
		Prompt:      "go",
	}
	cmd := a.BuildCommand(ctx)

	if !strings.Contains(cmd, "--model o3") {
		t.Errorf("expected --model flag, got: %q", cmd)
	}
}

func TestBuildCommandContinue(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
		Continue:    true,
		Prompt:      "resume context",
	}
	cmd := a.BuildCommand(ctx)

	if cmd != "codex resume" {
		t.Errorf("expected 'codex resume' for continue mode, got: %q", cmd)
	}
}

func TestBuildCommandNoPrompt(t *testing.T) {
	a := newAdapter()

	ctx := adapter.CommandContext{
		WorktreeDir: t.TempDir(),
	}
	cmd := a.BuildCommand(ctx)

	if cmd != "codex --full-auto" {
		t.Errorf("expected bare 'codex --full-auto', got: %q", cmd)
	}
}

func TestBuildCommandSOLSessionCommandOverride(t *testing.T) {
	t.Setenv("SOL_SESSION_COMMAND", "sleep 300")
	a := newAdapter()

	cmd := a.BuildCommand(adapter.CommandContext{WorktreeDir: t.TempDir()})
	if cmd != "sleep 300" {
		t.Errorf("expected SOL_SESSION_COMMAND override, got: %q", cmd)
	}
}

// ---- CredentialEnv ----

func TestCredentialEnvAPIKey(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "api_key", Token: "sk-abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := env["OPENAI_API_KEY"]; !ok || v != "sk-abc" {
		t.Errorf("expected OPENAI_API_KEY=sk-abc, got %v", env)
	}
}

func TestCredentialEnvUnknown(t *testing.T) {
	a := newAdapter()
	env, err := a.CredentialEnv(adapter.Credential{Type: "oauth_token", Token: "val"})
	if err == nil {
		t.Error("expected error for unrecognized credential type, got nil")
	}
	if len(env) != 0 {
		t.Errorf("expected nil/empty map for unknown credential type, got %v", env)
	}
}

func TestCredentialEnvCompletelyUnknown(t *testing.T) {
	a := newAdapter()
	_, err := a.CredentialEnv(adapter.Credential{Type: "unknown", Token: "val"})
	if err == nil {
		t.Error("expected error for unknown credential type, got nil")
	}
}

// ---- TelemetryEnv ----

func TestTelemetryEnvReturnsEmpty(t *testing.T) {
	a := newAdapter()
	env := a.TelemetryEnv(4318, "Toast", "myworld", "sol-abc123", "")
	if len(env) != 0 {
		t.Errorf("expected empty map, got %v", env)
	}
}

// ---- InjectPersona ----

func TestInjectPersona(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	content := []byte("# Agent Instructions\n\nDo the thing.\n")
	if err := a.InjectPersona(dir, content); err != nil {
		t.Fatalf("InjectPersona failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

// ---- InjectSystemPrompt ----

func TestInjectSystemPromptReplace(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	path, err := a.InjectSystemPrompt(dir, "system content", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}
	if path != "AGENTS.md" {
		t.Errorf("expected AGENTS.md, got %q", path)
	}

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}
	if string(got) != "system content" {
		t.Errorf("content mismatch: got %q", got)
	}
}

func TestInjectSystemPromptAppend(t *testing.T) {
	dir := t.TempDir()
	a := newAdapter()

	path, err := a.InjectSystemPrompt(dir, "override content", false)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}
	if path != ".codex/AGENTS.override.md" {
		t.Errorf("expected .codex/AGENTS.override.md, got %q", path)
	}

	got, err := os.ReadFile(filepath.Join(dir, ".codex", "AGENTS.override.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.override.md: %v", err)
	}
	if string(got) != "override content" {
		t.Errorf("content mismatch: got %q", got)
	}
}

// ---- ExtractTelemetry ----

func TestExtractTelemetryReturnsNil(t *testing.T) {
	a := newAdapter()
	result := a.ExtractTelemetry("some.event", map[string]string{"key": "val"})
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

// ---- Registry ----

func TestAdapterImplementsInterface(t *testing.T) {
	var _ adapter.RuntimeAdapter = (*Adapter)(nil)
}

func TestAdapterRegistered(t *testing.T) {
	// The init() function registers the adapter. Verify it's resolvable.
	a, ok := adapter.Get("codex")
	if !ok {
		t.Fatal("codex adapter not found in registry")
	}
	if a.Name() != "codex" {
		t.Errorf("expected Name()=codex, got %q", a.Name())
	}
}

package runtime_test

import (
	"testing"

	"github.com/nevinsm/sol/internal/runtime"
)

// stubRuntime is a minimal Runtime implementation for interface satisfaction tests.
// It embeds RuntimeDescriptor and implements all four interface methods.
type stubRuntime struct {
	runtime.RuntimeDescriptor
}

// Compile-time interface satisfaction check.
var _ runtime.Runtime = (*stubRuntime)(nil)

func (s *stubRuntime) Descriptor() runtime.RuntimeDescriptor { return s.RuntimeDescriptor }
func (s *stubRuntime) BuildCommand(ctx runtime.CommandContext) string {
	return "stub-runtime " + ctx.Prompt
}
func (s *stubRuntime) InstallHooks(_ string, _ runtime.HookSet) error           { return nil }
func (s *stubRuntime) ExtractTelemetry(_ string, _ map[string]string) *runtime.TelemetryRecord {
	return nil
}

// newStubDescriptor returns a synthetic RuntimeDescriptor with made-up values
// suitable for testing helpers without any Claude- or Codex-specific knowledge.
func newStubDescriptor() runtime.RuntimeDescriptor {
	return runtime.RuntimeDescriptor{
		Name:              "stub",
		PersonaFile:       "STUB.local.md",
		SkillsDir:         ".stub/skills",
		ConfigDirEnv:      "STUB_CONFIG_DIR",
		CredentialFile:    "auth.json",
		GlobalCredsPath:   "~/.stub/auth.json",
		DefaultModel:      "stub-model-v1",
		CalloutCommand:    "stub -p",
		SupportedHooks:    []string{"SessionStart", "PreCompact"},
		StaticEnv:         map[string]string{"STUB_TELEMETRY": "1"},
		CredentialEnvKeys: map[string]string{"api_key": "STUB_API_KEY", "oauth_token": "STUB_OAUTH_TOKEN"},
	}
}

func newStubRuntime() *stubRuntime {
	return &stubRuntime{RuntimeDescriptor: newStubDescriptor()}
}

// ---- Runtime interface satisfaction ----

func TestStubRuntimeImplementsInterface(t *testing.T) {
	// Compile-time: var _ runtime.Runtime = (*stubRuntime)(nil) above.
	// Runtime check: ensure methods return sensible zero values.
	r := newStubRuntime()

	d := r.Descriptor()
	if d.Name != "stub" {
		t.Errorf("Descriptor().Name = %q, want %q", d.Name, "stub")
	}

	ctx := runtime.CommandContext{Prompt: "hello"}
	cmd := r.BuildCommand(ctx)
	if cmd == "" {
		t.Error("BuildCommand returned empty string")
	}

	if err := r.InstallHooks("/tmp/worktree", runtime.HookSet{}); err != nil {
		t.Errorf("InstallHooks returned error: %v", err)
	}

	if rec := r.ExtractTelemetry("some.event", map[string]string{"model": "m"}); rec != nil {
		t.Errorf("stub ExtractTelemetry should return nil, got %+v", rec)
	}
}

// ---- RuntimeDescriptor.HasHookSupport ----

func TestHasHookSupportTrue(t *testing.T) {
	d := newStubDescriptor()
	for _, hookType := range []string{"SessionStart", "PreCompact"} {
		if !d.HasHookSupport(hookType) {
			t.Errorf("HasHookSupport(%q) = false, want true", hookType)
		}
	}
}

func TestHasHookSupportFalse(t *testing.T) {
	d := newStubDescriptor()
	for _, hookType := range []string{"TurnBoundary", "Guard", "Unknown"} {
		if d.HasHookSupport(hookType) {
			t.Errorf("HasHookSupport(%q) = true, want false", hookType)
		}
	}
}

func TestHasHookSupportEmptyList(t *testing.T) {
	d := runtime.RuntimeDescriptor{Name: "minimal"}
	if d.HasHookSupport("SessionStart") {
		t.Error("HasHookSupport on empty SupportedHooks should return false")
	}
}

// ---- Descriptor embedding pattern ----

// TestDescriptorEmbedding verifies the intended embedding pattern: an impl
// struct embeds RuntimeDescriptor and returns it from Descriptor().
func TestDescriptorEmbedding(t *testing.T) {
	r := newStubRuntime()

	// Descriptor() must return the embedded value.
	d := r.Descriptor()
	if d.Name != r.RuntimeDescriptor.Name {
		t.Errorf("Descriptor().Name = %q, want %q", d.Name, r.RuntimeDescriptor.Name)
	}
	if d.DefaultModel != r.RuntimeDescriptor.DefaultModel {
		t.Errorf("Descriptor().DefaultModel = %q, want %q", d.DefaultModel, r.RuntimeDescriptor.DefaultModel)
	}
}

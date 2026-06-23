package runtime_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/runtime"
)

// ---- WritePersonaFile ----

func TestWritePersonaCreatesFile(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor() // PersonaFile = "STUB.local.md"

	content := []byte("# Outpost Agent: Stub\n\nHello world.\n")
	if err := runtime.WritePersonaFile(d, dir, content); err != nil {
		t.Fatalf("WritePersona failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "STUB.local.md"))
	if err != nil {
		t.Fatalf("failed to read persona file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestWritePersonaOverwrites(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	_ = runtime.WritePersonaFile(d, dir, []byte("old content"))
	newContent := []byte("new content")
	if err := runtime.WritePersonaFile(d, dir, newContent); err != nil {
		t.Fatalf("WritePersona failed on overwrite: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, d.PersonaFile))
	if string(got) != string(newContent) {
		t.Errorf("expected overwrite, got %q", got)
	}
}

func TestWritePersonaCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()
	d.PersonaFile = "subdir/STUB.local.md"

	if err := runtime.WritePersonaFile(d, dir, []byte("content")); err != nil {
		t.Fatalf("WritePersona failed for nested path: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "subdir", "STUB.local.md")); err != nil {
		t.Errorf("expected nested persona file to exist: %v", err)
	}
}

// ---- InstallSkills ----

func TestInstallSkillsCreatesFiles(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor() // SkillsDir = ".stub/skills"

	skills := []runtime.Skill{
		{Name: "resolve-and-handoff", Content: "# Resolve & Handoff\n"},
		{Name: "code-review", Content: "# Code Review\n"},
	}

	if err := runtime.InstallSkills(d, dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	for _, s := range skills {
		p := filepath.Join(dir, ".stub", "skills", s.Name, "SKILL.md")
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("skill %q not found at %s: %v", s.Name, p, err)
			continue
		}
		if string(got) != s.Content {
			t.Errorf("skill %q content mismatch:\ngot:  %q\nwant: %q", s.Name, got, s.Content)
		}
		// Verify sol-managed marker.
		marker := filepath.Join(dir, ".stub", "skills", s.Name, ".sol-managed")
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("skill %q missing .sol-managed marker: %v", s.Name, err)
		}
	}
}

func TestInstallSkillsRemovesStale(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	// Pre-seed a stale sol-managed skill.
	staleDir := filepath.Join(dir, ".stub", "skills", "stale-skill")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "SKILL.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, ".sol-managed"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Install a different skill.
	skills := []runtime.Skill{{Name: "current-skill", Content: "# Current\n"}}
	if err := runtime.InstallSkills(d, dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale sol-managed skill directory should have been removed")
	}

	currentPath := filepath.Join(dir, ".stub", "skills", "current-skill", "SKILL.md")
	if _, err := os.Stat(currentPath); err != nil {
		t.Errorf("current skill should exist: %v", err)
	}
}

func TestInstallSkillsEmptyListRemovesStaleSolManaged(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	// Pre-seed a sol-managed skill.
	skillDir := filepath.Join(dir, ".stub", "skills", "old-skill")
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("old"), 0o644)
	_ = os.WriteFile(filepath.Join(skillDir, ".sol-managed"), nil, 0o644)

	if err := runtime.InstallSkills(d, dir, []runtime.Skill{}); err != nil {
		t.Fatalf("InstallSkills with empty list failed: %v", err)
	}

	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Error("old sol-managed skill should have been removed with empty input")
	}
}

func TestInstallSkillsPreservesNonSolSkills(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	// Pre-seed a custom project skill (no .sol-managed marker).
	customDir := filepath.Join(dir, ".stub", "skills", "custom-tool")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "SKILL.md"), []byte("# Custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := []runtime.Skill{{Name: "sol-skill", Content: "# Sol Skill\n"}}
	if err := runtime.InstallSkills(d, dir, skills); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Custom skill should be preserved.
	got, err := os.ReadFile(filepath.Join(customDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("custom skill was deleted: %v", err)
	}
	if string(got) != "# Custom\n" {
		t.Errorf("custom skill content changed: got %q", got)
	}
}

func TestInstallSkillsIdempotent(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	skills := []runtime.Skill{{Name: "my-skill", Content: "# My Skill\n"}}

	for i := range 3 {
		if err := runtime.InstallSkills(d, dir, skills); err != nil {
			t.Fatalf("InstallSkills call %d failed: %v", i+1, err)
		}
	}

	p := filepath.Join(dir, ".stub", "skills", "my-skill", "SKILL.md")
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("skill file missing after repeated calls: %v", err)
	}
	if string(got) != "# My Skill\n" {
		t.Errorf("skill content wrong after repeated calls: %q", got)
	}
}

// ---- InjectSystemPrompt ----

func TestInjectSystemPromptCreatesFile(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor() // SkillsDir = ".stub/skills" → prompt at ".stub/system-prompt.md"

	content := "You are a stub agent."
	relPath, err := runtime.InjectSystemPrompt(d, dir, content, true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt failed: %v", err)
	}

	if relPath != ".stub/system-prompt.md" {
		t.Errorf("relPath = %q, want %q", relPath, ".stub/system-prompt.md")
	}

	got, err := os.ReadFile(filepath.Join(dir, ".stub", "system-prompt.md"))
	if err != nil {
		t.Fatalf("failed to read system prompt: %v", err)
	}
	if string(got) != content {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, content)
	}
}

func TestInjectSystemPromptReplace(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	_, _ = runtime.InjectSystemPrompt(d, dir, "original content", true)
	_, err := runtime.InjectSystemPrompt(d, dir, "new content", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt replace failed: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, ".stub", "system-prompt.md"))
	if string(got) != "new content" {
		t.Errorf("expected replacement, got %q", got)
	}
}

func TestInjectSystemPromptAppend(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	_, _ = runtime.InjectSystemPrompt(d, dir, "first chunk", true)
	_, err := runtime.InjectSystemPrompt(d, dir, "second chunk", false)
	if err != nil {
		t.Fatalf("InjectSystemPrompt append failed: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, ".stub", "system-prompt.md"))
	if !strings.Contains(string(got), "first chunk") {
		t.Errorf("expected first chunk in appended content, got %q", got)
	}
	if !strings.Contains(string(got), "second chunk") {
		t.Errorf("expected second chunk in appended content, got %q", got)
	}
}

func TestInjectSystemPromptAppendToEmpty(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()

	// Append with no existing file — should create and write content.
	relPath, err := runtime.InjectSystemPrompt(d, dir, "initial content", false)
	if err != nil {
		t.Fatalf("InjectSystemPrompt append to empty failed: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(relPath)))
	if string(got) != "initial content" {
		t.Errorf("expected %q, got %q", "initial content", got)
	}
}

func TestInjectSystemPromptTopLevelSkillsDir(t *testing.T) {
	dir := t.TempDir()
	d := newStubDescriptor()
	d.SkillsDir = "skills" // no parent subdir

	relPath, err := runtime.InjectSystemPrompt(d, dir, "top-level content", true)
	if err != nil {
		t.Fatalf("InjectSystemPrompt with top-level skills dir failed: %v", err)
	}

	if relPath != "system-prompt.md" {
		t.Errorf("relPath = %q, want %q", relPath, "system-prompt.md")
	}

	got, err := os.ReadFile(filepath.Join(dir, "system-prompt.md"))
	if err != nil {
		t.Fatalf("expected top-level system-prompt.md: %v", err)
	}
	if string(got) != "top-level content" {
		t.Errorf("content mismatch: %q", got)
	}
}

// ---- EnsureConfigDir ----

func TestEnsureConfigDirCreatesDirectory(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor() // Name = "stub", ConfigDirEnv = "STUB_CONFIG_DIR"

	res, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Toast")
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	if _, err := os.Stat(res.Dir); err != nil {
		t.Errorf("expected config dir to exist at %q: %v", res.Dir, err)
	}

	// Path should be <worldDir>/.stub-config/outposts/Toast
	wantSuffix := filepath.Join(".stub-config", "outposts", "Toast")
	if !strings.HasSuffix(res.Dir, wantSuffix) {
		t.Errorf("config dir = %q, want suffix %q", res.Dir, wantSuffix)
	}
}

func TestEnsureConfigDirReturnsEnvVar(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()

	res, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Toast")
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	if v, ok := res.EnvVar["STUB_CONFIG_DIR"]; !ok {
		t.Error("expected STUB_CONFIG_DIR in EnvVar")
	} else if v != res.Dir {
		t.Errorf("STUB_CONFIG_DIR = %q, want %q", v, res.Dir)
	}
}

func TestEnsureConfigDirCreatesCredentialSymlink(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()
	// Use an absolute temp path as the global creds target (avoids ~ expansion in tests).
	fakeCreds := filepath.Join(t.TempDir(), "auth.json")
	d.GlobalCredsPath = fakeCreds

	res, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Toast")
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	credLink := filepath.Join(res.Dir, d.CredentialFile)
	target, err := os.Readlink(credLink)
	if err != nil {
		t.Fatalf("expected credential symlink at %q: %v", credLink, err)
	}
	if target != fakeCreds {
		t.Errorf("symlink target = %q, want %q", target, fakeCreds)
	}
}

func TestEnsureConfigDirIdempotent(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()
	fakeCreds := filepath.Join(t.TempDir(), "auth.json")
	d.GlobalCredsPath = fakeCreds

	// Call twice — should not fail.
	if _, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Toast"); err != nil {
		t.Fatalf("first EnsureConfigDir failed: %v", err)
	}
	res, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Toast")
	if err != nil {
		t.Fatalf("second EnsureConfigDir failed: %v", err)
	}

	// Symlink must still be valid.
	credLink := filepath.Join(res.Dir, d.CredentialFile)
	if _, err := os.Readlink(credLink); err != nil {
		t.Fatalf("credential symlink missing after second call: %v", err)
	}
}

func TestEnsureConfigDirNoCredentialFileSkipped(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()
	d.CredentialFile = ""      // no credential file
	d.GlobalCredsPath = ""     // no global creds

	res, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Ghost")
	if err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	// Directory should still be created.
	if _, err := os.Stat(res.Dir); err != nil {
		t.Errorf("expected config dir to exist: %v", err)
	}
}

// ---- CleanupConfigDir ----

func TestCleanupConfigDirRemovesCreatedDir(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()
	fakeCreds := filepath.Join(t.TempDir(), "auth.json")
	d.GlobalCredsPath = fakeCreds

	res, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Toast")
	if err != nil {
		t.Fatalf("EnsureConfigDir: %v", err)
	}
	if _, err := os.Stat(res.Dir); err != nil {
		t.Fatalf("config dir should exist: %v", err)
	}

	if err := runtime.CleanupConfigDir(d, worldDir, "outpost", "Toast"); err != nil {
		t.Fatalf("CleanupConfigDir failed: %v", err)
	}
	if _, err := os.Stat(res.Dir); !os.IsNotExist(err) {
		t.Errorf("expected config dir to be removed, stat err = %v", err)
	}
}

func TestCleanupConfigDirIdempotent(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()

	// Call without ever creating the dir — should be a no-op.
	if err := runtime.CleanupConfigDir(d, worldDir, "outpost", "Ghost"); err != nil {
		t.Errorf("CleanupConfigDir on non-existent path returned error: %v", err)
	}
	// Second call — still no-op.
	if err := runtime.CleanupConfigDir(d, worldDir, "outpost", "Ghost"); err != nil {
		t.Errorf("second CleanupConfigDir returned error: %v", err)
	}
}

func TestCleanupConfigDirOnlyTouchesNamedAgent(t *testing.T) {
	worldDir := t.TempDir()
	d := newStubDescriptor()
	fakeCreds := filepath.Join(t.TempDir(), "auth.json")
	d.GlobalCredsPath = fakeCreds

	// Create config dirs for two different agents.
	_, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Alpha")
	if err != nil {
		t.Fatalf("EnsureConfigDir Alpha: %v", err)
	}
	resBeta, err := runtime.EnsureConfigDir(d, worldDir, "outpost", "Beta")
	if err != nil {
		t.Fatalf("EnsureConfigDir Beta: %v", err)
	}

	if err := runtime.CleanupConfigDir(d, worldDir, "outpost", "Alpha"); err != nil {
		t.Fatalf("CleanupConfigDir Alpha: %v", err)
	}

	// Beta's dir should be untouched.
	if _, err := os.Stat(resBeta.Dir); err != nil {
		t.Errorf("Beta config dir was affected by Alpha cleanup: %v", err)
	}
}

// ---- BuildTelemetryEnv ----

func TestBuildTelemetryEnvReturnsEmptyForZeroPort(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, 0, "Toast", "myworld", "sol-abc", "")
	if len(env) != 0 {
		t.Errorf("expected empty map for port=0, got %v", env)
	}
}

func TestBuildTelemetryEnvReturnsEmptyForNegativePort(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, -1, "Toast", "myworld", "sol-abc", "")
	if len(env) != 0 {
		t.Errorf("expected empty map for port=-1, got %v", env)
	}
}

func TestBuildTelemetryEnvStandardVars(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, 4318, "Toast", "myworld", "sol-abc123", "")

	for _, key := range []string{
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_PROTOCOL",
		"OTEL_RESOURCE_ATTRIBUTES",
	} {
		if _, ok := env[key]; !ok {
			t.Errorf("expected env var %q to be set", key)
		}
	}
}

func TestBuildTelemetryEnvResourceAttributes(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, 4318, "Toast", "myworld", "sol-abc123", "personal")

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if !strings.Contains(attrs, "agent.name=Toast") {
		t.Errorf("expected agent.name=Toast in %q", attrs)
	}
	if !strings.Contains(attrs, "world=myworld") {
		t.Errorf("expected world=myworld in %q", attrs)
	}
	if !strings.Contains(attrs, "writ_id=sol-abc123") {
		t.Errorf("expected writ_id=sol-abc123 in %q", attrs)
	}
	if !strings.Contains(attrs, "account=personal") {
		t.Errorf("expected account=personal in %q", attrs)
	}
	if !strings.Contains(attrs, "service.name=stub") {
		t.Errorf("expected service.name=stub in %q", attrs)
	}
}

func TestBuildTelemetryEnvOmitsOptionalFields(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, 4318, "Toast", "myworld", "", "")

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	if strings.Contains(attrs, "writ_id") {
		t.Errorf("writ_id should be absent when empty: %q", attrs)
	}
	if strings.Contains(attrs, "account") {
		t.Errorf("account should be absent when empty: %q", attrs)
	}
}

func TestBuildTelemetryEnvEndpointPort(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, 9999, "Toast", "myworld", "", "")

	want := "http://localhost:9999/v1/logs"
	if env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != want {
		t.Errorf("endpoint = %q, want %q", env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"], want)
	}
}

func TestBuildTelemetryEnvMergesStaticEnv(t *testing.T) {
	d := newStubDescriptor() // StaticEnv = {"STUB_TELEMETRY": "1"}
	env := runtime.BuildTelemetryEnv(d, 4318, "Toast", "myworld", "", "")

	if env["STUB_TELEMETRY"] != "1" {
		t.Errorf("expected STUB_TELEMETRY=1 from StaticEnv, got %q", env["STUB_TELEMETRY"])
	}
}

func TestBuildTelemetryEnvStaticEnvNotSetWhenDisabled(t *testing.T) {
	d := newStubDescriptor()
	env := runtime.BuildTelemetryEnv(d, 0, "Toast", "myworld", "", "")

	if _, ok := env["STUB_TELEMETRY"]; ok {
		t.Error("StaticEnv should not be set when telemetry is disabled (port=0)")
	}
}

// ---- CredentialEnv ----

func TestCredentialEnvAPIKey(t *testing.T) {
	d := newStubDescriptor() // CredentialEnvKeys = {"api_key": "STUB_API_KEY", ...}

	env, err := runtime.CredentialEnv(d, runtime.Credential{Type: "api_key", Token: "sk-test"})
	if err != nil {
		t.Fatalf("CredentialEnv api_key: %v", err)
	}
	if v, ok := env["STUB_API_KEY"]; !ok || v != "sk-test" {
		t.Errorf("expected STUB_API_KEY=sk-test, got %v", env)
	}
}

func TestCredentialEnvOAuthToken(t *testing.T) {
	d := newStubDescriptor()

	env, err := runtime.CredentialEnv(d, runtime.Credential{Type: "oauth_token", Token: "tok-abc"})
	if err != nil {
		t.Fatalf("CredentialEnv oauth_token: %v", err)
	}
	if v, ok := env["STUB_OAUTH_TOKEN"]; !ok || v != "tok-abc" {
		t.Errorf("expected STUB_OAUTH_TOKEN=tok-abc, got %v", env)
	}
}

func TestCredentialEnvUnknownType(t *testing.T) {
	d := newStubDescriptor()

	env, err := runtime.CredentialEnv(d, runtime.Credential{Type: "unknown", Token: "val"})
	if err == nil {
		t.Error("expected error for unknown credential type, got nil")
	}
	if len(env) != 0 {
		t.Errorf("expected empty map for unknown type, got %v", env)
	}
}

func TestCredentialEnvNoMapping(t *testing.T) {
	d := newStubDescriptor()
	d.CredentialEnvKeys = nil // no mapping configured

	env, err := runtime.CredentialEnv(d, runtime.Credential{Type: "api_key", Token: "val"})
	if err == nil {
		t.Error("expected error when no credential mapping configured")
	}
	if len(env) != 0 {
		t.Errorf("expected empty map when no mapping, got %v", env)
	}
}

func TestCredentialEnvReturnsOnlyRequestedKey(t *testing.T) {
	d := newStubDescriptor()

	env, err := runtime.CredentialEnv(d, runtime.Credential{Type: "api_key", Token: "sk-xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should only set the one key for the requested credential type.
	if len(env) != 1 {
		t.Errorf("expected exactly 1 env var, got %d: %v", len(env), env)
	}
}

// ---- MemoryDir ----

func TestMemoryDirReturnsAbsolutePath(t *testing.T) {
	worldDir := "/tmp/sol/myworld"
	got := runtime.MemoryDir(worldDir, "envoy", "Polaris")

	if !filepath.IsAbs(got) {
		t.Errorf("MemoryDir returned relative path %q", got)
	}
	want := filepath.Join(worldDir, "envoys", "Polaris", "memory")
	if got != want {
		t.Errorf("MemoryDir = %q, want %q", got, want)
	}
}

func TestMemoryDirOutpostRole(t *testing.T) {
	worldDir := "/tmp/sol/myworld"
	got := runtime.MemoryDir(worldDir, "outpost", "Toast")

	want := filepath.Join(worldDir, "outposts", "Toast", "memory")
	if got != want {
		t.Errorf("MemoryDir(outpost) = %q, want %q", got, want)
	}
}

func TestMemoryDirEmptyForMissingArgs(t *testing.T) {
	cases := []struct {
		worldDir, role, agent string
		label                 string
	}{
		{"", "envoy", "Polaris", "empty worldDir"},
		{"/tmp/world", "", "Polaris", "empty role"},
		{"/tmp/world", "envoy", "", "empty agent"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got := runtime.MemoryDir(tc.worldDir, tc.role, tc.agent)
			if got != "" {
				t.Errorf("MemoryDir(%q, %q, %q) = %q, want empty", tc.worldDir, tc.role, tc.agent, got)
			}
		})
	}
}

func TestMemoryDirRelativeWorldDirPromotedToAbsolute(t *testing.T) {
	got := runtime.MemoryDir("relative/world", "envoy", "Polaris")
	if got == "" {
		t.Fatal("MemoryDir returned empty for relative worldDir; expected absolute")
	}
	if !filepath.IsAbs(got) {
		t.Errorf("MemoryDir did not promote relative worldDir to absolute: %q", got)
	}
}

func TestMemoryDirRolesPluralSuffix(t *testing.T) {
	// MemoryDir uses the roleDir mapping: envoy→envoys, outpost→outposts, else passthrough.
	cases := []struct {
		role    string
		wantDir string
	}{
		{"envoy", "envoys"},
		{"outpost", "outposts"},
		{"forge", "forge"}, // no pluralization for forge
	}
	for _, tc := range cases {
		got := runtime.MemoryDir("/tmp/world", tc.role, "Agent")
		if !strings.Contains(got, string(filepath.Separator)+tc.wantDir+string(filepath.Separator)) {
			t.Errorf("MemoryDir(role=%q) = %q, want directory %q in path", tc.role, got, tc.wantDir)
		}
	}
}

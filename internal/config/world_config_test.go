package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultWorldConfig(t *testing.T) {
	cfg := DefaultWorldConfig()
	if cfg.Agents.MaxActive != 0 {
		t.Fatalf("expected max_active 0, got %d", cfg.Agents.MaxActive)
	}
	if cfg.Agents.Model != "" {
		t.Fatalf("expected model '' (empty, adapter provides default), got %q", cfg.Agents.Model)
	}
	if cfg.World.Branch != "main" {
		t.Fatalf("expected world.branch 'main', got %q", cfg.World.Branch)
	}
	if cfg.Forge.QualityGates != nil {
		t.Fatalf("expected nil quality_gates, got %v", cfg.Forge.QualityGates)
	}
	if cfg.World.SourceRepo != "" {
		t.Fatalf("expected empty source_repo, got %q", cfg.World.SourceRepo)
	}
}

func TestLoadWorldConfigNoFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	defaults := DefaultWorldConfig()
	if cfg.Agents.Model != defaults.Agents.Model {
		t.Fatalf("expected model %q, got %q", defaults.Agents.Model, cfg.Agents.Model)
	}
	if cfg.World.Branch != defaults.World.Branch {
		t.Fatalf("expected world.branch %q, got %q", defaults.World.Branch, cfg.World.Branch)
	}
}

func TestLoadWorldConfigGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write sol.toml with model="opus".
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("[agents]\nmodel = \"opus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Model != "opus" {
		t.Fatalf("expected model 'opus', got %q", cfg.Agents.Model)
	}
	if cfg.World.Branch != "main" {
		t.Fatalf("expected world.branch 'main' (default), got %q", cfg.World.Branch)
	}
}

func TestLoadWorldConfigWorldOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write sol.toml with model="opus".
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("[agents]\nmodel = \"opus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write world.toml with model="haiku".
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[agents]\nmodel = \"haiku\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Model != "haiku" {
		t.Fatalf("expected model 'haiku' (world wins), got %q", cfg.Agents.Model)
	}
}

func TestLoadWorldConfigPartialOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write world.toml with only [world] source_repo.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[world]\nsource_repo = \"/path\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.World.SourceRepo != "/path" {
		t.Fatalf("expected source_repo '/path', got %q", cfg.World.SourceRepo)
	}
	// Other fields should remain defaults.
	if cfg.Agents.Model != "" {
		t.Fatalf("expected model '' (default), got %q", cfg.Agents.Model)
	}
	if cfg.World.Branch != "main" {
		t.Fatalf("expected world.branch 'main' (default), got %q", cfg.World.Branch)
	}
	if cfg.Agents.MaxActive != 0 {
		t.Fatalf("expected max_active 0 (default), got %d", cfg.Agents.MaxActive)
	}
}

func TestLoadWorldConfigSameSectionPartialOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// sol.toml sets both max_active and model in [agents].
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("[agents]\nmax_active = 10\nmodel = \"opus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// world.toml overrides only model in [agents] — max_active is not mentioned.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[agents]\nmodel = \"haiku\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Model != "haiku" {
		t.Fatalf("expected model 'haiku' (world override), got %q", cfg.Agents.Model)
	}
	// max_active must be preserved from sol.toml, not zeroed.
	if cfg.Agents.MaxActive != 10 {
		t.Fatalf("expected max_active 10 (preserved from sol.toml), got %d", cfg.Agents.MaxActive)
	}
}

func TestLoadWorldConfigQualityGates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write world.toml with quality_gates.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	content := "[forge]\nquality_gates = [\"make test\", \"make vet\"]\n"
	if err := os.WriteFile(worldPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Forge.QualityGates) != 2 {
		t.Fatalf("expected 2 quality_gates, got %d", len(cfg.Forge.QualityGates))
	}
	if cfg.Forge.QualityGates[0] != "make test" {
		t.Fatalf("expected quality_gates[0] 'make test', got %q", cfg.Forge.QualityGates[0])
	}
	if cfg.Forge.QualityGates[1] != "make vet" {
		t.Fatalf("expected quality_gates[1] 'make vet', got %q", cfg.Forge.QualityGates[1])
	}
}

func TestWriteWorldConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	original := WorldConfig{
		World: WorldSection{
			SourceRepo: "/home/user/myproject",
			Branch:     "develop",
		},
		Agents: AgentsSection{
			MaxActive:    10,
			NamePoolPath: "/custom/names.txt",
			Model:    "opus",
		},
		Forge: ForgeSection{
			QualityGates: []string{"make test", "make vet"},
		},
	}

	if err := WriteWorldConfig("roundtrip", original); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadWorldConfig("roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.World.SourceRepo != original.World.SourceRepo {
		t.Fatalf("source_repo: expected %q, got %q", original.World.SourceRepo, loaded.World.SourceRepo)
	}
	if loaded.Agents.MaxActive != original.Agents.MaxActive {
		t.Fatalf("max_active: expected %d, got %d", original.Agents.MaxActive, loaded.Agents.MaxActive)
	}
	if loaded.Agents.NamePoolPath != original.Agents.NamePoolPath {
		t.Fatalf("name_pool_path: expected %q, got %q", original.Agents.NamePoolPath, loaded.Agents.NamePoolPath)
	}
	if loaded.Agents.Model != original.Agents.Model {
		t.Fatalf("model: expected %q, got %q", original.Agents.Model, loaded.Agents.Model)
	}
	if loaded.World.Branch != original.World.Branch {
		t.Fatalf("world.branch: expected %q, got %q", original.World.Branch, loaded.World.Branch)
	}
	if len(loaded.Forge.QualityGates) != len(original.Forge.QualityGates) {
		t.Fatalf("quality_gates length: expected %d, got %d", len(original.Forge.QualityGates), len(loaded.Forge.QualityGates))
	}
	for i, gate := range original.Forge.QualityGates {
		if loaded.Forge.QualityGates[i] != gate {
			t.Fatalf("quality_gates[%d]: expected %q, got %q", i, gate, loaded.Forge.QualityGates[i])
		}
	}
}

func TestLoadWorldConfigInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write invalid TOML to world.toml.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("this is [not valid toml = {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWorldConfig("testworld")
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestWorldConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	expected := filepath.Join(dir, "myworld", "world.toml")
	got := WorldConfigPath("myworld")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	expected := filepath.Join(dir, "sol.toml")
	got := GlobalConfigPath()
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestRequireWorldExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create $SOL_HOME/{world}/world.toml.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RequireWorld("testworld"); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestRequireWorldNotExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	err := RequireWorld("nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected error containing 'does not exist', got: %v", err)
	}
}

func TestRequireWorldPreArc1(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Create $SOL_HOME/.store/{world}.db but NO world.toml.
	storeDir := filepath.Join(dir, ".store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "legacy.db"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := RequireWorld("legacy")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "before world lifecycle") {
		t.Fatalf("expected error containing 'before world lifecycle', got: %v", err)
	}
}

func TestResolveModelFallbackToDefault(t *testing.T) {
	// No model set → returns "" (caller applies adapter.DefaultModel()).
	cfg := WorldConfig{}
	for _, role := range []string{"outpost", "agent", "envoy", "forge", "forge-merge", "unknown"} {
		got := cfg.ResolveModel(role, "claude")
		if got != "" {
			t.Errorf("ResolveModel(%q, claude) with no config = %q, want %q", role, got, "")
		}
	}
}

func TestResolveModelFallbackToModel(t *testing.T) {
	// model set, no per-role overrides → uses model.
	cfg := WorldConfig{
		Agents: AgentsSection{Model: "opus"},
	}
	for _, role := range []string{"outpost", "agent", "envoy", "forge", "forge-merge", "unknown"} {
		got := cfg.ResolveModel(role, "claude")
		if got != "opus" {
			t.Errorf("ResolveModel(%q, claude) with model=opus = %q, want %q", role, got, "opus")
		}
	}
}

func TestResolveModelPerRoleOverride(t *testing.T) {
	cfg := WorldConfig{
		Agents: AgentsSection{
			Model: "opus",
			Models: map[string]RoleModels{
				"claude": {
					Outpost: "haiku",
					Envoy:   "sonnet",
					Forge:   "haiku",
				},
			},
		},
	}

	cases := []struct {
		role string
		want string
	}{
		{"outpost", "haiku"},
		{"agent", "haiku"},  // "agent" maps to Outpost
		{"envoy", "sonnet"},
		{"forge", "haiku"},
		{"forge-merge", "haiku"}, // "forge-merge" maps to Forge
		{"unknown", "opus"},      // unknown role falls back to model
	}

	for _, tc := range cases {
		got := cfg.ResolveModel(tc.role, "claude")
		if got != tc.want {
			t.Errorf("ResolveModel(%q, claude) = %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestResolveModelPartialOverride(t *testing.T) {
	// Only outpost has override; other roles use model.
	cfg := WorldConfig{
		Agents: AgentsSection{
			Model: "opus",
			Models: map[string]RoleModels{
				"claude": {
					Outpost: "sonnet",
				},
			},
		},
	}

	if got := cfg.ResolveModel("outpost", "claude"); got != "sonnet" {
		t.Errorf("ResolveModel(outpost, claude) = %q, want sonnet", got)
	}
	if got := cfg.ResolveModel("envoy", "claude"); got != "opus" {
		t.Errorf("ResolveModel(envoy, claude) = %q, want opus (model)", got)
	}
	if got := cfg.ResolveModel("forge", "claude"); got != "opus" {
		t.Errorf("ResolveModel(forge, claude) = %q, want opus (model)", got)
	}
}

func TestResolveModelCrossRuntimeIsolation(t *testing.T) {
	// Overrides for one runtime don't affect another runtime.
	cfg := WorldConfig{
		Agents: AgentsSection{
			Model: "default-model",
			Models: map[string]RoleModels{
				"claude": {
					Outpost: "sonnet",
					Envoy:   "opus",
				},
				"codex": {
					Outpost: "o3",
				},
			},
		},
	}

	// Claude runtime sees its own overrides.
	if got := cfg.ResolveModel("outpost", "claude"); got != "sonnet" {
		t.Errorf("ResolveModel(outpost, claude) = %q, want sonnet", got)
	}
	if got := cfg.ResolveModel("envoy", "claude"); got != "opus" {
		t.Errorf("ResolveModel(envoy, claude) = %q, want opus", got)
	}

	// Codex runtime sees its own overrides.
	if got := cfg.ResolveModel("outpost", "codex"); got != "o3" {
		t.Errorf("ResolveModel(outpost, codex) = %q, want o3", got)
	}
	// Codex envoy has no override → falls back to agents.model.
	if got := cfg.ResolveModel("envoy", "codex"); got != "default-model" {
		t.Errorf("ResolveModel(envoy, codex) = %q, want default-model", got)
	}

	// Unknown runtime falls back to agents.model.
	if got := cfg.ResolveModel("outpost", "unknown-runtime"); got != "default-model" {
		t.Errorf("ResolveModel(outpost, unknown-runtime) = %q, want default-model", got)
	}
}

func TestResolveRuntimeFallbackToClaude(t *testing.T) {
	cfg := WorldConfig{}
	for _, role := range []string{"outpost", "envoy", "forge", "unknown"} {
		got := cfg.ResolveRuntime(role)
		if got != "claude" {
			t.Errorf("ResolveRuntime(%q) with no config = %q, want %q", role, got, "claude")
		}
	}
}

func TestResolveRuntimeDefaultRuntime(t *testing.T) {
	cfg := WorldConfig{
		Agents: AgentsSection{
			DefaultRuntime: "claude",
		},
	}
	for _, role := range []string{"outpost", "envoy"} {
		got := cfg.ResolveRuntime(role)
		if got != "claude" {
			t.Errorf("ResolveRuntime(%q) with default_runtime=claude = %q, want %q", role, got, "claude")
		}
	}
}

func TestResolveRuntimePerRoleOverride(t *testing.T) {
	cfg := WorldConfig{
		Agents: AgentsSection{
			DefaultRuntime: "claude",
			Runtimes: RuntimesSection{
				Outpost: "custom",
			},
		},
	}
	if got := cfg.ResolveRuntime("outpost"); got != "custom" {
		t.Errorf("ResolveRuntime(outpost) = %q, want custom", got)
	}
	if got := cfg.ResolveRuntime("envoy"); got != "claude" {
		t.Errorf("ResolveRuntime(envoy) = %q, want claude (default)", got)
	}
}

func TestWorldConfigValidateModelAnyStringAccepted(t *testing.T) {
	// Any non-empty string is valid — no allowlist.
	for _, model := range []string{"sonnet", "opus", "gpt-4", "o3", "custom-model", ""} {
		cfg := DefaultWorldConfig()
		cfg.Agents.Model = model
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected model %q to be valid, got: %v", model, err)
		}
	}
}

func TestWorldConfigValidateMaxActive(t *testing.T) {
	valid := []int{0, 1, 100}
	for _, cap := range valid {
		cfg := DefaultWorldConfig()
		cfg.Agents.MaxActive = cap
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected max_active %d to be valid, got: %v", cap, err)
		}
	}

	invalid := []int{-1, -100}
	for _, cap := range invalid {
		cfg := DefaultWorldConfig()
		cfg.Agents.MaxActive = cap
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected max_active %d to be invalid, got nil error", cap)
		}
	}
}

func TestWorldConfigValidateGateTimeout(t *testing.T) {
	// Valid durations should pass.
	for _, d := range []string{"5m", "30s", "1h", ""} {
		cfg := DefaultWorldConfig()
		cfg.Forge.GateTimeout = d
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected gate_timeout %q to be valid, got: %v", d, err)
		}
	}
	// Invalid durations should fail.
	for _, d := range []string{"banana", "5x", "not-a-duration"} {
		cfg := DefaultWorldConfig()
		cfg.Forge.GateTimeout = d
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected gate_timeout %q to be invalid, got nil error", d)
		}
	}
}

func TestWorldConfigValidateBranchRequiredWithSourceRepo(t *testing.T) {
	// World.Branch must be non-empty when World.SourceRepo is set.
	cfg := DefaultWorldConfig()
	cfg.World.SourceRepo = "/path/to/repo"
	cfg.World.Branch = "" // explicitly clear the default

	if err := cfg.Validate(); err == nil {
		t.Error("expected error when world.branch is empty and world.source_repo is set")
	} else if !strings.Contains(err.Error(), "world.branch") {
		t.Errorf("expected error to mention world.branch, got: %v", err)
	}

	// With Branch set, validation should pass.
	cfg.World.Branch = "main"
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error with branch set, got: %v", err)
	}

	// Without SourceRepo, Branch can be empty.
	cfg.World.SourceRepo = ""
	cfg.World.Branch = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error when neither source_repo nor branch is set, got: %v", err)
	}
}

func TestWorldSectionProtectedBranches(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write world.toml with protected_branches.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	content := "[world]\nbranch = \"main\"\nprotected_branches = [\"release/*\", \"staging\"]\n"
	if err := os.WriteFile(worldPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.World.Branch != "main" {
		t.Fatalf("expected world.branch 'main', got %q", cfg.World.Branch)
	}
	if len(cfg.World.ProtectedBranches) != 2 {
		t.Fatalf("expected 2 protected_branches, got %d", len(cfg.World.ProtectedBranches))
	}
	if cfg.World.ProtectedBranches[0] != "release/*" {
		t.Fatalf("expected protected_branches[0] 'release/*', got %q", cfg.World.ProtectedBranches[0])
	}
	if cfg.World.ProtectedBranches[1] != "staging" {
		t.Fatalf("expected protected_branches[1] 'staging', got %q", cfg.World.ProtectedBranches[1])
	}
}

func TestValidateWorldNameReserved(t *testing.T) {
	reserved := []string{"store", "runtime", "sol", "workflows"}
	for _, name := range reserved {
		if err := ValidateWorldName(name); err == nil {
			t.Errorf("expected reserved name %q to be rejected, got nil error", name)
		} else if !strings.Contains(err.Error(), "reserved") {
			t.Errorf("expected 'reserved' in error for %q, got: %v", name, err)
		}
	}

	// Names that contain reserved words but aren't exact matches should pass.
	allowed := []string{"mystore", "store1", "runtime2", "sol-project"}
	for _, name := range allowed {
		if err := ValidateWorldName(name); err != nil {
			t.Errorf("expected name %q to be allowed, got: %v", name, err)
		}
	}
}

func TestValidateWorldNameTooLong(t *testing.T) {
	long := strings.Repeat("a", 65)
	err := ValidateWorldName(long)
	if err == nil {
		t.Fatal("expected error for name exceeding max length")
	}
	if !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected 'too long' in error, got: %v", err)
	}

	// Exactly 64 should be OK.
	exact := strings.Repeat("a", 64)
	if err := ValidateWorldName(exact); err != nil {
		t.Fatalf("expected 64-char name to be valid, got: %v", err)
	}
}

func TestWriteWorldConfigReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Make the SOL_HOME dir read-only so MkdirAll fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	err := WriteWorldConfig("readonly", DefaultWorldConfig())
	if err == nil {
		t.Fatal("expected error writing to read-only dir")
	}
}

func TestWriteWorldConfigCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// The world dir does not exist yet — WriteWorldConfig should create it.
	err := WriteWorldConfig("newworld", DefaultWorldConfig())
	if err != nil {
		t.Fatalf("WriteWorldConfig() error: %v", err)
	}

	// Verify file was written.
	path := filepath.Join(dir, "newworld", "world.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected world.toml to be created with parent dirs")
	}
}

func TestLoadWorldConfigInvalidGlobalTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write invalid TOML to sol.toml.
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("this is [not valid toml = {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWorldConfig("testworld")
	if err == nil {
		t.Fatal("expected error for invalid global TOML")
	}
	if !strings.Contains(err.Error(), "sol.toml") {
		t.Fatalf("expected error to mention sol.toml path, got: %v", err)
	}
}

func TestLoadWorldConfigModelPassthrough(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write world.toml with a non-Anthropic model — should be accepted (passthrough).
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[agents]\nmodel = \"gpt-4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatalf("expected no error for passthrough model, got: %v", err)
	}
	if cfg.Agents.Model != "gpt-4" {
		t.Fatalf("expected model 'gpt-4', got %q", cfg.Agents.Model)
	}
}

// ----- LoadGlobalConfig -----

func TestLoadGlobalConfigMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// No sol.toml — should return defaults without error.
	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error: %v", err)
	}
	defaults := DefaultWorldConfig()
	if cfg.Agents.Model != defaults.Agents.Model {
		t.Fatalf("model = %q, want %q", cfg.Agents.Model, defaults.Agents.Model)
	}
	if cfg.World.Branch != defaults.World.Branch {
		t.Fatalf("branch = %q, want %q", cfg.World.Branch, defaults.World.Branch)
	}
}

func TestLoadGlobalConfigValidFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[agents]\nmodel = \"haiku\"\n[world]\nbranch = \"develop\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error: %v", err)
	}
	if cfg.Agents.Model != "haiku" {
		t.Fatalf("model = %q, want %q", cfg.Agents.Model, "haiku")
	}
	if cfg.World.Branch != "develop" {
		t.Fatalf("branch = %q, want %q", cfg.World.Branch, "develop")
	}
}

func TestLoadGlobalConfigInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte("this is [not valid = {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadGlobalConfig()
	if err == nil {
		t.Fatal("LoadGlobalConfig() expected error for invalid TOML, got nil")
	}
	if !strings.Contains(err.Error(), "sol.toml") {
		t.Fatalf("expected error to mention sol.toml, got: %v", err)
	}
}

// ----- IsSleeping -----

func TestIsSleepingFalseByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// No world.toml — defaults to not sleeping.
	sleeping, err := IsSleeping("noworld")
	if err != nil {
		t.Fatalf("IsSleeping() returned unexpected error: %v", err)
	}
	if sleeping {
		t.Fatal("IsSleeping() = true for world with no config, want false")
	}
}

func TestIsSleepingTrue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, "sleepyworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sleeping, err := IsSleeping("sleepyworld")
	if err != nil {
		t.Fatalf("IsSleeping() returned unexpected error: %v", err)
	}
	if !sleeping {
		t.Fatal("IsSleeping() = false for world with sleeping = true, want true")
	}
}

func TestIsSleepingFalseExplicit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, "activeworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[world]\nsleeping = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sleeping, err := IsSleeping("activeworld")
	if err != nil {
		t.Fatalf("IsSleeping() returned unexpected error: %v", err)
	}
	if sleeping {
		t.Fatal("IsSleeping() = true for world with sleeping = false, want false")
	}
}

func TestIsSleepingReturnsErrorOnBadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write an invalid world.toml so LoadWorldConfig returns an error.
	worldDir := filepath.Join(dir, "badworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("this is [not valid toml = {\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sleeping, err := IsSleeping("badworld")
	if err == nil {
		t.Fatal("IsSleeping() returned nil error for invalid config, want error")
	}
	if sleeping {
		t.Fatal("IsSleeping() = true on error, want false")
	}
}

// ----- EscalationSection.AgingThreshold -----

func TestAgingThresholdEmptyUsesDefaults(t *testing.T) {
	// Empty strings for all fields → fall back to default thresholds.
	e := EscalationSection{}
	defaults := DefaultEscalationConfig()

	cases := []struct {
		sev  string
		want time.Duration
	}{
		{"critical", 30 * time.Minute},
		{"high", 2 * time.Hour},
		{"medium", 8 * time.Hour},
	}
	for _, tc := range cases {
		d, err := e.AgingThreshold(tc.sev)
		if err != nil {
			t.Errorf("AgingThreshold(%q) with empty config error: %v", tc.sev, err)
		}
		if d != tc.want {
			t.Errorf("AgingThreshold(%q) = %v, want %v (default)", tc.sev, d, tc.want)
		}
	}

	// Verify defaults match DefaultEscalationConfig.
	_ = defaults
}

func TestAgingThresholdExplicitValues(t *testing.T) {
	e := EscalationSection{
		AgingCritical: "30m",
		AgingHigh:     "2h",
		AgingMedium:   "8h",
	}

	cases := []struct {
		sev  string
		want time.Duration
	}{
		{"critical", 30 * time.Minute},
		{"high", 2 * time.Hour},
		{"medium", 8 * time.Hour},
	}

	for _, tc := range cases {
		d, err := e.AgingThreshold(tc.sev)
		if err != nil {
			t.Errorf("AgingThreshold(%q) error: %v", tc.sev, err)
			continue
		}
		if d != tc.want {
			t.Errorf("AgingThreshold(%q) = %v, want %v", tc.sev, d, tc.want)
		}
	}
}

func TestAgingThresholdLowSeverity(t *testing.T) {
	// "low" severity is never re-notified → always 0.
	e := DefaultEscalationConfig()
	d, err := e.AgingThreshold("low")
	if err != nil {
		t.Fatalf("AgingThreshold(\"low\") error: %v", err)
	}
	if d != 0 {
		t.Fatalf("AgingThreshold(\"low\") = %v, want 0", d)
	}
}

func TestAgingThresholdLowSeverityZero(t *testing.T) {
	// "low" severity with empty config returns (0, nil) — never re-notified.
	e := EscalationSection{}
	d, err := e.AgingThreshold("low")
	if err != nil {
		t.Fatalf("AgingThreshold(\"low\") with empty config error: %v", err)
	}
	if d != 0 {
		t.Fatalf("AgingThreshold(\"low\") with empty config = %v, want 0", d)
	}
}

func TestAgingThresholdUnknownSeverity(t *testing.T) {
	e := DefaultEscalationConfig()
	_, err := e.AgingThreshold("unknown")
	if err == nil {
		t.Fatal("AgingThreshold(\"unknown\") expected error, got nil")
	}
}

func TestAgingThresholdInvalidDuration(t *testing.T) {
	e := EscalationSection{AgingCritical: "not-a-duration"}
	_, err := e.AgingThreshold("critical")
	if err == nil {
		t.Fatal("AgingThreshold with invalid duration string expected error, got nil")
	}
}

// ----- Validate: escalation aging durations -----

func TestWorldConfigValidateEscalationAgingDurations(t *testing.T) {
	// Valid duration strings should pass.
	for _, d := range []string{"30m", "2h", "8h", "1s", ""} {
		cfg := DefaultWorldConfig()
		cfg.Escalation.AgingCritical = d
		cfg.Escalation.AgingHigh = d
		cfg.Escalation.AgingMedium = d
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected aging duration %q to be valid, got: %v", d, err)
		}
	}

	// Invalid duration strings should fail with the right field name.
	cases := []struct {
		field string
		set   func(*WorldConfig, string)
	}{
		{
			field: "escalation.aging_critical",
			set:   func(c *WorldConfig, v string) { c.Escalation.AgingCritical = v },
		},
		{
			field: "escalation.aging_high",
			set:   func(c *WorldConfig, v string) { c.Escalation.AgingHigh = v },
		},
		{
			field: "escalation.aging_medium",
			set:   func(c *WorldConfig, v string) { c.Escalation.AgingMedium = v },
		},
	}
	for _, tc := range cases {
		for _, bad := range []string{"30mins", "banana", "5x"} {
			cfg := DefaultWorldConfig()
			tc.set(&cfg, bad)
			err := cfg.Validate()
			if err == nil {
				t.Errorf("%s = %q: expected validation error, got nil", tc.field, bad)
			} else if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("%s = %q: expected error to mention %q, got: %v", tc.field, bad, tc.field, err)
			}
		}
	}
}

// ----- LoadGlobalConfig: validation -----

func TestLoadGlobalConfigModelPassthrough(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Any model string is accepted — no allowlist validation.
	content := "[agents]\nmodel = \"turbo\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() expected no error for passthrough model, got: %v", err)
	}
	if cfg.Agents.Model != "turbo" {
		t.Fatalf("model = %q, want %q", cfg.Agents.Model, "turbo")
	}
}

func TestLoadGlobalConfigInvalidAgingDuration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[escalation]\naging_critical = \"30mins\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadGlobalConfig()
	if err == nil {
		t.Fatal("LoadGlobalConfig() expected error for invalid aging duration, got nil")
	}
	if !strings.Contains(err.Error(), "aging_critical") {
		t.Fatalf("expected error to mention aging_critical, got: %v", err)
	}
}

func TestLoadWorldConfigInvalidAgingDuration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[escalation]\naging_critical = \"30mins\"\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWorldConfig("testworld")
	if err == nil {
		t.Fatal("LoadWorldConfig() expected error for invalid aging duration, got nil")
	}
	if !strings.Contains(err.Error(), "aging_critical") {
		t.Fatalf("expected error to mention aging_critical, got: %v", err)
	}
}

// ----- SphereSection -----

func TestSphereConfigFromSolToml(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[sphere]\nmax_sessions = 5\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sphere, err := LoadSphereConfig()
	if err != nil {
		t.Fatalf("LoadSphereConfig() error: %v", err)
	}
	if sphere.MaxSessions != 5 {
		t.Fatalf("max_sessions = %d, want 5", sphere.MaxSessions)
	}
}

func TestSphereConfigDefaultsToZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	sphere, err := LoadSphereConfig()
	if err != nil {
		t.Fatalf("LoadSphereConfig() error: %v", err)
	}
	if sphere.MaxSessions != 0 {
		t.Fatalf("max_sessions = %d, want 0 (default)", sphere.MaxSessions)
	}
}

func TestValidateMaxSessionsNegative(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Sphere.MaxSessions = -1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative max_sessions")
	}
	if !strings.Contains(err.Error(), "sphere.max_sessions") {
		t.Fatalf("expected error to mention sphere.max_sessions, got: %v", err)
	}
}

func TestValidateMaxSessionsValid(t *testing.T) {
	for _, v := range []int{0, 1, 100} {
		cfg := DefaultWorldConfig()
		cfg.Sphere.MaxSessions = v
		if err := cfg.Validate(); err != nil {
			t.Errorf("max_sessions=%d expected valid, got: %v", v, err)
		}
	}
}

// ----- MaxActive -----

func TestMaxActiveFromSolToml(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[agents]\nmax_active = 8\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.MaxActive != 8 {
		t.Fatalf("max_active = %d, want 8", cfg.Agents.MaxActive)
	}
}

func TestMaxActiveWorldOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// sol.toml sets max_active = 10
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte("[agents]\nmax_active = 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// world.toml overrides to 3
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte("[agents]\nmax_active = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.MaxActive != 3 {
		t.Fatalf("max_active = %d, want 3 (world override)", cfg.Agents.MaxActive)
	}
}

func TestValidateMaxActiveNegative(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Agents.MaxActive = -1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative max_active")
	}
	if !strings.Contains(err.Error(), "agents.max_active") {
		t.Fatalf("expected error to mention agents.max_active, got: %v", err)
	}
}

func TestValidateMaxActiveValid(t *testing.T) {
	for _, v := range []int{0, 1, 50} {
		cfg := DefaultWorldConfig()
		cfg.Agents.MaxActive = v
		if err := cfg.Validate(); err != nil {
			t.Errorf("max_active=%d expected valid, got: %v", v, err)
		}
	}
}

// ----- Budget validation -----

func TestValidateBudgetValid(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Budget = BudgetSection{
		Accounts: map[string]AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 20.0},
			"shared":   {DailyLimit: 100.0, AlertAt: 0}, // no alert
			"open":     {DailyLimit: 0},                  // unlimited
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid budget config, got: %v", err)
	}
}

func TestValidateBudgetAlertAtExceedsLimit(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Budget = BudgetSection{
		Accounts: map[string]AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 30.0},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when alert_at >= daily_limit")
	}
}

func TestValidateBudgetAlertAtEqualsLimit(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Budget = BudgetSection{
		Accounts: map[string]AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 25.0},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when alert_at == daily_limit")
	}
}

func TestValidateBudgetNegativeLimit(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Budget = BudgetSection{
		Accounts: map[string]AccountBudget{
			"personal": {DailyLimit: -1.0},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative daily_limit")
	}
}

func TestValidateBudgetNegativeAlertAt(t *testing.T) {
	cfg := DefaultWorldConfig()
	cfg.Budget = BudgetSection{
		Accounts: map[string]AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: -1.0},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative alert_at")
	}
}

func TestValidateBudgetNoBudgetSection(t *testing.T) {
	cfg := DefaultWorldConfig()
	// No budget section at all — should be valid.
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected valid config with no budget section, got: %v", err)
	}
}

func TestBudgetConfigParsing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := `
[world]
branch = "main"

[budget]
[budget.accounts.personal]
daily_limit = 25.0
alert_at = 20.0

[budget.accounts.shared]
daily_limit = 100.0
alert_at = 80.0
`
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Budget.Accounts) != 2 {
		t.Fatalf("expected 2 budget accounts, got %d", len(cfg.Budget.Accounts))
	}

	personal := cfg.Budget.Accounts["personal"]
	if personal.DailyLimit != 25.0 {
		t.Errorf("personal.daily_limit = %f, want 25.0", personal.DailyLimit)
	}
	if personal.AlertAt != 20.0 {
		t.Errorf("personal.alert_at = %f, want 20.0", personal.AlertAt)
	}

	shared := cfg.Budget.Accounts["shared"]
	if shared.DailyLimit != 100.0 {
		t.Errorf("shared.daily_limit = %f, want 100.0", shared.DailyLimit)
	}
	if shared.AlertAt != 80.0 {
		t.Errorf("shared.alert_at = %f, want 80.0", shared.AlertAt)
	}
}

// ----- Capacity removal -----

func TestCapacityFieldIgnored(t *testing.T) {
	// After removing agents.capacity, configs using it should still load
	// (TOML ignores unknown fields) but the value is not parsed.
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[agents]\ncapacity = 5\nmax_active = 10\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	// max_active should be parsed; capacity is ignored (no field in struct).
	if cfg.Agents.MaxActive != 10 {
		t.Fatalf("max_active = %d, want 10", cfg.Agents.MaxActive)
	}
}

func TestDeprecationWarningsEmpty(t *testing.T) {
	cfg := DefaultWorldConfig()
	warnings := cfg.DeprecationWarnings()
	if len(warnings) != 0 {
		t.Fatalf("expected no deprecation warnings, got: %v", warnings)
	}
}

func TestSphereInWorldConfigViaLoadWorldConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// sphere section in sol.toml should be loaded via LoadWorldConfig too.
	content := "[sphere]\nmax_sessions = 12\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sphere.MaxSessions != 12 {
		t.Fatalf("sphere.max_sessions = %d, want 12", cfg.Sphere.MaxSessions)
	}
}

// ----- Raw config helpers -----

func TestLoadWorldConfigRawOnlyReturnsFileKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write a minimal world.toml with only source_repo.
	worldDir := filepath.Join(dir, "rawtest")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "[world]\nsource_repo = \"/tmp/repo\"\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := LoadWorldConfigRaw("rawtest")
	if err != nil {
		t.Fatalf("LoadWorldConfigRaw() error: %v", err)
	}

	// Only the [world] section should be present.
	if _, ok := raw["world"]; !ok {
		t.Fatal("expected 'world' section in raw config")
	}

	// Sections not in the file should be absent.
	for _, key := range []string{"agents", "forge", "ledger", "sphere", "escalation"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("unexpected section %q in raw config", key)
		}
	}

	// The world section should only have source_repo.
	worldSec, ok := raw["world"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'world' section to be a map")
	}
	if worldSec["source_repo"] != "/tmp/repo" {
		t.Fatalf("source_repo = %v, want /tmp/repo", worldSec["source_repo"])
	}
	if _, ok := worldSec["branch"]; ok {
		t.Fatal("branch should not be in raw config (not in file)")
	}
}

func TestLoadWorldConfigRawMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	raw, err := LoadWorldConfigRaw("nonexistent")
	if err != nil {
		t.Fatalf("LoadWorldConfigRaw() error: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("expected empty map for missing file, got %d keys", len(raw))
	}
}

func TestPatchWorldConfigPreservesLayering(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Set up sol.toml with global defaults.
	solContent := "[agents]\nmodel = \"opus\"\nmax_active = 5\n\n[forge]\ngate_timeout = \"10m\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a minimal world.toml — only source_repo and branch.
	worldDir := filepath.Join(dir, "patchtest")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldContent := "[world]\nsource_repo = \"/tmp/repo\"\nbranch = \"main\"\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(worldContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Patch: set sleeping = true.
	if err := PatchWorldConfig("patchtest", func(raw map[string]interface{}) {
		sec := setRawSection(raw, "world")
		sec["sleeping"] = true
	}); err != nil {
		t.Fatalf("PatchWorldConfig() error: %v", err)
	}

	// Read back the raw file — should only have [world] with source_repo, branch, sleeping.
	raw, err := LoadWorldConfigRaw("patchtest")
	if err != nil {
		t.Fatalf("LoadWorldConfigRaw() error: %v", err)
	}

	// The [agents] and [forge] sections should NOT have been written (inherited from sol.toml).
	if _, ok := raw["agents"]; ok {
		t.Fatal("agents section should not be in world.toml after patch")
	}
	if _, ok := raw["forge"]; ok {
		t.Fatal("forge section should not be in world.toml after patch")
	}

	// The sleeping field should be present.
	worldSec, ok := raw["world"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'world' section to be a map")
	}
	if worldSec["sleeping"] != true {
		t.Fatalf("sleeping = %v, want true", worldSec["sleeping"])
	}

	// Verify resolved config still inherits from sol.toml.
	cfg, err := LoadWorldConfig("patchtest")
	if err != nil {
		t.Fatalf("LoadWorldConfig() error: %v", err)
	}
	if cfg.Agents.Model != "opus" {
		t.Fatalf("resolved model = %q, want opus (from sol.toml)", cfg.Agents.Model)
	}
	if cfg.Agents.MaxActive != 5 {
		t.Fatalf("resolved max_active = %d, want 5 (from sol.toml)", cfg.Agents.MaxActive)
	}
	if cfg.Forge.GateTimeout != "10m" {
		t.Fatalf("resolved gate_timeout = %q, want 10m (from sol.toml)", cfg.Forge.GateTimeout)
	}
	if !cfg.World.Sleeping {
		t.Fatal("resolved sleeping should be true after patch")
	}
}

func TestPatchWorldConfigSleepWakeCycle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Set up sol.toml with global model.
	solContent := "[agents]\nmodel = \"opus\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write minimal world.toml.
	worldDir := filepath.Join(dir, "cycletest")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldContent := "[world]\nsource_repo = \"/tmp/repo\"\nbranch = \"main\"\n"
	if err := os.WriteFile(filepath.Join(worldDir, "world.toml"), []byte(worldContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sleep: set sleeping = true.
	if err := PatchWorldConfig("cycletest", func(raw map[string]interface{}) {
		sec := setRawSection(raw, "world")
		sec["sleeping"] = true
	}); err != nil {
		t.Fatalf("PatchWorldConfig (sleep) error: %v", err)
	}

	cfg, err := LoadWorldConfig("cycletest")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.World.Sleeping {
		t.Fatal("expected sleeping=true after sleep")
	}
	if cfg.Agents.Model != "opus" {
		t.Fatalf("model after sleep = %q, want opus", cfg.Agents.Model)
	}

	// Wake: remove sleeping.
	if err := PatchWorldConfig("cycletest", func(raw map[string]interface{}) {
		if worldSec, ok := raw["world"].(map[string]interface{}); ok {
			delete(worldSec, "sleeping")
		}
	}); err != nil {
		t.Fatalf("PatchWorldConfig (wake) error: %v", err)
	}

	cfg, err = LoadWorldConfig("cycletest")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.World.Sleeping {
		t.Fatal("expected sleeping=false after wake")
	}
	if cfg.Agents.Model != "opus" {
		t.Fatalf("model after wake = %q, want opus", cfg.Agents.Model)
	}

	// Verify sol.toml changes propagate after sleep/wake cycle.
	solContent = "[agents]\nmodel = \"sonnet\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err = LoadWorldConfig("cycletest")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Model != "sonnet" {
		t.Fatalf("model after sol.toml change = %q, want sonnet (propagated)", cfg.Agents.Model)
	}
}

func TestCopyWorldConfigRawPreservesLayering(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Set up sol.toml with global defaults.
	solContent := "[agents]\nmodel = \"opus\"\nmax_active = 5\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Source world.toml with specific settings.
	srcDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcContent := "[world]\nsource_repo = \"/tmp/repo\"\nbranch = \"main\"\nsleeping = true\ndefault_account = \"acct1\"\n"
	if err := os.WriteFile(filepath.Join(srcDir, "world.toml"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create target directory.
	tgtDir := filepath.Join(dir, "target")
	if err := os.MkdirAll(tgtDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Clone: copy raw source, clear transient fields.
	if err := CopyWorldConfigRaw("source", "target", func(raw map[string]interface{}) {
		if worldSec, ok := raw["world"].(map[string]interface{}); ok {
			delete(worldSec, "sleeping")
			delete(worldSec, "default_account")
		}
	}); err != nil {
		t.Fatalf("CopyWorldConfigRaw() error: %v", err)
	}

	// Read raw target — should NOT have agents section (inherited from sol.toml).
	raw, err := LoadWorldConfigRaw("target")
	if err != nil {
		t.Fatalf("LoadWorldConfigRaw() error: %v", err)
	}
	if _, ok := raw["agents"]; ok {
		t.Fatal("agents section should not be in cloned world.toml")
	}

	// Resolved config should inherit from sol.toml.
	cfg, err := LoadWorldConfig("target")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Model != "opus" {
		t.Fatalf("resolved model = %q, want opus (from sol.toml)", cfg.Agents.Model)
	}
	if cfg.Agents.MaxActive != 5 {
		t.Fatalf("resolved max_active = %d, want 5 (from sol.toml)", cfg.Agents.MaxActive)
	}
	if cfg.World.Sleeping {
		t.Fatal("sleeping should be false in cloned world")
	}
	if cfg.World.DefaultAccount != "" {
		t.Fatalf("default_account = %q, want empty in cloned world", cfg.World.DefaultAccount)
	}
	if cfg.World.SourceRepo != "/tmp/repo" {
		t.Fatalf("source_repo = %q, want /tmp/repo", cfg.World.SourceRepo)
	}

	// Change sol.toml — should propagate to cloned world.
	solContent = "[agents]\nmodel = \"haiku\"\nmax_active = 3\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(solContent), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadWorldConfig("target")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.Model != "haiku" {
		t.Fatalf("model after sol.toml change = %q, want haiku (propagated)", cfg.Agents.Model)
	}
	if cfg.Agents.MaxActive != 3 {
		t.Fatalf("max_active after sol.toml change = %d, want 3 (propagated)", cfg.Agents.MaxActive)
	}
}

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
	if cfg.Agents.Capacity != 0 {
		t.Fatalf("expected capacity 0, got %d", cfg.Agents.Capacity)
	}
	if cfg.Agents.ModelTier != "sonnet" {
		t.Fatalf("expected model_tier 'sonnet', got %q", cfg.Agents.ModelTier)
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
	if cfg.Agents.ModelTier != defaults.Agents.ModelTier {
		t.Fatalf("expected model_tier %q, got %q", defaults.Agents.ModelTier, cfg.Agents.ModelTier)
	}
	if cfg.World.Branch != defaults.World.Branch {
		t.Fatalf("expected world.branch %q, got %q", defaults.World.Branch, cfg.World.Branch)
	}
}

func TestLoadWorldConfigGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write sol.toml with model_tier="opus".
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("[agents]\nmodel_tier = \"opus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.ModelTier != "opus" {
		t.Fatalf("expected model_tier 'opus', got %q", cfg.Agents.ModelTier)
	}
	if cfg.World.Branch != "main" {
		t.Fatalf("expected world.branch 'main' (default), got %q", cfg.World.Branch)
	}
}

func TestLoadWorldConfigWorldOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write sol.toml with model_tier="opus".
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("[agents]\nmodel_tier = \"opus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write world.toml with model_tier="haiku".
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[agents]\nmodel_tier = \"haiku\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.ModelTier != "haiku" {
		t.Fatalf("expected model_tier 'haiku' (world wins), got %q", cfg.Agents.ModelTier)
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
	if cfg.Agents.ModelTier != "sonnet" {
		t.Fatalf("expected model_tier 'sonnet' (default), got %q", cfg.Agents.ModelTier)
	}
	if cfg.World.Branch != "main" {
		t.Fatalf("expected world.branch 'main' (default), got %q", cfg.World.Branch)
	}
	if cfg.Agents.Capacity != 0 {
		t.Fatalf("expected capacity 0 (default), got %d", cfg.Agents.Capacity)
	}
}

func TestLoadWorldConfigSameSectionPartialOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// sol.toml sets both capacity and model_tier in [agents].
	globalPath := filepath.Join(dir, "sol.toml")
	if err := os.WriteFile(globalPath, []byte("[agents]\ncapacity = 10\nmodel_tier = \"opus\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// world.toml overrides only model_tier in [agents] — capacity is not mentioned.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[agents]\nmodel_tier = \"haiku\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWorldConfig("testworld")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Agents.ModelTier != "haiku" {
		t.Fatalf("expected model_tier 'haiku' (world override), got %q", cfg.Agents.ModelTier)
	}
	// capacity must be preserved from sol.toml, not zeroed.
	if cfg.Agents.Capacity != 10 {
		t.Fatalf("expected capacity 10 (preserved from sol.toml), got %d", cfg.Agents.Capacity)
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
			Capacity:     10,
			NamePoolPath: "/custom/names.txt",
			ModelTier:    "opus",
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
	if loaded.Agents.Capacity != original.Agents.Capacity {
		t.Fatalf("capacity: expected %d, got %d", original.Agents.Capacity, loaded.Agents.Capacity)
	}
	if loaded.Agents.NamePoolPath != original.Agents.NamePoolPath {
		t.Fatalf("name_pool_path: expected %q, got %q", original.Agents.NamePoolPath, loaded.Agents.NamePoolPath)
	}
	if loaded.Agents.ModelTier != original.Agents.ModelTier {
		t.Fatalf("model_tier: expected %q, got %q", original.Agents.ModelTier, loaded.Agents.ModelTier)
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
	// No model_tier set → falls back to "sonnet".
	cfg := WorldConfig{}
	for _, role := range []string{"outpost", "agent", "envoy", "governor", "forge", "forge-merge", "unknown"} {
		got := cfg.ResolveModel(role)
		if got != "sonnet" {
			t.Errorf("ResolveModel(%q) with no config = %q, want %q", role, got, "sonnet")
		}
	}
}

func TestResolveModelFallbackToModelTier(t *testing.T) {
	// model_tier set, no per-role overrides → uses model_tier.
	cfg := WorldConfig{
		Agents: AgentsSection{ModelTier: "opus"},
	}
	for _, role := range []string{"outpost", "agent", "envoy", "governor", "forge", "forge-merge", "unknown"} {
		got := cfg.ResolveModel(role)
		if got != "opus" {
			t.Errorf("ResolveModel(%q) with model_tier=opus = %q, want %q", role, got, "opus")
		}
	}
}

func TestResolveModelPerRoleOverride(t *testing.T) {
	cfg := WorldConfig{
		Agents: AgentsSection{
			ModelTier: "opus",
			Models: ModelsSection{
				Outpost:  "haiku",
				Envoy:    "sonnet",
				Governor: "opus",
				Forge:    "haiku",
			},
		},
	}

	cases := []struct {
		role string
		want string
	}{
		{"outpost", "haiku"},
		{"agent", "haiku"},   // "agent" maps to Outpost
		{"envoy", "sonnet"},
		{"governor", "opus"},
		{"forge", "haiku"},
		{"forge-merge", "haiku"}, // "forge-merge" maps to Forge
		{"unknown", "opus"},      // unknown role falls back to model_tier
	}

	for _, tc := range cases {
		got := cfg.ResolveModel(tc.role)
		if got != tc.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestResolveModelPartialOverride(t *testing.T) {
	// Only outpost has override; other roles use model_tier.
	cfg := WorldConfig{
		Agents: AgentsSection{
			ModelTier: "opus",
			Models: ModelsSection{
				Outpost: "sonnet",
			},
		},
	}

	if got := cfg.ResolveModel("outpost"); got != "sonnet" {
		t.Errorf("ResolveModel(outpost) = %q, want sonnet", got)
	}
	if got := cfg.ResolveModel("envoy"); got != "opus" {
		t.Errorf("ResolveModel(envoy) = %q, want opus (model_tier)", got)
	}
	if got := cfg.ResolveModel("forge"); got != "opus" {
		t.Errorf("ResolveModel(forge) = %q, want opus (model_tier)", got)
	}
}

func TestResolveRuntimeFallbackToClaude(t *testing.T) {
	cfg := WorldConfig{}
	for _, role := range []string{"outpost", "envoy", "governor", "forge", "chancellor", "unknown"} {
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
	for _, role := range []string{"outpost", "envoy", "governor"} {
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

func TestWorldConfigValidateModelsSection(t *testing.T) {
	valid := []ModelsSection{
		{},
		{Outpost: "sonnet"},
		{Envoy: "opus"},
		{Governor: "haiku"},
		{Forge: "sonnet"},
		{Outpost: "haiku", Envoy: "opus", Governor: "sonnet", Forge: "haiku"},
	}
	for _, m := range valid {
		cfg := DefaultWorldConfig()
		cfg.Agents.Models = m
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid models %+v, got error: %v", m, err)
		}
	}

	invalid := []struct {
		models ModelsSection
		field  string
	}{
		{ModelsSection{Outpost: "gpt-4"}, "agents.models.outpost"},
		{ModelsSection{Envoy: "claude"}, "agents.models.envoy"},
		{ModelsSection{Governor: "fast"}, "agents.models.governor"},
		{ModelsSection{Forge: "slow"}, "agents.models.forge"},
	}
	for _, tc := range invalid {
		cfg := DefaultWorldConfig()
		cfg.Agents.Models = tc.models
		err := cfg.Validate()
		if err == nil {
			t.Errorf("expected error for invalid models %+v, got nil", tc.models)
		} else if !strings.Contains(err.Error(), tc.field) {
			t.Errorf("expected error to mention %q, got: %v", tc.field, err)
		}
	}
}

func TestWorldConfigValidateModelTier(t *testing.T) {
	valid := []string{"sonnet", "opus", "haiku", ""}
	for _, tier := range valid {
		cfg := WorldConfig{Agents: AgentsSection{ModelTier: tier}}
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected tier %q to be valid, got: %v", tier, err)
		}
	}

	invalid := []string{"gpt-4", "claude", "fast"}
	for _, tier := range invalid {
		cfg := WorldConfig{Agents: AgentsSection{ModelTier: tier}}
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected tier %q to be invalid, got nil error", tier)
		}
	}
}

func TestWorldConfigValidateCapacity(t *testing.T) {
	valid := []int{0, 1, 100}
	for _, cap := range valid {
		cfg := DefaultWorldConfig()
		cfg.Agents.Capacity = cap
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected capacity %d to be valid, got: %v", cap, err)
		}
	}

	invalid := []int{-1, -100}
	for _, cap := range invalid {
		cfg := DefaultWorldConfig()
		cfg.Agents.Capacity = cap
		if err := cfg.Validate(); err == nil {
			t.Errorf("expected capacity %d to be invalid, got nil error", cap)
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

func TestLoadWorldConfigValidationError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	// Write world.toml with invalid model_tier.
	worldDir := filepath.Join(dir, "testworld")
	if err := os.MkdirAll(worldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	worldPath := filepath.Join(worldDir, "world.toml")
	if err := os.WriteFile(worldPath, []byte("[agents]\nmodel_tier = \"gpt-4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadWorldConfig("testworld")
	if err == nil {
		t.Fatal("expected error for invalid model_tier in world config")
	}
	if !strings.Contains(err.Error(), "model_tier") {
		t.Fatalf("expected error to mention model_tier, got: %v", err)
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
	if cfg.Agents.ModelTier != defaults.Agents.ModelTier {
		t.Fatalf("model_tier = %q, want %q", cfg.Agents.ModelTier, defaults.Agents.ModelTier)
	}
	if cfg.World.Branch != defaults.World.Branch {
		t.Fatalf("branch = %q, want %q", cfg.World.Branch, defaults.World.Branch)
	}
}

func TestLoadGlobalConfigValidFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[agents]\nmodel_tier = \"haiku\"\n[world]\nbranch = \"develop\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadGlobalConfig()
	if err != nil {
		t.Fatalf("LoadGlobalConfig() error: %v", err)
	}
	if cfg.Agents.ModelTier != "haiku" {
		t.Fatalf("model_tier = %q, want %q", cfg.Agents.ModelTier, "haiku")
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
	if IsSleeping("noworld") {
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

	if !IsSleeping("sleepyworld") {
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

	if IsSleeping("activeworld") {
		t.Fatal("IsSleeping() = true for world with sleeping = false, want false")
	}
}

func TestIsSleepingFailClosed(t *testing.T) {
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

	// Fail-closed: if config cannot be loaded, assume sleeping.
	if !IsSleeping("badworld") {
		t.Fatal("IsSleeping() = false when config read fails, want true (fail-closed)")
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

func TestLoadGlobalConfigInvalidModelTier(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	content := "[agents]\nmodel_tier = \"turbo\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sol.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadGlobalConfig()
	if err == nil {
		t.Fatal("LoadGlobalConfig() expected error for invalid model_tier, got nil")
	}
	if !strings.Contains(err.Error(), "model_tier") {
		t.Fatalf("expected error to mention model_tier, got: %v", err)
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

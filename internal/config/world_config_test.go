package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultWorldConfig(t *testing.T) {
	cfg := DefaultWorldConfig()
	if cfg.Agents.Capacity != 0 {
		t.Fatalf("expected capacity 0, got %d", cfg.Agents.Capacity)
	}
	if cfg.Agents.ModelTier != "sonnet" {
		t.Fatalf("expected model_tier 'sonnet', got %q", cfg.Agents.ModelTier)
	}
	if cfg.Forge.TargetBranch != "main" {
		t.Fatalf("expected target_branch 'main', got %q", cfg.Forge.TargetBranch)
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
	if cfg.Forge.TargetBranch != defaults.Forge.TargetBranch {
		t.Fatalf("expected target_branch %q, got %q", defaults.Forge.TargetBranch, cfg.Forge.TargetBranch)
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
	if cfg.Forge.TargetBranch != "main" {
		t.Fatalf("expected target_branch 'main' (default), got %q", cfg.Forge.TargetBranch)
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
	if cfg.Forge.TargetBranch != "main" {
		t.Fatalf("expected target_branch 'main' (default), got %q", cfg.Forge.TargetBranch)
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
		},
		Agents: AgentsSection{
			Capacity:     10,
			NamePoolPath: "/custom/names.txt",
			ModelTier:    "opus",
		},
		Forge: ForgeSection{
			TargetBranch: "develop",
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
	if loaded.Forge.TargetBranch != original.Forge.TargetBranch {
		t.Fatalf("target_branch: expected %q, got %q", original.Forge.TargetBranch, loaded.Forge.TargetBranch)
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

func TestValidateWorldNameReserved(t *testing.T) {
	reserved := []string{"store", "runtime", "sol", "formulas"}
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

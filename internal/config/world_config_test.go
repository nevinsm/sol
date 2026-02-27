package config

import (
	"os"
	"path/filepath"
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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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

func TestLoadWorldConfigQualityGates(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

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
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

	expected := filepath.Join(dir, "myworld", "world.toml")
	got := WorldConfigPath("myworld")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("SOL_HOME", dir)
	t.Cleanup(func() { os.Unsetenv("SOL_HOME") })

	expected := filepath.Join(dir, "sol.toml")
	got := GlobalConfigPath()
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

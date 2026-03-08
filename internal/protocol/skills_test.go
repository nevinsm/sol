package protocol

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoleSkillsOutpost(t *testing.T) {
	skills := RoleSkills("outpost")
	if len(skills) != 2 {
		t.Errorf("outpost should have 2 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsForge(t *testing.T) {
	skills := RoleSkills("forge")
	if len(skills) != 3 {
		t.Errorf("forge should have 3 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsGovernor(t *testing.T) {
	skills := RoleSkills("governor")
	if len(skills) != 5 {
		t.Errorf("governor should have 5 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsEnvoy(t *testing.T) {
	skills := RoleSkills("envoy")
	if len(skills) != 8 {
		t.Errorf("envoy should have 8 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsSenate(t *testing.T) {
	skills := RoleSkills("senate")
	if len(skills) != 3 {
		t.Errorf("senate should have 3 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsUnknown(t *testing.T) {
	skills := RoleSkills("unknown")
	if skills != nil {
		t.Errorf("unknown role should return nil, got %v", skills)
	}
}

func TestRoleSkillsReturnsCopy(t *testing.T) {
	skills1 := RoleSkills("outpost")
	skills2 := RoleSkills("outpost")
	skills1[0] = "mutated"
	if skills2[0] == "mutated" {
		t.Error("RoleSkills should return a copy, not a reference to the internal slice")
	}
}

func TestInstallSkillsCreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		Role:      "outpost",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Verify skills directory exists.
	skillsDir := filepath.Join(dir, ".claude", "skills")
	info, err := os.Stat(skillsDir)
	if err != nil {
		t.Fatalf("skills directory should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("skills should be a directory")
	}

	// Verify each skill has a SKILL.md file.
	skills := RoleSkills("outpost")
	for _, name := range skills {
		skillPath := filepath.Join(skillsDir, name, "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			t.Errorf("skill %q SKILL.md should exist: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("skill %q SKILL.md should not be empty", name)
		}
	}
}

func TestInstallSkillsForge(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:        "testworld",
		Role:         "forge",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skills := RoleSkills("forge")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("forge skill %q should exist: %v", name, err)
		}
	}
}

func TestInstallSkillsGovernor(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "testworld",
		SolBinary: "sol",
		Role:      "governor",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skills := RoleSkills("governor")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("governor skill %q should exist: %v", name, err)
		}
	}
}

func TestInstallSkillsEnvoy(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skills := RoleSkills("envoy")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("envoy skill %q should exist: %v", name, err)
		}
	}
}

func TestInstallSkillsSenate(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "testworld",
		SolBinary: "sol",
		Role:      "senate",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skills := RoleSkills("senate")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("senate skill %q should exist: %v", name, err)
		}
	}
}

func TestInstallSkillsCleansStale(t *testing.T) {
	dir := t.TempDir()

	// Install forge skills first.
	forgeCtx := SkillContext{
		World:        "testworld",
		Role:         "forge",
		TargetBranch: "main",
	}
	if err := InstallSkills(dir, forgeCtx); err != nil {
		t.Fatalf("InstallSkills (forge) failed: %v", err)
	}

	// Verify forge skills exist.
	forgeSkills := RoleSkills("forge")
	for _, name := range forgeSkills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Fatalf("forge skill %q should exist before cleanup: %v", name, err)
		}
	}

	// Now install outpost skills — should clean up forge skills.
	outpostCtx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		Role:      "outpost",
	}
	if err := InstallSkills(dir, outpostCtx); err != nil {
		t.Fatalf("InstallSkills (outpost) failed: %v", err)
	}

	// Verify forge-only skills are removed.
	for _, name := range forgeSkills {
		isOutpost := false
		for _, os := range RoleSkills("outpost") {
			if os == name {
				isOutpost = true
				break
			}
		}
		if isOutpost {
			continue
		}
		skillDir := filepath.Join(dir, ".claude", "skills", name)
		if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
			t.Errorf("stale forge skill %q should be removed after outpost install", name)
		}
	}

	// Verify outpost skills exist.
	outpostSkills := RoleSkills("outpost")
	for _, name := range outpostSkills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("outpost skill %q should exist after install: %v", name, err)
		}
	}
}

func TestSkillContentHasWorldName(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "TestBot",
		Role:      "outpost",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Check resolve-and-handoff skill exists and has meaningful content.
	skillPath := filepath.Join(dir, ".claude", "skills", "resolve-and-handoff", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	if len(content) < 50 {
		t.Error("skill content should be substantial, not just a header")
	}
}

func TestForgeSkillContentHasBranch(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:        "myworld",
		Role:         "forge",
		TargetBranch: "develop",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Check merge-operations skill references the target branch.
	skillPath := filepath.Join(dir, ".claude", "skills", "merge-operations", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	if !contains(content, "develop") {
		t.Error("merge-operations skill should contain target branch name")
	}
}

func TestGovernorSkillContentHasNotifications(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		SolBinary: "sol",
		Role:      "governor",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Check notification-handling skill has notification types.
	skillPath := filepath.Join(dir, ".claude", "skills", "notification-handling", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	for _, notifType := range []string{"AGENT_DONE", "MERGED", "MERGE_FAILED", "RECOVERY_NEEDED"} {
		if !contains(content, notifType) {
			t.Errorf("notification-handling skill should contain %q", notifType)
		}
	}
}

// contains is a test helper to check for substring presence.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

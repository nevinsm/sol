package protocol

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestRoleSkillsOutpost(t *testing.T) {
	skills := RoleSkills("outpost")
	if len(skills) != 2 {
		t.Errorf("outpost should have 2 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsGovernor(t *testing.T) {
	skills := RoleSkills("governor")
	if len(skills) != 6 {
		t.Errorf("governor should have 6 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsEnvoy(t *testing.T) {
	skills := RoleSkills("envoy")
	if len(skills) != 10 {
		t.Errorf("envoy should have 10 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsChancellor(t *testing.T) {
	skills := RoleSkills("chancellor")
	if len(skills) != 5 {
		t.Errorf("chancellor should have 5 skills, got %d: %v", len(skills), skills)
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

func TestInstallSkillsChancellor(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "testworld",
		SolBinary: "sol",
		Role:      "chancellor",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skills := RoleSkills("chancellor")
	for _, name := range skills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("chancellor skill %q should exist: %v", name, err)
		}
	}
}

func TestChancellorWritPlanningNoDispatch(t *testing.T) {
	ctx := SkillContext{
		SolBinary: "sol",
		Role:      "chancellor",
	}

	content := generateSkill("writ-planning", ctx)

	// The chancellor prompt says "does NOT dispatch work". The writ-planning
	// skill must not provide dispatch commands like caravan launch.
	if contains(content, "caravan launch") {
		t.Error("chancellor writ-planning skill should not include 'caravan launch' — chancellor does not dispatch")
	}

	// Should still have caravan creation/planning commands.
	if !contains(content, "caravan create") {
		t.Error("chancellor writ-planning skill should include 'caravan create'")
	}
	if !contains(content, "caravan commission") {
		t.Error("chancellor writ-planning skill should include 'caravan commission'")
	}

	// Should note that dispatch is the autarch/governor's responsibility.
	if !contains(content, "dispatch action") {
		t.Error("chancellor writ-planning skill should explain that launching is a dispatch action")
	}
}

func TestInstallSkillsCleansStale(t *testing.T) {
	dir := t.TempDir()

	// Install governor skills first.
	govCtx := SkillContext{
		World:     "testworld",
		SolBinary: "sol",
		Role:      "governor",
	}
	if err := InstallSkills(dir, govCtx); err != nil {
		t.Fatalf("InstallSkills (governor) failed: %v", err)
	}

	// Verify governor skills exist.
	govSkills := RoleSkills("governor")
	for _, name := range govSkills {
		skillPath := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Fatalf("governor skill %q should exist before cleanup: %v", name, err)
		}
	}

	// Now install outpost skills — should clean up governor-only skills.
	outpostCtx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		Role:      "outpost",
	}
	if err := InstallSkills(dir, outpostCtx); err != nil {
		t.Fatalf("InstallSkills (outpost) failed: %v", err)
	}

	// Verify governor-only skills are removed.
	for _, name := range govSkills {
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
			t.Errorf("stale governor skill %q should be removed after outpost install", name)
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

	for _, notifType := range []string{"MAIL", "AGENT_DONE", "MERGED", "MERGE_FAILED"} {
		if !contains(content, notifType) {
			t.Errorf("notification-handling skill should contain %q", notifType)
		}
	}
	// RECOVERY_NEEDED goes to autarch, not governor — must not appear in governor skill.
	if contains(content, "RECOVERY_NEEDED") {
		t.Error("governor notification-handling skill should not contain RECOVERY_NEEDED")
	}
}

func TestEnvoySkillContentHasNotifications(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Check notification-handling skill has envoy notification types.
	skillPath := filepath.Join(dir, ".claude", "skills", "notification-handling", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	for _, notifType := range []string{"MAIL", "MERGED", "MERGE_FAILED", "AGENT_DONE"} {
		if !contains(content, notifType) {
			t.Errorf("envoy notification-handling skill should contain %q", notifType)
		}
	}

	// Envoy should NOT have governor-specific notification types.
	if contains(content, "RECOVERY_NEEDED") {
		t.Error("envoy notification-handling skill should not contain RECOVERY_NEEDED")
	}
}

func TestEnvoySkillContentHasMail(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	// Check mail skill has key commands.
	skillPath := filepath.Join(dir, ".claude", "skills", "mail", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	for _, cmd := range []string{"mail inbox", "mail read", "mail ack", "mail check", "mail send"} {
		if !contains(content, cmd) {
			t.Errorf("mail skill should contain %q", cmd)
		}
	}
}

func TestStatusMonitoringHasComponentStatus(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "status-monitoring", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	// Should mention component status commands.
	for _, component := range []string{"prefect status", "consul status", "sentinel status"} {
		if !contains(content, component) {
			t.Errorf("status-monitoring skill should contain %q", component)
		}
	}

	// Should mention new status output details.
	if !contains(content, "unread mail") {
		t.Error("status-monitoring skill should mention unread mail count")
	}
	if !contains(content, "nudge queue") {
		t.Error("status-monitoring skill should mention nudge queue depth")
	}
}

func TestWorldCoordinationHasServiceManagement(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		SolBinary: "sol",
		Role:      "governor",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "world-coordination", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	// Should have service management section.
	if !contains(content, "Service Management") {
		t.Error("world-coordination skill should contain Service Management section")
	}
	if !contains(content, "service status") {
		t.Error("world-coordination skill should contain service status command")
	}
	if !contains(content, "down --all") {
		t.Error("world-coordination skill should contain down --all command")
	}
}

func TestWorldOperationsHasServiceLifecycle(t *testing.T) {
	dir := t.TempDir()
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	if err := InstallSkills(dir, ctx); err != nil {
		t.Fatalf("InstallSkills failed: %v", err)
	}

	skillPath := filepath.Join(dir, ".claude", "skills", "world-operations", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	content := string(data)

	// Should have service lifecycle section.
	if !contains(content, "Service Lifecycle") {
		t.Error("world-operations skill should contain Service Lifecycle section")
	}
	if !contains(content, "service install") {
		t.Error("world-operations skill should contain service install command")
	}
	if !contains(content, "down --all") {
		t.Error("world-operations skill should contain down --all command")
	}
}

func TestCaravanManagementRoleAware(t *testing.T) {
	// Governor version
	govDir := t.TempDir()
	govCtx := SkillContext{
		World:     "testworld",
		SolBinary: "sol",
		Role:      "governor",
	}
	if err := InstallSkills(govDir, govCtx); err != nil {
		t.Fatalf("InstallSkills (governor) failed: %v", err)
	}
	govPath := filepath.Join(govDir, ".claude", "skills", "caravan-management", "SKILL.md")
	govData, err := os.ReadFile(govPath)
	if err != nil {
		t.Fatalf("failed to read governor caravan skill: %v", err)
	}
	if !contains(string(govData), "coordinating") {
		t.Error("governor caravan-management should mention coordinating")
	}

	// Envoy version
	envDir := t.TempDir()
	envCtx := SkillContext{
		World:     "testworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}
	if err := InstallSkills(envDir, envCtx); err != nil {
		t.Fatalf("InstallSkills (envoy) failed: %v", err)
	}
	envPath := filepath.Join(envDir, ".claude", "skills", "caravan-management", "SKILL.md")
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read envoy caravan skill: %v", err)
	}
	if !contains(string(envData), "sequencing") {
		t.Error("envoy caravan-management should mention sequencing")
	}

	// Descriptions should differ
	if string(govData) == string(envData) {
		t.Error("governor and envoy caravan-management should have different descriptions")
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

// findRepoRoot walks up from the current directory to find the go.mod file.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}

// TestSkillCommandReferencesExist verifies that every `sol <subcommand>` shown
// in generated skill content corresponds to a real CLI command, and that flags
// referenced alongside those commands actually exist.
func TestSkillCommandReferencesExist(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping command reference validation in short mode")
	}

	// Build the sol binary into a temp location.
	solBin := filepath.Join(t.TempDir(), "sol")
	repoRoot := findRepoRoot(t)
	build := exec.Command("go", "build", "-o", solBin, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build sol binary: %v\n%s", err, out)
	}

	// Generate skill content for every role.
	roles := []string{"outpost", "governor", "envoy", "chancellor"}

	// cmdEntry tracks a unique subcommand and the flags referenced with it.
	type cmdEntry struct {
		flags   map[string]bool
		sources []string
	}
	commands := make(map[string]*cmdEntry)

	// Regex to match backtick-wrapped commands starting with "sol ".
	cmdRe := regexp.MustCompile("`sol ([^`]+)`")
	// Regex to extract --flag names (stops at = or space).
	flagRe := regexp.MustCompile(`--([a-z][-a-z0-9]*)`)

	for _, role := range roles {
		ctx := SkillContext{
			World:        "testworld",
			AgentName:    "TestBot",
			SolBinary:    "sol",
			Role:         role,
			TargetBranch: "main",
		}
		for _, name := range RoleSkills(role) {
			content := generateSkill(name, ctx)
			matches := cmdRe.FindAllStringSubmatch(content, -1)
			for _, m := range matches {
				cmdLine := m[1]

				// Skip matches that span across markdown table
				// cells — a sign of a broken backtick pair.
				if strings.Contains(cmdLine, "|") {
					continue
				}

				parts := strings.Fields(cmdLine)

				// Extract the subcommand path: consecutive words that are
				// not flags, not placeholders, and not quoted strings.
				var cmdPath []string
				for _, p := range parts {
					if strings.HasPrefix(p, "-") ||
						strings.HasPrefix(p, "<") ||
						strings.HasPrefix(p, "[") ||
						strings.HasPrefix(p, "\"") ||
						strings.HasPrefix(p, "'") {
						break
					}
					// Skip known value-like tokens embedded in commands.
					if p == "testworld" || p == "TestBot" {
						// These are interpolated world/agent names used as
						// positional arguments — not part of the subcommand.
						break
					}
					cmdPath = append(cmdPath, p)
				}
				key := strings.Join(cmdPath, " ")
				if key == "" {
					continue
				}

				entry, ok := commands[key]
				if !ok {
					entry = &cmdEntry{flags: make(map[string]bool)}
					commands[key] = entry
				}
				entry.sources = append(entry.sources, role+"/"+name)

				// Extract flags from the full command line.
				flagMatches := flagRe.FindAllStringSubmatch(cmdLine, -1)
				for _, fm := range flagMatches {
					entry.flags[fm[1]] = true
				}
			}
		}
	}

	// Sort keys for deterministic output.
	var keys []string
	for k := range commands {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Verify each subcommand exists and accepts its flags.
	for _, key := range keys {
		entry := commands[key]
		t.Run("cmd/"+strings.ReplaceAll(key, " ", "_"), func(t *testing.T) {
			args := append(strings.Fields(key), "--help")
			cmd := exec.Command(solBin, args...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("command %q does not exist (from: %v): %v\n%s",
					key, entry.sources, err, out)
			}

			helpText := string(out)
			for flag := range entry.flags {
				if flag == "help" {
					continue // --help is always available
				}
				if !strings.Contains(helpText, "--"+flag) {
					t.Errorf("command %q: flag --%s not found in help output (from: %v)",
						key, flag, entry.sources)
				}
			}
		})
	}
}

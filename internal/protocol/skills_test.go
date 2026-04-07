package protocol

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestSkillGoldenFiles snapshots the rendered content of skills whose prose
// is sensitive to subtle drift (e.g. failure-mode advice that must agree with
// the envoy/outpost branch model). Run with -update to refresh.
func TestSkillGoldenFiles(t *testing.T) {
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		SolBinary: "sol",
		Role:      "envoy",
	}

	cases := []struct {
		name   string
		golden string
	}{
		{"resolve-and-submit", "testdata/skill_resolve-and-submit.golden"},
		{"world-operations", "testdata/skill_world-operations.golden"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := generateSkill(tc.name, ctx)
			if got == "" {
				t.Fatalf("skill %q rendered empty", tc.name)
			}

			if *updateGolden {
				if err := os.WriteFile(tc.golden, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden %q: %v", tc.golden, err)
			}
			if string(want) != got {
				t.Errorf("skill %q drifted from golden file %s.\n"+
					"Re-run with -update if the change is intentional.\n"+
					"--- want ---\n%s\n--- got ---\n%s",
					tc.name, tc.golden, want, got)
			}
		})
	}
}

// resolve-and-submit must NOT mention "Pull main" or "checkout main && git pull"
// — these contradict the envoy/outpost worktree branch model.
func TestResolveAndSubmitNoPullMain(t *testing.T) {
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		SolBinary: "sol",
		Role:      "envoy",
	}
	content := generateSkill("resolve-and-submit", ctx)
	for _, banned := range []string{"Pull main", "checkout main && git pull", "git pull"} {
		if strings.Contains(content, banned) {
			t.Errorf("resolve-and-submit must not contain %q (contradicts worktree branch model)", banned)
		}
	}
	for _, required := range []string{
		"git fetch origin && git rebase origin/main",
		"envoy/<world>/<name>",
		"outpost/<name>/<writID>",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("resolve-and-submit should contain %q", required)
		}
	}
}

// world-operations must describe `sol down` and `sol down --all` correctly:
// plain `sol down` stops world services; `--all` additionally kills envoy
// sessions sphere-wide.
func TestWorldOperationsDownSemantics(t *testing.T) {
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		SolBinary: "sol",
		Role:      "envoy",
	}
	content := generateSkill("world-operations", ctx)
	if !strings.Contains(content, "`sol down`") {
		t.Error("world-operations should document plain `sol down`")
	}
	if !strings.Contains(content, "`sol down --all`") {
		t.Error("world-operations should document `sol down --all`")
	}
	if !strings.Contains(content, "envoy sessions") {
		t.Error("world-operations should mention that --all also kills envoy sessions")
	}
}

var updateGolden = flag.Bool("update", false, "update golden files")

func TestRoleSkillsOutpost(t *testing.T) {
	skills, err := RoleSkills("outpost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Errorf("outpost should have 1 skill, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsEnvoy(t *testing.T) {
	skills, err := RoleSkills("envoy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 9 {
		t.Errorf("envoy should have 9 skills, got %d: %v", len(skills), skills)
	}
}

func TestRoleSkillsUnknown(t *testing.T) {
	skills, err := RoleSkills("unknown")
	if err == nil {
		t.Error("expected error for unknown role, got nil")
	}
	if skills != nil {
		t.Errorf("unknown role should return nil, got %v", skills)
	}
}

func TestRoleSkillsReturnsCopy(t *testing.T) {
	skills1, _ := RoleSkills("outpost")
	skills2, _ := RoleSkills("outpost")
	skills1[0] = "mutated"
	if skills2[0] == "mutated" {
		t.Error("RoleSkills should return a copy, not a reference to the internal slice")
	}
}

func TestBuildSkillsOutpost(t *testing.T) {
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		Role:      "outpost",
	}

	skills, err := BuildSkills(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names, _ := RoleSkills("outpost")
	if len(skills) != len(names) {
		t.Fatalf("expected %d skills, got %d", len(names), len(skills))
	}
	for _, s := range skills {
		if s.Content == "" {
			t.Errorf("skill %q should not be empty", s.Name)
		}
	}
}

func TestBuildSkillsEnvoy(t *testing.T) {
	ctx := SkillContext{
		World:     "testworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	skills, err := BuildSkills(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names, _ := RoleSkills("envoy")
	if len(skills) != len(names) {
		t.Fatalf("expected %d skills, got %d", len(names), len(skills))
	}
}

func TestBuildSkillsUnknownRole(t *testing.T) {
	ctx := SkillContext{
		World: "testworld",
		Role:  "nonexistent",
	}

	skills, err := BuildSkills(ctx)
	if err == nil {
		t.Error("expected error for unknown role, got nil")
	}
	if skills != nil {
		t.Errorf("expected nil skills for unknown role, got %v", skills)
	}
}

func TestSkillContentHasWorldName(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "TestBot",
		Role:      "outpost",
	}

	skills, err := BuildSkills(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check resolve-and-handoff skill has meaningful content.
	for _, s := range skills {
		if s.Name == "resolve-and-handoff" {
			if len(s.Content) < 50 {
				t.Error("skill content should be substantial, not just a header")
			}
			return
		}
	}
	t.Error("resolve-and-handoff skill not found")
}

func TestEnvoySkillContentHasNotifications(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	content := generateSkill("notification-handling", ctx)

	// Envoy receives MAIL notifications only.
	if !contains(content, "MAIL") {
		t.Error("envoy notification-handling skill should contain MAIL")
	}

	// RECOVERY_NEEDED goes to autarch, not envoy — must not appear.
	if contains(content, "RECOVERY_NEEDED") {
		t.Error("envoy notification-handling skill should not contain RECOVERY_NEEDED")
	}
}

func TestEnvoySkillContentHasMail(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	content := generateSkill("mail", ctx)

	for _, cmd := range []string{"mail inbox", "mail read", "mail ack", "mail check", "mail send"} {
		if !contains(content, cmd) {
			t.Errorf("mail skill should contain %q", cmd)
		}
	}
}

func TestStatusMonitoringHasComponentStatus(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	content := generateSkill("status-monitoring", ctx)

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

func TestWorldOperationsHasServiceLifecycle(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	content := generateSkill("world-operations", ctx)

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
	// Envoy version
	envCtx := SkillContext{
		World:     "testworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}
	envContent := generateSkill("caravan-management", envCtx)
	if !contains(envContent, "sequencing") {
		t.Error("envoy caravan-management should mention sequencing")
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
	roles := []string{"outpost", "envoy"}

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
		roleSkills, _ := RoleSkills(role)
		for _, name := range roleSkills {
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

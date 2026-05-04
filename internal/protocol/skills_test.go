package protocol

import (
	"bytes"
	"flag"
	"log/slog"
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
			got, err := generateSkill(tc.name, ctx)
			if err != nil {
				t.Fatalf("skill %q failed to render: %v", tc.name, err)
			}
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
// — these contradict the envoy/outpost worktree branch model. The rebase
// command must reference the world's configured main branch via the
// MainBranch template variable, not a hardcoded "origin/main" string.
func TestResolveAndSubmitNoPullMain(t *testing.T) {
	baseCtx := SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		SolBinary: "sol",
		Role:      "envoy",
	}

	// 1. Default behavior: empty MainBranch falls back to "main".
	defaultContent, err := generateSkill("resolve-and-submit", baseCtx)
	if err != nil {
		t.Fatalf("render with default MainBranch: %v", err)
	}
	for _, banned := range []string{"Pull main", "checkout main && git pull", "git pull"} {
		if strings.Contains(defaultContent, banned) {
			t.Errorf("resolve-and-submit must not contain %q (contradicts worktree branch model)", banned)
		}
	}
	for _, required := range []string{
		"git fetch origin && git rebase origin/main",
		"envoy/<world>/<name>",
		"outpost/<name>/<writID>",
	} {
		if !strings.Contains(defaultContent, required) {
			t.Errorf("resolve-and-submit (default MainBranch) should contain %q", required)
		}
	}

	// 2. Custom MainBranch: rendered output must reflect the configured
	//    branch, proving the template uses {{.MainBranch}} rather than a
	//    hardcoded "origin/main".
	customCtx := baseCtx
	customCtx.MainBranch = "develop"
	customContent, err := generateSkill("resolve-and-submit", customCtx)
	if err != nil {
		t.Fatalf("render with MainBranch=develop: %v", err)
	}
	if !strings.Contains(customContent, "git fetch origin && git rebase origin/develop") {
		t.Errorf("resolve-and-submit with MainBranch=develop should reference origin/develop, got:\n%s", customContent)
	}
	if strings.Contains(customContent, "origin/main") {
		t.Errorf("resolve-and-submit with MainBranch=develop must not contain hardcoded 'origin/main'")
	}

	// 3. Template source must not hardcode "origin/main" — the literal
	//    must come from {{.MainBranch}} substitution at render time.
	tmplBytes, err := skillTemplates.ReadFile("skilltmpl/resolve-and-submit.md.tmpl")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if strings.Contains(string(tmplBytes), "origin/main") {
		t.Errorf("resolve-and-submit.md.tmpl must not hardcode 'origin/main'; use {{.MainBranch}} instead")
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
	content := mustGenerateSkill(t, "world-operations", ctx)
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
	if len(skills) != 8 {
		t.Errorf("envoy should have 8 skills, got %d: %v", len(skills), skills)
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

// TestBuildSkillsRenderFailureVisibleMarker verifies that when a skill
// template fails to render (e.g. references a missing field), BuildSkills
// does not silently drop the skill: it logs the failure via softfail.Log
// and inserts a visible '[skill render failed: <name>]' marker into the
// returned bundle so the agent has at least *some* signal that something
// went wrong.
func TestBuildSkillsRenderFailureVisibleMarker(t *testing.T) {
	const (
		brokenSkill = "broken-test-skill-rendering"
		brokenRole  = "broken-test-role-rendering"
	)

	// Inject a broken template: text/template's "missingkey=default" mode
	// is the default, but invoking a method on a missing/zero field reliably
	// errors at execute time. Use {{.Missing.Method}} to force a runtime error.
	if _, err := parsedTemplates.New(brokenSkill + ".md.tmpl").Parse(`{{.Missing.Method}}`); err != nil {
		t.Fatalf("failed to parse test template: %v", err)
	}

	// Register a fake role that bundles only the broken skill.
	roleSkillsMap[brokenRole] = []string{brokenSkill}
	t.Cleanup(func() { delete(roleSkillsMap, brokenRole) })

	// Capture slog output so we can assert that the render failure was
	// logged. softfail.Log uses slog.Default() when no logger is provided.
	var buf bytes.Buffer
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevDefault) })

	skills, err := BuildSkills(SkillContext{
		World:     "testworld",
		AgentName: "TestBot",
		SolBinary: "sol",
		Role:      brokenRole,
	})
	if err != nil {
		t.Fatalf("BuildSkills must tolerate render failures, got error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected exactly one skill in bundle (the failed one with marker), got %d: %+v", len(skills), skills)
	}
	if skills[0].Name != brokenSkill {
		t.Errorf("expected skill name %q, got %q", brokenSkill, skills[0].Name)
	}
	wantMarker := "skill render failed: " + brokenSkill
	if !strings.Contains(skills[0].Content, wantMarker) {
		t.Errorf("expected bundle content to contain visible marker %q; got:\n%s", wantMarker, skills[0].Content)
	}

	// Verify the failure was logged — softfail.Log writes a "soft failure"
	// warning that should reference our skill name.
	logged := buf.String()
	if !strings.Contains(logged, "soft failure") {
		t.Errorf("expected slog output to contain 'soft failure' warning; got:\n%s", logged)
	}
	if !strings.Contains(logged, brokenSkill) {
		t.Errorf("expected slog output to reference failing skill %q; got:\n%s", brokenSkill, logged)
	}
}

// TestResolveAndHandoffNoDeadConditional verifies that the resolve-and-handoff
// template no longer carries a dead {{if eq .Role "envoy"}} branch. Rendering
// the template under both predicate values (.Role="outpost" and .Role="envoy")
// must yield identical output, since the conditional was removed and replaced
// with the surviving 'committed code history' arm.
func TestResolveAndHandoffNoDeadConditional(t *testing.T) {
	// Render with .Role = "outpost" (the only role this skill is registered
	// for in production) and with .Role = "envoy" (the formerly-conditional
	// branch). Both must produce identical output.
	render := func(role string) string {
		t.Helper()
		// Bypass the role->skills lookup, which would reject "envoy" since
		// resolve-and-handoff is registered only for outpost. We're testing
		// the template itself, not the role mapping.
		content, err := generateSkill("resolve-and-handoff", SkillContext{
			World:     "testworld",
			AgentName: "TestBot",
			SolBinary: "sol",
			Role:      role,
		})
		if err != nil {
			t.Fatalf("render %q: %v", role, err)
		}
		return content
	}

	outpostOut := render("outpost")
	envoyOut := render("envoy")

	if outpostOut != envoyOut {
		t.Errorf("resolve-and-handoff renders differently for .Role=outpost vs .Role=envoy — dead conditional likely still present.\n--- outpost ---\n%s\n--- envoy ---\n%s", outpostOut, envoyOut)
	}

	// Sanity: the surviving 'committed code history' arm must appear, and
	// the dead 'memory' alternative must not (it was specific to the envoy
	// branch which has been removed).
	if !strings.Contains(outpostOut, "committed code history") {
		t.Errorf("resolve-and-handoff missing surviving arm 'committed code history'; got:\n%s", outpostOut)
	}
	// The literal "memory" word may legitimately appear in other prose, so
	// we check the more specific dead-branch output: ", memory." or "and memory"
	// in the handoff sentence. Use the full removed phrase as the canary.
	if strings.Contains(outpostOut, "tether, and memory.") {
		t.Errorf("resolve-and-handoff still contains the dead 'memory' arm")
	}
	if strings.Contains(outpostOut, "tether, memory.") {
		t.Errorf("resolve-and-handoff still contains the dead 'memory' arm in the command-reference row")
	}

	// Defense in depth: the template source itself must not contain the
	// removed conditional.
	tmplBytes, err := skillTemplates.ReadFile("skilltmpl/resolve-and-handoff.md.tmpl")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if strings.Contains(string(tmplBytes), `{{if eq .Role "envoy"}}`) {
		t.Errorf("resolve-and-handoff.md.tmpl still contains the dead {{if eq .Role \"envoy\"}} branch")
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

func TestMailSkillHasNotificationHandling(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	content := mustGenerateSkill(t, "mail", ctx)

	// Mail skill should contain notification handling content (merged from notification-handling).
	if !contains(content, "MAIL") {
		t.Error("mail skill should contain MAIL notification type")
	}
	if !contains(content, "Receiving Notifications") {
		t.Error("mail skill should contain Receiving Notifications section")
	}

	// RECOVERY_NEEDED goes to autarch, not envoy — must not appear.
	if contains(content, "RECOVERY_NEEDED") {
		t.Error("mail skill should not contain RECOVERY_NEEDED")
	}
}

func TestEnvoySkillContentHasMail(t *testing.T) {
	ctx := SkillContext{
		World:     "myworld",
		AgentName: "Echo",
		SolBinary: "sol",
		Role:      "envoy",
	}

	content := mustGenerateSkill(t, "mail", ctx)

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

	content := mustGenerateSkill(t, "status-monitoring", ctx)

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

	content := mustGenerateSkill(t, "world-operations", ctx)

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
	envContent := mustGenerateSkill(t, "caravan-management", envCtx)
	if !contains(envContent, "sequencing") {
		t.Error("envoy caravan-management should mention sequencing")
	}
}

// mustGenerateSkill renders a skill and fatals on error. Test helper for
// the common case where a render failure should fail the test outright.
func mustGenerateSkill(t *testing.T, name string, ctx SkillContext) string {
	t.Helper()
	content, err := generateSkill(name, ctx)
	if err != nil {
		t.Fatalf("generateSkill(%q): %v", name, err)
	}
	return content
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
			World:     "testworld",
			AgentName: "TestBot",
			SolBinary: "sol",
			Role:      role,
		}
		roleSkills, _ := RoleSkills(role)
		for _, name := range roleSkills {
			content := mustGenerateSkill(t, name, ctx)
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

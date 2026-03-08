package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// parseFrontmatter extracts the YAML frontmatter from a SKILL.md string.
// Returns the frontmatter content (without delimiters) and the body.
func parseFrontmatter(content string) (frontmatter, body string, ok bool) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content, false
	}
	rest := content[4:] // skip opening "---\n"
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", content, false
	}
	return rest[:end], rest[end+4:], true
}

// extractField extracts a field value from YAML-like frontmatter.
func extractField(frontmatter, field string) string {
	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.HasPrefix(line, field+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		}
	}
	return ""
}

// --- sol-resolve tests ---

func TestGenerateResolveProducesValidFrontmatter(t *testing.T) {
	ctx := ResolveContext{
		World:        "myworld",
		Agent:        "Toast",
		QualityGates: []string{"make test"},
		OutputDir:    "/tmp/output/sol-12345678",
	}

	content := GenerateResolve(ctx)

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateResolve should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != "sol-resolve" {
		t.Errorf("frontmatter name = %q, want %q", name, "sol-resolve")
	}

	desc := extractField(fm, "description")
	if desc == "" {
		t.Error("frontmatter description should not be empty")
	}
}

func TestGenerateResolveFrontmatterNameMatchesDirectory(t *testing.T) {
	ctx := ResolveContext{World: "w", Agent: "a"}
	content := GenerateResolve(ctx)

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("should have valid frontmatter")
	}

	name := extractField(fm, "name")
	// Per Agent Skills spec, the frontmatter name must match the directory name.
	// The directory is "sol-resolve".
	if name != "sol-resolve" {
		t.Errorf("frontmatter name %q does not match directory name %q", name, "sol-resolve")
	}
}

func TestGenerateResolveTemplatedValues(t *testing.T) {
	ctx := ResolveContext{
		World:        "ember",
		Agent:        "Altair",
		QualityGates: []string{"make build && make test"},
		OutputDir:    "/home/sol/ember/output/sol-aabbccdd",
	}

	content := GenerateResolve(ctx)

	// Quality gates should be substituted.
	if !strings.Contains(content, "make build && make test") {
		t.Error("should contain quality gate command")
	}

	// Output dir should be substituted.
	if !strings.Contains(content, "/home/sol/ember/output/sol-aabbccdd") {
		t.Error("should contain output directory path")
	}

	// Placeholder tokens should NOT remain.
	if strings.Contains(content, "{WORLD}") {
		t.Error("should not contain unreplaced {WORLD} placeholder")
	}
	if strings.Contains(content, "{AGENT}") {
		t.Error("should not contain unreplaced {AGENT} placeholder")
	}
	if strings.Contains(content, "{QUALITY_GATES}") {
		t.Error("should not contain unreplaced {QUALITY_GATES} placeholder")
	}
	if strings.Contains(content, "{OUTPUT_DIR}") {
		t.Error("should not contain unreplaced {OUTPUT_DIR} placeholder")
	}
	if strings.Contains(content, "§") {
		t.Error("should not contain unreplaced § backtick placeholder")
	}
}

func TestGenerateResolveExpectedSections(t *testing.T) {
	ctx := ResolveContext{
		World:        "myworld",
		Agent:        "Toast",
		QualityGates: []string{"make test"},
		OutputDir:    "/tmp/output",
	}

	content := GenerateResolve(ctx)

	sections := []string{
		"## Resolve Protocol",
		"## Code Writs",
		"## Non-Code Writs",
		"## Post-Resolve Behavior",
		"## Warning: Orphaned Tethers",
	}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("missing section %q", s)
		}
	}

	commands := []string{
		"sol resolve",
		"git add",
		"git commit",
		"sol escalate",
	}
	for _, cmd := range commands {
		if !strings.Contains(content, cmd) {
			t.Errorf("missing command %q", cmd)
		}
	}
}

func TestGenerateResolveNoQualityGates(t *testing.T) {
	ctx := ResolveContext{
		World: "myworld",
		Agent: "Toast",
	}

	content := GenerateResolve(ctx)

	// Should still have valid content without gates.
	if !strings.Contains(content, "sol resolve") {
		t.Error("should contain sol resolve even without quality gates")
	}
	if strings.Contains(content, "### Quality Gates") {
		t.Error("should not contain Quality Gates section when none configured")
	}
}

func TestGenerateResolveNoOutputDir(t *testing.T) {
	ctx := ResolveContext{
		World: "myworld",
		Agent: "Toast",
	}

	content := GenerateResolve(ctx)

	if strings.Contains(content, "Persistent output directory:") {
		t.Error("should not contain output directory line when not set")
	}
}

// --- sol-workflow tests ---

func TestGenerateWorkflowProducesValidFrontmatter(t *testing.T) {
	ctx := WorkflowContext{
		World: "myworld",
		Agent: "Toast",
	}

	content := GenerateWorkflow(ctx)

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateWorkflow should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != "sol-workflow" {
		t.Errorf("frontmatter name = %q, want %q", name, "sol-workflow")
	}

	desc := extractField(fm, "description")
	if desc == "" {
		t.Error("frontmatter description should not be empty")
	}
}

func TestGenerateWorkflowFrontmatterNameMatchesDirectory(t *testing.T) {
	ctx := WorkflowContext{World: "w", Agent: "a"}
	content := GenerateWorkflow(ctx)

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("should have valid frontmatter")
	}

	name := extractField(fm, "name")
	if name != "sol-workflow" {
		t.Errorf("frontmatter name %q does not match directory name %q", name, "sol-workflow")
	}
}

func TestGenerateWorkflowTemplatedValues(t *testing.T) {
	ctx := WorkflowContext{
		World: "ember",
		Agent: "Altair",
	}

	content := GenerateWorkflow(ctx)

	// World should be substituted in commands.
	if !strings.Contains(content, "--world=ember") {
		t.Error("should contain --world=ember")
	}

	// Agent should be substituted in commands.
	if !strings.Contains(content, "--agent=Altair") {
		t.Error("should contain --agent=Altair")
	}

	// Placeholder tokens should NOT remain.
	if strings.Contains(content, "{WORLD}") {
		t.Error("should not contain unreplaced {WORLD} placeholder")
	}
	if strings.Contains(content, "{AGENT}") {
		t.Error("should not contain unreplaced {AGENT} placeholder")
	}
	if strings.Contains(content, "§") {
		t.Error("should not contain unreplaced § backtick placeholder")
	}
}

func TestGenerateWorkflowExpectedSections(t *testing.T) {
	ctx := WorkflowContext{
		World: "myworld",
		Agent: "Toast",
	}

	content := GenerateWorkflow(ctx)

	sections := []string{
		"## Step Loop",
		"## Commands",
		"## Workflow Completion",
		"## Looping Workflows",
	}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("missing section %q", s)
		}
	}

	commands := []string{
		"sol workflow current --world=myworld --agent=Toast",
		"sol workflow advance --world=myworld --agent=Toast",
		"sol workflow status --world=myworld --agent=Toast",
		"sol resolve",
		"sol escalate",
	}
	for _, cmd := range commands {
		if !strings.Contains(content, cmd) {
			t.Errorf("missing command %q", cmd)
		}
	}
}

// --- sol-forge-ops tests ---

func TestGenerateForgeOpsProducesValidFrontmatter(t *testing.T) {
	ctx := ForgeOpsContext{
		World:        "myworld",
		TargetBranch: "main",
	}

	content := GenerateForgeOps(ctx)

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateForgeOps should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != "sol-forge-ops" {
		t.Errorf("frontmatter name = %q, want %q", name, "sol-forge-ops")
	}

	desc := extractField(fm, "description")
	if desc == "" {
		t.Error("frontmatter description should not be empty")
	}
}

func TestGenerateForgeOpsFrontmatterNameMatchesDirectory(t *testing.T) {
	ctx := ForgeOpsContext{World: "w", TargetBranch: "main"}
	content := GenerateForgeOps(ctx)

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("should have valid frontmatter")
	}

	name := extractField(fm, "name")
	if name != "sol-forge-ops" {
		t.Errorf("frontmatter name %q does not match directory name %q", name, "sol-forge-ops")
	}
}

func TestGenerateForgeOpsTemplatedValues(t *testing.T) {
	ctx := ForgeOpsContext{
		World:        "testworld",
		TargetBranch: "develop",
	}

	content := GenerateForgeOps(ctx)

	// World should be substituted.
	if !strings.Contains(content, "--world=testworld") {
		t.Error("should contain --world=testworld")
	}

	// Target branch should be substituted.
	if !strings.Contains(content, "HEAD:develop") {
		t.Error("should contain HEAD:develop for push command")
	}
	if !strings.Contains(content, "origin/develop") {
		t.Error("should contain origin/develop for reset command")
	}

	// Placeholder tokens should NOT remain.
	if strings.Contains(content, "{WORLD}") {
		t.Error("should not contain unreplaced {WORLD} placeholder")
	}
	if strings.Contains(content, "{TARGET_BRANCH}") {
		t.Error("should not contain unreplaced {TARGET_BRANCH} placeholder")
	}
	if strings.Contains(content, "§") {
		t.Error("should not contain unreplaced § backtick placeholder")
	}
}

func TestGenerateForgeOpsExpectedSections(t *testing.T) {
	ctx := ForgeOpsContext{
		World:        "myworld",
		TargetBranch: "main",
	}

	content := GenerateForgeOps(ctx)

	sections := []string{
		"## Queue Scanning",
		"## Claiming",
		"## Sync",
		"## Squash Merge",
		"## Quality Gates",
		"## Push",
		"## Mark Results",
		"## Conflict Handling",
		"## Release",
		"## Check Unblocked",
		"## Pause and Resume",
		"## Error Handling",
		"## Command Quick-Reference",
	}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("missing section %q", s)
		}
	}

	commands := []string{
		"sol forge ready --world=myworld --json",
		"sol forge claim --world=myworld --json",
		"sol forge sync --world=myworld",
		"git merge --squash",
		"git push origin HEAD:main",
		"sol forge mark-merged --world=myworld",
		"sol forge mark-failed --world=myworld",
		"sol forge create-resolution --world=myworld",
		"sol forge release --world=myworld",
		"sol forge check-unblocked --world=myworld",
		"sol forge status myworld --json",
		"sol forge await --world=myworld",
		"git merge --abort",
	}
	for _, cmd := range commands {
		if !strings.Contains(content, cmd) {
			t.Errorf("missing command %q", cmd)
		}
	}
}

func TestGenerateForgeOpsErrorHandlingTable(t *testing.T) {
	ctx := ForgeOpsContext{
		World:        "myworld",
		TargetBranch: "main",
	}

	content := GenerateForgeOps(ctx)

	// Error handling table should reference world-specific commands.
	situations := []string{
		"Merge succeeds, gates pass, push succeeds",
		"Merge has conflicts",
		"Quality gates fail",
		"Push rejected",
		"Unexpected error",
		"sol command fails",
	}
	for _, sit := range situations {
		if !strings.Contains(content, sit) {
			t.Errorf("error handling table missing situation %q", sit)
		}
	}

	// Should contain target-branch-specific reset command.
	if !strings.Contains(content, "git reset --hard origin/main") {
		t.Error("error handling table should contain target-branch-specific reset")
	}
}

// --- InstallSkill tests ---

func TestInstallSkill(t *testing.T) {
	dir := t.TempDir()

	content := GenerateResolve(ResolveContext{World: "w", Agent: "a"})
	if err := InstallSkill(dir, "sol-resolve", content); err != nil {
		t.Fatalf("InstallSkill failed: %v", err)
	}

	path := filepath.Join(dir, ".claude", "agents", "sol-resolve", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read installed SKILL.md: %v", err)
	}

	if !strings.HasPrefix(string(data), "---\n") {
		t.Error("installed SKILL.md should start with frontmatter delimiter")
	}
	if !strings.Contains(string(data), "name: sol-resolve") {
		t.Error("installed SKILL.md should contain name field")
	}
}

func TestInstallSkillAllGenerators(t *testing.T) {
	dir := t.TempDir()

	skills := map[string]string{
		"sol-resolve":    GenerateResolve(ResolveContext{World: "w", Agent: "a"}),
		"sol-workflow":   GenerateWorkflow(WorkflowContext{World: "w", Agent: "a"}),
		"sol-forge-ops":  GenerateForgeOps(ForgeOpsContext{World: "w", TargetBranch: "main"}),
		SkillDispatch:    GenerateDispatch("w", "sol"),
		SkillCaravan:     GenerateCaravan("w", "sol"),
		SkillTetherMgmt:  GenerateTetherMgmt("w", "agent"),
		SkillNotify:      GenerateNotify("w"),
		SkillStatus:      GenerateStatus("w", "sol"),
	}

	for name, content := range skills {
		if err := InstallSkill(dir, name, content); err != nil {
			t.Fatalf("InstallSkill(%q) failed: %v", name, err)
		}

		// Verify file exists.
		path := filepath.Join(dir, ".claude", "agents", name, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s/SKILL.md: %v", name, err)
		}

		// Verify frontmatter name matches directory name.
		fm, _, ok := parseFrontmatter(string(data))
		if !ok {
			t.Errorf("%s: missing valid frontmatter", name)
			continue
		}
		fmName := extractField(fm, "name")
		if fmName != name {
			t.Errorf("%s: frontmatter name %q does not match directory name %q", name, fmName, name)
		}
	}
}

// --- sol-dispatch tests ---

func TestGenerateDispatchProducesValidFrontmatter(t *testing.T) {
	content := GenerateDispatch("myworld", "sol")

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateDispatch should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != SkillDispatch {
		t.Errorf("frontmatter name = %q, want %q", name, SkillDispatch)
	}

	desc := extractField(fm, "description")
	if desc == "" {
		t.Error("frontmatter description should not be empty")
	}
}

func TestGenerateDispatchExpectedCommands(t *testing.T) {
	content := GenerateDispatch("myworld", "sol")

	for _, cmd := range []string{
		"sol writ create --world=myworld",
		"sol writ dep add",
		"sol cast <writ-id> --world=myworld",
		"sol writ ready --world=myworld",
		"sol agent list --world=myworld",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("dispatch skill should contain %q", cmd)
		}
	}
}

func TestGenerateDispatchKindGuidance(t *testing.T) {
	content := GenerateDispatch("myworld", "sol")

	if !strings.Contains(content, "--kind=code") {
		t.Error("dispatch skill should contain code kind guidance")
	}
	if !strings.Contains(content, "--kind=analysis") {
		t.Error("dispatch skill should contain analysis kind guidance")
	}
	if !strings.Contains(content, "One concern per writ") {
		t.Error("dispatch skill should contain one-concern guidance")
	}
	if !strings.Contains(content, "enough context") {
		t.Error("dispatch skill should contain context guidance")
	}
}

func TestGenerateDispatchTemplateSubstitution(t *testing.T) {
	content := GenerateDispatch("testworld", "/usr/bin/sol")

	if !strings.Contains(content, "testworld") {
		t.Error("dispatch skill should contain substituted world name")
	}
	if !strings.Contains(content, "/usr/bin/sol") {
		t.Error("dispatch skill should contain substituted sol binary path")
	}
	for _, placeholder := range []string{"{WORLD}", "{SOL_BINARY}", "§"} {
		if strings.Contains(content, placeholder) {
			t.Errorf("dispatch skill should not contain unreplaced placeholder %q", placeholder)
		}
	}
}

// --- sol-caravan tests ---

func TestGenerateCaravanProducesValidFrontmatter(t *testing.T) {
	content := GenerateCaravan("myworld", "sol")

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateCaravan should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != SkillCaravan {
		t.Errorf("frontmatter name = %q, want %q", name, SkillCaravan)
	}
}

func TestGenerateCaravanExpectedCommands(t *testing.T) {
	content := GenerateCaravan("myworld", "sol")

	for _, cmd := range []string{
		"sol caravan create",
		"sol caravan add",
		"sol caravan status",
		"sol caravan set-phase",
		"sol caravan commission",
		"sol caravan launch",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("caravan skill should contain %q", cmd)
		}
	}
}

func TestGenerateCaravanPhaseGating(t *testing.T) {
	content := GenerateCaravan("myworld", "sol")

	if !strings.Contains(content, "phase order") {
		t.Error("caravan skill should explain phase sequencing")
	}
	if !strings.Contains(content, "parallel") {
		t.Error("caravan skill should mention parallel execution within phases")
	}
	if !strings.Contains(content, "writ ready") {
		t.Error("caravan skill should mention ready query interaction")
	}
	if !strings.Contains(content, "phase gating") {
		t.Error("caravan skill should mention phase gating in ready query")
	}
}

func TestGenerateCaravanTemplateSubstitution(t *testing.T) {
	content := GenerateCaravan("prodworld", "/opt/sol")

	if !strings.Contains(content, "prodworld") {
		t.Error("caravan skill should contain substituted world name")
	}
	if !strings.Contains(content, "/opt/sol") {
		t.Error("caravan skill should contain substituted sol binary path")
	}
	for _, placeholder := range []string{"{WORLD}", "{SOL_BINARY}", "§"} {
		if strings.Contains(content, placeholder) {
			t.Errorf("caravan skill should not contain unreplaced placeholder %q", placeholder)
		}
	}
}

// --- sol-tether-mgmt tests ---

func TestGenerateTetherMgmtProducesValidFrontmatter(t *testing.T) {
	content := GenerateTetherMgmt("myworld", "Echo")

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateTetherMgmt should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != SkillTetherMgmt {
		t.Errorf("frontmatter name = %q, want %q", name, SkillTetherMgmt)
	}
}

func TestGenerateTetherMgmtExpectedCommands(t *testing.T) {
	content := GenerateTetherMgmt("myworld", "Echo")

	for _, cmd := range []string{
		"sol tether <writ-id> --agent=Echo --world=myworld",
		"sol untether <writ-id> --agent=Echo --world=myworld",
		"sol writ activate <writ-id> --agent=Echo --world=myworld",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("tether-mgmt skill should contain %q", cmd)
		}
	}
}

func TestGenerateTetherMgmtContent(t *testing.T) {
	content := GenerateTetherMgmt("myworld", "Echo")

	if !strings.Contains(content, ".tether/") {
		t.Error("tether-mgmt skill should describe the tether directory")
	}
	if !strings.Contains(content, "active") {
		t.Error("tether-mgmt skill should describe the active writ concept")
	}
	if !strings.Contains(content, "Cast") {
		t.Error("tether-mgmt skill should explain cast vs tether")
	}
}

func TestGenerateTetherMgmtTemplateSubstitution(t *testing.T) {
	content := GenerateTetherMgmt("devworld", "Spark")

	if !strings.Contains(content, "devworld") {
		t.Error("tether-mgmt skill should contain substituted world name")
	}
	if !strings.Contains(content, "Spark") {
		t.Error("tether-mgmt skill should contain substituted agent name")
	}
	for _, placeholder := range []string{"{WORLD}", "{AGENT}", "§"} {
		if strings.Contains(content, placeholder) {
			t.Errorf("tether-mgmt skill should not contain unreplaced placeholder %q", placeholder)
		}
	}
}

// --- sol-notify tests ---

func TestGenerateNotifyProducesValidFrontmatter(t *testing.T) {
	content := GenerateNotify("myworld")

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateNotify should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != SkillNotify {
		t.Errorf("frontmatter name = %q, want %q", name, SkillNotify)
	}
}

func TestGenerateNotifyNotificationTypes(t *testing.T) {
	content := GenerateNotify("myworld")

	for _, notifType := range []string{
		"AGENT_DONE",
		"MERGED",
		"MERGE_FAILED",
		"RECOVERY_NEEDED",
		"MR_READY",
		"FORGE_PAUSED",
		"FORGE_RESUMED",
	} {
		if !strings.Contains(content, notifType) {
			t.Errorf("notify skill should contain notification type %q", notifType)
		}
	}
}

func TestGenerateNotifyDeliveryMechanism(t *testing.T) {
	content := GenerateNotify("myworld")

	if !strings.Contains(content, "UserPromptSubmit") {
		t.Error("notify skill should describe the delivery mechanism")
	}
	if !strings.Contains(content, "[NOTIFICATION]") {
		t.Error("notify skill should describe the notification format")
	}
}

func TestGenerateNotifyResponsePatterns(t *testing.T) {
	content := GenerateNotify("myworld")

	if !strings.Contains(content, "caravan") {
		t.Error("notify skill AGENT_DONE should mention checking caravan")
	}
	if !strings.Contains(content, "dispatch") {
		t.Error("notify skill AGENT_DONE should mention dispatching next work")
	}
	if !strings.Contains(content, "brief") {
		t.Error("notify skill MERGED should mention updating brief")
	}
	if !strings.Contains(content, "unblocked") {
		t.Error("notify skill MERGED should mention checking unblocked items")
	}
	if !strings.Contains(content, "re-dispatch") {
		t.Error("notify skill MERGE_FAILED should mention re-dispatching")
	}
	if !strings.Contains(strings.ToLower(content), "escalate") {
		t.Error("notify skill MERGE_FAILED should mention escalation")
	}
	if !strings.Contains(content, "respawn") || !strings.Contains(content, "attempts") {
		t.Error("notify skill RECOVERY_NEEDED should mention respawn attempts")
	}
}

func TestGenerateNotifyTemplateSubstitution(t *testing.T) {
	content := GenerateNotify("testworld")

	for _, placeholder := range []string{"{WORLD}", "§"} {
		if strings.Contains(content, placeholder) {
			t.Errorf("notify skill should not contain unreplaced placeholder %q", placeholder)
		}
	}
}

// --- sol-status tests ---

func TestGenerateStatusProducesValidFrontmatter(t *testing.T) {
	content := GenerateStatus("myworld", "sol")

	fm, _, ok := parseFrontmatter(content)
	if !ok {
		t.Fatal("GenerateStatus should produce valid YAML frontmatter")
	}

	name := extractField(fm, "name")
	if name != SkillStatus {
		t.Errorf("frontmatter name = %q, want %q", name, SkillStatus)
	}
}

func TestGenerateStatusExpectedCommands(t *testing.T) {
	content := GenerateStatus("myworld", "sol")

	for _, cmd := range []string{
		"sol status\n",
		"sol status --world=myworld",
		"sol agent list --world=myworld",
		"sol writ ready --world=myworld",
		"sol world sync --world=myworld",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("status skill should contain %q", cmd)
		}
	}
}

func TestGenerateStatusSections(t *testing.T) {
	content := GenerateStatus("myworld", "sol")

	if !strings.Contains(content, "Sphere Overview") {
		t.Error("status skill should contain sphere overview section")
	}
	if !strings.Contains(content, "Per-World Detail") {
		t.Error("status skill should contain per-world detail section")
	}
}

func TestGenerateStatusTemplateSubstitution(t *testing.T) {
	content := GenerateStatus("stagingworld", "/home/sol/bin/sol")

	if !strings.Contains(content, "stagingworld") {
		t.Error("status skill should contain substituted world name")
	}
	if !strings.Contains(content, "/home/sol/bin/sol") {
		t.Error("status skill should contain substituted sol binary path")
	}
	for _, placeholder := range []string{"{WORLD}", "{SOL_BINARY}", "§"} {
		if strings.Contains(content, placeholder) {
			t.Errorf("status skill should not contain unreplaced placeholder %q", placeholder)
		}
	}
}

// --- Cross-skill validation (all generators) ---

func TestAllSkillsFrontmatterNameMatchesConstant(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{SkillDispatch, GenerateDispatch("w", "sol")},
		{SkillCaravan, GenerateCaravan("w", "sol")},
		{SkillTetherMgmt, GenerateTetherMgmt("w", "agent")},
		{SkillNotify, GenerateNotify("w")},
		{SkillStatus, GenerateStatus("w", "sol")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _, ok := parseFrontmatter(tt.content)
			if !ok {
				t.Errorf("%s: should have valid YAML frontmatter", tt.name)
				return
			}
			if got := extractField(fm, "name"); got != tt.name {
				t.Errorf("%s: frontmatter name = %q, want %q", tt.name, got, tt.name)
			}
		})
	}
}

func TestAllSkillsHaveDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{SkillDispatch, GenerateDispatch("w", "sol")},
		{SkillCaravan, GenerateCaravan("w", "sol")},
		{SkillTetherMgmt, GenerateTetherMgmt("w", "agent")},
		{SkillNotify, GenerateNotify("w")},
		{SkillStatus, GenerateStatus("w", "sol")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.content, "description:") {
				t.Errorf("%s: should have a description in frontmatter", tt.name)
			}
		})
	}
}

func TestAllSkillsNoUnreplacedPlaceholders(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{SkillDispatch, GenerateDispatch("w", "sol")},
		{SkillCaravan, GenerateCaravan("w", "sol")},
		{SkillTetherMgmt, GenerateTetherMgmt("w", "agent")},
		{SkillNotify, GenerateNotify("w")},
		{SkillStatus, GenerateStatus("w", "sol")},
	}

	placeholders := []string{"{WORLD}", "{SOL_BINARY}", "{AGENT}", "§"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, p := range placeholders {
				if strings.Contains(tt.content, p) {
					t.Errorf("%s: should not contain unreplaced placeholder %q", tt.name, p)
				}
			}
		})
	}
}

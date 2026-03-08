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

func TestInstallSkillAllThree(t *testing.T) {
	dir := t.TempDir()

	skills := map[string]string{
		"sol-resolve":   GenerateResolve(ResolveContext{World: "w", Agent: "a"}),
		"sol-workflow":  GenerateWorkflow(WorkflowContext{World: "w", Agent: "a"}),
		"sol-forge-ops": GenerateForgeOps(ForgeOpsContext{World: "w", TargetBranch: "main"}),
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

package protocol_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nevinsm/sol/internal/protocol"
)

func TestClaudeMDWithWorkflow(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: true,
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Lean persona should contain workflow protocol (behavioral).
	if !strings.Contains(content, "Read your current workflow step") {
		t.Error("CLAUDE.md should contain workflow protocol instructions")
	}
	if !strings.Contains(content, "Advance to the next step") {
		t.Error("CLAUDE.md should contain workflow advance instruction")
	}
	if !strings.Contains(content, "sol resolve") {
		t.Error("CLAUDE.md should contain sol resolve reference")
	}
}

func TestGuidedInitClaudeMD(t *testing.T) {
	ctx := protocol.GuidedInitClaudeMDContext{
		SOLHome:   "/tmp/sol-test",
		SolBinary: "/usr/local/bin/sol",
	}

	content := protocol.GenerateGuidedInitClaudeMD(ctx)

	if !strings.Contains(content, "World name") {
		t.Error("CLAUDE.md should contain 'World name'")
	}
	if !strings.Contains(content, "Source repository") {
		t.Error("CLAUDE.md should contain 'Source repository'")
	}
	if !strings.Contains(content, "Setup Command") {
		t.Error("CLAUDE.md should contain 'Setup Command'")
	}
	if !strings.Contains(content, "/tmp/sol-test") {
		t.Error("CLAUDE.md should contain the SOL_HOME path")
	}
	if !strings.Contains(content, "/usr/local/bin/sol") {
		t.Error("CLAUDE.md should contain the sol binary path")
	}
	if !strings.Contains(content, "init --name=") {
		t.Error("CLAUDE.md should contain 'init --name=' command template")
	}
	if !strings.Contains(content, "--skip-checks") {
		t.Error("CLAUDE.md should contain '--skip-checks' in the setup command")
	}
}

func TestGenerateGovernorClaudeMD(t *testing.T) {
	ctx := protocol.GovernorClaudeMDContext{
		World:     "myworld",
		SolBinary: "sol",
		MirrorDir: "../repo",
	}

	content := protocol.GenerateGovernorClaudeMD(ctx)

	// Verify world name appears.
	if !strings.Contains(content, "myworld") {
		t.Error("CLAUDE.md should contain world name")
	}

	// Verify managed repo reference.
	if !strings.Contains(content, "../repo/") {
		t.Error("CLAUDE.md should contain managed repo directory reference")
	}

	// Verify world sync command (kept in codebase research section).
	if !strings.Contains(content, "sol world sync --world=myworld") {
		t.Error("CLAUDE.md should contain 'sol world sync --world=myworld'")
	}

	// Lean persona should NOT contain dispatch flow commands (moved to skills).
	for _, cmd := range []string{
		"sol store create",
		"sol cast",
		"sol caravan create",
	} {
		if strings.Contains(content, cmd) {
			t.Errorf("lean governor CLAUDE.md should not contain dispatch command %q (moved to skills)", cmd)
		}
	}

	// Lean persona should NOT contain notification handling section (moved to skills).
	if strings.Contains(content, "## Notification Handling") {
		t.Error("lean governor CLAUDE.md should not contain Notification Handling section (moved to skills)")
	}

	// Should NOT contain CLI reference link (replaced by skills).
	if strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("lean governor CLAUDE.md should not reference sol-cli-reference.md")
	}

	// Verify brief instructions still present.
	if !strings.Contains(content, ".brief/memory.md") {
		t.Error("CLAUDE.md should contain brief path reference")
	}
	if !strings.Contains(content, "200 lines") {
		t.Error("CLAUDE.md should contain brief size guidance")
	}

	// Verify world summary format still present.
	if !strings.Contains(content, ".brief/world-summary.md") {
		t.Error("CLAUDE.md should contain world summary path")
	}
	if !strings.Contains(content, "## Project") {
		t.Error("CLAUDE.md should contain world summary format sections")
	}
	if !strings.Contains(content, "## Architecture") {
		t.Error("CLAUDE.md should contain world summary format sections")
	}

	// Verify identity section.
	if !strings.Contains(content, "work coordinator") {
		t.Error("CLAUDE.md should contain governor identity")
	}

	// Verify guidelines.
	if !strings.Contains(content, "You coordinate") {
		t.Error("CLAUDE.md should contain coordination guideline")
	}
}

func TestInstallGovernorClaudeMD(t *testing.T) {
	govDir := t.TempDir()

	ctx := protocol.GovernorClaudeMDContext{
		World:     "testworld",
		SolBinary: "sol",
		MirrorDir: "../repo",
	}

	if err := protocol.InstallGovernorClaudeMD(govDir, ctx); err != nil {
		t.Fatalf("InstallGovernorClaudeMD failed: %v", err)
	}

	// Verify CLAUDE.local.md written.
	path := filepath.Join(govDir, "CLAUDE.local.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.local.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "testworld") {
		t.Error("installed CLAUDE.local.md should contain world name")
	}
	if !strings.Contains(content, "Governor") {
		t.Error("installed CLAUDE.local.md should contain 'Governor'")
	}

	// Verify skills installed.
	skills := protocol.RoleSkills("governor")
	for _, name := range skills {
		skillPath := filepath.Join(govDir, ".claude", "skills", name, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("governor skill %q should be installed: %v", name, err)
		}
	}
}

func TestEnvoyClaudeMDAutoMemoryProhibition(t *testing.T) {
	ctx := protocol.EnvoyClaudeMDContext{
		AgentName: "Echo",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := protocol.GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "DO NOT") || !strings.Contains(content, "auto-memory") {
		t.Error("envoy CLAUDE.md should contain auto-memory prohibition")
	}
	if !strings.Contains(content, ".brief/memory.md") {
		t.Error("envoy CLAUDE.md should reference .brief/memory.md")
	}
}

func TestGovernorClaudeMDAutoMemoryProhibition(t *testing.T) {
	ctx := protocol.GovernorClaudeMDContext{
		World:     "myworld",
		SolBinary: "sol",
		MirrorDir: "../repo",
	}

	content := protocol.GenerateGovernorClaudeMD(ctx)

	if !strings.Contains(content, "DO NOT") || !strings.Contains(content, "auto-memory") {
		t.Error("governor CLAUDE.md should contain auto-memory prohibition")
	}
	if !strings.Contains(content, ".brief/memory.md") {
		t.Error("governor CLAUDE.md should reference .brief/memory.md")
	}
}

func TestClaudeMDWithoutWorkflow(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: false,
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Should not contain workflow step instructions.
	if strings.Contains(content, "current workflow step") {
		t.Error("CLAUDE.md should not contain workflow step instructions without workflow")
	}
	if !strings.Contains(content, "sol resolve") {
		t.Error("CLAUDE.md should contain 'sol resolve'")
	}
	// Should NOT reference CLI reference (replaced by skills).
	if strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("lean CLAUDE.md should not reference sol-cli-reference.md")
	}
}

func TestEnvoyClaudeMDLean(t *testing.T) {
	ctx := protocol.EnvoyClaudeMDContext{
		AgentName: "Echo",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := protocol.GenerateEnvoyClaudeMD(ctx)

	// Should NOT contain CLI reference link (replaced by skills).
	if strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("lean envoy CLAUDE.md should not reference sol-cli-reference.md")
	}

	// Should contain key behavioral/identity elements.
	for _, check := range []string{
		"Envoy: Echo (world: myworld)",
		"Echo",
		"myworld",
		".brief/memory.md",
		"200 lines",
		"Brief Maintenance",
		"human-supervised",
		"Three Modes",
		"Tethered work",
		"Self-service",
		"Freeform",
		"Submitting Work",
		"Never use `git push` alone",
		"Never push directly or bypass forge",
	} {
		if !strings.Contains(content, check) {
			t.Errorf("lean envoy CLAUDE.md should contain %q", check)
		}
	}

	// Should NOT contain detailed tether/resolve command syntax (moved to skills).
	if strings.Contains(content, "sol resolve --world=myworld --agent=Echo") {
		t.Error("lean envoy CLAUDE.md should not contain detailed resolve syntax (moved to skills)")
	}
	if strings.Contains(content, "sol store create --world=myworld") {
		t.Error("lean envoy CLAUDE.md should not contain store create syntax (moved to skills)")
	}
	if strings.Contains(content, "sol tether") {
		t.Error("lean envoy CLAUDE.md should not contain tether command syntax (moved to skills)")
	}
}

func TestClaudeMDWarningSectionPresent(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "## Warning") {
		t.Error("CLAUDE.md should contain Warning section")
	}
	if !strings.Contains(content, "tether is orphaned") {
		t.Error("Warning should explain orphaned tether consequence")
	}
	if !strings.Contains(content, "sol escalate") {
		t.Error("Warning should mention sol escalate")
	}
	if !strings.Contains(content, "do not silently exit") {
		t.Error("Warning should warn against silently exiting")
	}
}

func TestClaudeMDApproachSectionPresent(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "## Approach") {
		t.Error("CLAUDE.md should contain Approach section")
	}
	if !strings.Contains(content, "Read existing code") {
		t.Error("Approach should instruct reading existing code")
	}
	if !strings.Contains(content, "Follow existing patterns") {
		t.Error("Approach should instruct following existing patterns")
	}
	if !strings.Contains(content, "focused, minimal changes") {
		t.Error("Approach should instruct minimal changes")
	}
}

func TestClaudeMDCompletionChecklistPresent(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "## Completion Checklist") {
		t.Error("CLAUDE.md should contain Completion Checklist section")
	}
	if !strings.Contains(content, "MANDATORY FINAL STEP") {
		t.Error("Completion Checklist should mark sol resolve as mandatory")
	}
	if !strings.Contains(content, "Stage and commit") {
		t.Error("Completion Checklist should include commit step")
	}
}

func TestClaudeMDQualityGatesDefault(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "Run the project test suite before resolving.") {
		t.Error("CLAUDE.md should contain default quality gate instruction when none configured")
	}
}

func TestClaudeMDQualityGatesConfigured(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:    "TestBot",
		World:        "ember",
		WritID:       "sol-12345678",
		Title:        "Test task",
		Description:  "Test description",
		QualityGates: []string{"make test", "make vet"},
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "`make test`") {
		t.Error("CLAUDE.md should contain configured quality gate 'make test'")
	}
	if !strings.Contains(content, "`make vet`") {
		t.Error("CLAUDE.md should contain configured quality gate 'make vet'")
	}
	if strings.Contains(content, "Run the project test suite before resolving.") {
		t.Error("CLAUDE.md should not contain default instruction when gates are configured")
	}
}

func TestClaudeMDSectionOrder(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:    "TestBot",
		World:        "ember",
		WritID:       "sol-12345678",
		Title:        "Test task",
		Description:  "Test description",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Verify section ordering: Warning < Assignment < Approach < Checklist < Protocol
	warningIdx := strings.Index(content, "## Warning")
	assignmentIdx := strings.Index(content, "## Your Assignment")
	approachIdx := strings.Index(content, "## Approach")
	checklistIdx := strings.Index(content, "## Completion Checklist")
	protocolIdx := strings.Index(content, "## Protocol")

	if warningIdx >= assignmentIdx {
		t.Error("Warning section should come before Assignment")
	}
	if assignmentIdx >= approachIdx {
		t.Error("Assignment section should come before Approach")
	}
	if approachIdx >= checklistIdx {
		t.Error("Approach section should come before Completion Checklist")
	}
	if checklistIdx >= protocolIdx {
		t.Error("Completion Checklist section should come before Protocol")
	}
}

func TestClaudeMDCodeKindWithOutputDir(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:    "TestBot",
		World:        "ember",
		WritID:       "sol-12345678",
		Title:        "Test task",
		Description:  "Test description",
		Kind:         "code",
		OutputDir:    "/home/sol/ember/output/sol-12345678",
		QualityGates: []string{"make build && make test"},
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "auxiliary output") {
		t.Error("code writ should mention output dir for auxiliary output")
	}
	if !strings.Contains(content, "/home/sol/ember/output/sol-12345678") {
		t.Error("code writ should contain the output directory path")
	}
	if !strings.Contains(content, "make build && make test") {
		t.Error("code writ should contain quality gates")
	}
	if !strings.Contains(content, "Commit early and often") {
		t.Error("code writ should have git-based session resilience advice")
	}
	if !strings.Contains(content, "pushes your branch") {
		// Note: resolve description removed from lean persona — no longer check.
	}
}

func TestClaudeMDDefaultKindIsCode(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		// Kind is empty — should default to code behavior.
		OutputDir: "/home/sol/ember/output/sol-12345678",
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "auxiliary output") {
		t.Error("empty kind should default to code behavior with auxiliary output mention")
	}
	if !strings.Contains(content, "Commit early and often") {
		t.Error("empty kind should default to code-style session resilience")
	}
}

func TestClaudeMDAnalysisKind(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:    "TestBot",
		World:        "ember",
		WritID:       "sol-12345678",
		Title:        "Analyze metrics",
		Description:  "Analyze the system metrics",
		Kind:         "analysis",
		OutputDir:    "/home/sol/ember/output/sol-12345678",
		QualityGates: []string{"make build && make test"},
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "primary output surface") {
		t.Error("analysis writ should describe output dir as primary output surface")
	}
	if !strings.Contains(content, "No branch or MR is created") {
		t.Error("analysis writ should state no branch or MR is created")
	}
	if strings.Contains(content, "make build && make test") {
		t.Error("analysis writ should not contain quality gates")
	}
	if !strings.Contains(content, "Review your output") {
		t.Error("analysis writ completion checklist should mention reviewing output")
	}
	if strings.Contains(content, "Stage and commit") {
		t.Error("analysis writ completion checklist should not mention staging and committing")
	}
	if strings.Contains(content, "Commit early and often") {
		t.Error("analysis writ should not have git-based session resilience advice")
	}
	if !strings.Contains(content, "Write findings to your output directory early") {
		t.Error("analysis writ should have output-dir-based session resilience advice")
	}
}

func TestClaudeMDDirectDepsPopulated(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Follow-up analysis",
		Description: "Build on upstream analysis",
		Kind:        "analysis",
		OutputDir:   "/home/sol/ember/output/sol-12345678",
		DirectDeps: []protocol.DepOutput{
			{
				WritID:    "sol-aabbccdd",
				Title:     "Gather metrics",
				Kind:      "analysis",
				OutputDir: "/home/sol/ember/output/sol-aabbccdd",
			},
			{
				WritID:    "sol-11223344",
				Title:     "Build adapter",
				Kind:      "code",
				OutputDir: "/home/sol/ember/output/sol-11223344",
			},
		},
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "## Direct Dependencies") {
		t.Error("should contain Direct Dependencies section when deps are populated")
	}
	if !strings.Contains(content, "Read them for context before starting work") {
		t.Error("should instruct agent to read dependency output")
	}
	if !strings.Contains(content, "Gather metrics") {
		t.Error("should list first dependency title")
	}
	if !strings.Contains(content, "sol-aabbccdd") {
		t.Error("should list first dependency writ ID")
	}
	if !strings.Contains(content, "/home/sol/ember/output/sol-aabbccdd") {
		t.Error("should list first dependency output dir")
	}
	if !strings.Contains(content, "Build adapter") {
		t.Error("should list second dependency title")
	}
}

func TestClaudeMDDirectDepsEmpty(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
	}

	content := protocol.GenerateClaudeMD(ctx)

	if strings.Contains(content, "## Direct Dependencies") {
		t.Error("should not contain Direct Dependencies section when deps are empty")
	}
}

// TestClaudeMDNoCommandSyntax verifies the lean persona does not contain
// detailed command syntax that belongs in skills.
func TestClaudeMDNoCommandSyntax(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WritID:      "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Should not contain command sections moved to skills.
	if strings.Contains(content, "## Commands\n") {
		t.Error("lean CLAUDE.md should not have a Commands section")
	}
	if strings.Contains(content, "## Session Management") {
		t.Error("lean CLAUDE.md should not have Session Management section")
	}
	if strings.Contains(content, "## Memories") {
		t.Error("lean CLAUDE.md should not have Memories section")
	}
	if strings.Contains(content, "sol handoff") {
		t.Error("lean CLAUDE.md should not contain sol handoff (moved to skills)")
	}
	if strings.Contains(content, "sol remember") {
		t.Error("lean CLAUDE.md should not contain sol remember (moved to skills)")
	}
	if strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("lean CLAUDE.md should not reference sol-cli-reference.md")
	}
}

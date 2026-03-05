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
		WorkItemID:  "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: true,
	}

	content := protocol.GenerateClaudeMD(ctx)

	if !strings.Contains(content, "sol workflow current") {
		t.Error("CLAUDE.md should contain 'sol workflow current'")
	}
	if !strings.Contains(content, "sol workflow advance") {
		t.Error("CLAUDE.md should contain 'sol workflow advance'")
	}
	if !strings.Contains(content, "sol workflow status") {
		t.Error("CLAUDE.md should contain 'sol workflow status'")
	}
	if !strings.Contains(content, "Repeat from step 1") {
		t.Error("CLAUDE.md should contain workflow protocol")
	}
}

func TestGuidedInitClaudeMD(t *testing.T) {
	ctx := protocol.GuidedInitClaudeMDContext{
		SOLHome:   "/tmp/sol-test",
		SolBinary: "/usr/local/bin/sol",
	}

	content := protocol.GenerateGuidedInitClaudeMD(ctx)

	// Verify it contains key sections.
	if !strings.Contains(content, "World name") {
		t.Error("CLAUDE.md should contain 'World name'")
	}
	if !strings.Contains(content, "Source repository") {
		t.Error("CLAUDE.md should contain 'Source repository'")
	}
	if !strings.Contains(content, "Setup Command") {
		t.Error("CLAUDE.md should contain 'Setup Command'")
	}

	// Verify it includes the SOL_HOME path.
	if !strings.Contains(content, "/tmp/sol-test") {
		t.Error("CLAUDE.md should contain the SOL_HOME path")
	}

	// Verify it includes the sol binary path.
	if !strings.Contains(content, "/usr/local/bin/sol") {
		t.Error("CLAUDE.md should contain the sol binary path")
	}

	// Verify it contains the init command template.
	if !strings.Contains(content, "init --name=") {
		t.Error("CLAUDE.md should contain 'init --name=' command template")
	}

	// Verify --skip-checks is included in the command.
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

	// Verify sol world sync command.
	if !strings.Contains(content, "sol world sync --world=myworld") {
		t.Error("CLAUDE.md should contain 'sol world sync --world=myworld'")
	}

	// Verify inline dispatch flow commands still present.
	for _, cmd := range []string{
		"sol store create",
		"sol cast",
		"sol caravan create",
		"sol caravan status",
		"sol status --world=myworld",
		"sol agent list",
		"sol escalate",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("CLAUDE.md should contain %q", cmd)
		}
	}

	// Verify CLI reference line replaces hardcoded command block.
	if !strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("CLAUDE.md should contain CLI reference")
	}

	// Verify brief instructions.
	if !strings.Contains(content, ".brief/memory.md") {
		t.Error("CLAUDE.md should contain brief path reference")
	}
	if !strings.Contains(content, "200 lines") {
		t.Error("CLAUDE.md should contain brief size guidance")
	}

	// Verify world summary format.
	if !strings.Contains(content, ".brief/world-summary.md") {
		t.Error("CLAUDE.md should contain world summary path")
	}
	if !strings.Contains(content, "## Project") {
		t.Error("CLAUDE.md should contain world summary format sections")
	}
	if !strings.Contains(content, "## Architecture") {
		t.Error("CLAUDE.md should contain world summary format sections")
	}
	if !strings.Contains(content, "## Priorities") {
		t.Error("CLAUDE.md should contain world summary format sections")
	}
	if !strings.Contains(content, "## Constraints") {
		t.Error("CLAUDE.md should contain world summary format sections")
	}

	// Verify identity section.
	if !strings.Contains(content, "work coordinator") {
		t.Error("CLAUDE.md should contain governor identity")
	}

	// Verify notification handling section.
	for _, notifType := range []string{
		"AGENT_DONE",
		"MERGED",
		"MERGE_FAILED",
		"RECOVERY_NEEDED",
	} {
		if !strings.Contains(content, "**"+notifType+"**") {
			t.Errorf("CLAUDE.md should contain notification type %q", notifType)
		}
	}
	if !strings.Contains(content, "## Notification Handling") {
		t.Error("CLAUDE.md should contain Notification Handling section")
	}
	if !strings.Contains(content, "[NOTIFICATION]") {
		t.Error("CLAUDE.md should describe notification format")
	}

	// Verify guidelines.
	if !strings.Contains(content, "You coordinate") {
		t.Error("CLAUDE.md should contain coordination guideline")
	}

	// Verify no wrong cast syntax.
	for _, bad := range []string{
		"cast --work-item=",
	} {
		if strings.Contains(content, bad) {
			t.Errorf("GenerateGovernorClaudeMD should not contain %q", bad)
		}
	}

	// Verify correct cast syntax.
	if !strings.Contains(content, "sol cast <item-id> --world=myworld") {
		t.Error("GenerateGovernorClaudeMD should contain correct cast syntax")
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

	// Verify file written.
	path := filepath.Join(govDir, ".claude", "CLAUDE.local.md")
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
		WorkItemID:  "sol-12345678",
		Title:       "Test task",
		Description: "Test description",
		HasWorkflow: false,
	}

	content := protocol.GenerateClaudeMD(ctx)

	if strings.Contains(content, "sol workflow current") {
		t.Error("CLAUDE.md should not contain workflow commands without workflow")
	}
	if !strings.Contains(content, "sol resolve") {
		t.Error("CLAUDE.md should contain 'sol resolve'")
	}
	if !strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("CLAUDE.md should contain CLI reference")
	}
}

func TestEnvoyClaudeMDCLIReference(t *testing.T) {
	ctx := protocol.EnvoyClaudeMDContext{
		AgentName: "Echo",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := protocol.GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("envoy CLAUDE.md should contain CLI reference")
	}
	// Key workflow commands should still be inline.
	for _, cmd := range []string{
		"sol resolve",
		"sol store create",
		"sol escalate",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("envoy CLAUDE.md should contain inline command %q", cmd)
		}
	}
}

func TestForgeClaudeMDCLIReference(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	if !strings.Contains(content, ".claude/sol-cli-reference.md") {
		t.Error("forge CLAUDE.md should contain CLI reference")
	}
	// Patrol loop commands should still be inline.
	if !strings.Contains(content, "sol forge ready") {
		t.Error("forge CLAUDE.md should contain patrol loop commands inline")
	}
	// Squash merge command should be present.
	if !strings.Contains(content, "sol forge merge") {
		t.Error("forge CLAUDE.md should contain sol forge merge command")
	}
	// Old rebase instructions should be gone.
	if strings.Contains(content, "git rebase") {
		t.Error("forge CLAUDE.md should not contain git rebase instructions")
	}
	if strings.Contains(content, "Conflict Judgment Framework") {
		t.Error("forge CLAUDE.md should not contain Conflict Judgment Framework")
	}
	if strings.Contains(content, "Sequential Rebase Rule") {
		t.Error("forge CLAUDE.md should not contain Sequential Rebase Rule")
	}
}

func TestForgeClaudeMDTheoryOfOperation(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	if !strings.Contains(content, "## Theory of Operation") {
		t.Error("forge CLAUDE.md should contain Theory of Operation section")
	}
	if !strings.Contains(content, "merge processor for world myworld") {
		t.Error("forge CLAUDE.md should describe mechanical role with world name")
	}
	if !strings.Contains(content, "claim → merge → handle result → loop") {
		t.Error("forge CLAUDE.md should describe the mechanical loop")
	}
	if !strings.Contains(content, "You never touch git directly") {
		t.Error("forge CLAUDE.md should state never touch git directly")
	}
	if !strings.Contains(content, "patrol loop is your ONLY activity") {
		t.Error("forge CLAUDE.md should state patrol loop is only activity")
	}
}

func TestForgeClaudeMDForbiddenExpanded(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	if !strings.Contains(content, "## FORBIDDEN") {
		t.Error("forge CLAUDE.md should contain FORBIDDEN section")
	}
	// Each FORBIDDEN item should have consequences.
	for _, sub := range []string{
		"FORBIDDEN: Running git commands directly",
		"corrupt the forge worktree state",
		"Enforced by PreToolUse hooks",
		"FORBIDDEN: Running `go test`",
		"FORBIDDEN: Reading outpost code",
		"FORBIDDEN: Extended analysis of test output",
		"FORBIDDEN: Writing or modifying application code",
		"FORBIDDEN: Using plan mode",
	} {
		if !strings.Contains(content, sub) {
			t.Errorf("forge CLAUDE.md FORBIDDEN section should contain %q", sub)
		}
	}
}

func TestForgeClaudeMDStepBanners(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	for _, banner := range []string{
		"STEP 1/6: UNBLOCK",
		"STEP 2/6: SCAN QUEUE",
		"STEP 3/6: CLAIM",
		"STEP 4/6: MERGE",
		"STEP 5/6: MARK MERGED",
		"STEP 6/6: LOOP",
	} {
		if !strings.Contains(content, banner) {
			t.Errorf("forge CLAUDE.md should contain step banner %q", banner)
		}
	}
}

func TestForgeClaudeMDVerificationGates(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	gates := []string{
		"You CANNOT proceed to Step 3",
		"You CANNOT proceed to Step 4",
		"You CANNOT proceed to Step 5",
	}
	for _, gate := range gates {
		if !strings.Contains(content, gate) {
			t.Errorf("forge CLAUDE.md should contain verification gate %q", gate)
		}
	}
}

func TestForgeClaudeMDErrorHandlingProtocol(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	if !strings.Contains(content, "## Error Handling Protocol") {
		t.Error("forge CLAUDE.md should contain Error Handling Protocol section")
	}
	if !strings.Contains(content, "Errors are reported, never investigated") {
		t.Error("forge CLAUDE.md should state errors are never investigated")
	}
	// Table should contain world-specific commands.
	if !strings.Contains(content, "sol forge mark-merged --world=myworld") {
		t.Error("error handling table should contain world-specific mark-merged command")
	}
}

func TestForgeClaudeMDWaitBehavior(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	if !strings.Contains(content, "## Wait Behavior") {
		t.Error("forge CLAUDE.md should contain Wait Behavior section")
	}
	for _, sub := range []string{
		"sol forge await --world=myworld --timeout=30",
		"Do NOT investigate why the queue is empty",
		"Do NOT explore the codebase while waiting",
		"Your ONLY activity during idle time is waiting",
	} {
		if !strings.Contains(content, sub) {
			t.Errorf("forge CLAUDE.md Wait Behavior should contain %q", sub)
		}
	}
}

func TestForgeClaudeMDCommandQuickReference(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "myworld",
		TargetBranch: "main",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	if !strings.Contains(content, "## Command Quick-Reference") {
		t.Error("forge CLAUDE.md should contain Command Quick-Reference section")
	}
	// Table should have correct commands with world substitution.
	for _, cmd := range []string{
		"sol forge check-unblocked --world=myworld",
		"sol forge ready --world=myworld --json",
		"sol forge claim --world=myworld --json",
		"sol forge merge --world=myworld",
		"sol forge mark-merged --world=myworld",
		"sol forge mark-failed --world=myworld",
		"sol forge create-resolution --world=myworld",
		"sol forge release --world=myworld",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("forge CLAUDE.md quick-reference should contain %q", cmd)
		}
	}
	// Table should have "Common mistake" column with strikethrough git commands.
	for _, bad := range []string{
		"~~git fetch~~",
		"~~git branch -r~~",
		"~~git merge~~",
		"~~git push origin main~~",
	} {
		if !strings.Contains(content, bad) {
			t.Errorf("forge CLAUDE.md quick-reference should contain common mistake %q", bad)
		}
	}
}

func TestForgeClaudeMDWorldSubstitution(t *testing.T) {
	ctx := protocol.ForgeClaudeMDContext{
		World:        "testworld",
		TargetBranch: "develop",
		QualityGates: []string{"make vet", "make test"},
	}

	content := protocol.GenerateForgeClaudeMD(ctx)

	// World name should appear throughout.
	if !strings.Contains(content, "world: testworld") {
		t.Error("forge CLAUDE.md should contain world name in title")
	}
	if !strings.Contains(content, "sol forge merge --world=testworld") {
		t.Error("forge CLAUDE.md should use correct world in merge command")
	}

	// Target branch should appear.
	if !strings.Contains(content, "develop") {
		t.Error("forge CLAUDE.md should contain target branch")
	}

	// Quality gates should appear.
	if !strings.Contains(content, "`make vet`") {
		t.Error("forge CLAUDE.md should contain quality gate 'make vet'")
	}
	if !strings.Contains(content, "`make test`") {
		t.Error("forge CLAUDE.md should contain quality gate 'make test'")
	}

	// Placeholder tokens should NOT remain.
	if strings.Contains(content, "{WORLD}") {
		t.Error("forge CLAUDE.md should not contain unreplaced {WORLD} placeholder")
	}
	if strings.Contains(content, "{TARGET_BRANCH}") {
		t.Error("forge CLAUDE.md should not contain unreplaced {TARGET_BRANCH} placeholder")
	}
	if strings.Contains(content, "{QUALITY_GATES}") {
		t.Error("forge CLAUDE.md should not contain unreplaced {QUALITY_GATES} placeholder")
	}
	if strings.Contains(content, "§") {
		t.Error("forge CLAUDE.md should not contain unreplaced § backtick placeholder")
	}
}

func TestClaudeMDWarningSectionPresent(t *testing.T) {
	ctx := protocol.ClaudeMDContext{
		AgentName:   "TestBot",
		World:       "ember",
		WorkItemID:  "sol-12345678",
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
		WorkItemID:  "sol-12345678",
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
		WorkItemID:  "sol-12345678",
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
		WorkItemID:  "sol-12345678",
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
		WorkItemID:   "sol-12345678",
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
		WorkItemID:   "sol-12345678",
		Title:        "Test task",
		Description:  "Test description",
		QualityGates: []string{"make test"},
	}

	content := protocol.GenerateClaudeMD(ctx)

	// Verify section ordering: Warning < Assignment < Approach < Commands < Checklist < Protocol
	warningIdx := strings.Index(content, "## Warning")
	assignmentIdx := strings.Index(content, "## Your Assignment")
	approachIdx := strings.Index(content, "## Approach")
	commandsIdx := strings.Index(content, "## Commands")
	checklistIdx := strings.Index(content, "## Completion Checklist")
	protocolIdx := strings.Index(content, "## Protocol")

	if warningIdx >= assignmentIdx {
		t.Error("Warning section should come before Assignment")
	}
	if assignmentIdx >= approachIdx {
		t.Error("Assignment section should come before Approach")
	}
	if approachIdx >= commandsIdx {
		t.Error("Approach section should come before Commands")
	}
	if commandsIdx >= checklistIdx {
		t.Error("Commands section should come before Completion Checklist")
	}
	if checklistIdx >= protocolIdx {
		t.Error("Completion Checklist section should come before Protocol")
	}
}

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
		MirrorDir: "mirror",
	}

	content := protocol.GenerateGovernorClaudeMD(ctx)

	// Verify world name appears.
	if !strings.Contains(content, "myworld") {
		t.Error("CLAUDE.md should contain world name")
	}

	// Verify mirror reference.
	if !strings.Contains(content, "mirror/") {
		t.Error("CLAUDE.md should contain mirror directory reference")
	}

	// Verify sol CLI commands.
	for _, cmd := range []string{
		"sol store create",
		"sol store list",
		"sol cast",
		"sol caravan create",
		"sol caravan add",
		"sol caravan check",
		"sol caravan status",
		"sol caravan launch",
		"sol status myworld",
		"sol agent list",
		"sol escalate",
	} {
		if !strings.Contains(content, cmd) {
			t.Errorf("CLAUDE.md should contain %q", cmd)
		}
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
		MirrorDir: "mirror",
	}

	if err := protocol.InstallGovernorClaudeMD(govDir, ctx); err != nil {
		t.Fatalf("InstallGovernorClaudeMD failed: %v", err)
	}

	// Verify file written.
	path := filepath.Join(govDir, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "testworld") {
		t.Error("installed CLAUDE.md should contain world name")
	}
	if !strings.Contains(content, "Governor") {
		t.Error("installed CLAUDE.md should contain 'Governor'")
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
}

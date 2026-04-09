package protocol

import (
	"strings"
	"testing"
)

func TestGenerateClaudeMD(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file with project info",
	}

	content := GenerateClaudeMD(ctx)

	checks := []string{
		"Outpost Agent: Toast (world: myworld)",
		"sol-a1b2c3d4",
		"Add a README",
		"Create a README.md file with project info",
		"sol resolve",
		"sol escalate",
		"isolated git worktree",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("GenerateClaudeMD missing %q", check)
		}
	}
}

func TestGenerateClaudeMDWithModelTier(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
		Title:       "Test task",
		Description: "Testing model tier",
		ModelTier:   "opus",
	}

	content := GenerateClaudeMD(ctx)

	if !strings.Contains(content, "## Model") {
		t.Error("GenerateClaudeMD missing Model section header")
	}
	if !strings.Contains(content, "model: opus") {
		t.Error("GenerateClaudeMD missing model value")
	}
}

func TestGenerateClaudeMDWithoutModelTier(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
		Title:       "Test task",
		Description: "Testing no model tier",
	}

	content := GenerateClaudeMD(ctx)

	if strings.Contains(content, "## Model") {
		t.Error("GenerateClaudeMD should not contain Model section when ModelTier is empty")
	}
}

func TestGenerateEnvoyClaudeMD(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	checks := []string{
		"Envoy: scout (world: myworld)",
		"scout",
		"myworld",
		"MEMORY.md",
		"Memory Maintenance",
		"human-supervised",
		"Three Modes",
		"Tethered work",
		"Self-service",
		"Freeform",
		"Submitting Work",
		"All code changes MUST go through",
		"Never use `git push` alone",
		"Never push directly or bypass forge",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("GenerateEnvoyClaudeMD missing %q", check)
		}
	}

	// Verify no wrong command names.
	for _, bad := range []string{
		"store create-item",
		"store list-items",
		"caravan add-items",
	} {
		if strings.Contains(content, bad) {
			t.Errorf("GenerateEnvoyClaudeMD should not contain %q", bad)
		}
	}

	// Verify tether check uses status command, not outpost path.
	if strings.Contains(content, "outposts") {
		t.Error("GenerateEnvoyClaudeMD should not reference outposts directory")
	}
}

func TestGenerateEnvoyClaudeMDDefaultBinary(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		// SolBinary intentionally empty
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "sol resolve") {
		t.Error("GenerateEnvoyClaudeMD should default to 'sol' binary")
	}
}

func TestGenerateEnvoyClaudeMDNoPersonaSection(t *testing.T) {
	// Persona content is now delivered via system prompt, not CLAUDE.local.md.
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if strings.Contains(content, "## Persona") {
		t.Error("GenerateEnvoyClaudeMD should not contain Persona section (persona is now in system prompt)")
	}
}

func TestGenerateEnvoyClaudeMDIdentitySections(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "scout",
		World:     "myworld",
		SolBinary: "sol",
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "## Identity") {
		t.Error("GenerateEnvoyClaudeMD missing Identity section")
	}
	if !strings.Contains(content, "scout") {
		t.Error("GenerateEnvoyClaudeMD missing agent name")
	}
}

func TestGenerateEnvoyClaudeMDMultiWritActive(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "Meridian",
		World:     "myworld",
		SolBinary: "sol",
		WritContext: WritContext{
			TetheredWrits: []WritSummary{
				{ID: "sol-aaa1", Title: "Task A", Kind: "code", Status: "tethered"},
				{ID: "sol-bbb2", Title: "Task B", Kind: "analysis", Status: "tethered"},
				{ID: "sol-ccc3", Title: "Task C", Kind: "code", Status: "tethered"},
			},
			ActiveWritID: "sol-bbb2",
			ActiveTitle:  "Task B",
			ActiveDesc:   "Analyze the system",
			ActiveKind:   "analysis",
			ActiveOutput: "/tmp/output/sol-bbb2",
		},
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "## Active Writ") {
		t.Error("missing Active Writ section")
	}
	if !strings.Contains(content, "sol-bbb2") {
		t.Error("missing active writ ID")
	}
	if !strings.Contains(content, "Task B") {
		t.Error("missing active writ title")
	}
	if !strings.Contains(content, "Analyze the system") {
		t.Error("missing active writ description")
	}
	if !strings.Contains(content, "## Background Writs") {
		t.Error("missing Background Writs section")
	}
	if !strings.Contains(content, "Task A") {
		t.Error("missing background writ 'Task A'")
	}
	if !strings.Contains(content, "Task C") {
		t.Error("missing background writ 'Task C'")
	}
	if !strings.Contains(content, "Work only on your active writ") {
		t.Error("missing constraint text")
	}
	if !strings.Contains(content, "Envoy: Meridian") {
		t.Error("missing envoy identity header")
	}
}

func TestGenerateEnvoyClaudeMDMultiWritNoActive(t *testing.T) {
	ctx := EnvoyClaudeMDContext{
		AgentName: "Meridian",
		World:     "myworld",
		SolBinary: "sol",
		WritContext: WritContext{
			TetheredWrits: []WritSummary{
				{ID: "sol-aaa1", Title: "Task A", Kind: "code", Status: "tethered"},
				{ID: "sol-bbb2", Title: "Task B", Kind: "code", Status: "tethered"},
			},
			// No ActiveWritID set.
		},
	}

	content := GenerateEnvoyClaudeMD(ctx)

	if !strings.Contains(content, "Wait for the operator to activate one") {
		t.Error("missing wait-for-activation message")
	}
	if !strings.Contains(content, "2 tethered writs") {
		t.Error("missing tethered writ count")
	}
	if !strings.Contains(content, "Task A") {
		t.Error("missing writ 'Task A'")
	}
	if !strings.Contains(content, "Task B") {
		t.Error("missing writ 'Task B'")
	}
	if strings.Contains(content, "## Active Writ") {
		t.Error("no-active persona should not have Active Writ section")
	}
}

func TestGenerateClaudeMDOutpostNoBackgroundSection(t *testing.T) {
	ctx := ClaudeMDContext{
		AgentName:   "Toast",
		World:       "myworld",
		WritID:      "sol-a1b2c3d4",
		Title:       "Add a README",
		Description: "Create a README.md file",
		Kind:        "code",
	}

	content := GenerateClaudeMD(ctx)

	if !strings.Contains(content, "Outpost Agent: Toast") {
		t.Error("missing outpost header")
	}
	if strings.Contains(content, "Background Writs") {
		t.Error("outpost GenerateClaudeMD should NOT contain Background Writs section")
	}
	if strings.Contains(content, "Tethered Writs") {
		t.Error("outpost GenerateClaudeMD should NOT contain Tethered Writs section")
	}
}


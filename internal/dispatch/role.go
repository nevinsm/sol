package dispatch

import (
	"fmt"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
	"github.com/nevinsm/sol/internal/workflow"
)

// OutpostRoleConfig returns the startup.RoleConfig for the outpost agent role.
func OutpostRoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:                "outpost",
		WorktreeDir:         func(w, a string) string { return WorktreePath(w, a) },
		Persona:             outpostPersona,
		Hooks:               outpostHooks,
		SystemPromptContent: protocol.OutpostSystemPrompt,
		ReplacePrompt:       true, // full replace — outpost gets its own system prompt
		NeedsItem:           true,
		SkillInstaller:      outpostSkillInstaller,
		PrimeBuilder:        outpostPrime,
	}
}

// outpostSkillInstaller builds role-appropriate skills for outpost agents.
func outpostSkillInstaller(world, agent string) []adapter.Skill {
	// Read world config for quality gates.
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		// Non-fatal: build skills without quality gates.
		worldCfg = config.WorldConfig{}
	}

	// Read tether to find writ for output dir.
	writID, _ := tether.Read(world, agent, "outpost")
	var outputDir string
	if writID != "" {
		outputDir = config.WritOutputDir(world, writID)
	}

	return protocol.BuildSkills(protocol.SkillContext{
		World:        world,
		AgentName:    agent,
		Role:         "outpost",
		QualityGates: worldCfg.Forge.QualityGates,
		OutputDir:    outputDir,
	})
}

// OutpostResumeState builds a startup.ResumeState for outpost compact recovery.
func OutpostResumeState(world, agent string) startup.ResumeState {
	return handoff.CaptureResumeState(world, agent, "outpost", "compact", nil)
}

// outpostPersona generates the outpost CLAUDE.local.md content.
func outpostPersona(world, agent string) ([]byte, error) {
	// Read tether to find writ.
	writID, err := tether.Read(world, agent, "outpost")
	if err != nil || writID == "" {
		return []byte(fmt.Sprintf("# Outpost Agent: %s (world: %s)\n\nNo writ tethered.\n", agent, world)), nil
	}

	// Read writ from world store.
	ws, err := store.OpenWorld(world)
	if err != nil {
		return nil, fmt.Errorf("outpost persona: failed to open world store: %w", err)
	}
	defer ws.Close()

	item, err := ws.GetWrit(writID)
	if err != nil {
		return nil, fmt.Errorf("outpost persona: failed to get writ %q: %w", writID, err)
	}

	// Read world config for model tier and quality gates.
	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		return nil, fmt.Errorf("outpost persona: failed to load world config: %w", err)
	}

	// Check if a workflow is active.
	wfState, _ := workflow.ReadState(world, agent, "outpost")
	hasWorkflow := wfState != nil && wfState.Status == "running"

	// Resolve direct dependencies.
	var directDeps []protocol.DepOutput
	depIDs, err := ws.GetDependencies(writID)
	if err != nil {
		return nil, fmt.Errorf("outpost persona: failed to get dependencies for %q: %w", writID, err)
	}
	for _, depID := range depIDs {
		depWrit, err := ws.GetWrit(depID)
		if err != nil {
			return nil, fmt.Errorf("outpost persona: failed to get dependency writ %q: %w", depID, err)
		}
		depKind := depWrit.Kind
		if depKind == "" {
			depKind = "code"
		}
		directDeps = append(directDeps, protocol.DepOutput{
			WritID:    depID,
			Title:     depWrit.Title,
			Kind:      depKind,
			OutputDir: config.WritOutputDir(world, depID),
		})
	}

	kind := item.Kind
	if kind == "" {
		kind = "code"
	}

	ctx := protocol.ClaudeMDContext{
		AgentName:    agent,
		World:        world,
		WritID:       writID,
		Title:        item.Title,
		Description:  item.Description,
		Kind:         kind,
		HasWorkflow:  hasWorkflow,
		ModelTier:    worldCfg.Agents.ModelTier,
		QualityGates: worldCfg.Forge.QualityGates,
		OutputDir:    config.WritOutputDir(world, writID),
		DirectDeps:   directDeps,
	}
	content := protocol.GenerateClaudeMD(ctx)
	return []byte(content), nil
}

// outpostHooks returns the runtime-agnostic hook configuration for outpost agents.
func outpostHooks(world, agent string) startup.HookSet {
	return startup.HookSet{
		SessionStart: []startup.HookCommand{
			{Command: fmt.Sprintf("sol prime --world=%s --agent=%s", world, agent)},
		},
		PreCompact: []startup.HookCommand{
			{Command: fmt.Sprintf("sol prime --world=%s --agent=%s --compact", world, agent)},
		},
		TurnBoundary: []startup.HookCommand{
			{Command: fmt.Sprintf("sol nudge drain --world=%s --agent=%s", world, agent)},
		},
		Guards: append([]startup.Guard{
			{Pattern: "EnterPlanMode", Command: protocol.PlanModeBlockCommand},
		}, protocol.RoleGuards("outpost")...),
	}
}

// outpostPrime builds the initial prompt for the outpost session.
func outpostPrime(world, agent string) string {
	return fmt.Sprintf(
		"Agent %s, world %s. If no context appears, run: sol prime --world=%s --agent=%s",
		agent, world, world, agent)
}

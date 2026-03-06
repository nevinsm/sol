package forge

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
)

// ForgeRoleConfig returns the startup.RoleConfig for the forge role.
func ForgeRoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:                "forge",
		WorktreeDir:         func(world, _ string) string { return WorktreePath(world) },
		Persona:             forgePersona,
		Hooks:               forgeHooks,
		SystemPromptContent: protocol.ForgeSystemPrompt,
		ReplacePrompt:       true,
		Formula:             "forge-patrol",
		NeedsItem:           false,
		PrimeBuilder:        forgePrime,
	}
}

// forgePersona generates the forge CLAUDE.local.md content.
func forgePersona(world, _ string) ([]byte, error) {
	cfg, err := resolveForgeConfigForRole(world)
	if err != nil {
		return nil, err
	}

	ctx := protocol.ForgeClaudeMDContext{
		World:        world,
		TargetBranch: cfg.TargetBranch,
		WorktreeDir:  WorktreePath(world),
		QualityGates: cfg.QualityGates,
	}
	content := protocol.GenerateForgeClaudeMD(ctx)
	return []byte(content), nil
}

// forgeHooks returns the Claude Code hook configuration for the forge.
func forgeHooks(world, _ string) startup.HookSet {
	return protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"SessionStart": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol forge sync --world=%s && sol prime --world=%s --agent=forge", world, world),
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=forge --reason=compact", world),
						},
					},
				},
			},
			"PreToolUse": append([]protocol.HookMatcherGroup{
				{
					Matcher: "EnterPlanMode",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode requires human approval — no one is watching. Outline your approach in conversation, then implement directly." >&2; exit 2`,
						},
					},
				},
			}, protocol.GuardHooks("forge")...),
			"UserPromptSubmit": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=forge", world),
						},
					},
				},
			},
		},
	}
}

// forgePrime builds the initial prompt for the forge session.
func forgePrime(world, _ string) string {
	return fmt.Sprintf(
		"Execute your current formula step. Run sol workflow current --world=%s --agent=forge.",
		world)
}

// ForgeResumeState builds a startup.ResumeState for forge compact recovery.
// If an MR is claimed (tether has work item), the agent resumes from the
// current workflow step (typically gates, not scan). If no MR is claimed,
// the workflow step reflects the scan phase.
func ForgeResumeState(world string) startup.ResumeState {
	return handoff.CaptureResumeState(world, "forge", "forge", "compact")
}

// resolveForgeConfigForRole loads world config and builds a forge.Config.
func resolveForgeConfigForRole(world string) (Config, error) {
	cfg := DefaultConfig()

	worldCfg, err := config.LoadWorldConfig(world)
	if err != nil {
		// Graceful fallback to defaults if config can't be loaded.
		return cfg, nil
	}

	if len(worldCfg.Forge.QualityGates) > 0 {
		cfg.QualityGates = worldCfg.Forge.QualityGates
	} else {
		gatesPath := filepath.Join(config.WorldDir(world), "forge", "quality-gates.txt")
		gates, err := LoadQualityGates(gatesPath, cfg.QualityGates)
		if err == nil {
			cfg.QualityGates = gates
		}
	}
	if worldCfg.Forge.TargetBranch != "" {
		cfg.TargetBranch = worldCfg.Forge.TargetBranch
	}
	if worldCfg.Forge.GateTimeout != "" {
		parsed, _ := time.ParseDuration(worldCfg.Forge.GateTimeout)
		if parsed > 0 {
			cfg.GateTimeout = parsed
		}
	}

	return cfg, nil
}

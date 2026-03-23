package chancellor

import (
	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
)

// RoleConfig returns the startup.RoleConfig for the chancellor role.
func RoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:                "chancellor",
		WorktreeDir:         func(_, _ string) string { return ChancellorDir() },
		Persona:             chancellorPersona,
		Hooks:               chancellorHooks,
		SystemPromptContent: protocol.ChancellorSystemPrompt,
		ReplacePrompt:       false, // append mode — keep default system prompt
		SkillInstaller:      chancellorSkillInstaller,
		PrimeBuilder:        chancellorPrime,
	}
}

// chancellorSkillInstaller builds role-appropriate skills for the chancellor.
func chancellorSkillInstaller(_, _ string) []adapter.Skill {
	return protocol.BuildSkills(protocol.SkillContext{
		SolBinary: "sol",
		Role:      "chancellor",
	})
}

// chancellorPersona generates the chancellor CLAUDE.local.md content.
func chancellorPersona(_, _ string) ([]byte, error) {
	ctx := protocol.ChancellorClaudeMDContext{
		SolBinary: "sol",
	}
	content := protocol.GenerateChancellorClaudeMD(ctx)
	return []byte(content), nil
}

// chancellorHooks returns the runtime-agnostic hook configuration for the chancellor.
// The chancellor is sphere-level (no world), so sol prime --compact is omitted.
// Nudge drain uses --agent=chancellor; SOL_AGENT is set in the session env by startup.
func chancellorHooks(_, _ string) startup.HookSet {
	return startup.HookSet{
		SessionStart: []startup.HookCommand{
			{
				Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
				Matcher: "startup|resume",
			},
		},
		PreCompact: []startup.HookCommand{
			{Command: "sol brief inject --path=.brief/memory.md --max-lines=200"},
		},
		TurnBoundary: []startup.HookCommand{
			{Command: "sol nudge drain --agent=chancellor"},
		},
		Guards: append([]startup.Guard{
			{Pattern: "Write|Edit", Command: protocol.AutoMemoryBlockCommand},
			{Pattern: "EnterPlanMode", Command: protocol.PlanModeBlockCommand},
		}, protocol.RoleGuards("chancellor")...),
	}
}

// chancellorPrime builds the initial prompt for the chancellor session.
func chancellorPrime(_, _ string) string {
	return "Chancellor. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200"
}

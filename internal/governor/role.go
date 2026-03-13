package governor

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
)

// RoleConfig returns the startup.RoleConfig for the governor role.
func RoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:                "governor",
		WorktreeDir:         func(w, _ string) string { return GovernorDir(w) },
		Persona:             governorPersona,
		Hooks:               governorHooks,
		SystemPromptContent: protocol.GovernorSystemPrompt,
		ReplacePrompt:       false, // append mode — keep default system prompt
		SkillInstaller:      governorSkillInstaller,
		PrimeBuilder:        governorPrime,
	}
}

// governorSkillInstaller builds role-appropriate skills for the governor.
func governorSkillInstaller(world, _ string) []adapter.Skill {
	return protocol.BuildSkills(protocol.SkillContext{
		World:     world,
		SolBinary: "sol",
		Role:      "governor",
	})
}

// governorPersona generates the governor CLAUDE.local.md content.
func governorPersona(world, _ string) ([]byte, error) {
	ctx := protocol.GovernorClaudeMDContext{
		World:     world,
		SolBinary: "sol",
		MirrorDir: "../repo",
	}

	// Read tethered writs for multi-writ awareness.
	wctx, err := protocol.PopulateWritContext(world, "governor", "governor")
	if err != nil {
		// Non-fatal: log and continue with base persona.
		fmt.Fprintf(os.Stderr, "governor persona: failed to populate writ context: %v\n", err)
	}
	ctx.WritContext = wctx

	content := protocol.GenerateGovernorClaudeMD(ctx)
	return []byte(content), nil
}

// governorHooks returns the runtime-agnostic hook configuration for the governor.
func governorHooks(world, _ string) startup.HookSet {
	return startup.HookSet{
		SessionStart: []startup.HookCommand{
			{
				Command: fmt.Sprintf("sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s", world),
				Matcher: "startup|resume",
			},
		},
		PreCompact: []startup.HookCommand{
			{Command: fmt.Sprintf("sol prime --world=%s --agent=governor --compact", world)},
		},
		TurnBoundary: []startup.HookCommand{
			{Command: fmt.Sprintf("sol nudge drain --world=%s --agent=governor", world)},
		},
		Guards: append([]startup.Guard{
			{Pattern: "Write|Edit", Command: protocol.AutoMemoryBlockCommand},
			{Pattern: "EnterPlanMode", Command: protocol.PlanModeBlockCommand},
		}, protocol.RoleGuards("governor")...),
	}
}

// governorPrime builds the initial prompt for the governor session.
func governorPrime(world, _ string) string {
	return fmt.Sprintf(
		"Governor, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s",
		world, world)
}

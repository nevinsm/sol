package governor

import (
	"fmt"
	"os"

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

// governorSkillInstaller installs role-appropriate skills for the governor.
func governorSkillInstaller(worktreeDir, world, _ string) error {
	return protocol.InstallSkills(worktreeDir, protocol.SkillContext{
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

// governorHooks returns the Claude Code hook configuration for the governor.
func governorHooks(world, _ string) startup.HookSet {
	return protocol.BaseHooks(protocol.HookOptions{
		Role:             "governor",
		BriefPath:        ".brief/memory.md",
		SessionStartCmds: []string{fmt.Sprintf("sol world sync --world=%s", world)},
		PreCompactCmd:    fmt.Sprintf("sol prime --world=%s --agent=governor --compact", world),
		NudgeDrainCmd:    fmt.Sprintf("sol nudge drain --world=%s --agent=governor", world),
	})
}

// governorPrime builds the initial prompt for the governor session.
func governorPrime(world, _ string) string {
	return fmt.Sprintf(
		"Governor, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s",
		world, world)
}

package envoy

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
)

// RoleConfig returns the startup.RoleConfig for the envoy role.
func RoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:                "envoy",
		WorktreeDir:         func(w, a string) string { return WorktreePath(w, a) },
		Persona:             envoyPersona,
		Hooks:               envoyHooks,
		SystemPromptContent: protocol.EnvoySystemPrompt,
		ReplacePrompt:       false, // append mode — keep default system prompt
		SkillInstaller:      envoySkillInstaller,
		PrimeBuilder:        envoyPrime,
	}
}

// envoySkillInstaller installs role-appropriate skills for envoy agents.
func envoySkillInstaller(worktreeDir, world, agent string) error {
	return protocol.InstallSkills(worktreeDir, protocol.SkillContext{
		World:     world,
		AgentName: agent,
		SolBinary: "sol",
		Role:      "envoy",
	})
}

// envoyPersona generates the envoy CLAUDE.local.md content.
func envoyPersona(world, agent string) ([]byte, error) {
	// Read optional persona file.
	var personaContent string
	personaPath := PersonaPath(world, agent)
	if data, err := os.ReadFile(personaPath); err == nil {
		personaContent = string(data)
	}

	ctx := protocol.EnvoyClaudeMDContext{
		AgentName:      agent,
		World:          world,
		SolBinary:      "sol",
		PersonaContent: personaContent,
	}

	// Read tethered writs for multi-writ awareness.
	wctx, err := protocol.PopulateWritContext(world, agent, "envoy")
	if err != nil {
		// Non-fatal: log and continue with base persona.
		fmt.Fprintf(os.Stderr, "envoy persona: failed to populate writ context: %v\n", err)
	}
	ctx.WritContext = wctx

	content := protocol.GenerateEnvoyClaudeMD(ctx)
	return []byte(content), nil
}

// envoyHooks returns the Claude Code hook configuration for the envoy.
func envoyHooks(world, agent string) startup.HookSet {
	return protocol.BaseHooks(protocol.HookOptions{
		Role:          "envoy",
		BriefPath:     ".brief/memory.md",
		PreCompactCmd: fmt.Sprintf("sol prime --world=%s --agent=%s --compact", world, agent),
		NudgeDrainCmd: fmt.Sprintf("sol nudge drain --world=%s --agent=%s", world, agent),
	})
}

// envoyPrime builds the initial prompt for the envoy session.
// If the envoy has an active writ, include it in the prime output so the
// envoy knows about its assignment immediately on startup/restart.
func envoyPrime(world, agentName string) string {
	base := fmt.Sprintf(
		"Envoy %s, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200",
		agentName, world)

	// Look up active writ from sphere store.
	agentID := world + "/" + agentName
	sphereStore, err := store.OpenSphere()
	if err != nil {
		return base
	}
	defer sphereStore.Close()

	ag, err := sphereStore.GetAgent(agentID)
	if err != nil || ag.ActiveWrit == "" {
		return base
	}

	// Look up writ title from world store.
	writTitle := ag.ActiveWrit // fallback to ID
	worldStore, err := store.OpenWorld(world)
	if err == nil {
		defer worldStore.Close()
		if writ, err := worldStore.GetWrit(ag.ActiveWrit); err == nil {
			writTitle = writ.Title
		}
	}

	return fmt.Sprintf("%s\nActive writ: %s — %s\nRun `sol prime --world=%s --agent=%s` for full writ context.",
		base, ag.ActiveWrit, writTitle, world, agentName)
}

package envoy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/adapter"
	"github.com/nevinsm/sol/internal/config"
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
		PersonaFile:         func(w, a string) string { return PersonaPath(w, a) },
		SkillInstaller:      envoySkillInstaller,
		PrimeBuilder:        envoyPrime,
	}
}

// envoySkillInstaller builds role-appropriate skills for envoy agents.
func envoySkillInstaller(world, agent string) []adapter.Skill {
	// Resolve the world's main branch so resolve-and-submit (and any future
	// branch-aware skill) renders the correct rebase target. Best-effort:
	// fall back to the SkillContext default ("main") on load error.
	mainBranch := ""
	if cfg, err := config.LoadWorldConfig(world); err == nil {
		mainBranch = cfg.World.Branch
	} else {
		fmt.Fprintf(os.Stderr, "envoy skills: failed to load world config for %q (using default main branch): %v\n", world, err)
	}

	skills, err := protocol.BuildSkills(protocol.SkillContext{
		World:      world,
		AgentName:  agent,
		SolBinary:  "sol",
		Role:       "envoy",
		MainBranch: mainBranch,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return nil
	}
	return skills
}

// envoyPersona generates the envoy CLAUDE.local.md content.
func envoyPersona(world, agent string) ([]byte, error) {
	ctx := protocol.EnvoyClaudeMDContext{
		AgentName: agent,
		World:     world,
		SolBinary: "sol",
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

// envoyHooks returns the runtime-agnostic hook configuration for the envoy.
func envoyHooks(world, agent string) startup.HookSet {
	return startup.HookSet{
		PreCompact: []startup.HookCommand{
			{Command: fmt.Sprintf("sol prime --world=%s --agent=%s --compact", world, agent)},
		},
		TurnBoundary: []startup.HookCommand{
			{Command: fmt.Sprintf("sol nudge drain --world=%s --agent=%s", world, agent)},
		},
		Guards: append([]startup.Guard{
			{Pattern: "EnterPlanMode", Command: protocol.PlanModeBlockCommand},
		}, protocol.RoleGuards("envoy")...),
	}
}

// envoyPrime builds the initial prompt for the envoy session.
func envoyPrime(world, agentName string) string {
	memoryPath := filepath.Join(EnvoyDir(world, agentName), "memory", "MEMORY.md")
	base := fmt.Sprintf(
		"Envoy %s, world %s. Your persistent memory is at %s (Claude Code auto-memory). Run: sol prime --world=%s --agent=%s for full context.",
		agentName, world, memoryPath, world, agentName)

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

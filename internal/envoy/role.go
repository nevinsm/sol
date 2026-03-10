package envoy

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/startup"
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
	return protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"SessionStart": {
				{
					Matcher: "startup|resume",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
						},
					},
				},
			},
			"PreToolUse": append([]protocol.HookMatcherGroup{
				{
					Matcher: "Write|Edit",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `FILE=$(jq -r '.tool_input.file_path // empty'); if echo "$FILE" | grep -q '.claude/projects/.*/memory/'; then echo "BLOCKED: Use .brief/memory.md, not Claude Code auto-memory." >&2; exit 2; fi`,
						},
					},
				},
				{
					Matcher: "EnterPlanMode",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode overrides your persona and context. Outline your approach in conversation instead. Your persistent memory is at .brief/memory.md — consult it for your role constraints and accumulated knowledge." >&2; exit 2`,
						},
					},
				},
			}, protocol.GuardHooks("envoy")...),
			"PreCompact": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol prime --world=%s --agent=%s --compact", world, agent),
						},
					},
				},
			},
			"UserPromptSubmit": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=%s", world, agent),
						},
					},
				},
			},
		},
	}
}

// envoyPrime builds the initial prompt for the envoy session.
func envoyPrime(world, agent string) string {
	return fmt.Sprintf(
		"Envoy %s, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200",
		agent, world)
}

package dispatch

import (
	"fmt"

	"github.com/nevinsm/sol/internal/protocol"
	"github.com/nevinsm/sol/internal/protocol/prompts"
	"github.com/nevinsm/sol/internal/startup"
)

const outpostSystemPromptFile = ".claude/outpost-system-prompt.md"

// OutpostRoleConfig returns the startup.RoleConfig for outpost agents.
// Formula is left empty — callers set it from the --formula flag at cast time.
func OutpostRoleConfig() startup.RoleConfig {
	return startup.RoleConfig{
		Role:             "agent",
		WorktreeDir:      func(world, agent string) string { return WorktreePath(world, agent) },
		Persona:          outpostPersona,
		Hooks:            outpostHooks,
		SystemPromptFile: outpostSystemPromptFile,
		SystemPromptData: prompts.OutpostSystemPrompt,
		ReplacePrompt:    true,
		NeedsItem:        true,
		PrimeBuilder:     outpostPrime,
	}
}

// outpostPersona generates the outpost CLAUDE.local.md content.
// The persona is generated with default context — Cast enriches it with
// work item details via protocol.GenerateClaudeMD before calling Launch.
func outpostPersona(world string) ([]byte, error) {
	// During respawn, we don't have work item details. Generate a minimal
	// persona that instructs the agent to prime itself.
	ctx := protocol.ClaudeMDContext{
		World: world,
	}
	content := protocol.GenerateClaudeMD(ctx)
	return []byte(content), nil
}

// outpostHooks returns the Claude Code hook configuration for outpost agents.
func outpostHooks(world, agent string) startup.HookSet {
	return protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"SessionStart": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol prime --world=%s --agent=%s", world, agent),
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=%s --reason=compact", world, agent),
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
			}, protocol.GuardHooks("outpost")...),
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

// outpostPrime builds the initial prompt for an outpost session.
func outpostPrime(world, agent string) string {
	return fmt.Sprintf(
		"Agent %s, world %s. If no context appears, run: sol prime --world=%s --agent=%s",
		agent, world, world, agent)
}

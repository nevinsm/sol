package governor

import (
	"fmt"

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
		PrimeBuilder:        governorPrime,
	}
}

// governorPersona generates the governor CLAUDE.local.md content.
func governorPersona(world, _ string) ([]byte, error) {
	ctx := protocol.GovernorClaudeMDContext{
		World:     world,
		SolBinary: "sol",
		MirrorDir: "../repo",
	}
	content := protocol.GenerateGovernorClaudeMD(ctx)
	return []byte(content), nil
}

// governorHooks returns the Claude Code hook configuration for the governor.
func governorHooks(world, _ string) startup.HookSet {
	return protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"SessionStart": {
				{
					Matcher: "startup|resume",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s", world),
						},
					},
				},
				{
					Matcher: "compact",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: "sol brief inject --path=.brief/memory.md --max-lines=200",
						},
					},
				},
			},
			"PreCompact": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol handoff --world=%s --agent=governor --reason=compact", world),
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
			}, protocol.GuardHooks("governor")...),
			"UserPromptSubmit": {
				{
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: fmt.Sprintf("sol nudge drain --world=%s --agent=governor", world),
						},
					},
				},
			},
		},
	}
}

// governorPrime builds the initial prompt for the governor session.
func governorPrime(world, _ string) string {
	return fmt.Sprintf(
		"Governor, world %s. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200 && sol world sync --world=%s",
		world, world)
}

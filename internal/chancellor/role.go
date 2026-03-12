package chancellor

import (
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

// chancellorSkillInstaller installs role-appropriate skills for the chancellor.
func chancellorSkillInstaller(worktreeDir, _, _ string) error {
	return protocol.InstallSkills(worktreeDir, protocol.SkillContext{
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

// chancellorHooks returns the Claude Code hook configuration for the chancellor.
// The chancellor is sphere-level (no world), so world-dependent hooks (nudge drain,
// sol prime --compact) are omitted.
func chancellorHooks(_, _ string) startup.HookSet {
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
			}, protocol.GuardHooks("chancellor")...),
		},
	}
}

// chancellorPrime builds the initial prompt for the chancellor session.
func chancellorPrime(_, _ string) string {
	return "Chancellor. If no context appears, run: sol brief inject --path=.brief/memory.md --max-lines=200"
}

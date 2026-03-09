package forge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nevinsm/sol/internal/protocol"
)

// forgePersonaTemplate returns the persona content for a forge merge session.
// The persona is written to CLAUDE.local.md in the forge worktree root.
func forgePersonaTemplate(world string) string {
	return fmt.Sprintf(`# Forge Merge Engineer — %s

You are a senior merge engineer. Your sole job is to merge the branch
described in your injection context cleanly into main.

## What You Do
- Sync the worktree to latest origin/main
- Squash merge the source branch
- Resolve merge conflicts using your judgment about code intent
- Run quality gates and analyze failures
- Push successful merges to main
- Report your result

## What You Do NOT Do
- Write new features or add functionality
- Refactor code beyond what's needed for conflict resolution
- Explore the codebase beyond the merge scope
- Modify files not involved in the merge
- Make "improvements" to code you're merging

## Conflict Resolution
When you encounter merge conflicts:
1. Read both sides carefully — understand the intent of each change
2. Resolve by combining both intents correctly
3. Only touch the conflicting hunks — leave everything else unchanged
4. If both sides make incompatible architectural changes that you cannot
   confidently reconcile, report "conflict" rather than guessing

## Gate Failures
When quality gates fail after your merge:
1. Read the failure output carefully
2. Determine: is this caused by the branch changes, or was it pre-existing?
3. To test: stash your changes, run gates on base. If base also fails,
   it's pre-existing — proceed with the merge.
4. If branch-caused: analyze what went wrong and report "failed" with
   your analysis.

## Reporting
When done, write .forge-result.json in the worktree root:
{ "result": "merged"|"failed"|"conflict", "summary": "...",
  "files_changed": [...], "gate_output": "..." }

Then exit — your session will be recycled.
`, world)
}

// forgeHookConfig returns the hook configuration for a forge merge session.
// Hooks installed:
//   - PreToolUse: block EnterPlanMode (forge should not enter plan mode)
//   - PreToolUse: guard hooks appropriate for forge role (allows git reset --hard, push to main)
func forgeHookConfig() protocol.HookConfig {
	return protocol.HookConfig{
		Hooks: map[string][]protocol.HookMatcherGroup{
			"PreToolUse": append([]protocol.HookMatcherGroup{
				{
					Matcher: "EnterPlanMode",
					Hooks: []protocol.HookHandler{
						{
							Type:    "command",
							Command: `echo "BLOCKED: Plan mode is not permitted in forge merge sessions." >&2; exit 2`,
						},
					},
				},
			}, protocol.GuardHooks("forge")...),
		},
	}
}

// InstallForgePersona writes the forge merge persona and hook configuration
// into the given worktree directory. This prepares the worktree for a forge
// merge session.
//
// Files written:
//   - CLAUDE.local.md at worktree root (persona content)
//   - .claude/settings.local.json (hook configuration)
//
// This function is idempotent — safe to call before every merge session.
func InstallForgePersona(worktreeDir, world string) error {
	// Write persona to CLAUDE.local.md at worktree root.
	persona := forgePersonaTemplate(world)
	personaPath := filepath.Join(worktreeDir, "CLAUDE.local.md")
	if err := os.WriteFile(personaPath, []byte(persona), 0o644); err != nil {
		return fmt.Errorf("failed to write forge persona to %s: %w", personaPath, err)
	}

	// Write hook configuration to .claude/settings.local.json.
	hookCfg := forgeHookConfig()
	if err := protocol.WriteHookSettings(worktreeDir, hookCfg); err != nil {
		return fmt.Errorf("failed to write forge hook settings: %w", err)
	}

	return nil
}

// CleanForgeResult removes the .forge-result.json file from the worktree.
// Called before each merge session so stale results from previous runs don't
// confuse the result reader. Ignores not-found errors (idempotent).
func CleanForgeResult(worktreeDir string) error {
	path := filepath.Join(worktreeDir, resultFileName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clean forge result file: %w", err)
	}
	return nil
}

// ForgePersonaContent returns the raw persona markdown for testing or inspection.
func ForgePersonaContent(world string) string {
	return forgePersonaTemplate(world)
}

// ForgePersonaContains checks that the persona contains expected content markers.
// Useful for validation in tests.
func ForgePersonaContains(persona string, markers []string) []string {
	var missing []string
	for _, m := range markers {
		if !strings.Contains(persona, m) {
			missing = append(missing, m)
		}
	}
	return missing
}

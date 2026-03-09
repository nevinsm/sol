package protocol

import (
	"fmt"
	"os"
	"path/filepath"
)

// SkillContext holds common fields used when generating skill content for agents.
type SkillContext struct {
	World        string
	AgentName    string
	SolBinary    string   // path to sol binary (defaults to "sol")
	Role         string   // outpost, forge, governor, envoy, senate
	TargetBranch string   // forge: target branch for merges
	QualityGates []string // commands to run before resolving
	OutputDir    string   // persistent output directory for writ
}

func (ctx SkillContext) sol() string {
	if ctx.SolBinary != "" {
		return ctx.SolBinary
	}
	return "sol"
}

// roleSkillsMap defines which skills belong to each role.
var roleSkillsMap = map[string][]string{
	"outpost":  {"resolve-and-handoff", "memories"},
	"forge":    {"forge-patrol", "forge-toolbox", "merge-operations"},
	"governor": {"writ-dispatch", "caravan-management", "world-coordination", "notification-handling", "memories"},
	"envoy":    {"resolve-and-submit", "writ-management", "dispatch", "session-management", "status-monitoring", "caravan-management", "world-operations", "notification-handling", "mail", "memories"},
	"senate":   {"world-queries", "writ-planning", "memories"},
}

// RoleSkills returns the skill names for a given role.
func RoleSkills(role string) []string {
	skills, ok := roleSkillsMap[role]
	if !ok {
		return nil
	}
	// Return a copy to prevent mutation.
	out := make([]string, len(skills))
	copy(out, skills)
	return out
}

// InstallSkills generates and writes .claude/skills/{name}/SKILL.md for each
// role-appropriate skill. Stale skill directories (from a previous role set)
// are removed.
func InstallSkills(dir string, ctx SkillContext) error {
	skillsDir := filepath.Join(dir, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	skills := RoleSkills(ctx.Role)
	currentSet := make(map[string]bool, len(skills))
	for _, name := range skills {
		currentSet[name] = true
	}

	// Clean up stale skill directories.
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return fmt.Errorf("failed to read skills directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() && !currentSet[e.Name()] {
			stale := filepath.Join(skillsDir, e.Name())
			if err := os.RemoveAll(stale); err != nil {
				return fmt.Errorf("failed to remove stale skill %q: %w", e.Name(), err)
			}
		}
	}

	// Generate and write each skill.
	for _, name := range skills {
		content := generateSkill(name, ctx)
		if content == "" {
			continue
		}
		skillDir := filepath.Join(skillsDir, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("failed to create skill dir %q: %w", name, err)
		}
		skillPath := filepath.Join(skillDir, "SKILL.md")
		if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write skill %q: %w", name, err)
		}
	}

	return nil
}

// generateSkill dispatches to the appropriate skill content generator.
func generateSkill(name string, ctx SkillContext) string {
	switch name {
	case "resolve-and-handoff":
		return skillResolveAndHandoff(ctx)
	case "resolve-and-submit":
		return skillResolveAndSubmit(ctx)
	case "memories":
		return skillMemories(ctx)
	case "forge-patrol":
		return skillForgePatrol(ctx)
	case "forge-toolbox":
		return skillForgeToolbox(ctx)
	case "merge-operations":
		return skillMergeOperations(ctx)
	case "writ-dispatch":
		return skillWritDispatch(ctx)
	case "caravan-management":
		return skillCaravanManagement(ctx)
	case "world-coordination":
		return skillWorldCoordination(ctx)
	case "notification-handling":
		if ctx.Role == "envoy" {
			return skillEnvoyNotificationHandling(ctx)
		}
		return skillNotificationHandling(ctx)
	case "mail":
		return skillMail(ctx)
	case "writ-management":
		return skillWritManagement(ctx)
	case "dispatch":
		return skillDispatch(ctx)
	case "session-management":
		return skillSessionManagement(ctx)
	case "status-monitoring":
		return skillStatusMonitoring(ctx)
	case "world-operations":
		return skillWorldOperations(ctx)
	case "world-queries":
		return skillWorldQueries(ctx)
	case "writ-planning":
		return skillWritPlanning(ctx)
	default:
		return ""
	}
}

// --- Skill content generators ---

func skillResolveAndHandoff(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Resolve & Handoff

Commands for completing work and managing session continuity.

## Completing Work

| Command | Description |
|---------|-------------|
| %[1]s resolve%[2]s | Push branch, clear tether, end session. **Mandatory final step.** |
| %[1]s escalate%[3]s | Request help when stuck. Always include a description. |

## Session Handoff

| Command | Description |
|---------|-------------|
| %[1]s handoff%[4]s | Hand off to a fresh session (preserves context) |

Options:
- %[5]s — provide progress summary for successor
- %[6]s — tag reason (compact, manual, health-check)
`,
		"`"+sol, flagsForResolve(ctx), flagsForEscalate(ctx), flagsForHandoff(ctx),
		"`--summary=\"...\"`", "`--reason=compact`")
}

func skillResolveAndSubmit(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Resolve & Submit

Commands for submitting work through the forge pipeline.

## Submitting Work

All code changes MUST go through %[1]s resolve%[2]s. Never use %[3]s alone —
pushing your branch does not create a merge request.

| Command | Description |
|---------|-------------|
| %[1]s resolve%[2]s | Push branch, create merge request, clear tether |
| %[1]s escalate "description"%[4]s | Request help when stuck |

## Submit Workflow

1. Commit your changes to your branch
2. Run %[1]s resolve%[2]s
3. Your session stays alive — continue working after resolve
4. Reset worktree for next task: %[5]s
5. Update your brief
`,
		"`"+sol, " --world="+ctx.World+" --agent="+ctx.AgentName+"`",
		"`git push`",
		"`",
		"`git checkout main && git pull`")
}

func skillMemories(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Agent Memories

Persist insights across sessions so successors inherit your knowledge.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s remember "key" "insight"%[2]s | Save with explicit key |
| %[1]s remember "insight"%[2]s | Save with auto-generated key |
| %[1]s memories%[2]s | Review stored memories |
| %[1]s forget "key"%[2]s | Remove outdated memory |

Memories are injected during prime — successor sessions see them automatically.
`, "`"+sol, "`")
}

func skillForgePatrol(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Forge Patrol

Workflow commands for driving the forge patrol loop.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s workflow current --world=%[2]s --agent=forge%[3]s | Read current step instructions |
| %[1]s workflow advance --world=%[2]s --agent=forge%[3]s | Mark step complete, advance to next |
| %[1]s workflow status --world=%[2]s --agent=forge%[3]s | Check progress |

## Protocol

1. Read current step: %[1]s workflow current --world=%[2]s --agent=forge%[3]s
2. Execute the step instructions exactly.
3. Advance: %[1]s workflow advance --world=%[2]s --agent=forge%[3]s
4. Repeat from step 1.

The workflow handles looping — when the last step completes, it cycles back.
`, "`"+sol, world, "`")
}

func skillForgeToolbox(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Forge Toolbox

Forge-specific commands for queue management and merge processing.

## Queue Commands

| Command | Description |
|---------|-------------|
| %[1]s forge check-unblocked --world=%[2]s%[3]s | Check for unblocked MRs |
| %[1]s forge ready --world=%[2]s --json%[3]s | Scan ready queue |
| %[1]s forge claim --world=%[2]s --json%[3]s | Claim next MR |
| %[1]s forge release --world=%[2]s <id>%[3]s | Release for retry |
| %[1]s forge sync --world=%[2]s%[3]s | Sync worktree with target |

## Result Commands

| Command | Description |
|---------|-------------|
| %[1]s forge mark-merged --world=%[2]s <id>%[3]s | Mark MR as merged |
| %[1]s forge mark-failed --world=%[2]s <id>%[3]s | Mark MR as failed |
| %[1]s forge create-resolution --world=%[2]s <id>%[3]s | Request conflict resolution |

## Pause/Wait

| Command | Description |
|---------|-------------|
| %[1]s forge status %[2]s --json%[3]s | Check pause state |
| %[1]s forge await --world=%[2]s --timeout=120%[3]s | Wait for nudge or timeout |
`, "`"+sol, world, "`")
}

func skillMergeOperations(ctx SkillContext) string {
	branch := ctx.TargetBranch
	if branch == "" {
		branch = "main"
	}
	return fmt.Sprintf(`# Merge Operations

Git commands for the forge merge workflow.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s | Squash merge |
| %[2]s | Push to target |
| %[3]s | Reset on failure |

## Conflict Handling

If merge has conflicts: %[4]s then use %[5]s.

## Important

- Never use %[6]s
- Never create branches (%[7]s)
- Never modify application code
`, "`git merge --squash origin/<branch>`",
		"`git push origin HEAD:"+branch+"`",
		"`git reset --hard origin/"+branch+"`",
		"`git merge --abort`",
		"`sol forge create-resolution`",
		"`git push --force`",
		"`git checkout -b`")
}

func skillWritDispatch(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Writ Dispatch

Commands for creating writs and dispatching work to agents.

## Creating Work

| Command | Description |
|---------|-------------|
| %[1]s writ create --world=%[2]s --title="..." --description="..."%[3]s | Create a new writ |

Options: %[4]s, %[5]s (repeatable), %[6]s, %[7]s (JSON).

## Dispatching

| Command | Description |
|---------|-------------|
| %[1]s cast <id> --world=%[2]s%[3]s | Dispatch writ to an agent |
| %[1]s tether <id> --agent=<agent> --world=%[2]s%[3]s | Bind writ to persistent agent |
| %[1]s untether <id> --agent=<agent> --world=%[2]s%[3]s | Unbind writ from agent |

Cast options: %[8]s (auto if omitted), %[9]s, %[10]s.
`, "`"+sol, world, "`",
		"`--priority` (1=high, 2=normal, 3=low)",
		"`--label`",
		"`--kind` (code or analysis)",
		"`--metadata`",
		"`--agent`",
		"`--workflow`",
		"`--account`")
}

func skillCaravanManagement(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	desc := "Commands for grouping and sequencing related writs."
	switch ctx.Role {
	case "governor":
		desc = "Commands for coordinating related writs across agents."
	case "envoy":
		desc = "Commands for sequencing your own multi-step work."
	}
	return fmt.Sprintf(`# Caravan Management

%[4]s

## Commands

| Command | Description |
|---------|-------------|
| %[1]s caravan create "name" <id> [<id>...] --world=%[2]s%[3]s | Create caravan with items |
| %[1]s caravan add <caravan-id> <id> --world=%[2]s%[3]s | Add item to caravan |
| %[1]s caravan status [<caravan-id>]%[3]s | Check caravan progress |
| %[1]s caravan launch <caravan-id> --world=%[2]s%[3]s | Dispatch all ready items |
| %[1]s caravan commission <caravan-id>%[3]s | Mark caravan as commissioned |
| %[1]s caravan set-phase <caravan-id> <phase>%[3]s | Set current phase |
| %[1]s caravan check <caravan-id>%[3]s | Check phase-gate readiness |
| %[1]s caravan list%[3]s | List all caravans |

## Dependencies

| Command | Description |
|---------|-------------|
| %[1]s caravan dep add <caravan-id> <dep-id>%[3]s | Add inter-caravan dependency |
| %[1]s caravan dep list <caravan-id>%[3]s | List caravan dependencies |
`, "`"+sol, world, "`", desc)
}

func skillWorldCoordination(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# World Coordination

Commands for monitoring world state and coordinating agents.

## Status

| Command | Description |
|---------|-------------|
| %[1]s status%[3]s | Sphere overview (processes, worlds, unread mail count) |
| %[1]s status %[2]s%[3]s | World detail (agents, writs, forge, nudge queue depth) |
| %[1]s agent list%[3]s | List agents and availability |

## World Sync

| Command | Description |
|---------|-------------|
| %[1]s world sync --world=%[2]s%[3]s | Sync managed repo from upstream |

## Service Management

| Command | Description |
|---------|-------------|
| %[1]s sentinel status --world=%[2]s%[3]s | Check sentinel health |
| %[1]s sentinel restart --world=%[2]s%[3]s | Restart sentinel |
| %[1]s forge status %[2]s%[3]s | Check forge status |
| %[1]s service status%[3]s | Show all sphere daemon status |
| %[1]s down --all%[3]s | Stop all world services |

## Escalation

| Command | Description |
|---------|-------------|
| %[1]s escalate "description"%[3]s | Escalate to operator |
`, "`"+sol, world, "`")
}

func skillNotificationHandling(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Notification Handling

Notifications arrive at each turn boundary via UserPromptSubmit hook.
Format: %[1]s[NOTIFICATION] TYPE: Subject — Body%[2]s

## Notification Types

**MAIL** — Operator sent a message via %[3]s mail send%[5]s.
- Fields: %[1]ssubject%[2]s, %[1]sbody%[2]s
- Action: Read and respond to operator communication. Check %[3]s mail inbox%[5]s for full context.

**AGENT_DONE** — An outpost resolved a writ.
- Fields: %[1]swrit_id%[2]s, %[1]sagent_name%[2]s, %[1]sbranch%[2]s, %[1]stitle%[2]s, %[1]smerge_request_id%[2]s
- Check caravan status: %[3]s caravan status%[5]s
- Dispatch next ready work if agents are available

**MERGED** — Forge successfully merged a writ.
- Fields: %[1]swrit_id%[2]s, %[1]smerge_request_id%[2]s
- Check if blocked items are now unblocked

**MERGE_FAILED** — Forge failed to merge.
- Fields: %[1]swrit_id%[2]s, %[1]smerge_request_id%[2]s, %[1]sreason%[2]s
- Consider re-dispatching for conflict resolution

**RECOVERY_NEEDED** — Sentinel exhausted respawn attempts.
- Fields: %[1]sagent_id%[2]s, %[1]swrit_id%[2]s, %[1]sreason%[2]s, %[1]sattempts%[2]s
- Assess whether to re-dispatch or escalate

Always update your brief after handling a notification.
`, "`", "`", "`"+sol, world, "`")
}

func skillEnvoyNotificationHandling(_ SkillContext) string {
	return `# Notification Handling

Notifications arrive at each turn boundary via UserPromptSubmit hook.
Format: ` + "`[NOTIFICATION] TYPE: Subject — Body`" + `

## Notification Types

**MAIL** — Operator sent a message via ` + "`sol mail send`" + `.
- Fields: ` + "`subject`" + `, ` + "`body`" + `
- Action: Read and acknowledge. The operator is communicating directly — respond to the content.

**MERGED** — Forge merged one of your resolved writs.
- Fields: ` + "`writ_id`" + `, ` + "`merge_request_id`" + `
- Action: Note the merge. If you have follow-up work, proceed.

**MERGE_FAILED** — Forge failed to merge your writ.
- Fields: ` + "`writ_id`" + `, ` + "`merge_request_id`" + `, ` + "`reason`" + `
- Action: Investigate the failure reason. May need to pull latest main, resolve conflicts, and re-resolve.

**AGENT_DONE** — Another agent resolved a writ you dispatched.
- Fields: ` + "`writ_id`" + `, ` + "`agent_name`" + `, ` + "`branch`" + `, ` + "`title`" + `, ` + "`merge_request_id`" + `
- Action: Note the completion. Review the result if relevant to your current work.

Always update your brief after handling a notification.
`
}

func skillMail(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Mail

Commands for sending and reading operator mail.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s mail inbox%[2]s | List pending messages |
| %[1]s mail read <message-id>%[2]s | Read a message (marks as read) |
| %[1]s mail ack <message-id>%[2]s | Acknowledge a message |
| %[1]s mail check%[2]s | Count unread messages |
| %[1]s mail send --to=<identity> --subject="..." --body="..."%[2]s | Send a message |

Options for send: %[3]s (1=high, 2=normal, 3=low), %[4]s.

Mail is delivered as a MAIL notification via the nudge system.
Use %[1]s mail inbox%[2]s to see full message history.
`, "`"+sol, "`",
		"`--priority`",
		"`--no-notify`")
}

func skillWritManagement(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Writ Management

Commands for creating and managing writs.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s writ create --world=%[2]s --title="..." --description="..."%[3]s | Create a new writ |
| %[1]s tether <id> --agent=%[4]s --world=%[2]s%[3]s | Bind writ to yourself |
| %[1]s untether <id> --agent=%[4]s --world=%[2]s%[3]s | Unbind writ |
| %[1]s writ activate <id> --world=%[2]s --agent=%[4]s%[3]s | Switch active writ |
| %[1]s writ status <id> --world=%[2]s%[3]s | Check writ status |
| %[1]s writ list --world=%[2]s%[3]s | List writs |

Options for create: %[5]s, %[6]s (repeatable), %[7]s (code or analysis), %[8]s (JSON).
`, "`"+sol, world, "`",
		ctx.AgentName,
		"`--priority` (1=high, 2=normal, 3=low)",
		"`--label`",
		"`--kind`",
		"`--metadata`")
}

func skillDispatch(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Dispatch

Commands for dispatching work to outpost agents.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s cast <id> --world=%[2]s%[3]s | Dispatch writ to an agent |
| %[1]s agent list%[3]s | List agents and availability |

Cast options: %[4]s (auto if omitted), %[5]s, %[6]s.
`, "`"+sol, world, "`",
		"`--agent`",
		"`--workflow`",
		"`--account`")
}

func skillSessionManagement(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Session Management

Commands for managing session continuity.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s handoff%[2]s | Hand off to a fresh session |

## Options

- %[3]s — provide progress summary for successor
- %[4]s — tag reason (compact, manual, health-check)
`, "`"+sol, "`",
		"`--summary=\"...\"`",
		"`--reason=compact`")
}

func skillStatusMonitoring(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Status Monitoring

Commands for checking world and agent state.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s status%[3]s | Sphere overview (processes, worlds, unread mail count) |
| %[1]s status %[2]s%[3]s | World detail (agents, writs, forge, nudge queue depth) |
| %[1]s writ list --world=%[2]s%[3]s | List all writs |
| %[1]s writ status <id> --world=%[2]s%[3]s | Check specific writ |
| %[1]s agent list%[3]s | List agents and states |
| %[1]s forge queue --world=%[2]s%[3]s | Check forge queue |

## Component Status

| Command | Description |
|---------|-------------|
| %[1]s prefect status%[3]s | Prefect health and uptime |
| %[1]s consul status%[3]s | Consul patrol status |
| %[1]s chronicle status%[3]s | Chronicle status |
| %[1]s ledger status%[3]s | Ledger OTLP receiver status |
| %[1]s sentinel status --world=%[2]s%[3]s | Sentinel health for this world |
| %[1]s governor status --world=%[2]s%[3]s | Governor status for this world |
| %[1]s forge status %[2]s%[3]s | Forge status for this world |
`, "`"+sol, world, "`")
}

func skillWorldOperations(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# World Operations

Commands for world-level operations.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s world sync --world=%[2]s%[3]s | Sync managed repo from upstream |
| %[1]s world status %[2]s%[3]s | World health overview |
| %[1]s world query %[2]s "question"%[3]s | Query the governor |
| %[1]s world summary %[2]s%[3]s | Read governor's world summary |

## Service Lifecycle

| Command | Description |
|---------|-------------|
| %[1]s service status%[3]s | Show all sphere daemon status |
| %[1]s service install%[3]s | Install systemd units |
| %[1]s service uninstall%[3]s | Remove systemd units |
| %[1]s down --all%[3]s | Stop all world services |
`, "`"+sol, world, "`")
}

func skillWorldQueries(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# World Queries

Commands for cross-world intelligence gathering.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s world summary <world>%[2]s | Read a governor's world summary |
| %[1]s world query <world> "question"%[2]s | Query a governor |
| %[1]s world list%[2]s | List all worlds |
| %[1]s world export <world>%[2]s | Export world state |

Use these to gather context from multiple worlds before planning.
`, "`"+sol, "`")
}

func skillWritPlanning(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Writ Planning

Commands for creating writs and caravans across worlds.

## Creating Writs

| Command | Description |
|---------|-------------|
| %[1]s writ create --world=<world> --title="..." --description="..."%[2]s | Create writ in any world |

Options: %[3]s, %[4]s, %[5]s, %[6]s (JSON).

## Caravans

| Command | Description |
|---------|-------------|
| %[1]s caravan create "name" <id> [<id>...] --world=<world>%[2]s | Create caravan with items |
| %[1]s caravan commission <id>%[2]s | Mark caravan as commissioned |
| %[1]s caravan launch <id> --world=<world>%[2]s | Dispatch all ready items |
| %[1]s caravan status [<caravan-id>]%[2]s | Check caravan progress |
`, "`"+sol, "`",
		"`--priority` (1=high, 2=normal, 3=low)",
		"`--label` (repeatable)",
		"`--kind` (code or analysis)",
		"`--metadata`")
}

// --- Helpers ---

func flagsForResolve(ctx SkillContext) string {
	// Outpost agents don't need explicit flags — world/agent come from env.
	// The trailing backtick closes the inline code span opened by %[1]s.
	return "`"
}

func flagsForEscalate(ctx SkillContext) string {
	return " \"description\"`"
}

func flagsForHandoff(ctx SkillContext) string {
	return "`"
}

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
	Role         string   // outpost, forge, governor, envoy, chancellor
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
	"governor": {"writ-dispatch", "caravan-management", "world-coordination", "notification-handling", "handoff", "memories"},
	"envoy":    {"resolve-and-submit", "writ-management", "dispatch", "handoff", "status-monitoring", "caravan-management", "world-operations", "notification-handling", "mail", "memories"},
	"chancellor": {"world-queries", "writ-planning", "memories", "mail", "handoff"},
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
	case "handoff":
		return skillHandoff(ctx)
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

%[1]s resolve%[2]s is your **mandatory final step** — it pushes your branch, creates a merge request, clears the tether, and ends your session. There is no coming back after resolve; the worktree is cleaned up.

## When to Use

- Work is complete → %[1]s resolve%[2]s
- Stuck or blocked → %[1]s escalate%[3]s instead of resolving

## Completing Work

| Command | Description |
|---------|-------------|
| %[1]s resolve%[2]s | Push branch, clear tether, end session. **Mandatory final step.** |
| %[1]s escalate%[3]s | Request help when stuck. Always include a description. |

## Session Handoff

| Command | Description |
|---------|-------------|
| %[1]s handoff%[4]s | Hand off to a fresh session (preserves context) |

Always pass %[5]s describing what you were doing, what's completed, what's in progress, and what the next step is. This becomes the first thing your successor sees.

Options:
- %[5]s — **required**: summary for your successor (what you were doing, what's done, what's next)
- %[6]s — tag reason (compact, manual, health-check)

## Common Patterns

**Normal completion:** commit changes → %[1]s resolve%[2]s — session ends.

**Partially done:** commit progress → %[1]s escalate%[3]s — describe what remains.

**Context running long:** commit → update %[7]s → %[1]s handoff --summary="..."%[4]s

## Failure Modes

- **No tether found** → exit 1. Manual re-cast or tether restore needed.
- **git push fails** → NON-FATAL. Resolve exits 0. MR in "failed" state; forge handles it. Writ reopens for re-dispatch.
- **Database locked** → exit 1. Transient — retry.
- Resolve is idempotent — safe to call multiple times.
`,
		"`"+sol, flagsForResolve(ctx), flagsForEscalate(ctx), flagsForHandoff(ctx),
		"`--summary=\"...\"`", "`--reason=compact`", "`.brief/memory.md`")
}

func skillResolveAndSubmit(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Resolve & Submit

%[1]s resolve%[2]s submits your work through the forge pipeline — it pushes your branch, creates a merge request, clears the tether, and keeps your session alive. Never use %[3]s alone; pushing does not create a merge request.

## When to Use

- Code changes are complete and committed → %[1]s resolve%[2]s
- Stuck or blocked → %[1]s escalate "description"%[4]s

## Submitting Work

| Command | Description |
|---------|-------------|
| %[1]s resolve%[2]s | Push branch, create merge request, clear tether |
| %[1]s escalate "description"%[4]s | Request help when stuck |

## Common Patterns

**Normal submit:** commit all changes → %[1]s resolve%[2]s → %[5]s → update brief → tether next writ.

**More tethered writs:** after resolve you stay "working" — activate the next writ and continue.

**No remaining work:** after resolve you go idle — check for new writs or await instructions.

## Failure Modes

- **git push fails** → NON-FATAL. Resolve exits 0. MR in "failed" state; writ reopens. Pull main and re-resolve.
- **No tether found** → exit 1. Check %[6]s to see your tethered writs.
- **Database locked** → exit 1. Transient — retry.
- Resolve is idempotent — safe to call multiple times.
`,
		"`"+sol, " --world="+ctx.World+" --agent="+ctx.AgentName+"`",
		"`git push`",
		"`",
		"`git checkout main && git pull`",
		"`sol writ list`")
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
	var roleSection string
	switch ctx.Role {
	case "governor":
		desc = "Commands for coordinating related writs across agents."
		roleSection = `
## Mental Model

A caravan is an ordered batch of writs across phases (0, 1, 2, ...). Items in the same phase run in parallel. Items in phase N are blocked until ALL items in phases < N are **closed** (fully merged, not just done). Phase assignment is manual; consul auto-dispatches ready items every 5 minutes.

## Common Patterns

**Setup:** create writs → ` + "`sol caravan create`" + ` → ` + "`sol caravan set-phase`" + ` for each → ` + "`sol caravan commission`" + `.

**After AGENT_DONE:** ` + "`sol caravan status`" + ` to check progress; consul handles dispatch on next patrol (or ` + "`sol caravan launch`" + ` for immediate).

**Stalled:** ` + "`sol caravan check <id>`" + ` shows which items are blocking — typically an item stuck in forge.

## Failure Modes

- **Phase won't advance:** all prior-phase items must be "closed" (merged), not just "done". Check forge queue for stuck MRs.
- **No idle agents:** items stay ready; consul dispatches on next patrol cycle.
- **` + "`sol caravan launch`" + ` with no agents:** exits with message, items stay ready.`
	case "envoy":
		desc = "Commands for sequencing your own multi-step work."
		roleSection = `
## Mental Model

A caravan sequences your own multi-step work across phases. Items in the same phase can run in parallel; items in phase N wait until all prior-phase items are **closed** (fully merged). You are the single worker — resolve each phase and consul advances to the next.

## Common Patterns

**Setup:** create writs → ` + "`sol caravan create`" + ` → assign phases via ` + "`sol caravan set-phase`" + ` → ` + "`sol caravan commission`" + `.

**Working:** resolve phase 0 writs → consul detects closed items → dispatches phase 1 → repeat.

**Check progress:** ` + "`sol caravan status`" + ` after each resolve to see what's next.

## Failure Modes

- **Phase won't advance:** prior items must be "closed" (merged), not just "done". Check forge queue.
- **` + "`sol caravan launch`" + ` with no agents:** exits with message; wait for consul patrol.`
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
`, "`"+sol, world, "`", desc) + roleSection
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
	return fmt.Sprintf(`# Notification Handling

Notifications arrive at each turn boundary via UserPromptSubmit hook — a file-based queue drained each turn. Each notification may require dispatching more work or updating caravan state.

Format: %[1]s[NOTIFICATION] TYPE: Subject — Body%[1]s

## When to Act

Read notifications immediately at each turn. Most require a caravan status check and potentially dispatching next work.

## Notification Types

**MAIL** — Operator sent a message via %[2]s mail send%[1]s.
- Fields: %[1]ssubject%[1]s, %[1]sbody%[1]s
- Action: Read and respond. Check %[2]s mail inbox%[1]s for full context.

**AGENT_DONE** — An outpost resolved a writ.
- Fields: %[1]swrit_id%[1]s, %[1]sagent_name%[1]s, %[1]sbranch%[1]s, %[1]stitle%[1]s, %[1]smerge_request_id%[1]s
- Action: (1) Note resolution. (2) %[2]s caravan status%[1]s if part of caravan. (3) Dispatch next ready work if agents idle. MR is already in forge queue — no merge action needed.

**MERGED** — Forge successfully merged a writ.
- Fields: %[1]swrit_id%[1]s, %[1]smerge_request_id%[1]s, %[1]stitle%[1]s
- Action: (1) Check if this unblocks next-phase caravan items. (2) Dispatch newly-ready items. (3) Caravan auto-closes when all items merged. (4) Update brief.

**MERGE_FAILED** — Forge failed to merge.
- Fields: %[1]swrit_id%[1]s, %[1]smerge_request_id%[1]s, %[1]sreason%[1]s
- Side effects: writ reopened, escalation created, agent set idle.
- Action: (1) Check reason (conflicts or gate failure). (2) For conflicts: writ already open — re-dispatch. (3) For gate failure: investigate test/lint. (4) Re-dispatch when ready.

Always update your brief after handling notifications.
`, "`", "`"+sol)
}

func skillEnvoyNotificationHandling(_ SkillContext) string {
	return `# Notification Handling

Notifications arrive at each turn boundary via UserPromptSubmit hook — a file-based queue drained each turn. As an envoy you receive a focused subset; act on each promptly.

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
- Action: (1) Investigate the failure reason. (2) ` + "`git checkout main && git pull`" + `. (3) Fix conflicts. (4) Re-resolve.

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

func skillHandoff(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Handoff

Handoff cycles your session to a fresh one when context runs long. Your brief, worktree, and tether all persist — only the conversation context resets.

## Manual Handoff

Invoke handoff manually if context feels heavy and you want a clean start, or before a major phase transition in your work.

Before a manual handoff:
1. Commit any work-in-progress
2. Update your brief (.brief/memory.md) with current state, decisions, and next steps
3. Run %[1]s handoff%[2]s with a %[3]s describing what you were doing, what's completed, what's in progress, and what the next step is — this becomes the first thing your successor sees

## Commands

| Command | Description |
|---------|-------------|
| %[1]s handoff%[2]s | Hand off to a fresh session |

Options:
- %[3]s — **required**: summary for your successor (what you were doing, what's done, what's next)
- %[4]s — tag reason (compact, manual, health-check)
`, "`"+sol, "`",
		"`--summary=\"...\"`", "`--reason=compact`")
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

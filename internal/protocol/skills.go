package protocol

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nevinsm/sol/internal/adapter"
)

// BuildSkills generates skill content for the given context and returns it as
// []adapter.Skill without writing to disk.
// The returned slice has the same skills that InstallSkills would write.
func BuildSkills(ctx SkillContext) []adapter.Skill {
	names := RoleSkills(ctx.Role)
	result := make([]adapter.Skill, 0, len(names))
	for _, name := range names {
		content := generateSkill(name, ctx)
		if content == "" {
			continue
		}
		result = append(result, adapter.Skill{Name: name, Content: content})
	}
	return result
}

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

Memories are key-value pairs persisted in the sphere store and injected
automatically during prime — every successor session sees them. Use memories
for durable facts that remain true across many sessions: learned patterns,
recurring gotchas, important constraints. For current work state (what you're
doing right now, what's next), use your brief instead — the brief is for
context, memories are for knowledge.

Keep memories focused. Successor agents have limited context windows; a
hundred vague memories are worse than ten sharp ones. Retire stale memories
with %[3]s forget%[2]s when they're no longer useful.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s remember "key" "insight"%[2]s | Save with explicit key |
| %[1]s remember "insight"%[2]s | Save with auto-generated key |
| %[1]s memories%[2]s | Review stored memories |
| %[1]s forget "key"%[2]s | Remove outdated memory |

## Patterns

**Saving a discovery:** After finding a non-obvious fact (a quirky build step,
a brittle integration point), save it immediately:
%[1]s remember "key" "insight"%[2]s

**Pruning stale memories:** When a memory is no longer accurate, remove it
before it misleads your successor:
%[1]s memories%[2]s → review → %[1]s forget "old-key"%[2]s
`, "`"+sol, "`", "`"+sol)
}

func skillWritDispatch(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Writ Dispatch

As governor, you create work and send it to agents — you never do the implementation yourself. A writ is the unit of work: size it so an outpost can complete it in a coherent session. Too large and agents lose focus mid-task; too small and dispatch/merge overhead exceeds the value of the work.

## When to Use

- Create writs for any discrete, self-contained task an outpost can execute
- Use %[1]s cast%[3]s for disposable outposts (most tasks); use %[1]s tether%[3]s for persistent agents (envoy, governor)
- Priority 1 = urgent/blocking, 2 = normal, 3 = background
- Group related writs in a caravan when order matters (see caravan-management skill)

## Creating Work

| Command | Description |
|---------|-------------|
| %[1]s writ create --world=%[2]s --title="..." --description="..."%[3]s | Create a new writ |

Options: %[4]s, %[5]s (repeatable), %[6]s, %[7]s (JSON).

## Dispatching

| Command | Description |
|---------|-------------|
| %[1]s cast <id> --world=%[2]s%[3]s | Dispatch writ to a disposable outpost |
| %[1]s tether <id> --agent=<agent> --world=%[2]s%[3]s | Bind writ to a persistent agent |
| %[1]s untether <id> --agent=<agent> --world=%[2]s%[3]s | Unbind writ from agent |

Cast options: %[8]s (auto if omitted), %[9]s, %[10]s.

## Common Patterns

**Normal dispatch:** %[1]s writ create%[3]s → %[1]s cast <id>%[3]s → await AGENT_DONE notification.

**Batched work:** create all writs → %[1]s caravan create%[3]s → assign phases → %[1]s caravan commission%[3]s → consul dispatches ready items automatically.

**No idle agents:** writ stays in "ready" state — consul/sentinel picks it up on next patrol (every 5 min). No action needed.

## Failure Modes

- **No agents idle:** writ stays ready — consul dispatches on next patrol. Use %[1]s agent list%[3]s to check availability.
- **Dispatched to wrong agent:** %[1]s untether <id> --agent=<wrong>%[3]s then re-cast.
- **Writ too large:** agent loses coherence. Split into smaller writs and use a caravan to sequence them.
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

As governor, you monitor agent states, keep the managed repo current, watch service health, and escalate what you cannot handle. You don't implement code — you orchestrate the agents that do.

## When to Use

- Check status before dispatching to verify agents are available and forge isn't backed up
- Sync repo before dispatching work that depends on latest main
- Escalate when the issue is outside your control (infrastructure, permissions, external services)
- Handle locally when you can re-dispatch, adjust priorities, or unblock a stuck writ yourself

## Status

| Command | Description |
|---------|-------------|
| %[1]s status%[3]s | Sphere overview (processes, worlds, unread mail count) |
| %[1]s status %[2]s%[3]s | World detail (agents, writs, forge, nudge queue depth) |
| %[1]s agent list%[3]s | Agent states: working (active), idle (available), stuck (sentinel handles) |

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

## Common Patterns

**Before dispatching:** %[1]s status %[2]s%[3]s → verify idle agents → %[1]s world sync%[3]s if work depends on latest main → cast.

**After AGENT_DONE:** check caravan status → dispatch next-phase items → update brief.

## Failure Modes

- **Agent stuck:** sentinel respawns; sends RECOVERY_NEEDED after exhaustion. Re-dispatch or escalate if pattern repeats.
- **Forge backlogged:** check %[1]s forge status %[2]s%[3]s — usually self-clears. Escalate if forge process is down.
- **Sentinel/forge process down:** restart with commands above; escalate if restart fails (operator territory).
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

Mail is the channel for informational and conversational communication with
the operator. Use mail when you have something to report, a question to ask,
or a status update to share. Use %[1]s escalate%[2]s instead when you are
blocked and need the operator's help to continue — escalation is for urgent
needs, mail is for everything else.

For outbound messages, choose priority intentionally: 1 = urgent (operator
notified immediately), 2 = normal (default), 3 = low (batch-friendly, no
interruption).

## Commands

| Command | Description |
|---------|-------------|
| %[3]s mail inbox%[2]s | List pending messages |
| %[3]s mail read <message-id>%[2]s | Read a message (marks as read) |
| %[3]s mail ack <message-id>%[2]s | Acknowledge a message |
| %[3]s mail check%[2]s | Count unread messages |
| %[3]s mail send --to=<identity> --subject="..." --body="..."%[2]s | Send a message |

Options for send: %[4]s (1=urgent, 2=normal, 3=low), %[5]s.
`, "`"+sol+" escalate", "`", "`"+sol,
		"`--priority`",
		"`--no-notify`")
}

func skillWritManagement(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Writ Management

A writ moves through a fixed lifecycle: **open** → **tethered** → **working** → **done** → **closed**. "Done" means code complete with MR in the forge queue. "Closed" means merged. You interact with writs at creation, while working on them yourself, and when delegating to outposts.

## When to Use

- Create writs for work you plan to do yourself or dispatch to an outpost
- Tether yourself to a writ to track it as your active work
- Use %[1]s writ activate%[3]s to switch focus when managing multiple writs
- Labels for filtering/grouping; %[8]s for structured data the description can't capture

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

## Common Patterns

**Self-service work:** %[1]s writ create%[3]s → %[1]s tether <id>%[3]s → do work → %[1]s resolve%[3]s (writ moves to done → closed when merged).

**Multi-writ session:** tether several writs → %[1]s writ activate <id>%[3]s to switch focus → resolve each when done.

**Delegating after creation:** %[1]s writ create%[3]s → %[1]s cast <id>%[3]s (see dispatch skill).

## Failure Modes

- **Writ already tethered:** check %[1]s writ status%[3]s — untether first if reassigning.
- **Writ stuck in "done":** "closed" requires the MR to merge. Check forge queue for the MR.
- **Lost active writ:** %[1]s writ list --world=%[2]s%[3]s to find your tethered writs.
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

As envoy, you dispatch work to outpost agents when a task is better handled by a dedicated, isolated session than by doing it yourself. Outposts are disposable — they execute one writ and resolve. You remain responsible for reviewing their output after the writ is merged.

## When to Dispatch vs Do It Yourself

**Dispatch when:** the task is scoped and independent — it doesn't need your accumulated context, can be described completely in a writ, and benefits from a fresh isolated worktree.

**Do it yourself when:** the task is exploratory, requires iterative interaction, or depends on context only you hold from the current session.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s cast <id> --world=%[2]s%[3]s | Dispatch writ to an outpost agent |
| %[1]s agent list%[3]s | Check agent availability before dispatching |

Cast options: %[4]s (auto if omitted), %[5]s, %[6]s.

## Common Patterns

**Standard dispatch:** check agents (%[1]s agent list%[3]s) → %[1]s cast <id>%[3]s → continue your work → review on AGENT_DONE notification.

**No idle agents:** writ stays ready — consul dispatches on next patrol. Continue with other work.

## Failure Modes

- **No agents available:** writ stays ready. If urgent, wait for AGENT_DONE notification before re-dispatching.
- **Wrong agent selected:** %[1]s untether <id> --agent=<wrong>%[3]s then re-cast.
- **Outpost gets stuck:** you receive a RECOVERY_NEEDED notification — re-dispatch or escalate if pattern repeats.
`, "`"+sol, world, "`",
		"`--agent`",
		"`--workflow`",
		"`--account`")
}

func skillHandoff(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Handoff

Handoff cycles your session to a fresh one, resetting the conversation context
while preserving your brief, worktree, and tether. Use it when context feels
heavy (responses slow, compression artifacts appearing), at a major phase
transition in your work, or after a long run where early context has been
compressed beyond usefulness.

## When to Handoff

- Context feels heavy — compression artifacts or noticeably slow responses
- You've completed a major phase and are starting a new one
- You've been running long enough that earliest context is compressed

Do not handoff mid-task without committing first. Your successor inherits only
what's in git and your brief.

## Procedure

**Update brief BEFORE running handoff.** If your session crashes during the
handoff procedure, your brief is all your successor gets. Do not rely on the
summary alone.

1. Commit any work-in-progress with a meaningful message
2. Update %[1]s.brief/memory.md%[2]s: current state, decisions made, what's
   done, what's in progress, and the exact next step
3. Run %[3]s handoff%[2]s with a clear %[4]s

## Commands

| Command | Description |
|---------|-------------|
| %[3]s handoff%[2]s | Hand off to a fresh session |

Options:
- %[4]s — **required**: summary for your successor (what you were doing, what's done, what's next)
- %[5]s — tag reason (compact, manual, health-check)

## Failure Modes

**Summary too vague** — Successor starts confused. Be specific: name the file
you were editing, the exact command you ran last, the decision you made.

**Brief not updated before handoff** — Session crashes mid-handoff. Successor
gets stale brief. Always update brief first, then hand off.

**Handoff with unstaged changes** — Successor inherits a dirty worktree with
no context. Commit everything (even as WIP) before handing off.
`, "`", "`", "`"+sol,
		"`--summary=\"...\"`", "`--reason=compact`")
}

func skillStatusMonitoring(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# Status Monitoring

Status commands give you a live picture of what's happening across the sphere.
Check status proactively — before dispatching work (to confirm agents are
available and forge is healthy), after receiving a notification (to see the
full picture), or when something feels stuck (a writ that should be done
but isn't).

## When to Check

- **Before dispatching:** Confirm agents are available and forge is healthy
- **After MERGED/MERGE_FAILED notification:** See what's now unblocked or needs attention
- **When something feels stuck:** Check writ status, forge queue, agent health

Status indicators: %[4]s✓%[5]s = healthy/running, %[4]s○%[5]s = not running, %[4]s?%[5]s = unknown

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

## Common Patterns

**Before dispatching work:**
%[1]s status%[3]s → confirm agents available and forge healthy → dispatch

**Investigating a stuck writ:**
%[1]s writ status <id> --world=%[2]s%[3]s → %[1]s forge queue --world=%[2]s%[3]s → %[1]s sentinel status --world=%[2]s%[3]s
`, "`"+sol, world, "`", "`", "`")
}

func skillWorldOperations(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`# World Operations

World operations let you sync the managed repo, inspect world health, and
query the governor. As an envoy, you'll mainly use world sync and governor
queries. Service lifecycle commands (install, uninstall, down) are usually
operator territory — use them only if the operator has asked or you're doing
explicit infrastructure work.

## When to Use

- **World sync:** Before starting work that depends on latest main, or when
  merge conflicts appear from an outdated base
- **World summary:** When you need a current-state snapshot — reads a file,
  doesn't wake the governor
- **World query:** When you need live governor context not captured in the
  summary — wakes the session if idle, so prefer summary when possible

## Commands

| Command | Description |
|---------|-------------|
| %[1]s world sync --world=%[2]s%[3]s | Sync managed repo from upstream |
| %[1]s world status %[2]s%[3]s | World health overview |
| %[1]s world summary %[2]s%[3]s | Read governor's world summary (cheap) |
| %[1]s world query %[2]s "question"%[3]s | Query the governor directly (wakes session) |

## Service Lifecycle

| Command | Description |
|---------|-------------|
| %[1]s service status%[3]s | Show all sphere daemon status |
| %[1]s service install%[3]s | Install systemd units |
| %[1]s service uninstall%[3]s | Remove systemd units |
| %[1]s down --all%[3]s | Stop all world services |

## Common Patterns

**Syncing before starting work:**
%[1]s world sync --world=%[2]s%[3]s → pull latest in your worktree

**Checking world state before dispatching:**
%[1]s world summary %[2]s%[3]s → read snapshot → run %[1]s world query%[3]s only if you need live detail

## Failure Modes

**Sync fails with conflict:** The managed repo has diverged. Check
%[1]s world status %[2]s%[3]s and escalate if operator intervention is needed.

**Governor not responding:** %[1]s world query%[3]s hangs or errors. Check
%[1]s governor status --world=%[2]s%[3]s. The summary may still be readable
even when the governor is idle — try that first.
`, "`"+sol, world, "`")
}

func skillWorldQueries(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# World Queries

World queries let you gather intelligence across worlds before planning or
advising the autarch. The key discipline is cost awareness: you have three
tiers of context ranging from free to expensive, and good planning uses
them in order.

**Three tiers (cheapest to most expensive):**
1. **Brief** (%[1]s$SOL_HOME/chancellor/.brief/memory.md%[2]s) — your own
   accumulated knowledge. Free to read. Always check first.
2. **World summaries** (%[3]s world summary <world>%[2]s) — governor-maintained
   snapshots. Cheap — reads a file, doesn't wake anyone.
3. **Live queries** (%[3]s world query <world> "question"%[2]s) — asks the
   governor directly. Expensive — wakes the governor session if idle.

A planning session that wakes 5 governors to ask questions already answered
in their summaries is wasteful. Prefer summaries. Use live queries only for
specific unknowns not covered by tier 1 or 2.

## When to Use

- Check your brief first for everything you already know about a world
- Read world summaries for current-state snapshots before any planning work
- Run live queries only when summaries don't answer your specific question
- Batch live queries — if you need multiple things from one governor, ask once

## Commands

| Command | Description |
|---------|-------------|
| %[3]s world list%[2]s | List all worlds |
| %[3]s world summary <world>%[2]s | Read governor's world summary (cheap) |
| %[3]s world query <world> "question"%[2]s | Query a governor directly (wakes session) |
| %[3]s world export <world>%[2]s | Export full world state |

## Common Patterns

**Gathering context before a planning session:**
Read brief → %[3]s world list%[2]s → %[3]s world summary <world>%[2]s for each
relevant world → identify gaps → %[3]s world query%[2]s only for specific unknowns

**Batching a live query:**
Combine questions: %[3]s world query <world> "What agents are available, what
writs are in progress, and are there any blockers?"%[2]s

## Failure Modes

**Governor not running:** %[3]s world query%[2]s fails or times out. Check
%[3]s world status <world>%[2]s. The summary may still be readable even if
the governor is idle — try that first.

**Stale summary:** Summary hasn't been updated recently. If the timestamp looks
old and you need current state, fall back to a live query. Note it in your
brief so future sessions know.
`, "`", "`", "`"+sol)
}

func skillWritPlanning(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`# Writ Planning

Writ planning translates an autarch's initiative into a structured set of
writs and caravans ready for dispatch. The planning session itself is cheap —
gather context from briefs and summaries, design the work breakdown, present
for approval. Only dispatch after the autarch confirms the plan.

## Planning Workflow

1. **Gather context (tiers 1+2):** Read your brief, then read world summaries
   for each relevant world. This costs nothing and covers most questions.
2. **Identify gaps (tier 3):** Use live queries only for specific unknowns not
   answered by summaries. Batch your questions per governor.
3. **Decompose:** Break the initiative into writs. Identify dependencies, group
   related work into caravans, assign to appropriate worlds.
4. **Present for approval:** Show the autarch the plan before dispatching.
   Dispatch only after explicit approval.

## When to Use

- When the autarch describes a multi-writ or multi-world initiative
- When work needs phase sequencing (phase-gated caravans)
- When scope is large enough that speculative dispatch would be risky

## Creating Writs

| Command | Description |
|---------|-------------|
| %[1]s writ create --world=<world> --title="..." --description="..."%[2]s | Create writ in any world |

Options: %[3]s, %[4]s, %[5]s, %[6]s (JSON).

## Managing Caravans

| Command | Description |
|---------|-------------|
| %[1]s caravan create "name" <id> [<id>...] --world=<world>%[2]s | Create caravan with items |
| %[1]s caravan commission <id>%[2]s | Mark caravan as commissioned |
| %[1]s caravan launch <id> --world=<world>%[2]s | Dispatch all ready items |
| %[1]s caravan status [<caravan-id>]%[2]s | Check caravan progress |
| %[1]s caravan dep add <caravan-id> <dep-id>%[2]s | Add inter-caravan dependency |

## Common Patterns

**Planning a phased initiative:**
Brief → summaries → identify worlds and phases → create writs → group into
caravans with phase gates → present to autarch → dispatch on approval

**Simple multi-world task:**
Summaries for each world → create one writ per world → single caravan →
commission → present → launch

## Failure Modes

**Dispatching without approval** — Never dispatch speculatively. Present the
plan, wait for autarch confirmation. If unavailable, save the plan to your
brief and wait.

**Over-querying governors** — If you're waking governors to answer questions
their summaries already cover, stop. Re-read the summaries. Prefer stale
summary data over expensive live queries for planning purposes.

**Writ descriptions too thin** — Outpost agents execute from the description
alone. A vague description produces vague work. Invest time in clear,
complete writ descriptions before dispatch.
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

package protocol

import (
	"fmt"
	"os"

	"github.com/nevinsm/sol/internal/adapter"
)

// BuildSkills generates skill content for the given context and returns it as
// []adapter.Skill without writing to disk. Returns an error if the role is unknown.
func BuildSkills(ctx SkillContext) ([]adapter.Skill, error) {
	names, err := RoleSkills(ctx.Role)
	if err != nil {
		return nil, err
	}
	result := make([]adapter.Skill, 0, len(names))
	for _, name := range names {
		content := generateSkill(name, ctx)
		if content == "" {
			continue
		}
		result = append(result, adapter.Skill{Name: name, Content: content})
	}
	return result, nil
}

// SkillContext holds common fields used when generating skill content for agents.
type SkillContext struct {
	World        string
	AgentName    string
	SolBinary    string   // path to sol binary (defaults to "sol")
	Role         string   // outpost, forge, envoy
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

func init() {
	// Validate no duplicate skill names within any role.
	for role, skills := range roleSkillsMap {
		seen := make(map[string]bool, len(skills))
		for _, name := range skills {
			if seen[name] {
				panic(fmt.Sprintf("skills: duplicate skill %q in role %q", name, role))
			}
			seen[name] = true
		}
	}
}

// roleSkillsMap defines which skills belong to each role.
var roleSkillsMap = map[string][]string{
	"outpost": {"resolve-and-handoff"},
	"envoy":   {"resolve-and-submit", "writ-management", "dispatch", "handoff", "status-monitoring", "caravan-management", "world-operations", "notification-handling", "mail"},
}

// RoleSkills returns the skill names for a given role.
// Returns an error if the role is not recognized.
func RoleSkills(role string) ([]string, error) {
	skills, ok := roleSkillsMap[role]
	if !ok {
		return nil, fmt.Errorf("skills: unknown role %q — no skills installed", role)
	}
	// Return a copy to prevent mutation.
	out := make([]string, len(skills))
	copy(out, skills)
	return out, nil
}


// generateSkill dispatches to the appropriate skill content generator.
func generateSkill(name string, ctx SkillContext) string {
	switch name {
	case "resolve-and-handoff":
		return skillResolveAndHandoff(ctx)
	case "resolve-and-submit":
		return skillResolveAndSubmit(ctx)
	case "caravan-management":
		return skillCaravanManagement(ctx)
	case "notification-handling":
		return skillEnvoyNotificationHandling(ctx)
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
	default:
		fmt.Fprintf(os.Stderr, "protocol: unknown skill name %q\n", name)
		return ""
	}
}

// --- Skill content generators ---

func skillResolveAndHandoff(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`---
name: resolve-and-handoff
description: Submit completed work and end your session — clears tether and ends session (code writs also push branch and create MR)
---

# Resolve & Handoff

%[1]s resolve%[2]s is your **mandatory final step** — it clears the tether and signals work complete.

**Behavior varies by writ kind:**
- **Code writs:** resolve pushes your branch, creates a merge request, clears the tether, and ends your session.
- **Analysis writs (kind=analysis):** resolve closes the writ directly — no branch or MR is created. The session ends.

**Session behavior:** For outpost agents, the session ends and the worktree is cleaned up after resolve. Persistent roles (envoy) keep their session alive after resolve.

## When to Use

- Work is complete → %[1]s resolve%[2]s
- Stuck or blocked → %[1]s escalate%[3]s instead of resolving

## Completing Work

| Command | Description |
|---------|-------------|
| %[1]s resolve%[2]s | Clear tether, signal complete. Code writs: pushes branch + creates MR. Analysis writs: closes directly. **Mandatory final step.** |
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

**Context running long:** commit → %[1]s handoff --summary="..."%[4]s

## Failure Modes

- **No tether found** → exit 1. Manual re-cast or tether restore needed.
- **git push fails** → NON-FATAL. Resolve exits 0. MR in "failed" state; forge handles it. Writ reopens for re-dispatch.
- **Database locked** → exit 1. Transient — retry.
- Resolve is idempotent — safe to call multiple times.
`,
		"`"+sol, flagsForResolve(ctx), flagsForEscalate(ctx), flagsForHandoff(ctx),
		"`--summary=\"...\"`", "`--reason=compact`")
}

func skillResolveAndSubmit(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`---
name: resolve-and-submit
description: Submit completed work through the forge pipeline — pushes branch, creates merge request, keeps session alive
---

# Resolve & Submit

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

**Freeform work (no tether):** resolve requires an active tether. If you did freeform work without an assigned writ, self-tether before resolving:
1. %[1]s writ create --world=%[7]s --title="..." --description="..." --kind=code%[4]s — creates the writ, prints the ID
2. %[1]s tether <writ-id> --agent=%[8]s%[4]s — binds the writ to you
3. %[1]s writ activate <writ-id>%[4]s — makes it your active writ
4. %[1]s resolve%[2]s — now works normally

**More tethered writs:** after resolve you stay "working" — activate the next writ and continue.

**No remaining work:** after resolve you go idle — check for new writs or await instructions.

## Failure Modes

- **git push fails** → NON-FATAL. Resolve exits 0. MR in "failed" state; writ reopens. Pull main and re-resolve.
- **No tether found** → exit 1. Self-tether first (see Freeform work pattern above), or check %[6]s.
- **Database locked** → exit 1. Transient — retry.
- Resolve is idempotent — safe to call multiple times.
`,
		"`"+sol, " --world="+ctx.World+" --agent="+ctx.AgentName+"`",
		"`git push`",
		"`",
		"`git checkout main && git pull`",
		"`sol writ list`",
		ctx.World,
		ctx.AgentName)
}


func skillCaravanManagement(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	fmDesc := "Sequence related writs across phases — group, order, and track batch progress"
	desc := "Commands for grouping and sequencing related writs."
	var roleSection string
	switch ctx.Role {
	case "envoy":
		desc = "Commands for sequencing your own multi-step work."
		roleSection = `
## Mental Model

A caravan sequences your own multi-step work across phases. Items in the same phase can run in parallel; items in phase N wait until all prior-phase items are **closed** (fully merged). You are the single worker — resolve each phase and consul advances to the next.

## Phases vs Dependencies

**Phases** gate your work into sequential stages — useful when later work builds on earlier work across many files. **Writ dependencies** (` + "`sol writ dep add <from> <to>`" + `) handle targeted ordering where one writ's changes must merge before another starts, but the rest can proceed. Prefer dependencies over phases when only a few writs have conflicts — it avoids serializing unrelated work.

## Common Patterns

**Setup:** create writs → ` + "`sol caravan create`" + ` → assign phases via ` + "`sol caravan set-phase`" + ` → ` + "`sol caravan commission`" + `.

**Working:** resolve phase 0 writs → consul detects closed items → dispatches phase 1 → repeat.

**Check progress:** ` + "`sol caravan status`" + ` after each resolve to see what's next.

## Failure Modes

- **Phase won't advance:** prior items must be "closed" (merged), not just "done". Check forge queue.
- **` + "`sol caravan launch`" + ` with no agents:** exits with message; wait for consul patrol.`
	}
	return fmt.Sprintf(`---
name: caravan-management
description: %[5]s
---

# Caravan Management

%[4]s

## Commands

| Command | Description |
|---------|-------------|
| %[1]s caravan create "name" <id> [<id>...] --world=%[2]s%[3]s | Create caravan with items |
| %[1]s caravan add <caravan-id> <id> --world=%[2]s%[3]s | Add item to caravan |
| %[1]s caravan status [<caravan-id>]%[3]s | Check caravan progress |
| %[1]s caravan launch <caravan-id> --world=%[2]s%[3]s | Dispatch all ready items |
| %[1]s caravan commission <caravan-id>%[3]s | Mark caravan as commissioned |
| %[1]s caravan set-phase <caravan-id> [<item-id>] <phase>%[3]s | Set phase for one item |
| %[1]s caravan set-phase <caravan-id> --all <phase>%[3]s | Set phase for all items |
| %[1]s caravan check <caravan-id>%[3]s | Check phase-gate readiness |
| %[1]s caravan list%[3]s | List all caravans |

## Dependencies

| Command | Description |
|---------|-------------|
| %[1]s caravan dep add <caravan-id> <dep-id>%[3]s | Add inter-caravan dependency |
| %[1]s caravan dep list <caravan-id>%[3]s | List caravan dependencies |
`, "`"+sol, world, "`", desc, fmDesc) + roleSection
}

func skillEnvoyNotificationHandling(_ SkillContext) string {
	return `---
name: notification-handling
description: Handle system notifications — MAIL — and take appropriate action
---

# Notification Handling

Notifications arrive at each turn boundary via UserPromptSubmit hook — a file-based queue drained each turn. As an envoy you receive MAIL notifications; act on each promptly.

Format: ` + "`[NOTIFICATION] TYPE: Subject — Body`" + `

## Notification Types

**MAIL** — Operator or another agent sent a message via ` + "`sol mail send`" + `.
- Fields: ` + "`subject`" + `, ` + "`body`" + `
- Action: Read and acknowledge. The sender is communicating directly — respond to the content.

Always update your brief after handling a notification.
`
}

func skillMail(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`---
name: mail
description: Inter-agent and operator messaging — send, receive, and acknowledge messages
---

# Mail

Mail is the channel for inter-agent and operator messaging. Use mail to send
status updates, reports, and questions to the operator or other agents. Use
%[1]s escalate "description"%[2]s instead when you are blocked and need the
operator's help to continue — escalation is for urgent blockers, mail is for
everything else.

**Identity auto-detection:** %[3]s mail inbox%[2]s, %[3]s mail check%[2]s,
%[3]s mail read%[2]s, and %[3]s mail ack%[2]s automatically detect your
identity from SOL_WORLD/SOL_AGENT env vars (set as world/agent, e.g.
%[1]ssol-dev/Nova%[2]s). Pass %[6]s to override. Sender is also
auto-detected for %[3]s mail send%[2]s.

**Recipient format:** Agent recipients are stored as world/agent. You can pass
a plain agent name with %[3]s mail send --to=<agent-name>%[2]s and it is
resolved to world/agent using SOL_WORLD. Pass %[1]sautarch%[2]s to reach
the operator.

For outbound messages, choose priority intentionally: 1 = urgent (notified
immediately), 2 = normal (default), 3 = low (batch-friendly, no interruption).

## Commands

| Command | Description |
|---------|-------------|
| %[3]s mail inbox%[2]s | List pending messages (identity auto-detected) |
| %[3]s mail read <message-id>%[2]s | Read a message (marks as read) |
| %[3]s mail ack <message-id>%[2]s | Acknowledge a message |
| %[3]s mail check%[2]s | Count unread messages (identity auto-detected) |
| %[3]s mail send --to=<recipient> --subject="..." --body="..."%[2]s | Send a message |

Options for send: %[4]s (1=urgent, 2=normal, 3=low), %[5]s.

## Common Patterns

**Status update to operator:** %[3]s mail send --to=autarch --subject="..." --body="..."%[2]s with priority 3 — informational, no interruption needed.

**Agent-to-agent message:** %[3]s mail send --to=<agent-name> --subject="..." --body="..."%[2]s — plain name is resolved to world/agent using SOL_WORLD.

**Question that blocks your work:** Use %[1]s escalate "description"%[2]s instead of mail — escalation is for blockers, mail is for everything else.

**Check and process your inbox:** %[3]s mail inbox%[2]s → %[3]s mail read <id>%[2]s → respond or act → %[3]s mail ack <id>%[2]s.

## Failure Modes

- **Priority 1 overuse:** Reserve urgent priority for genuinely time-sensitive communication. Frequent urgent mail trains the operator to ignore it.
- **Unacknowledged messages:** %[3]s mail check%[2]s shows unread count. Acknowledge messages after reading to keep the inbox clean.
- **Wrong inbox:** If SOL_WORLD/SOL_AGENT are set, inbox auto-detects your identity. Use %[6]s to override if needed.
`, "`"+sol, "`", "`"+sol,
		"`--priority`",
		"`--no-notify`",
		"`--identity`")
}

func skillWritManagement(ctx SkillContext) string {
	sol := ctx.sol()
	world := ctx.World
	return fmt.Sprintf(`---
name: writ-management
description: Create, tether, activate, and track writs through their lifecycle
---

# Writ Management

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
	return fmt.Sprintf(`---
name: dispatch
description: Dispatch work to outpost agents — cast writs to isolated sessions for independent tasks
---

# Dispatch

As envoy, you dispatch work to outpost agents when a task is better handled by a dedicated, isolated session than by doing it yourself. Outposts are disposable — they execute one writ and resolve. You remain responsible for reviewing their output after the writ is merged.

## When to Dispatch vs Do It Yourself

**Dispatch when:** the task is scoped and independent — it doesn't need your accumulated context, can be described completely in a writ, and benefits from a fresh isolated worktree.

**Do it yourself when:** the task is exploratory, requires iterative interaction, or depends on context only you hold from the current session.

## Commands

| Command | Description |
|---------|-------------|
| %[1]s cast <id> --world=%[2]s%[3]s | Dispatch writ to an outpost agent |
| %[1]s agent list%[3]s | Check agent availability before dispatching |

Cast options: %[4]s (auto if omitted), %[5]s, %[6]s, %[7]s (pass template variables to workflow manifests).

## Common Patterns

**Standard dispatch:** check agents (%[1]s agent list%[3]s) → %[1]s cast <id>%[3]s → continue your work → review on AGENT_DONE notification.

**No idle agents:** writ stays ready — consul dispatches on next patrol. Continue with other work.

## Failure Modes

- **No agents available:** writ stays ready. If urgent, wait for AGENT_DONE notification before re-dispatching.
- **Wrong agent selected:** %[1]s untether <id> --agent=<wrong>%[3]s then re-cast.
- **Outpost gets stuck:** check writ status with ` + "`" + `sol writ status <id>` + "`" + ` — re-dispatch or escalate if pattern repeats.
`, "`"+sol, world, "`",
		"`--agent`",
		"`--guidelines`",
		"`--account`",
		"`--var`")
}

func skillHandoff(ctx SkillContext) string {
	sol := ctx.sol()
	return fmt.Sprintf(`---
name: handoff
description: Cycle to a fresh session — preserves brief, worktree, and tether across the transition
---

# Handoff

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
	return fmt.Sprintf(`---
name: status-monitoring
description: Check sphere and world health — agents, writs, forge queue, service status
---

# Status Monitoring

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
	return fmt.Sprintf(`---
name: world-operations
description: Sync the managed repo and inspect world health
---

# World Operations

World operations let you sync the managed repo and inspect world health.
Service lifecycle commands (install, uninstall, down) are usually
operator territory — use them only if the operator has asked or you're doing
explicit infrastructure work.

## When to Use

- **World sync:** Before starting work that depends on latest main, or when
  merge conflicts appear from an outdated base
- **World status:** Check overall world health, agent states, forge queue

## Commands

| Command | Description |
|---------|-------------|
| %[1]s world sync --world=%[2]s%[3]s | Sync managed repo from upstream |
| %[1]s world status %[2]s%[3]s | World health overview |

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
%[1]s world status %[2]s%[3]s → check agents, forge, sentinel status

## Failure Modes

**Sync fails with conflict:** The managed repo has diverged. Check
%[1]s world status %[2]s%[3]s and escalate if operator intervention is needed.
`, "`"+sol, world, "`")
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

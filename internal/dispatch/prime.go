package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nevinsm/sol/internal/handoff"
	"github.com/nevinsm/sol/internal/startup"
	"github.com/nevinsm/sol/internal/store"
	"github.com/nevinsm/sol/internal/tether"
)

// handoffMaxAge bounds how old an unconsumed handoff state file may be
// before Prime treats it as stale/orphaned rather than live context to
// inject. Without this, a handoff file left behind by a long-dead session
// (or one written for a since-abandoned writ) would get replayed into a
// successor session's prime forever, since MarkConsumed only fires along
// the injection path (Defect 2, 2026-08-19 handoff audit).
const handoffMaxAge = 24 * time.Hour

// PrimeResult holds the output of a prime operation.
type PrimeResult struct {
	Output string
}

// Prime assembles execution context from durable state and returns it.
// It also performs four startup mutations:
//   - ClearResolveLocksForAgent: clears any stale resolve lock from a prior interrupted session
//   - startup.ClearResumeState: clears any leaked resume state from a prior successful self-invoked handoff
//   - handoff.RemoveMarker: removes the handoff marker after reading it
//   - handoff.MarkConsumed: marks a handoff state as consumed so it is not replayed
//
// These side effects are non-idempotent: Prime must be called exactly once per session start.
func Prime(world, agentName, role string, worldStore WorldStore, compact ...bool) (*PrimeResult, error) {
	if role == "" {
		role = "outpost"
	}

	// Clear any leaked resume_state.json (Defect 1, 2026-08-19 handoff
	// audit). In a self-invoked handoff, respawn-pane -k kills the calling
	// process at startup.Launch's session-op step, so handoff.Exec's own
	// post-launch cleanup path is dead on the success path and never runs —
	// every successful self-invoked handoff leaves resume_state.json on
	// disk. This is the successor-side backstop: prefect's Respawn always
	// reads AND clears resume state before starting a session, so any
	// resume state still present by the time ANY Prime call (any role, any
	// mode) observes it is by definition leaked from an earlier cycle, not
	// live context for the running session — Respawn already consumed and
	// cleared it before this session (or this compaction event within it)
	// could have started. Runs before the compact/forge early returns below
	// so the backstop covers every role and mode. Best-effort — a failure
	// here shouldn't block priming.
	if err := startup.ClearResumeState(world, agentName, role); err != nil {
		fmt.Fprintf(os.Stderr, "prime: failed to clear resume state: %v\n", err)
	}

	// Compact mode: short focus reminder during native context compaction.
	if len(compact) > 0 && compact[0] {
		return primeCompact(world, agentName, role, worldStore)
	}

	// Forge gets a special prime context.
	if role == "forge" {
		return primeForge(world)
	}

	// Check for stale resolve lock(s) (previous session died mid-resolve).
	if IsResolveInProgress(world, agentName, role) {
		ClearResolveLocksForAgent(world, agentName, role) // clean up stale lock(s)
		fmt.Fprintf(os.Stderr, "prime: detected stale resolve lock — previous session interrupted during resolve\n")
	}

	// Check for handoff marker (loop prevention).
	// If present, the agent was just handed off — prepend a warning and remove the marker.
	// The reason distinguishes compact recovery ("compact") from other handoffs.
	freshSession := false
	compactRecovery := false
	markerTS, markerReason, _ := handoff.ReadMarker(world, agentName, role)
	if !markerTS.IsZero() {
		freshSession = true
		compactRecovery = markerReason == "compact"
		// Remove marker after reading — the message will be in Claude's context.
		handoff.RemoveMarker(world, agentName, role)
	}

	// Read all tethered writs (directory-based).
	allWritIDs, err := tether.List(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(allWritIDs) == 0 {
		return primeUntethered(world, agentName, role)
	}

	// Determine the active writ ID.
	// For outpost agents (role="outpost"): single tether, always active.
	// For persistent agents: read active_writ from sphere store, validated
	// against the tether list.
	isPersistent := persistentRoles[role]
	activeWritID := resolveActiveWrit(world, agentName, isPersistent, allWritIDs)

	// No active writ for persistent agent: summary + wait message.
	if isPersistent && activeWritID == "" {
		return primeNoActiveWrit(world, agentName, allWritIDs, worldStore)
	}

	// Get the active writ.
	item, err := worldStore.GetWrit(activeWritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", activeWritID, err)
	}

	// Check for handoff context (session continuity).
	handoffState, err := handoff.Read(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to read handoff state: %w", err)
	}

	// Guard against injecting a handoff state that no longer applies: either
	// it was written for a different writ (the agent's active writ changed
	// since handoff — e.g. a persistent agent got reassigned) or it's old
	// enough to be orphaned rather than live continuity (Defect 2,
	// 2026-08-19 handoff audit). Skip injection and mark it consumed so the
	// file stops being a landmine for every future prime.
	if handoffState != nil && !handoffState.Consumed {
		age := time.Since(handoffState.HandedOffAt)
		mismatch := handoffState.WritID != "" && handoffState.WritID != activeWritID
		stale := age > handoffMaxAge
		if mismatch || stale {
			fmt.Fprintf(os.Stderr, "prime: skipping handoff injection (mismatch=%v stale=%v handoff_writ=%q active_writ=%q age=%s)\n",
				mismatch, stale, handoffState.WritID, activeWritID, age.Round(time.Second))
			if markErr := handoff.MarkConsumed(world, agentName, role); markErr != nil {
				fmt.Fprintf(os.Stderr, "prime: failed to mark stale/mismatched handoff consumed: %v\n", markErr)
			}
			handoffState = nil
		}
	}

	var result *PrimeResult

	if handoffState != nil && !handoffState.Consumed {
		if compactRecovery {
			// Compact recovery: lightweight prime that trusts compressed context
			// from the predecessor session (via --continue). Omits the full work
			// item description to save tokens.
			result, err = primeCompactRecovery(world, agentName, item, handoffState)
		} else {
			result, err = primeWithHandoff(world, agentName, item, handoffState)
		}
		if err != nil {
			return nil, err
		}
		// Mark handoff as consumed (durable — file remains for crash recovery).
		if markErr := handoff.MarkConsumed(world, agentName, role); markErr != nil {
			fmt.Fprintf(os.Stderr, "prime: failed to mark handoff consumed: %v\n", markErr)
		}
	} else {
		// Standard prime — inject writ context and guidelines if present.
		var b strings.Builder
		fmt.Fprintf(&b, "=== WORK CONTEXT ===\n")
		fmt.Fprintf(&b, "Agent: %s (world: %s)\n", agentName, world)
		fmt.Fprintf(&b, "Writ: %s\n", item.ID)
		fmt.Fprintf(&b, "Title: %s\n", item.Title)
		fmt.Fprintf(&b, "Status: %s\n", item.Status)
		fmt.Fprintf(&b, "\nDescription:\n%s\n", item.Description)

		// Inject guidelines if .guidelines.md exists in the worktree.
		worktreeDir := WorktreePath(world, agentName)
		guidelinesPath := filepath.Join(worktreeDir, ".guidelines.md")
		if guidelinesContent, err := os.ReadFile(guidelinesPath); err == nil && len(guidelinesContent) > 0 {
			fmt.Fprintf(&b, "\n--- GUIDELINES ---\n")
			b.Write(guidelinesContent)
			fmt.Fprintf(&b, "\n--- END GUIDELINES ---\n")
		} else {
			fmt.Fprintf(&b, "\nInstructions:\n")
			fmt.Fprintf(&b, "Execute this writ. When complete, run: sol resolve\n")
			fmt.Fprintf(&b, "If stuck, run: sol escalate \"description\"\n")
		}

		fmt.Fprintf(&b, "=== END CONTEXT ===")
		result = &PrimeResult{Output: b.String()}
	}

	// Append background writ summaries for persistent agents with multiple tethers.
	if isPersistent && len(allWritIDs) > 1 && result != nil {
		bgSection := primeBackgroundWrits(activeWritID, allWritIDs, worldStore)
		if bgSection != "" {
			result.Output += bgSection
		}
	}

	// Prepend fresh-session warning if this is a non-compact handoff continuation.
	// Compact recovery has its own framing — no need for the generic warning.
	if freshSession && !compactRecovery && result != nil {
		result.Output = "NOTE: You are a fresh session (handoff from predecessor). Continue working — do NOT call sol handoff.\n\n" + result.Output
	}

	return result, nil
}

// primeUntethered handles the no-tether prime path. Before reporting
// "No work tethered", it checks for an unconsumed, fresh handoff state:
// untethered agents (envoys, primarily, which spend most of their time
// with no tether) previously had their handoff summary vanish entirely,
// since there was no writ to attach it to and this early return happened
// before the handoff file was ever read (Defect 2, 2026-08-19 handoff
// audit). A fresh, unconsumed summary is injected and marked consumed;
// stale (>24h) or already-consumed state is left untouched (stale state
// is marked consumed here too, so it doesn't linger as a landmine for the
// next untethered prime).
func primeUntethered(world, agentName, role string) (*PrimeResult, error) {
	const base = "No work tethered"

	state, err := handoff.Read(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to read handoff state: %w", err)
	}
	if state == nil || state.Consumed {
		return &PrimeResult{Output: base}, nil
	}

	if age := time.Since(state.HandedOffAt); age > handoffMaxAge {
		fmt.Fprintf(os.Stderr, "prime: skipping stale untethered handoff (age=%s)\n", age.Round(time.Second))
		if markErr := handoff.MarkConsumed(world, agentName, role); markErr != nil {
			fmt.Fprintf(os.Stderr, "prime: failed to mark stale handoff consumed: %v\n", markErr)
		}
		return &PrimeResult{Output: base}, nil
	}

	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n--- PREVIOUS SESSION SUMMARY ---\n")
	b.WriteString(state.Summary)
	b.WriteString("\n--- END SUMMARY ---")

	if markErr := handoff.MarkConsumed(world, agentName, role); markErr != nil {
		fmt.Fprintf(os.Stderr, "prime: failed to mark handoff consumed: %v\n", markErr)
	}

	return &PrimeResult{Output: b.String()}, nil
}

// primeCompact generates a short focus reminder for context compaction.
// Reads the tether to find the active writ, looks up its title, and checks
// workflow state. Returns a concise message to keep the agent on track.
func primeCompact(world, agentName, role string, worldStore WorldStore) (*PrimeResult, error) {
	// Read tethered writs.
	allWritIDs, err := tether.List(world, agentName, role)
	if err != nil {
		return nil, fmt.Errorf("failed to list tethers: %w", err)
	}
	if len(allWritIDs) == 0 {
		// Persistent roles (envoy) may have no tether during freeform
		// conversation — return a role-appropriate grounding reminder.
		if role == "envoy" {
			return &PrimeResult{Output: fmt.Sprintf(
				"[sol] Context compaction in progress. You are envoy %s in world %s.\nYour persistent memory is at <envoyDir>/memory/MEMORY.md (Claude Code auto-memory) — use /memory to review.\nContinue the current conversation.",
				agentName, world)}, nil
		}
		return &PrimeResult{Output: "[sol] Context compaction in progress. No active work tethered."}, nil
	}

	// Determine active writ, validated against the tether list.
	activeWritID := resolveActiveWrit(world, agentName, persistentRoles[role], allWritIDs)
	if activeWritID == "" {
		return &PrimeResult{Output: "[sol] Context compaction in progress. No active writ."}, nil
	}

	// Look up writ title.
	item, err := worldStore.GetWrit(activeWritID)
	if err != nil {
		return nil, fmt.Errorf("failed to get writ %q: %w", activeWritID, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[sol] Context compaction in progress. Stay focused on your current assignment.\n\n")
	fmt.Fprintf(&b, "Writ: %s — %s\n", item.ID, item.Title)
	fmt.Fprintf(&b, "\nContinue where you left off. Do not restart from scratch.")
	return &PrimeResult{Output: b.String()}, nil
}

// readActiveWrit reads the active_writ field for an agent from the sphere store.
// Returns empty string on any error (best-effort).
func readActiveWrit(world, agentName string) string {
	s, err := store.OpenSphere()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prime: failed to open sphere store: %v\n", err)
		return ""
	}
	defer s.Close()

	agentID := world + "/" + agentName
	agent, err := s.GetAgent(agentID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prime: failed to get agent %q: %v\n", agentID, err)
		return ""
	}
	return agent.ActiveWrit
}

// resolveActiveWrit returns the active writ ID for the given agent, validated
// against the current tether list. Returns "" if the agent has no active writ
// or if the recorded active writ is not in the tether list (with a stderr
// warning).
func resolveActiveWrit(world, agentName string, persistent bool, allWritIDs []string) string {
	if !persistent {
		if len(allWritIDs) == 0 {
			return ""
		}
		return allWritIDs[0]
	}
	activeWritID := readActiveWrit(world, agentName)
	if activeWritID == "" {
		return ""
	}
	for _, id := range allWritIDs {
		if id == activeWritID {
			return activeWritID
		}
	}
	fmt.Fprintf(os.Stderr, "prime: active_writ %s not in tether list — clearing\n", activeWritID)
	return ""
}

// primeNoActiveWrit generates prime context when a persistent agent has tethered writs
// but none is active. Lists all writs and tells the agent to wait.
func primeNoActiveWrit(world, agentName string, writIDs []string, worldStore WorldStore) (*PrimeResult, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "=== WORK CONTEXT ===\nAgent: %s (world: %s)\n\n", agentName, world)
	fmt.Fprintf(&b, "You have %d tethered writs. Wait for the operator to activate one.\n\n", len(writIDs))

	for _, id := range writIDs {
		writ, err := worldStore.GetWrit(id)
		if err != nil {
			fmt.Fprintf(&b, "- %s — (failed to load)\n", id)
			continue
		}
		kind := writ.Kind
		if kind == "" {
			kind = "code"
		}
		fmt.Fprintf(&b, "- %s — %s (kind: %s, status: %s)\n", id, writ.Title, kind, writ.Status)
	}

	b.WriteString("\n=== END CONTEXT ===")
	return &PrimeResult{Output: b.String()}, nil
}

// primeBackgroundWrits generates the background writs summary section
// appended after the active writ's prime context for persistent agents.
func primeBackgroundWrits(activeWritID string, allWritIDs []string, worldStore WorldStore) string {
	var b strings.Builder
	b.WriteString("\n\n## Background Writs\n")

	hasBackground := false
	for _, id := range allWritIDs {
		if id == activeWritID {
			continue
		}
		writ, err := worldStore.GetWrit(id)
		if err != nil {
			fmt.Fprintf(&b, "- %s — (failed to load)\n", id)
			hasBackground = true
			continue
		}
		kind := writ.Kind
		if kind == "" {
			kind = "code"
		}
		fmt.Fprintf(&b, "- %s — %s (kind: %s, status: %s)\n", id, writ.Title, kind, writ.Status)
		hasBackground = true
	}

	if !hasBackground {
		return ""
	}

	b.WriteString("\nWork only on your active writ. Background writs are listed for awareness.\n")
	return b.String()
}

// primeWithHandoff returns handoff-aware context for the prime command.
func primeWithHandoff(world, agentName string, item *store.Writ,
	state *handoff.State) (*PrimeResult, error) {

	output := fmt.Sprintf(`=== HANDOFF CONTEXT ===
Agent: %s (world: %s)
Writ: %s
Title: %s

This is a continuation of a previous session. The previous session
handed off to preserve context.

--- PREVIOUS SESSION SUMMARY ---
%s
--- END SUMMARY ---

--- RECENT COMMITS ---
%s
--- END COMMITS ---
`, agentName, world, item.ID, item.Title, state.Summary, strings.Join(state.RecentCommits, "\n"))

	// Add git worktree state if captured.
	if state.GitStatus != "" {
		output += fmt.Sprintf("--- GIT STATUS ---\n%s\n--- END GIT STATUS ---\n\n", state.GitStatus)
	}
	if state.DiffStat != "" {
		output += fmt.Sprintf("--- UNCOMMITTED CHANGES ---\n%s\n--- END UNCOMMITTED CHANGES ---\n\n", state.DiffStat)
	}
	if state.GitStash != "" {
		output += fmt.Sprintf("--- STASHED WORK ---\n%s\n--- END STASHED WORK ---\n\n", state.GitStash)
	}

	output += fmt.Sprintf(`Continue from where the previous session left off.
When complete, run: sol resolve
If you need to hand off again: sol handoff --summary="<what you've done>"
=== END HANDOFF ===`)

	return &PrimeResult{Output: output}, nil
}

// primeCompactRecovery returns a lightweight prime for sessions recovering from
// context compaction. Unlike primeWithHandoff, it omits the full writ
// description because the agent has compressed context from its predecessor
// session (via --continue). This saves tokens and avoids confusing the agent
// about whether it's starting fresh or continuing.
func primeCompactRecovery(world, agentName string, item *store.Writ,
	state *handoff.State) (*PrimeResult, error) {

	var b strings.Builder
	fmt.Fprintf(&b, `=== SESSION RECOVERY ===
Agent: %s (world: %s)
Writ: %s — %s
Reason: Context compaction recovery

You are continuing a previous session. Your prior conversation has been compressed.

`, agentName, world, item.ID, item.Title)

	// Previous session state from handoff.
	fmt.Fprintf(&b, "PREVIOUS SESSION STATE:\n")
	fmt.Fprintf(&b, "Summary: %s\n", state.Summary)
	if len(state.RecentCommits) > 0 {
		fmt.Fprintf(&b, "Recent commits:\n%s\n", strings.Join(state.RecentCommits, "\n"))
	}
	if state.GitStatus != "" {
		fmt.Fprintf(&b, "Git status:\n%s\n", state.GitStatus)
	}
	if state.DiffStat != "" {
		fmt.Fprintf(&b, "Uncommitted changes:\n%s\n", state.DiffStat)
	}
	if state.GitStash != "" {
		fmt.Fprintf(&b, "Stashed work:\n%s\n", state.GitStash)
	}

	fmt.Fprintf(&b, `
Continue from where you left off. Do NOT re-read the writ description
or restart from scratch — pick up where the previous session stopped.

When complete: sol resolve
=== END RECOVERY ===`)

	return &PrimeResult{Output: b.String()}, nil
}

// primeForge returns forge-specific context for the prime command.
func primeForge(world string) (*PrimeResult, error) {
	output := fmt.Sprintf(`=== FORGE CONTEXT ===
World: %s
Role: forge (merge queue processor)

Begin your patrol loop. Run 'sol forge check-unblocked --world=%s' first,
then scan the queue with 'sol forge ready --world=%s --json'.
=== END CONTEXT ===`, world, world, world)

	return &PrimeResult{Output: output}, nil
}

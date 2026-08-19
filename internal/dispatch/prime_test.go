package dispatch

import (
	"os"
	"testing"

	"github.com/nevinsm/sol/internal/startup"
)

// TestPrimeClearsLeakedResumeState covers Defect 1 (2026-08-19 handoff
// audit): in a self-invoked handoff, respawn-pane -k kills the calling
// process at startup.Launch's session-op step, so every code path that
// would otherwise clear resume_state.json after a successful launch
// (handoff.Exec's own post-launch cleanup, and even startup.Launch's own
// end-of-function ClearResumeState call) is dead on the success path —
// every successful self-invoked handoff leaves resume_state.json on disk.
// Prime is the successor-side backstop: it runs at the start of every new
// session, and by the time it runs, any resume state still on disk is by
// definition leaked (a live Respawn always reads-and-clears it before the
// session it starts gets to run Prime). This test writes a resume state
// file directly (simulating the leak) and verifies Prime clears it.
func TestPrimeClearsLeakedResumeState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world, agent, role := "ember", "Toast", "outpost"

	// Simulate a leaked resume_state.json from an earlier successful
	// self-invoked handoff cycle.
	leaked := startup.ResumeState{
		ClaimedResource: "sol-abc1234500000000",
		Reason:          "manual",
		Summary:         "stale summary from an arbitrary earlier epoch",
	}
	if err := startup.WriteResumeState(world, agent, role, leaked); err != nil {
		t.Fatalf("failed to seed leaked resume state: %v", err)
	}

	// Sanity: the leaked file is present before Prime runs.
	if rs, err := startup.ReadResumeState(world, agent, role); err != nil {
		t.Fatalf("ReadResumeState failed: %v", err)
	} else if rs == nil {
		t.Fatal("expected leaked resume state to be present before Prime runs")
	}

	// No tethers for this agent — Prime takes the untethered path, which
	// doesn't touch worldStore, so nil is safe here.
	if _, err := Prime(world, agent, role, nil); err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	// The leaked resume state must be gone after Prime runs.
	rs, err := startup.ReadResumeState(world, agent, role)
	if err != nil {
		t.Fatalf("ReadResumeState after Prime failed: %v", err)
	}
	if rs != nil {
		t.Errorf("expected resume state to be cleared after Prime, got %+v", rs)
	}
}

// TestPrimeClearsLeakedResumeStateForForgeRole verifies the backstop also
// covers the forge role, which takes an early-return path (primeForge)
// before the rest of Prime's normal logic. The resume-state clear must run
// before that early return, not be skipped by it.
func TestPrimeClearsLeakedResumeStateForForgeRole(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world, agent, role := "ember", "Anvil", "forge"

	leaked := startup.ResumeState{ClaimedResource: "sol-f0f0f0f0f0f0f0f0", Reason: "manual"}
	if err := startup.WriteResumeState(world, agent, role, leaked); err != nil {
		t.Fatalf("failed to seed leaked resume state: %v", err)
	}

	if _, err := Prime(world, agent, role, nil); err != nil {
		t.Fatalf("Prime failed: %v", err)
	}

	rs, err := startup.ReadResumeState(world, agent, role)
	if err != nil {
		t.Fatalf("ReadResumeState after Prime failed: %v", err)
	}
	if rs != nil {
		t.Errorf("expected resume state to be cleared after Prime (forge role), got %+v", rs)
	}
}

// TestPrimeClearsLeakedResumeStateCompactMode verifies the backstop also
// runs for --compact invocations (primeCompact), which take their own
// early-return path.
func TestPrimeClearsLeakedResumeStateCompactMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world, agent, role := "ember", "Toast", "outpost"

	leaked := startup.ResumeState{ClaimedResource: "sol-c0c0c0c0c0c0c0c0", Reason: "manual"}
	if err := startup.WriteResumeState(world, agent, role, leaked); err != nil {
		t.Fatalf("failed to seed leaked resume state: %v", err)
	}

	if _, err := Prime(world, agent, role, nil, true); err != nil {
		t.Fatalf("Prime (compact) failed: %v", err)
	}

	rs, err := startup.ReadResumeState(world, agent, role)
	if err != nil {
		t.Fatalf("ReadResumeState after Prime failed: %v", err)
	}
	if rs != nil {
		t.Errorf("expected resume state to be cleared after compact Prime, got %+v", rs)
	}
}

// TestPrimeNoResumeStateIsNoop verifies Prime doesn't error or otherwise
// misbehave when there is no resume state file to clear (the common case).
func TestPrimeNoResumeStateIsNoop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SOL_HOME", dir)

	world, agent, role := "ember", "Toast", "outpost"

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("temp dir missing: %v", err)
	}

	if _, err := Prime(world, agent, role, nil); err != nil {
		t.Fatalf("Prime failed with no resume state present: %v", err)
	}
}

package status

import (
	"fmt"
	"os"
	"testing"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/store"
)

// --- computeWorldHealthLevel / levelString — the single shared rule encoding ---

// TestComputeWorldHealthLevel exercises the shared world-health rule
// function directly, covering each individual trigger plus the precedence
// between them (prefect down is checked first, so it wins over everything
// else; dead sessions / failed MRs are checked next, so "unhealthy" wins
// over a merely-degraded forge).
func TestComputeWorldHealthLevel(t *testing.T) {
	tests := []struct {
		name                string
		prefectRunning      bool
		deadSessions        int
		failedMRs           int
		forgeRemoteFailures int
		want                int
	}{
		{
			name:           "all nominal is healthy",
			prefectRunning: true,
			want:           0,
		},
		{
			name:           "prefect down is degraded",
			prefectRunning: false,
			want:           2,
		},
		{
			name:           "dead sessions is unhealthy",
			prefectRunning: true,
			deadSessions:   1,
			want:           1,
		},
		{
			name:           "failed MRs is unhealthy",
			prefectRunning: true,
			failedMRs:      1,
			want:           1,
		},
		{
			name:                "forge failures below threshold stays healthy",
			prefectRunning:      true,
			forgeRemoteFailures: 2,
			want:                0,
		},
		{
			name:                "forge failures at threshold is degraded",
			prefectRunning:      true,
			forgeRemoteFailures: 3,
			want:                2,
		},
		{
			name:                "forge failures above threshold is degraded",
			prefectRunning:      true,
			forgeRemoteFailures: 9,
			want:                2,
		},
		{
			name:           "precedence: prefect down + dead sessions stays degraded",
			prefectRunning: false,
			deadSessions:   1,
			want:           2,
		},
		{
			name:                "precedence: dead sessions trumps forge-degraded",
			prefectRunning:      true,
			deadSessions:        1,
			forgeRemoteFailures: 10,
			want:                1,
		},
		{
			name:                "precedence: failed MRs trumps forge-degraded",
			prefectRunning:      true,
			failedMRs:           1,
			forgeRemoteFailures: 10,
			want:                1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeWorldHealthLevel(tt.prefectRunning, tt.deadSessions, tt.failedMRs, tt.forgeRemoteFailures)
			if got != tt.want {
				t.Errorf("computeWorldHealthLevel(%v, %d, %d, %d) = %d, want %d",
					tt.prefectRunning, tt.deadSessions, tt.failedMRs, tt.forgeRemoteFailures, got, tt.want)
			}
		})
	}
}

// TestLevelString covers the level→string mapping used by both
// WorldStatus.HealthString() and gatherWorldSummary.
func TestLevelString(t *testing.T) {
	tests := []struct {
		level int
		want  string
	}{
		{0, "healthy"},
		{1, "unhealthy"},
		{2, "degraded"},
		{7, "unknown(7)"},
	}
	for _, tt := range tests {
		if got := levelString(tt.level); got != tt.want {
			t.Errorf("levelString(%d) = %q, want %q", tt.level, got, tt.want)
		}
	}
}

// --- Cross-check: WorldStatus.Health()/HealthString() vs gatherWorldSummary ---

// makeFailedMRs creates n merge requests in the "failed" phase (each backed
// by its own open writ, so isFailedMRRecast does not exclude them) in the
// given world's database.
func makeFailedMRs(t *testing.T, world string, n int) {
	t.Helper()
	ws, err := store.OpenWorld(world)
	if err != nil {
		t.Fatalf("OpenWorld(%q): %v", world, err)
	}
	defer ws.Close()

	for i := range n {
		writID, err := ws.CreateWrit(fmt.Sprintf("writ %d", i), "", "test", 0, nil)
		if err != nil {
			t.Fatalf("CreateWrit: %v", err)
		}
		mrID, err := ws.CreateMergeRequest(writID, fmt.Sprintf("branch-%d", i), 0)
		if err != nil {
			t.Fatalf("CreateMergeRequest: %v", err)
		}
		claimed, err := ws.ClaimMergeRequest("claimer", 0)
		if err != nil {
			t.Fatalf("ClaimMergeRequest: %v", err)
		}
		if claimed == nil || claimed.ID != mrID {
			t.Fatalf("ClaimMergeRequest did not claim %s (got %+v)", mrID, claimed)
		}
		if err := ws.UpdateMergeRequestPhase(mrID, "failed"); err != nil {
			t.Fatalf("UpdateMergeRequestPhase(failed): %v", err)
		}
	}
}

// TestWorldHealthCrossCheck_StatusMatchesSphereSummary verifies that
// WorldStatus.Health()/HealthString() (the per-world detail view) and
// gatherWorldSummary (the sphere overview, exercised via GatherSphere)
// produce the same health for identical underlying conditions. Both now
// delegate to computeWorldHealthLevel — this test guards against the two
// call sites drifting apart again.
func TestWorldHealthCrossCheck_StatusMatchesSphereSummary(t *testing.T) {
	const world = "haven"

	tests := []struct {
		name                string
		prefectRunning      bool
		deadSessions        int
		failedMRs           int
		forgeRemoteFailures int
		wantHealth          string
	}{
		{
			name:           "healthy",
			prefectRunning: true,
			wantHealth:     "healthy",
		},
		{
			name:           "dead sessions",
			prefectRunning: true,
			deadSessions:   1,
			wantHealth:     "unhealthy",
		},
		{
			name:           "failed merge requests",
			prefectRunning: true,
			failedMRs:      2,
			wantHealth:     "unhealthy",
		},
		{
			name:                "forge remote failures at threshold",
			prefectRunning:      true,
			forgeRemoteFailures: 3,
			wantHealth:          "degraded",
		},
		{
			name:           "prefect down",
			prefectRunning: false,
			wantHealth:     "degraded",
		},
		{
			name:           "prefect down trumps dead sessions",
			prefectRunning: false,
			deadSessions:   1,
			wantHealth:     "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestHome(t)

			if tt.prefectRunning {
				cleanup := writePrefectPID(t, os.Getpid())
				defer cleanup()
			} else {
				clearPrefectPID(t)
			}

			if tt.forgeRemoteFailures > 0 {
				if err := forge.WriteHeartbeat(world, &forge.Heartbeat{
					Status:                    "idle",
					ConsecutiveRemoteFailures: tt.forgeRemoteFailures,
				}); err != nil {
					t.Fatalf("WriteHeartbeat: %v", err)
				}
			}

			// Direct check: build a WorldStatus with the same inputs.
			direct := &WorldStatus{
				World:      world,
				Prefect:    PrefectInfo{Running: tt.prefectRunning},
				Summary:    Summary{Dead: tt.deadSessions},
				MergeQueue: MergeQueueInfo{Failed: tt.failedMRs},
				Forge:      ForgeInfo{ConsecutiveRemoteFailures: tt.forgeRemoteFailures},
			}
			if got := direct.HealthString(); got != tt.wantHealth {
				t.Fatalf("WorldStatus.HealthString() = %q, want %q", got, tt.wantHealth)
			}

			// Sphere-summary check: build equivalent conditions via real
			// agents/sessions/merge requests and run the sphere gather path.
			var agents []store.Agent
			alive := map[string]bool{}
			for i := range tt.deadSessions {
				name := fmt.Sprintf("dead-%d", i)
				agents = append(agents, store.Agent{
					ID: world + "/" + name, Name: name, World: world, State: store.AgentWorking,
				})
				alive[config.SessionName(world, name)] = false
			}
			makeFailedMRs(t, world, tt.failedMRs)

			lister := &mockWorldLister{worlds: []store.World{{Name: world}}}
			sphereStore := &mockSphereStore{agents: agents}
			checker := &mockChecker{alive: alive}
			opener := func(w string) (*store.WorldStore, error) { return store.OpenWorld(w) }
			tracked := NewTrackingOpener(opener)
			defer tracked.CloseAll()

			result := GatherSphere(sphereStore, lister, checker, tracked.Open, opener, nil)
			if len(result.Worlds) != 1 {
				t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
			}
			got := result.Worlds[0].Health
			if got != tt.wantHealth {
				t.Errorf("gatherWorldSummary Health = %q, want %q", got, tt.wantHealth)
			}
			if got != direct.HealthString() {
				t.Errorf("cross-check mismatch: WorldStatus.HealthString() = %q, gatherWorldSummary Health = %q",
					direct.HealthString(), got)
			}
		})
	}
}

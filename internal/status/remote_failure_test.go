package status

import (
	"os"
	"testing"

	"github.com/nevinsm/sol/internal/forge"
	"github.com/nevinsm/sol/internal/store"
)

// --- WorldStatus.Health() — forge remote-git failure threshold (Task B) ---

// TestHealth_ForgeRemoteFailuresDegraded verifies that Health() reports
// degraded once consecutive forge remote-git failures reach the threshold,
// and healthy below it — mirroring the acceptance criteria for
// sol-0ec6b898c083264f ("status renders degraded at threshold and healthy
// after recovery").
func TestHealth_ForgeRemoteFailuresDegraded(t *testing.T) {
	tests := []struct {
		name    string
		forge   ForgeInfo
		want    int
		wantStr string
	}{
		{
			name:    "below threshold stays healthy",
			forge:   ForgeInfo{ConsecutiveRemoteFailures: 2},
			want:    0,
			wantStr: "healthy",
		},
		{
			name:    "at threshold is degraded",
			forge:   ForgeInfo{ConsecutiveRemoteFailures: 3},
			want:    2,
			wantStr: "degraded",
		},
		{
			name:    "above threshold is degraded",
			forge:   ForgeInfo{ConsecutiveRemoteFailures: 9},
			want:    2,
			wantStr: "degraded",
		},
		{
			name:    "recovered (reset to zero) is healthy",
			forge:   ForgeInfo{ConsecutiveRemoteFailures: 0},
			want:    0,
			wantStr: "healthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &WorldStatus{
				World:   "test",
				Prefect: PrefectInfo{Running: true, PID: 123},
				Forge:   tt.forge,
			}
			if got := rs.Health(); got != tt.want {
				t.Errorf("Health() = %d, want %d", got, tt.want)
			}
			if got := rs.HealthString(); got != tt.wantStr {
				t.Errorf("HealthString() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

// TestHealth_DeadSessionsTrumpForgeRemoteDegraded verifies unhealthy (dead
// sessions / failed MRs) still wins over a merely-degraded forge, matching
// the existing precedence where prefect-down trumps everything else.
func TestHealth_DeadSessionsTrumpForgeRemoteDegraded(t *testing.T) {
	rs := &WorldStatus{
		World:   "test",
		Prefect: PrefectInfo{Running: true, PID: 123},
		Summary: Summary{Dead: 1},
		Forge:   ForgeInfo{ConsecutiveRemoteFailures: 10},
	}
	if got := rs.Health(); got != 1 {
		t.Errorf("Health() = %d, want 1 (unhealthy trumps degraded forge)", got)
	}
}

// --- Gather() — forge heartbeat remote-failure fields end-to-end ---

// TestGatherWithForge_RemoteFailuresDegraded verifies that Gather reads the
// forge heartbeat's consecutive remote-git failure count into WorldStatus,
// and that the resulting world reports degraded — the GLASS-fix path from
// forge heartbeat to `sol status` world detail.
func TestGatherWithForge_RemoteFailuresDegraded(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()
	forgePIDCleanup := writeForgePID(t, "haven", os.Getpid())
	defer forgePIDCleanup()

	if err := forge.WriteHeartbeat("haven", &forge.Heartbeat{
		Status:                    "idle",
		ConsecutiveRemoteFailures: 5,
		LastRemoteError:           "fatal: Authentication failed",
	}); err != nil {
		t.Fatalf("WriteHeartbeat error: %v", err)
	}

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if result.Forge.ConsecutiveRemoteFailures != 5 {
		t.Errorf("Forge.ConsecutiveRemoteFailures = %d, want 5", result.Forge.ConsecutiveRemoteFailures)
	}
	if result.Forge.LastRemoteError != "fatal: Authentication failed" {
		t.Errorf("Forge.LastRemoteError = %q, want %q", result.Forge.LastRemoteError, "fatal: Authentication failed")
	}
	if got := result.HealthString(); got != "degraded" {
		t.Errorf("HealthString() = %q, want %q", got, "degraded")
	}
}

// TestGatherWithForge_RemoteFailuresRecovered verifies that a heartbeat with
// a reset counter (post-recovery) reports the world healthy again.
func TestGatherWithForge_RemoteFailuresRecovered(t *testing.T) {
	setupTestHome(t)

	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()
	forgePIDCleanup := writeForgePID(t, "haven", os.Getpid())
	defer forgePIDCleanup()

	if err := forge.WriteHeartbeat("haven", &forge.Heartbeat{
		Status:                    "idle",
		ConsecutiveRemoteFailures: 0,
	}); err != nil {
		t.Fatalf("WriteHeartbeat error: %v", err)
	}

	sphere := &mockSphereStore{agents: nil}
	world := &mockWorldStore{items: nil}
	checker := &mockChecker{alive: nil}

	result, err := Gather("haven", sphere, world, emptyMQStore(), checker)
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	if got := result.HealthString(); got != "healthy" {
		t.Errorf("HealthString() = %q, want %q", got, "healthy")
	}
}

// --- computeSphereHealth — a degraded world propagates to sphere health ---

// TestComputeSphereHealth_DegradedWorldPropagates verifies a world reporting
// "degraded" (e.g. from persistent forge remote-git failures) surfaces at
// the sphere level too, not just in the per-world detail view.
func TestComputeSphereHealth_DegradedWorldPropagates(t *testing.T) {
	s := &SphereStatus{
		Prefect: PrefectInfo{Running: true},
		Consul:  ConsulInfo{Stale: false},
		Worlds: []WorldSummary{
			{Health: "degraded"},
		},
	}
	if got := computeSphereHealth(s); got != "degraded" {
		t.Errorf("computeSphereHealth() = %q, want %q", got, "degraded")
	}
}

// TestComputeSphereHealth_UnhealthyTrumpsDegradedWorld verifies that a worse
// "unhealthy" world still wins over a merely-degraded one when aggregating.
func TestComputeSphereHealth_UnhealthyTrumpsDegradedWorld(t *testing.T) {
	s := &SphereStatus{
		Prefect: PrefectInfo{Running: true},
		Worlds: []WorldSummary{
			{Health: "degraded"},
			{Health: "unhealthy"},
		},
	}
	if got := computeSphereHealth(s); got != "unhealthy" {
		t.Errorf("computeSphereHealth() = %q, want %q", got, "unhealthy")
	}
}

// --- GatherSphere — forge remote failures reflected in the sphere overview ---

// TestGatherSphere_ForgeRemoteFailuresDegradesWorldAndSphere verifies the
// full path from a forge heartbeat carrying a persistent remote-git failure
// through to the sphere overview: the world's WorldSummary.Health and the
// sphere-wide SphereStatus.Health both report degraded, not healthy.
func TestGatherSphere_ForgeRemoteFailuresDegradesWorldAndSphere(t *testing.T) {
	setupTestHome(t)
	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	if err := forge.WriteHeartbeat("haven", &forge.Heartbeat{
		Status:                    "idle",
		ConsecutiveRemoteFailures: 4,
	}); err != nil {
		t.Fatalf("WriteHeartbeat error: %v", err)
	}

	lister := &mockWorldLister{worlds: []store.World{{Name: "haven"}}}
	sphereStore := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}
	opener := func(w string) (*store.WorldStore, error) { return store.OpenWorld(w) }

	result := GatherSphere(sphereStore, lister, checker, opener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	if result.Worlds[0].Health != "degraded" {
		t.Errorf("Worlds[0].Health = %q, want %q", result.Worlds[0].Health, "degraded")
	}
	if result.Health != "degraded" {
		t.Errorf("sphere Health = %q, want %q", result.Health, "degraded")
	}
}

// TestGatherSphere_ForgeRemoteFailuresRecoveredHealthy verifies that once
// the heartbeat's counter is back to zero, both the world and sphere report
// healthy again.
func TestGatherSphere_ForgeRemoteFailuresRecoveredHealthy(t *testing.T) {
	setupTestHome(t)
	pidCleanup := writePrefectPID(t, os.Getpid())
	defer pidCleanup()

	if err := forge.WriteHeartbeat("haven", &forge.Heartbeat{
		Status:                    "idle",
		ConsecutiveRemoteFailures: 0,
	}); err != nil {
		t.Fatalf("WriteHeartbeat error: %v", err)
	}

	lister := &mockWorldLister{worlds: []store.World{{Name: "haven"}}}
	sphereStore := &mockSphereStore{}
	checker := &mockChecker{alive: map[string]bool{}}
	opener := func(w string) (*store.WorldStore, error) { return store.OpenWorld(w) }

	result := GatherSphere(sphereStore, lister, checker, opener, nil)

	if len(result.Worlds) != 1 {
		t.Fatalf("Worlds = %d, want 1", len(result.Worlds))
	}
	if result.Worlds[0].Health != "healthy" {
		t.Errorf("Worlds[0].Health = %q, want %q", result.Worlds[0].Health, "healthy")
	}
	if result.Health != "healthy" {
		t.Errorf("sphere Health = %q, want %q", result.Health, "healthy")
	}
}

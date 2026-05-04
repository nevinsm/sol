//go:build linux

package service

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnitName(t *testing.T) {
	tests := []struct {
		component string
		want      string
	}{
		{"prefect", "sol-prefect.service"},
		{"consul", "sol-consul.service"},
		{"chronicle", "sol-chronicle.service"},
		{"broker", "sol-broker.service"},
	}
	for _, tt := range tests {
		got := UnitName(tt.component)
		if got != tt.want {
			t.Errorf("UnitName(%q) = %q, want %q", tt.component, got, tt.want)
		}
	}
}

func TestGenerateUnit(t *testing.T) {
	content, err := GenerateUnit("consul", "/usr/local/bin/sol", "/home/user/sol")
	if err != nil {
		t.Fatalf("GenerateUnit failed: %v", err)
	}

	checks := []string{
		"Description=Sol consul daemon",
		"Type=simple",
		"ExecStart=/usr/local/bin/sol consul run",
		"Restart=on-failure",
		"RestartSec=5",
		"Environment=SOL_HOME=/home/user/sol",
		"WantedBy=default.target",
	}
	for _, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("unit missing %q\ngot:\n%s", want, content)
		}
	}
}

func TestGenerateUnitPrefectDependencies(t *testing.T) {
	content, err := GenerateUnit("prefect", "/usr/local/bin/sol", "/home/user/sol")
	if err != nil {
		t.Fatalf("GenerateUnit(prefect) failed: %v", err)
	}

	// Prefect must have After and Wants directives for all other components.
	depUnits := []string{
		"sol-consul.service",
		"sol-chronicle.service",
		"sol-ledger.service",
		"sol-broker.service",
	}
	for _, dep := range depUnits {
		if !strings.Contains(content, dep) {
			t.Errorf("prefect unit missing dependency on %s\ngot:\n%s", dep, content)
		}
	}

	// Verify both After= and Wants= lines exist (beyond the base After=network.target).
	lines := strings.Split(content, "\n")
	var afterCount, wantsCount int
	for _, line := range lines {
		if strings.HasPrefix(line, "After=") && line != "After=network.target" {
			afterCount++
		}
		if strings.HasPrefix(line, "Wants=") {
			wantsCount++
		}
	}
	if afterCount != 1 {
		t.Errorf("expected 1 dependency After= line, got %d\ngot:\n%s", afterCount, content)
	}
	if wantsCount != 1 {
		t.Errorf("expected 1 Wants= line, got %d\ngot:\n%s", wantsCount, content)
	}

	// Prefect must NOT list itself in dependencies.
	if strings.Contains(content, "sol-prefect.service") {
		t.Errorf("prefect unit should not depend on itself\ngot:\n%s", content)
	}
}

func TestGenerateUnitNonPrefectNoDependencies(t *testing.T) {
	nonPrefect := []string{"consul", "chronicle", "ledger", "broker"}
	for _, comp := range nonPrefect {
		content, err := GenerateUnit(comp, "/usr/local/bin/sol", "/home/user/sol")
		if err != nil {
			t.Fatalf("GenerateUnit(%s) failed: %v", comp, err)
		}

		// Non-prefect units should not have Wants= directives.
		if strings.Contains(content, "Wants=") {
			t.Errorf("%s unit should not have Wants= directive\ngot:\n%s", comp, content)
		}

		// Should only have the base After=network.target, not dependency After=.
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "After=") && line != "After=network.target" {
				t.Errorf("%s unit has unexpected After= directive: %s\ngot:\n%s", comp, line, content)
			}
		}
	}
}

func TestRestartHappyPath(t *testing.T) {
	var calls []string
	origSystemctl := systemctl
	systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	defer func() { systemctl = origSystemctl }()

	if err := Restart(); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	// Expect a stop+start for every component.
	wantCalls := 2 * len(Components)
	if len(calls) != wantCalls {
		t.Errorf("expected %d systemctl calls, got %d: %v", wantCalls, len(calls), calls)
	}

	// Verify all stops happen before any start (mirrors Darwin's stop-all-then-start-all
	// shape — a partial-stop failure must abort before any start runs).
	seenStart := false
	for _, c := range calls {
		switch {
		case strings.HasPrefix(c, "start "):
			seenStart = true
		case strings.HasPrefix(c, "stop "):
			if seenStart {
				t.Errorf("stop call after start call (calls: %v)", calls)
			}
		}
	}
}

func TestRestartRollsBackOnPartialStopFailure(t *testing.T) {
	// Simulate `systemctl stop` failing on the third component. The first two
	// should have been stopped, and Restart should attempt to start them again
	// (rollback toward "running") and report the original stop failure.
	failAt := 2
	stopCount := 0
	var calls []string

	origSystemctl := systemctl
	systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "stop":
			if stopCount == failAt {
				return fmt.Errorf("simulated stop failure for %s", args[1])
			}
			stopCount++
			return nil
		}
		return nil
	}
	defer func() { systemctl = origSystemctl }()

	err := Restart()
	if err == nil {
		t.Fatal("Restart should have returned an error after partial stop failure")
	}
	if !strings.Contains(err.Error(), "restart failed") {
		t.Errorf("error should mention restart failure: %v", err)
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention rollback outcome: %v", err)
	}

	// The stop error should be wrapped — operator needs to see which unit failed.
	if !strings.Contains(err.Error(), "failed to stop") {
		t.Errorf("error should wrap the stop failure: %v", err)
	}

	// Expected call sequence:
	//   stop unit[0] (ok), stop unit[1] (ok), stop unit[2] (fail),
	//   start unit[0], start unit[1]  -- rollback only, no new starts beyond what we stopped
	wantStops := failAt + 1 // includes the failing one
	wantStarts := failAt    // rollback restarts only the previously-stopped components
	stops := 0
	starts := 0
	for _, c := range calls {
		switch {
		case strings.HasPrefix(c, "stop "):
			stops++
		case strings.HasPrefix(c, "start "):
			starts++
		}
	}
	if stops != wantStops {
		t.Errorf("got %d stop calls, want %d (calls: %v)", stops, wantStops, calls)
	}
	if starts != wantStarts {
		t.Errorf("got %d start calls (rollback), want %d (calls: %v)", starts, wantStarts, calls)
	}

	// Confirm the rollback restarts targeted exactly the previously-stopped
	// components, in order.
	for i := 0; i < failAt; i++ {
		want := "start " + UnitName(Components[i])
		found := false
		for _, c := range calls {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected rollback call %q in %v", want, calls)
		}
	}
}

func TestRestartStopFailureFirstUnitDoesNoStart(t *testing.T) {
	// Acceptance criterion: when `systemctl stop` fails on the very first unit,
	// no `start` should be attempted (nothing was stopped, so nothing to roll
	// back) and the stop error must be returned.
	var calls []string
	origSystemctl := systemctl
	systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "stop" {
			return fmt.Errorf("simulated stop failure")
		}
		return nil
	}
	defer func() { systemctl = origSystemctl }()

	err := Restart()
	if err == nil {
		t.Fatal("Restart should fail when stop fails on the first unit")
	}
	if !strings.Contains(err.Error(), "failed to stop") {
		t.Errorf("error should wrap the stop failure: %v", err)
	}

	// No starts should have been attempted (nothing to roll back).
	for _, c := range calls {
		if strings.HasPrefix(c, "start ") {
			t.Errorf("no start should be attempted after stop fails on first unit; got %q", c)
		}
	}
	// Exactly one stop call should have happened (the failing one).
	if len(calls) != 1 {
		t.Errorf("expected exactly 1 systemctl call, got %d: %v", len(calls), calls)
	}
}

func TestRestartStopSucceedsStartFailsReportsBoth(t *testing.T) {
	// Acceptance criterion: when stop succeeds for all units but start then
	// fails on a later unit, the function must report the start failure
	// explicitly. Stop success is implicit in the call sequence (we got past
	// the rollback gate) — the start error reaches the caller unwrapped by
	// rollback messaging.
	failStartAt := 1 // start fails on the second unit
	startCount := 0
	var calls []string

	origSystemctl := systemctl
	systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		switch args[0] {
		case "stop":
			return nil
		case "start":
			if startCount == failStartAt {
				return fmt.Errorf("simulated start failure for %s", args[1])
			}
			startCount++
			return nil
		}
		return nil
	}
	defer func() { systemctl = origSystemctl }()

	err := Restart()
	if err == nil {
		t.Fatal("Restart should fail when start fails after stop succeeds")
	}

	// Error must clearly attribute failure to the start phase, not stop.
	if !strings.Contains(err.Error(), "start") {
		t.Errorf("error should mention start failure: %v", err)
	}
	if strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should not mention rollback (rollback only runs on stop failure): %v", err)
	}
	if strings.Contains(err.Error(), "failed to stop") {
		t.Errorf("error should not blame stop (stop succeeded): %v", err)
	}

	// All stops must have run (stop phase completed before start began).
	stops := 0
	starts := 0
	for _, c := range calls {
		switch {
		case strings.HasPrefix(c, "stop "):
			stops++
		case strings.HasPrefix(c, "start "):
			starts++
		}
	}
	if stops != len(Components) {
		t.Errorf("expected all %d units to be stopped, got %d (calls: %v)", len(Components), stops, calls)
	}
	// Start ran until the failing one — failStartAt successes plus 1 failure.
	wantStarts := failStartAt + 1
	if starts != wantStarts {
		t.Errorf("expected %d start calls, got %d (calls: %v)", wantStarts, starts, calls)
	}
}

func TestRestartReportsRollbackFailure(t *testing.T) {
	// When stop fails partway AND the rollback start also fails, the error
	// must surface both failures so the operator knows the recovery attempt
	// did not succeed.
	stopCount := 0
	origSystemctl := systemctl
	systemctl = func(args ...string) error {
		switch args[0] {
		case "stop":
			if stopCount == 1 {
				return fmt.Errorf("simulated stop failure")
			}
			stopCount++
			return nil
		case "start":
			return fmt.Errorf("simulated start failure")
		}
		return nil
	}
	defer func() { systemctl = origSystemctl }()

	err := Restart()
	if err == nil {
		t.Fatal("Restart should fail when both stop and rollback fail")
	}
	if !strings.Contains(err.Error(), "rollback also failed") {
		t.Errorf("error should mention rollback failure: %v", err)
	}
	if !strings.Contains(err.Error(), "restart failed") {
		t.Errorf("error should mention restart failure: %v", err)
	}
}

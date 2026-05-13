package store

import (
	"errors"
	"testing"
)

// TestSafelyReopenWrit_TerminalStatusesBlocked verifies that 'done' and 'closed'
// writs cannot be reopened via SafelyReopenWrit when they are not in the
// allowedFromStatuses list.
func TestSafelyReopenWrit_TerminalStatusesBlocked(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	for _, tc := range []struct {
		name          string
		terminalStatus string
	}{
		{"done", WritDone},
		{"closed", WritClosed},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := s.CreateWrit("Terminal writ", "", "autarch", 2, nil)
			if err != nil {
				t.Fatalf("CreateWrit: %v", err)
			}

			// Move writ to terminal status.
			switch tc.terminalStatus {
			case WritDone:
				if err := s.UpdateWrit(id, WritUpdates{Status: WritDone}); err != nil {
					t.Fatalf("UpdateWrit→done: %v", err)
				}
			case WritClosed:
				if _, err := s.CloseWrit(id); err != nil {
					t.Fatalf("CloseWrit: %v", err)
				}
			}

			// Attempt to reopen using only non-terminal statuses in the allowed set.
			reopened, err := s.SafelyReopenWrit(id, []string{WritTethered, WritWorking})
			if err != nil {
				t.Fatalf("SafelyReopenWrit returned unexpected error: %v", err)
			}
			if reopened {
				t.Errorf("SafelyReopenWrit returned reopened=true for a %s writ, want false", tc.terminalStatus)
			}

			// Confirm the writ status is unchanged.
			writ, err := s.GetWrit(id)
			if err != nil {
				t.Fatalf("GetWrit: %v", err)
			}
			if writ.Status != tc.terminalStatus {
				t.Errorf("writ status = %q after skip, want %q (unchanged)", writ.Status, tc.terminalStatus)
			}
		})
	}
}

// TestSafelyReopenWrit_AllowedStatusesReopened verifies that writs in 'tethered'
// and 'working' status ARE reopened when those statuses are in the allowed list,
// and that the assignee is cleared.
func TestSafelyReopenWrit_AllowedStatusesReopened(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	for _, tc := range []struct {
		name           string
		initialStatus  string
		allowedStatuses []string
	}{
		{"tethered", WritTethered, []string{WritTethered}},
		{"working", WritWorking, []string{WritTethered, WritWorking}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id, err := s.CreateWrit("Reopenable writ", "", "autarch", 2, nil)
			if err != nil {
				t.Fatalf("CreateWrit: %v", err)
			}

			// Set initial status with an assignee.
			if err := s.UpdateWrit(id, WritUpdates{Status: tc.initialStatus, Assignee: "ember/Toast"}); err != nil {
				t.Fatalf("UpdateWrit→%s: %v", tc.initialStatus, err)
			}

			reopened, err := s.SafelyReopenWrit(id, tc.allowedStatuses)
			if err != nil {
				t.Fatalf("SafelyReopenWrit: %v", err)
			}
			if !reopened {
				t.Errorf("SafelyReopenWrit returned reopened=false for a %s writ, want true", tc.initialStatus)
			}

			// Verify the writ is now open and assignee is cleared.
			writ, err := s.GetWrit(id)
			if err != nil {
				t.Fatalf("GetWrit: %v", err)
			}
			if writ.Status != WritOpen {
				t.Errorf("writ status = %q, want %q", writ.Status, WritOpen)
			}
			if writ.Assignee != "" {
				t.Errorf("writ assignee = %q after reopen, want empty (cleared)", writ.Assignee)
			}
		})
	}
}

// TestSafelyReopenWrit_NotFound verifies that ErrNotFound is returned (as the
// error value) when the writ does not exist, distinguishable via errors.Is.
func TestSafelyReopenWrit_NotFound(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	reopened, err := s.SafelyReopenWrit("sol-doesnotexist00", []string{WritTethered, WritWorking})
	if err == nil {
		t.Fatal("expected error for nonexistent writ, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected errors.Is(err, ErrNotFound), got: %v", err)
	}
	if reopened {
		t.Error("expected reopened=false for nonexistent writ")
	}
}

// TestSafelyReopenWrit_EmptyAllowedStatuses verifies that an empty
// allowedFromStatuses slice returns an error.
func TestSafelyReopenWrit_EmptyAllowedStatuses(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id, err := s.CreateWrit("Test writ", "", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	_, err = s.SafelyReopenWrit(id, nil)
	if err == nil {
		t.Fatal("expected error for empty allowedFromStatuses, got nil")
	}
}

// TestSafelyReopenWrit_AlreadyOpen verifies that calling SafelyReopenWrit on an
// already-open writ with 'open' NOT in the allowed set returns (false, nil).
func TestSafelyReopenWrit_AlreadyOpen(t *testing.T) {
	t.Parallel()
	s := setupWorld(t)

	id, err := s.CreateWrit("Open writ", "", "autarch", 2, nil)
	if err != nil {
		t.Fatalf("CreateWrit: %v", err)
	}

	// Writ is open; allowedFromStatuses does not include 'open'.
	reopened, err := s.SafelyReopenWrit(id, []string{WritTethered, WritWorking})
	if err != nil {
		t.Fatalf("SafelyReopenWrit: %v", err)
	}
	if reopened {
		t.Error("SafelyReopenWrit returned reopened=true for already-open writ, want false (noop)")
	}
}

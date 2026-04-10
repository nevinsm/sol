package forge

import (
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/store"
)

func TestFromStoreMR(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	merged := now.Add(time.Hour)

	smr := store.MergeRequest{
		ID:        "mr-0000000000000001",
		WritID:    "sol-a1b2c3d4e5f6a7b8",
		Branch:    "outpost/Nova/sol-a1b2c3d4e5f6a7b8",
		Phase:     "merged",
		BlockedBy: "",
		Attempts:  2,
		CreatedAt: now,
		MergedAt:  &merged,
	}

	mr := FromStoreMR(smr)

	if mr.ID != smr.ID {
		t.Errorf("ID = %q, want %q", mr.ID, smr.ID)
	}
	if mr.WritID != smr.WritID {
		t.Errorf("WritID = %q, want %q", mr.WritID, smr.WritID)
	}
	if mr.Branch != smr.Branch {
		t.Errorf("Branch = %q, want %q", mr.Branch, smr.Branch)
	}
	if mr.Status != "merged" {
		t.Errorf("Status = %q, want %q", mr.Status, "merged")
	}
	if mr.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", mr.Attempts)
	}
	if mr.MergedAt == nil || !mr.MergedAt.Equal(merged) {
		t.Errorf("MergedAt = %v, want %v", mr.MergedAt, merged)
	}
}

func TestFromForgeStatus(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	hb := &ForgeHeartbeat{
		Timestamp:  now,
		QueueDepth: 3,
		CurrentMR:  "mr-0001",
	}

	fs := FromForgeStatus(true, false, hb)

	if !fs.Running {
		t.Error("Running = false, want true")
	}
	if fs.Paused {
		t.Error("Paused = true, want false")
	}
	if fs.QueueDepth != 3 {
		t.Errorf("QueueDepth = %d, want 3", fs.QueueDepth)
	}
	if fs.CurrentMR != "mr-0001" {
		t.Errorf("CurrentMR = %q, want %q", fs.CurrentMR, "mr-0001")
	}
	if fs.LastHeartbeatAt == nil || !fs.LastHeartbeatAt.Equal(now) {
		t.Errorf("LastHeartbeatAt = %v, want %v", fs.LastHeartbeatAt, now)
	}
}

func TestFromForgeStatusNilHeartbeat(t *testing.T) {
	fs := FromForgeStatus(false, true, nil)

	if fs.Running {
		t.Error("Running = true, want false")
	}
	if !fs.Paused {
		t.Error("Paused = false, want true")
	}
	if fs.LastHeartbeatAt != nil {
		t.Errorf("LastHeartbeatAt = %v, want nil", fs.LastHeartbeatAt)
	}
}

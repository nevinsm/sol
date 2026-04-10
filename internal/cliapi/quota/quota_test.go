package quota

import (
	"testing"
	"time"

	iquota "github.com/nevinsm/sol/internal/quota"
)

func TestFromBrokerQuota(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	resets := now.Add(time.Hour)

	state := iquota.AccountState{
		Status:    iquota.Limited,
		LimitedAt: &now,
		ResetsAt:  &resets,
		LastUsed:  &now,
	}

	q := FromBrokerQuota("primary", state)

	if q.Account != "primary" {
		t.Errorf("Account = %q, want %q", q.Account, "primary")
	}
	if q.Status != "limited" {
		t.Errorf("Status = %q, want %q", q.Status, "limited")
	}
	if q.LimitedAt == nil || !q.LimitedAt.Equal(now) {
		t.Errorf("LimitedAt = %v, want %v", q.LimitedAt, now)
	}
	if q.ResetsAt == nil || !q.ResetsAt.Equal(resets) {
		t.Errorf("ResetsAt = %v, want %v", q.ResetsAt, resets)
	}
	if q.LastUsedAt == nil || !q.LastUsedAt.Equal(now) {
		t.Errorf("LastUsedAt = %v, want %v", q.LastUsedAt, now)
	}
}

func TestFromBrokerQuotaAvailable(t *testing.T) {
	state := iquota.AccountState{
		Status: iquota.Available,
	}

	q := FromBrokerQuota("secondary", state)

	if q.Status != "available" {
		t.Errorf("Status = %q, want %q", q.Status, "available")
	}
	if q.LimitedAt != nil {
		t.Errorf("LimitedAt = %v, want nil", q.LimitedAt)
	}
}

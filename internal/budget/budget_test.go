package budget

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// withFixedNow overrides nowFunc for the duration of the test, restoring
// the original on cleanup.
func withFixedNow(t *testing.T, fixed time.Time) {
	t.Helper()
	prev := nowFunc
	nowFunc = func() time.Time { return fixed }
	t.Cleanup(func() { nowFunc = prev })
}

// mockLedger implements LedgerReader for testing.
type mockLedger struct {
	spend map[string]float64
	err   error
}

func (m *mockLedger) DailySpendByAccount(account string) (float64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.spend[account], nil
}

// mockEscalations implements EscalationStore for testing.
type mockEscalations struct {
	created    []createdEscalation
	existing   map[string][]store.Escalation
	createErr  error
}

type createdEscalation struct {
	severity    string
	source      string
	description string
	sourceRef   string
}

func (m *mockEscalations) CreateEscalation(severity, source, description string, sourceRef ...string) (string, error) {
	if m.createErr != nil {
		return "", m.createErr
	}
	ref := ""
	if len(sourceRef) > 0 {
		ref = sourceRef[0]
	}
	m.created = append(m.created, createdEscalation{severity, source, description, ref})
	return "esc-test", nil
}

func (m *mockEscalations) ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error) {
	if m.existing != nil {
		return m.existing[sourceRef], nil
	}
	return nil, nil
}

func TestCheckBudget_Unlimited(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 100}}
	result, err := CheckBudget(ledger, "personal", 0)
	if err != nil {
		t.Fatalf("expected no error for unlimited, got: %v", err)
	}
	if result.Remaining != math.MaxFloat64 {
		t.Errorf("expected MaxFloat64 remaining, got %f", result.Remaining)
	}
}

func TestCheckBudget_WithinLimit(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 10.0}}
	result, err := CheckBudget(ledger, "personal", 25.0)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.Remaining != 15.0 {
		t.Errorf("expected remaining 15.0, got %f", result.Remaining)
	}
	if result.Spend != 10.0 {
		t.Errorf("expected spend 10.0, got %f", result.Spend)
	}
}

func TestCheckBudget_Exhausted(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 25.0}}
	_, err := CheckBudget(ledger, "personal", 25.0)
	if err == nil {
		t.Fatal("expected error when budget exhausted")
	}
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("expected ErrBudgetExhausted, got: %v", err)
	}
}

func TestCheckBudget_Exceeded(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 30.0}}
	_, err := CheckBudget(ledger, "personal", 25.0)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Errorf("expected ErrBudgetExhausted, got: %v", err)
	}
}

func TestCheckAccountBudget_NoAccount(t *testing.T) {
	err := CheckAccountBudget(nil, nil, "", config.BudgetSection{})
	if err != nil {
		t.Fatalf("expected nil for empty account, got: %v", err)
	}
}

func TestCheckAccountBudget_NoBudgetConfig(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 100}}
	err := CheckAccountBudget(ledger, nil, "personal", config.BudgetSection{})
	if err != nil {
		t.Fatalf("expected nil when no budget configured, got: %v", err)
	}
}

func TestCheckAccountBudget_UnlimitedAccount(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 100}}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 0},
		},
	}
	err := CheckAccountBudget(ledger, nil, "personal", budgetCfg)
	if err != nil {
		t.Fatalf("expected nil for unlimited, got: %v", err)
	}
}

func TestCheckAccountBudget_WithinBudget(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 10.0}}
	esc := &mockEscalations{}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 20.0},
		},
	}
	err := CheckAccountBudget(ledger, esc, "personal", budgetCfg)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if len(esc.created) != 0 {
		t.Errorf("expected no escalation, got %d", len(esc.created))
	}
}

func TestCheckAccountBudget_AlertThreshold(t *testing.T) {
	withFixedNow(t, time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC))
	ledger := &mockLedger{spend: map[string]float64{"personal": 21.0}}
	esc := &mockEscalations{}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 20.0},
		},
	}
	err := CheckAccountBudget(ledger, esc, "personal", budgetCfg)
	if err != nil {
		t.Fatalf("expected nil (within limit), got: %v", err)
	}
	if len(esc.created) != 1 {
		t.Fatalf("expected 1 escalation for alert, got %d", len(esc.created))
	}
	if esc.created[0].severity != "medium" {
		t.Errorf("expected medium severity, got %q", esc.created[0].severity)
	}
	want := "budget-alert:personal:2026-04-07"
	if esc.created[0].sourceRef != want {
		t.Errorf("expected %q sourceRef, got %q", want, esc.created[0].sourceRef)
	}
}

func TestCheckAccountBudget_AlertNotDuplicated(t *testing.T) {
	withFixedNow(t, time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC))
	ledger := &mockLedger{spend: map[string]float64{"personal": 21.0}}
	esc := &mockEscalations{
		existing: map[string][]store.Escalation{
			"budget-alert:personal:2026-04-07": {{ID: "esc-123", Status: "open"}},
		},
	}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 20.0},
		},
	}
	err := CheckAccountBudget(ledger, esc, "personal", budgetCfg)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if len(esc.created) != 0 {
		t.Errorf("expected no new escalation (already exists), got %d", len(esc.created))
	}
}

func TestCheckAccountBudget_Exhausted(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"personal": 25.0}}
	esc := &mockEscalations{}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 20.0},
		},
	}
	err := CheckAccountBudget(ledger, esc, "personal", budgetCfg)
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got: %v", err)
	}
	// Should fire budget-reached escalation.
	if len(esc.created) != 1 {
		t.Fatalf("expected 1 escalation for budget reached, got %d", len(esc.created))
	}
	if esc.created[0].severity != "high" {
		t.Errorf("expected high severity, got %q", esc.created[0].severity)
	}
	if esc.created[0].sourceRef != "budget-reached:personal" {
		t.Errorf("expected budget-reached:personal sourceRef, got %q", esc.created[0].sourceRef)
	}
}

func TestCheckAccountBudget_UnconfiguredAccount(t *testing.T) {
	ledger := &mockLedger{spend: map[string]float64{"other": 100}}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 25.0},
		},
	}
	err := CheckAccountBudget(ledger, nil, "other", budgetCfg)
	if err != nil {
		t.Fatalf("expected nil for unconfigured account, got: %v", err)
	}
}

// TestCheckAccountBudget_AlertAcrossDays verifies that an acknowledged
// alert from one day does NOT silently suppress alerts on subsequent
// days. Without per-day source_refs, day-2 alerts for the same account
// would never fire (CF-M6).
func TestCheckAccountBudget_AlertAcrossDays(t *testing.T) {
	day1 := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)

	ledger := &mockLedger{spend: map[string]float64{"personal": 21.0}}
	budgetCfg := config.BudgetSection{
		Accounts: map[string]config.AccountBudget{
			"personal": {DailyLimit: 25.0, AlertAt: 20.0},
		},
	}

	// Existing acknowledged alert from day 1 — its source_ref includes day 1.
	esc := &mockEscalations{
		existing: map[string][]store.Escalation{
			"budget-alert:personal:2026-04-07": {{ID: "esc-day1", Status: "open"}},
		},
	}

	// Day 1: alert already exists for this day → no new escalation.
	withFixedNow(t, day1)
	if err := CheckAccountBudget(ledger, esc, "personal", budgetCfg); err != nil {
		t.Fatalf("day 1: unexpected error: %v", err)
	}
	if len(esc.created) != 0 {
		t.Fatalf("day 1: expected no new escalation, got %d", len(esc.created))
	}

	// Day 2: same account, same spend, same outstanding day-1 alert —
	// but a fresh day means a fresh source_ref, so a new alert MUST fire.
	withFixedNow(t, day2)
	if err := CheckAccountBudget(ledger, esc, "personal", budgetCfg); err != nil {
		t.Fatalf("day 2: unexpected error: %v", err)
	}
	if len(esc.created) != 1 {
		t.Fatalf("day 2: expected exactly 1 new escalation, got %d", len(esc.created))
	}
	wantRef := "budget-alert:personal:2026-04-08"
	if esc.created[0].sourceRef != wantRef {
		t.Errorf("day 2: expected sourceRef %q, got %q", wantRef, esc.created[0].sourceRef)
	}
}

func TestCheckBudget_LedgerError(t *testing.T) {
	ledger := &mockLedger{err: fmt.Errorf("ledger connection failed")}
	_, err := CheckBudget(ledger, "personal", 25.0)
	if err == nil {
		t.Fatal("expected error when ledger fails")
	}
	if errors.Is(err, ErrBudgetExhausted) {
		t.Error("ledger error should not be ErrBudgetExhausted")
	}
	if !strings.Contains(err.Error(), "failed to check budget") {
		t.Errorf("expected wrapped error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ledger connection failed") {
		t.Errorf("expected original error in chain, got: %v", err)
	}
}

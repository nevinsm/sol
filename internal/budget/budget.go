// Package budget implements per-account daily budget controls.
//
// Budget checks gate dispatch and session spawning. When an account's
// daily spend reaches its configured limit, new work using that account
// is refused. Running sessions complete normally.
package budget

import (
	"errors"
	"fmt"
	"math"

	"github.com/nevinsm/sol/internal/config"
	"github.com/nevinsm/sol/internal/store"
)

// ErrBudgetExhausted is returned when an account's daily budget has been reached.
// Use errors.Is to check for this error.
var ErrBudgetExhausted = errors.New("account daily budget exhausted")

// LedgerReader provides read access to token usage data for budget checks.
type LedgerReader interface {
	DailySpendByAccount(account string) (float64, error)
}

// EscalationStore provides access to escalation operations for budget alerts.
type EscalationStore interface {
	CreateEscalation(severity, source, description string, sourceRef ...string) (string, error)
	ListEscalationsBySourceRef(sourceRef string) ([]store.Escalation, error)
}

// CheckResult holds the outcome of a budget check.
type CheckResult struct {
	Remaining float64 // remaining budget (math.MaxFloat64 if unlimited)
	Spend     float64 // current daily spend
	Limit     float64 // configured daily limit (0 = unlimited)
}

// CheckBudget verifies that an account has remaining daily budget.
// Returns remaining budget and nil error if within budget.
// Returns ErrBudgetExhausted (wrapped with details) if budget is exceeded.
// A daily_limit of 0 means unlimited — always passes.
func CheckBudget(ledger LedgerReader, account string, limit float64) (CheckResult, error) {
	if limit <= 0 {
		return CheckResult{Remaining: math.MaxFloat64}, nil
	}

	spend, err := ledger.DailySpendByAccount(account)
	if err != nil {
		return CheckResult{}, fmt.Errorf("failed to check budget for account %q: %w", account, err)
	}

	result := CheckResult{
		Remaining: limit - spend,
		Spend:     spend,
		Limit:     limit,
	}

	if spend >= limit {
		return result, fmt.Errorf("account %q daily budget exhausted ($%.2f / $%.2f): %w",
			account, spend, limit, ErrBudgetExhausted)
	}

	return result, nil
}

// CheckAccountBudget checks the budget for a named account using the global config.
// Returns nil if the account has no configured budget limit or has remaining budget.
// Returns ErrBudgetExhausted if the limit is reached.
// Also fires alert escalations when the alert_at threshold is crossed.
func CheckAccountBudget(ledger LedgerReader, escalations EscalationStore, account string, budgetCfg config.BudgetSection) error {
	if account == "" {
		return nil // no account attribution, skip budget check
	}

	ab, ok := budgetCfg.Accounts[account]
	if !ok {
		return nil // no budget configured for this account
	}

	if ab.DailyLimit <= 0 {
		return nil // unlimited
	}

	result, err := CheckBudget(ledger, account, ab.DailyLimit)
	if err != nil {
		// Fire budget-reached escalation.
		if errors.Is(err, ErrBudgetExhausted) {
			fireBudgetReachedEscalation(escalations, account, ab.DailyLimit)
		}
		return err
	}

	// Check alert threshold — fire once per budget period.
	if ab.AlertAt > 0 && result.Spend >= ab.AlertAt && escalations != nil {
		fireAlertIfNeeded(escalations, account, result.Spend, ab.AlertAt, ab.DailyLimit)
	}

	return nil
}

// fireAlertIfNeeded fires a budget alert escalation if one hasn't already been
// fired for this account in the current budget period.
func fireAlertIfNeeded(escalations EscalationStore, account string, spend, alertAt, limit float64) {
	if escalations == nil {
		return
	}
	sourceRef := fmt.Sprintf("budget-alert:%s", account)

	// Check if we already fired an alert for this account (open escalation with this sourceRef).
	existing, err := escalations.ListEscalationsBySourceRef(sourceRef)
	if err == nil && len(existing) > 0 {
		return // already alerted this period
	}

	desc := fmt.Sprintf("Account %q daily spend approaching limit: $%.2f / $%.2f (alert threshold: $%.2f)",
		account, spend, limit, alertAt)
	escalations.CreateEscalation("medium", "budget", desc, sourceRef)
}

// fireBudgetReachedEscalation fires a high-severity escalation when an account's
// daily budget is fully exhausted.
func fireBudgetReachedEscalation(escalations EscalationStore, account string, limit float64) {
	if escalations == nil {
		return
	}
	sourceRef := fmt.Sprintf("budget-reached:%s", account)

	// Don't spam — check if already fired.
	existing, err := escalations.ListEscalationsBySourceRef(sourceRef)
	if err == nil && len(existing) > 0 {
		return
	}

	desc := fmt.Sprintf("Account %q daily budget reached ($%.2f limit)", account, limit)
	escalations.CreateEscalation("high", "budget", desc, sourceRef)
}

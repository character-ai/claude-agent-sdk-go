package claudeagent

import (
	"testing"
	"time"
)

func TestBudgetTrackerNilIsNoop(t *testing.T) {
	var b *budgetTracker
	if err := b.record(1000, 1000, 5.0); err != nil {
		t.Errorf("nil tracker should be noop, got: %v", err)
	}
	if err := b.check(); err != nil {
		t.Errorf("nil tracker check should be noop, got: %v", err)
	}
}

func TestBudgetTrackerNilConfig(t *testing.T) {
	b := newBudgetTracker(nil)
	if b != nil {
		t.Error("expected nil tracker for nil config")
	}
}

func TestBudgetTrackerTokenLimit(t *testing.T) {
	b := newBudgetTracker(&BudgetConfig{MaxTokens: 100})

	if err := b.record(40, 40, 0); err != nil {
		t.Fatalf("unexpected error at 80 tokens: %v", err)
	}
	// 80 tokens — under limit
	if err := b.check(); err != nil {
		t.Fatalf("unexpected error at 80 tokens: %v", err)
	}

	// 80+30 = 110 tokens — over limit
	err := b.record(20, 10, 0)
	if err == nil {
		t.Fatal("expected BudgetExceededError at 110 tokens")
	}
	var budgetErr *BudgetExceededError
	if !isBudgetExceeded(err, &budgetErr) {
		t.Fatalf("expected *BudgetExceededError, got %T: %v", err, err)
	}
}

func TestBudgetTrackerCostLimit(t *testing.T) {
	b := newBudgetTracker(&BudgetConfig{MaxCostUSD: 0.10})

	if err := b.record(0, 0, 0.05); err != nil {
		t.Fatalf("unexpected error at $0.05: %v", err)
	}
	err := b.record(0, 0, 0.06) // $0.11 total
	if err == nil {
		t.Fatal("expected BudgetExceededError at $0.11")
	}
}

func TestBudgetTrackerDurationLimit(t *testing.T) {
	b := newBudgetTracker(&BudgetConfig{MaxDuration: 10 * time.Millisecond})

	// Immediately under limit
	if err := b.check(); err != nil {
		t.Fatalf("unexpected error immediately: %v", err)
	}

	time.Sleep(15 * time.Millisecond)

	err := b.check()
	if err == nil {
		t.Fatal("expected BudgetExceededError after duration exceeded")
	}
}

func TestBudgetTrackerNoLimitsSetNeverErrors(t *testing.T) {
	b := newBudgetTracker(&BudgetConfig{})

	for i := 0; i < 10; i++ {
		if err := b.record(10000, 10000, 100.0); err != nil {
			t.Fatalf("unexpected error with no limits: %v", err)
		}
	}
}

func TestBudgetExceededErrorMessage(t *testing.T) {
	err := &BudgetExceededError{Reason: "token limit: used 200, max 100"}
	if err.Error() != "budget exceeded: token limit: used 200, max 100" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

// isBudgetExceeded is a helper for type-asserting budget errors.
func isBudgetExceeded(err error, target **BudgetExceededError) bool {
	if e, ok := err.(*BudgetExceededError); ok {
		*target = e
		return true
	}
	return false
}

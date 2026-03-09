package claudeagent

import (
	"fmt"
	"time"
)

// BudgetConfig sets resource limits for a session. Any field left at its
// zero value is treated as unlimited.
type BudgetConfig struct {
	// MaxTokens stops the session once cumulative input+output tokens exceed
	// this value. Works for both Agent and APIAgent.
	MaxTokens int

	// MaxCostUSD stops the session once cumulative cost exceeds this value (USD).
	// Populated only for the CLI-based Agent via ResultMessage.Cost.
	MaxCostUSD float64

	// MaxDuration stops the session once wall-clock time since session start
	// exceeds this duration. Works for both Agent and APIAgent.
	MaxDuration time.Duration
}

// BudgetExceededError is returned as an AgentEventError when a budget limit is hit.
type BudgetExceededError struct {
	Reason string
}

func (e *BudgetExceededError) Error() string {
	return "budget exceeded: " + e.Reason
}

// budgetTracker accumulates per-turn usage and checks limits.
// nil receiver is safe — all methods are no-ops.
type budgetTracker struct {
	cfg          BudgetConfig
	sessionStart time.Time
	tokens       int
	costUSD      float64
}

func newBudgetTracker(cfg *BudgetConfig) *budgetTracker {
	if cfg == nil {
		return nil
	}
	return &budgetTracker{
		cfg:          *cfg,
		sessionStart: time.Now(),
	}
}

// record adds usage from a completed turn and returns an error if any limit
// is now exceeded. Safe to call with a nil receiver.
func (b *budgetTracker) record(inputTokens, outputTokens int, costUSD float64) error {
	if b == nil {
		return nil
	}
	b.tokens += inputTokens + outputTokens
	b.costUSD += costUSD
	return b.check()
}

// check returns an error if any limit is currently exceeded without recording
// new usage. Safe to call with a nil receiver.
func (b *budgetTracker) check() error {
	if b == nil {
		return nil
	}
	if b.cfg.MaxTokens > 0 && b.tokens > b.cfg.MaxTokens {
		return &BudgetExceededError{
			Reason: fmt.Sprintf("token limit: used %d, max %d", b.tokens, b.cfg.MaxTokens),
		}
	}
	if b.cfg.MaxCostUSD > 0 && b.costUSD > b.cfg.MaxCostUSD {
		return &BudgetExceededError{
			Reason: fmt.Sprintf("cost limit: spent $%.4f, max $%.4f", b.costUSD, b.cfg.MaxCostUSD),
		}
	}
	if b.cfg.MaxDuration > 0 && time.Since(b.sessionStart) > b.cfg.MaxDuration {
		return &BudgetExceededError{
			Reason: fmt.Sprintf("time limit: elapsed %v, max %v",
				time.Since(b.sessionStart).Round(time.Millisecond), b.cfg.MaxDuration),
		}
	}
	return nil
}

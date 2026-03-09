package claudeagent

import (
	"context"
	"time"
)

// RetryConfig configures automatic retry behavior for tool execution.
// Attach it globally via AgentConfig.Retry / APIAgentConfig.Retry,
// or per-tool via ToolDefinition.RetryConfig (per-tool takes precedence).
type RetryConfig struct {
	// MaxAttempts is the total number of execution attempts including the first.
	// 0 or 1 means no retry.
	MaxAttempts int

	// Backoff is the base wait duration before the first retry.
	// Each subsequent retry doubles the wait: Backoff, 2×Backoff, 4×Backoff, …
	Backoff time.Duration

	// RetryOn determines whether a given error should trigger a retry.
	// If nil, all errors are retried up to MaxAttempts.
	RetryOn func(err error) bool
}

// executeWithRetry calls fn up to cfg.MaxAttempts times with exponential backoff.
// Returns the last error if all attempts fail or ctx is cancelled.
func executeWithRetry(ctx context.Context, cfg *RetryConfig, fn func() (string, error)) (string, error) {
	if cfg == nil || cfg.MaxAttempts <= 1 {
		return fn()
	}

	var (
		result string
		err    error
	)

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		result, err = fn()
		if err == nil {
			return result, nil
		}

		// Non-retryable error
		if cfg.RetryOn != nil && !cfg.RetryOn(err) {
			return result, err
		}

		// Last attempt — return immediately without sleeping
		if attempt == cfg.MaxAttempts-1 {
			break
		}

		// Exponential backoff: Backoff * 2^attempt
		wait := cfg.Backoff << uint(attempt)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}

	return result, err
}

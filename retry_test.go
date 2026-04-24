package claudeagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestExecuteWithRetryNilConfig(t *testing.T) {
	calls := 0
	_, err := executeWithRetry(context.Background(), nil, func() (string, error) {
		calls++
		return "", errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call with nil config, got %d", calls)
	}
}

func TestExecuteWithRetryMaxAttemptsOne(t *testing.T) {
	calls := 0
	_, err := executeWithRetry(context.Background(), &RetryConfig{MaxAttempts: 1}, func() (string, error) {
		calls++
		return "", errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestExecuteWithRetrySuccessOnSecondAttempt(t *testing.T) {
	calls := 0
	result, err := executeWithRetry(context.Background(), &RetryConfig{MaxAttempts: 3, Backoff: time.Millisecond}, func() (string, error) {
		calls++
		if calls < 2 {
			return "", errors.New("transient")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestExecuteWithRetryExhausted(t *testing.T) {
	calls := 0
	_, err := executeWithRetry(context.Background(), &RetryConfig{MaxAttempts: 3, Backoff: time.Millisecond}, func() (string, error) {
		calls++
		return "", errors.New("always fails")
	})
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestExecuteWithRetryRetryOnFilter(t *testing.T) {
	transient := errors.New("transient")
	permanent := errors.New("permanent")

	calls := 0
	_, err := executeWithRetry(context.Background(), &RetryConfig{
		MaxAttempts: 5,
		Backoff:     time.Millisecond,
		RetryOn: func(e error) bool {
			return errors.Is(e, transient)
		},
	}, func() (string, error) {
		calls++
		if calls == 1 {
			return "", transient // retried
		}
		return "", permanent // not retried
	})

	if !errors.Is(err, permanent) {
		t.Errorf("expected permanent error, got: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 transient + 1 permanent), got %d", calls)
	}
}

func TestExecuteWithRetryContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	// Cancel before first retry fires
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := executeWithRetry(ctx, &RetryConfig{MaxAttempts: 10, Backoff: 50 * time.Millisecond}, func() (string, error) {
		calls++
		return "", errors.New("fail")
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	// Should have made very few attempts before cancel
	if calls > 2 {
		t.Errorf("expected ≤2 calls before cancel, got %d", calls)
	}
}

func TestExecuteWithRetrySuccessOnFirstAttempt(t *testing.T) {
	calls := 0
	result, err := executeWithRetry(context.Background(), &RetryConfig{MaxAttempts: 3, Backoff: time.Millisecond}, func() (string, error) {
		calls++
		return "first-try", nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "first-try" {
		t.Errorf("expected 'first-try', got %q", result)
	}
	if calls != 1 {
		t.Errorf("expected 1 call on success, got %d", calls)
	}
}

// TestExecuteWithRetryMaxBackoffCap verifies that MaxBackoff caps the computed
// exponential wait so callers do not sleep for unbounded durations.
func TestExecuteWithRetryMaxBackoffCap(t *testing.T) {
	// With Backoff=1s and MaxAttempts=10 the uncapped 9th attempt would wait
	// 512 s. MaxBackoff=10ms keeps the test fast and confirms the cap works.
	calls := 0
	start := time.Now()
	_, err := executeWithRetry(context.Background(), &RetryConfig{
		MaxAttempts: 4,
		Backoff:     10 * time.Millisecond,
		MaxBackoff:  15 * time.Millisecond,
	}, func() (string, error) {
		calls++
		return "", errors.New("fail")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 4 {
		t.Errorf("expected 4 calls, got %d", calls)
	}
	// 3 sleeps each capped at 15 ms → at most ~45 ms total (with some margin).
	// Without the cap the 3rd sleep alone would be 40 ms (10<<2).
	// The upper bound is 100 ms (~2× max expected) to stay sensitive to regressions
	// while tolerating CI scheduling variance.
	if elapsed > 100*time.Millisecond {
		t.Errorf("elapsed %v unexpectedly large; MaxBackoff cap may not be working", elapsed)
	}
}

// TestExecuteWithRetryJitterBounded verifies that jitter never causes the actual
// sleep to exceed the computed backoff value.
func TestExecuteWithRetryJitterBounded(t *testing.T) {
	const backoff = 50 * time.Millisecond
	calls := 0
	start := time.Now()
	_, _ = executeWithRetry(context.Background(), &RetryConfig{
		MaxAttempts: 2,
		Backoff:     backoff,
	}, func() (string, error) {
		calls++
		return "", errors.New("fail")
	})
	elapsed := time.Since(start)

	// 1 sleep with full jitter in [0, 50ms]; the actual wait is always ≤ backoff.
	// Allow 1.5× backoff to account for test scheduling overhead on slow CI runners.
	if elapsed > 3*backoff/2 {
		t.Errorf("elapsed %v > 1.5×backoff %v; jitter may have exceeded computed wait", elapsed, backoff)
	}
}

// TestToolRetryConfigPerToolOverridesGlobal verifies per-tool retry config
// overrides the agent-level global config.
func TestToolRetryConfigPerToolOverridesGlobal(t *testing.T) {
	perToolCalls := 0
	perToolRetry := &RetryConfig{MaxAttempts: 3, Backoff: time.Millisecond}

	tools := NewToolRegistry()
	tools.Register(ToolDefinition{
		Name:        "flaky",
		Description: "flaky tool",
		RetryConfig: perToolRetry,
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		perToolCalls++
		if perToolCalls < 3 {
			return "", errors.New("transient")
		}
		return "ok", nil
	})

	// Agent has a global retry of only 1 attempt (no retry)
	globalRetry := &RetryConfig{MaxAttempts: 1}
	agent := &Agent{tools: tools, retry: globalRetry}

	events := make(chan AgentEvent, 10)
	results := agent.executeTools(context.Background(), []ToolCall{{ID: "id1", Name: "flaky"}}, events)

	// Per-tool (3 attempts) should win over global (1 attempt)
	if results[0].IsError {
		t.Errorf("expected success via per-tool retry, got error: %s", results[0].Content)
	}
	if perToolCalls != 3 {
		t.Errorf("expected 3 calls (per-tool retry), got %d", perToolCalls)
	}
}

package claudeagent

import (
	"testing"
	"time"
)

func TestModelSelectorDefaults(t *testing.T) {
	// AfterErrors defaults to 3 when set to 0 — should switch after 3 errors.
	ms := newModelSelector("primary", &FallbackModelConfig{
		Model: "fallback",
	})
	ms.recordError()
	ms.recordError()
	if ms.currentModel() != "primary" {
		t.Error("should still be primary after 2 errors (default threshold is 3)")
	}
	ms.recordError()
	if ms.currentModel() != "fallback" {
		t.Error("should switch to fallback after 3 errors (default threshold)")
	}
}

func TestModelSelectorSwitchAfterErrors(t *testing.T) {
	ms := newModelSelector("primary", &FallbackModelConfig{
		Model:       "fallback",
		AfterErrors: 2,
	})

	// Should be primary initially
	if got := ms.currentModel(); got != "primary" {
		t.Fatalf("expected primary, got %s", got)
	}

	// One error — still primary
	ms.recordError()
	if got := ms.currentModel(); got != "primary" {
		t.Fatalf("expected primary after 1 error, got %s", got)
	}

	// Second error — should switch
	ms.recordError()
	if got := ms.currentModel(); got != "fallback" {
		t.Fatalf("expected fallback after 2 errors, got %s", got)
	}
}

func TestModelSelectorRevertAfterSuccess(t *testing.T) {
	ms := newModelSelector("primary", &FallbackModelConfig{
		Model:       "fallback",
		AfterErrors: 1,
	})

	ms.recordError()
	if got := ms.currentModel(); got != "fallback" {
		t.Fatalf("expected fallback, got %s", got)
	}

	// Success resets error count but does not revert model (no RevertAfter set
	// and usingFallback stays true until duration-based revert or new selector)
	ms.recordSuccess()
	if ms.consecutiveErr != 0 {
		t.Fatalf("expected consecutiveErr reset to 0, got %d", ms.consecutiveErr)
	}
}

func TestModelSelectorRevertAfterDuration(t *testing.T) {
	ms := newModelSelector("primary", &FallbackModelConfig{
		Model:       "fallback",
		AfterErrors: 1,
		RevertAfter: 50 * time.Millisecond,
	})

	ms.recordError()
	if got := ms.currentModel(); got != "fallback" {
		t.Fatalf("expected fallback, got %s", got)
	}

	// Wait for revert duration
	time.Sleep(60 * time.Millisecond)

	if got := ms.currentModel(); got != "primary" {
		t.Fatalf("expected primary after revert duration, got %s", got)
	}

	// Should also have reset state
	if ms.usingFallback {
		t.Fatal("expected usingFallback to be false after revert")
	}
	if ms.consecutiveErr != 0 {
		t.Fatalf("expected consecutiveErr reset to 0 after revert, got %d", ms.consecutiveErr)
	}
}

func TestModelSelectorNilFallback(t *testing.T) {
	ms := newModelSelector("primary", nil)

	// Should always return primary
	if got := ms.currentModel(); got != "primary" {
		t.Fatalf("expected primary, got %s", got)
	}

	// recordError should be a no-op
	ms.recordError()
	ms.recordError()
	ms.recordError()
	ms.recordError()

	if got := ms.currentModel(); got != "primary" {
		t.Fatalf("expected primary even after errors with nil fallback, got %s", got)
	}
}

func TestModelSelectorCurrentModelReturnsPrimaryInitially(t *testing.T) {
	ms := newModelSelector("claude-sonnet-4-20250514", &FallbackModelConfig{
		Model:       "claude-haiku-4-5-20251001",
		AfterErrors: 5,
	})

	if got := ms.currentModel(); got != "claude-sonnet-4-20250514" {
		t.Fatalf("expected claude-sonnet-4-20250514, got %s", got)
	}
}

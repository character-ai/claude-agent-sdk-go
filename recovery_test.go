package claudeagent

import (
	"testing"
)

func TestWithDefaults_FillsZeroValues(t *testing.T) {
	r := &MaxTokensRecovery{}
	d := r.withDefaults()

	if d.ScaleFactor != 2.0 {
		t.Errorf("ScaleFactor = %v, want 2.0", d.ScaleFactor)
	}
	if d.MaxRetries != 2 {
		t.Errorf("MaxRetries = %v, want 2", d.MaxRetries)
	}
	if d.Ceiling != 16384 {
		t.Errorf("Ceiling = %v, want 16384", d.Ceiling)
	}
}

func TestWithDefaults_PreservesExplicitValues(t *testing.T) {
	r := &MaxTokensRecovery{ScaleFactor: 1.5, MaxRetries: 3, Ceiling: 8192}
	d := r.withDefaults()

	if d.ScaleFactor != 1.5 {
		t.Errorf("ScaleFactor = %v, want 1.5", d.ScaleFactor)
	}
	if d.MaxRetries != 3 {
		t.Errorf("MaxRetries = %v, want 3", d.MaxRetries)
	}
	if d.Ceiling != 8192 {
		t.Errorf("Ceiling = %v, want 8192", d.Ceiling)
	}
}

func TestNextMaxTokens_Scaling(t *testing.T) {
	r := &MaxTokensRecovery{ScaleFactor: 2.0, Ceiling: 16384}
	d := r.withDefaults()

	got := d.nextMaxTokens(4096)
	if got != 8192 {
		t.Errorf("nextMaxTokens(4096) = %d, want 8192", got)
	}
}

func TestNextMaxTokens_Ceiling(t *testing.T) {
	r := &MaxTokensRecovery{ScaleFactor: 2.0, Ceiling: 10000}
	d := r.withDefaults()

	got := d.nextMaxTokens(8000)
	if got != 10000 {
		t.Errorf("nextMaxTokens(8000) = %d, want 10000 (ceiling)", got)
	}
}

func TestNextMaxTokens_AlreadyAtCeiling(t *testing.T) {
	r := &MaxTokensRecovery{ScaleFactor: 2.0, Ceiling: 4096}
	d := r.withDefaults()

	got := d.nextMaxTokens(4096)
	if got != 4096 {
		t.Errorf("nextMaxTokens(4096) = %d, want 4096 (ceiling)", got)
	}
}

func TestShouldRecoverMaxTokens_TrueWhenMaxTokensNoTools(t *testing.T) {
	cfg := &MaxTokensRecovery{}
	if !shouldRecoverMaxTokens("max_tokens", nil, cfg, 0) {
		t.Error("expected true for max_tokens with no tool calls, attempt 0")
	}
	if !shouldRecoverMaxTokens("max_tokens", []ToolCall{}, cfg, 1) {
		t.Error("expected true for max_tokens with empty tool calls, attempt 1")
	}
}

func TestShouldRecoverMaxTokens_FalseWhenToolsPresent(t *testing.T) {
	cfg := &MaxTokensRecovery{}
	tools := []ToolCall{{ID: "1", Name: "test"}}
	if shouldRecoverMaxTokens("max_tokens", tools, cfg, 0) {
		t.Error("expected false when tool calls are present")
	}
}

func TestShouldRecoverMaxTokens_FalseWhenAttemptsExhausted(t *testing.T) {
	cfg := &MaxTokensRecovery{MaxRetries: 2}
	if shouldRecoverMaxTokens("max_tokens", nil, cfg, 2) {
		t.Error("expected false when attempts >= MaxRetries")
	}
	if shouldRecoverMaxTokens("max_tokens", nil, cfg, 3) {
		t.Error("expected false when attempts > MaxRetries")
	}
}

func TestShouldRecoverMaxTokens_FalseWhenNotMaxTokens(t *testing.T) {
	cfg := &MaxTokensRecovery{}
	if shouldRecoverMaxTokens("end_turn", nil, cfg, 0) {
		t.Error("expected false for non-max_tokens stop reason")
	}
}

func TestShouldRecoverMaxTokens_FalseWhenNilConfig(t *testing.T) {
	if shouldRecoverMaxTokens("max_tokens", nil, nil, 0) {
		t.Error("expected false when config is nil")
	}
}

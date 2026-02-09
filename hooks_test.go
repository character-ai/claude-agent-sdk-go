package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestHooksAllow(t *testing.T) {
	hooks := NewHooks()
	hooks.OnTool("test").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		return AllowHook()
	})

	hookCtx := HookContext{
		ToolName:  "test",
		ToolUseID: "123",
		Input:     json.RawMessage(`{"arg": "value"}`),
	}

	result, err := hooks.RunPreHooks(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != HookAllow {
		t.Fatalf("expected allow, got %s", result.Decision)
	}
}

func TestHooksDeny(t *testing.T) {
	hooks := NewHooks()
	hooks.OnTool("dangerous").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		return DenyHook("tool is dangerous")
	})

	hookCtx := HookContext{
		ToolName:  "dangerous",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	result, err := hooks.RunPreHooks(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != HookDeny {
		t.Fatalf("expected deny, got %s", result.Decision)
	}
	if result.Reason != "tool is dangerous" {
		t.Fatalf("unexpected reason: %s", result.Reason)
	}
}

func TestHooksModify(t *testing.T) {
	hooks := NewHooks()
	hooks.OnTool("search").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		newInput := json.RawMessage(`{"query": "modified"}`)
		return ModifyHook(newInput)
	})

	hookCtx := HookContext{
		ToolName:  "search",
		ToolUseID: "123",
		Input:     json.RawMessage(`{"query": "original"}`),
	}

	result, err := hooks.RunPreHooks(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != HookModify {
		t.Fatalf("expected modify, got %s", result.Decision)
	}
	if string(result.ModifiedInput) != `{"query": "modified"}` {
		t.Fatalf("unexpected modified input: %s", result.ModifiedInput)
	}
}

func TestHooksWildcard(t *testing.T) {
	called := false
	hooks := NewHooks()
	hooks.OnAllTools().Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		called = true
		return AllowHook()
	})

	hookCtx := HookContext{
		ToolName:  "any_tool",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	_, _ = hooks.RunPreHooks(context.Background(), hookCtx)
	if !called {
		t.Fatal("wildcard hook was not called")
	}
}

func TestHooksNoMatch(t *testing.T) {
	hooks := NewHooks()
	hooks.OnTool("specific").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		return DenyHook("should not be called")
	})

	hookCtx := HookContext{
		ToolName:  "other",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	result, err := hooks.RunPreHooks(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should allow by default when no hooks match
	if result.Decision != HookAllow {
		t.Fatalf("expected allow when no hooks match, got %s", result.Decision)
	}
}

func TestHooksChaining(t *testing.T) {
	order := []string{}

	hooks := NewHooks()
	hooks.OnTool("test").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		order = append(order, "first")
		return AllowHook()
	})
	hooks.OnTool("test").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		order = append(order, "second")
		return AllowHook()
	})

	hookCtx := HookContext{
		ToolName:  "test",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	_, _ = hooks.RunPreHooks(context.Background(), hookCtx)
	if len(order) != 2 {
		t.Fatalf("expected 2 hooks to run, got %d", len(order))
	}
	if order[0] != "first" || order[1] != "second" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestPostToolUseHook(t *testing.T) {
	var capturedResult string
	var capturedIsError bool

	hooks := NewHooks()
	hooks.OnTool("test").After(func(ctx context.Context, hookCtx HookContext, result string, isError bool) HookResult {
		capturedResult = result
		capturedIsError = isError
		return AllowHook()
	})

	hookCtx := HookContext{
		ToolName:  "test",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	_ = hooks.RunPostHooks(context.Background(), hookCtx, "tool output", false)

	if capturedResult != "tool output" {
		t.Fatalf("unexpected result: %s", capturedResult)
	}
	if capturedIsError {
		t.Fatal("expected isError to be false")
	}
}

func TestHooksRegexMatcher(t *testing.T) {
	called := false
	hooks := NewHooks()
	hooks.OnToolRegex(`^mcp__.*`).Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		called = true
		return AllowHook()
	})

	hookCtx := HookContext{
		ToolName:  "mcp__server__tool",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	_, _ = hooks.RunPreHooks(context.Background(), hookCtx)
	if !called {
		t.Fatal("regex hook was not called for matching tool")
	}
}

func TestHooksRegexNoMatch(t *testing.T) {
	called := false
	hooks := NewHooks()
	hooks.OnToolRegex(`^mcp__.*`).Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		called = true
		return AllowHook()
	})

	hookCtx := HookContext{
		ToolName:  "Bash",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	_, _ = hooks.RunPreHooks(context.Background(), hookCtx)
	if called {
		t.Fatal("regex hook should not match non-matching tool")
	}
}

func TestHooksTimeout(t *testing.T) {
	hooks := NewHooks()
	hooks.OnTool("slow").WithTimeout(50 * time.Millisecond).Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		// Simulate a slow hook
		select {
		case <-time.After(500 * time.Millisecond):
			return AllowHook()
		case <-ctx.Done():
			return DenyHook("context canceled")
		}
	})

	hookCtx := HookContext{
		ToolName:  "slow",
		ToolUseID: "123",
		Input:     json.RawMessage(`{}`),
	}

	result, err := hooks.RunPreHooks(context.Background(), hookCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Decision != HookDeny {
		t.Fatalf("expected deny from timeout, got %s", result.Decision)
	}
	if result.Reason != "hook timed out" {
		t.Fatalf("expected 'hook timed out' reason, got: %s", result.Reason)
	}
}

func TestHooksEventHandler(t *testing.T) {
	var capturedEvent HookEventData
	hooks := NewHooks()
	hooks.OnEvent(HookSessionStart, func(ctx context.Context, data HookEventData) {
		capturedEvent = data
	})

	hooks.EmitEvent(context.Background(), HookEventData{
		Event:   HookSessionStart,
		Message: "test session",
	})

	if capturedEvent.Event != HookSessionStart {
		t.Fatalf("expected SessionStart event, got %s", capturedEvent.Event)
	}
	if capturedEvent.Message != "test session" {
		t.Fatalf("expected 'test session' message, got %s", capturedEvent.Message)
	}
}

func TestHooksEventHandlerNoMatch(t *testing.T) {
	called := false
	hooks := NewHooks()
	hooks.OnEvent(HookSessionStart, func(ctx context.Context, data HookEventData) {
		called = true
	})

	// Emit a different event
	hooks.EmitEvent(context.Background(), HookEventData{
		Event:   HookSessionEnd,
		Message: "session ended",
	})

	if called {
		t.Fatal("SessionStart handler should not be called for SessionEnd event")
	}
}

func TestHooksPostToolFailureEvent(t *testing.T) {
	var capturedFailure HookEventData
	hooks := NewHooks()
	hooks.OnEvent(HookPostToolUseFailure, func(ctx context.Context, data HookEventData) {
		capturedFailure = data
	})

	hookCtx := HookContext{
		ToolName:  "failing_tool",
		ToolUseID: "456",
		Input:     json.RawMessage(`{}`),
	}

	_ = hooks.RunPostHooks(context.Background(), hookCtx, "some error occurred", true)

	if capturedFailure.Event != HookPostToolUseFailure {
		t.Fatalf("expected PostToolUseFailure event, got %s", capturedFailure.Event)
	}
	if capturedFailure.ToolName != "failing_tool" {
		t.Fatalf("expected tool name 'failing_tool', got %s", capturedFailure.ToolName)
	}
	if capturedFailure.Error != "some error occurred" {
		t.Fatalf("expected error message, got %s", capturedFailure.Error)
	}
}

func TestHooksEmitEventNilSafe(t *testing.T) {
	// Should not panic on nil hooks
	var hooks *Hooks
	hooks.EmitEvent(context.Background(), HookEventData{Event: HookSessionStart})
}

func TestHooksEnhancedResult(t *testing.T) {
	result := HookResult{
		Decision:          HookAllow,
		AdditionalContext: "extra context",
		SystemMessage:     "system instruction",
		Continue:          true,
		SuppressOutput:    false,
	}

	if result.AdditionalContext != "extra context" {
		t.Fatalf("expected additional context")
	}
	if result.SystemMessage != "system instruction" {
		t.Fatalf("expected system message")
	}
	if !result.Continue {
		t.Fatalf("expected continue to be true")
	}
}

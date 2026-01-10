package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
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

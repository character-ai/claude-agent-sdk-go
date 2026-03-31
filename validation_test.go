package claudeagent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestValidateInput_RejectsPreventsExecution(t *testing.T) {
	tools := NewToolRegistry()
	executed := false
	tools.Register(ToolDefinition{
		Name:        "guarded",
		Description: "tool with validation",
		InputSchema: map[string]any{"type": "object"},
		ValidateInput: func(_ context.Context, _ json.RawMessage) error {
			return errors.New("bad input")
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		executed = true
		return "ok", nil
	})

	events := make(chan AgentEvent, 10)
	tc := ToolCall{ID: "1", Name: "guarded", Input: json.RawMessage(`{}`)}
	resp := executeOneTool(context.Background(), tc, tools, nil, nil, nil, nil, events)

	if !resp.IsError {
		t.Fatal("expected error response")
	}
	if resp.Content != "Validation error: bad input" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}
	if executed {
		t.Fatal("handler should not have been executed")
	}
}

func TestCheckPermissions_RejectsPreventsExecution(t *testing.T) {
	tools := NewToolRegistry()
	executed := false
	tools.Register(ToolDefinition{
		Name:        "restricted",
		Description: "tool with permission check",
		InputSchema: map[string]any{"type": "object"},
		CheckPermissions: func(_ context.Context, _ json.RawMessage) error {
			return errors.New("not authorized")
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		executed = true
		return "ok", nil
	})

	events := make(chan AgentEvent, 10)
	tc := ToolCall{ID: "1", Name: "restricted", Input: json.RawMessage(`{}`)}
	resp := executeOneTool(context.Background(), tc, tools, nil, nil, nil, nil, events)

	if !resp.IsError {
		t.Fatal("expected error response")
	}
	if resp.Content != "Permission denied: not authorized" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}
	if executed {
		t.Fatal("handler should not have been executed")
	}
}

func TestNilValidators_AllowExecution(t *testing.T) {
	tools := NewToolRegistry()
	executed := false
	tools.Register(ToolDefinition{
		Name:        "open",
		Description: "tool without validators",
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		executed = true
		return "done", nil
	})

	events := make(chan AgentEvent, 10)
	tc := ToolCall{ID: "1", Name: "open", Input: json.RawMessage(`{}`)}
	resp := executeOneTool(context.Background(), tc, tools, nil, nil, nil, nil, events)

	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	if !executed {
		t.Fatal("handler should have been executed")
	}
	if resp.Content != "done" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}
}

func TestCheckPermissions_RunsBeforeValidateInput(t *testing.T) {
	tools := NewToolRegistry()
	var order []string
	var mu sync.Mutex
	tools.Register(ToolDefinition{
		Name:        "ordered",
		Description: "tool with both validators",
		InputSchema: map[string]any{"type": "object"},
		CheckPermissions: func(_ context.Context, _ json.RawMessage) error {
			mu.Lock()
			order = append(order, "permissions")
			mu.Unlock()
			return errors.New("denied")
		},
		ValidateInput: func(_ context.Context, _ json.RawMessage) error {
			mu.Lock()
			order = append(order, "validate")
			mu.Unlock()
			return errors.New("invalid")
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	events := make(chan AgentEvent, 10)
	tc := ToolCall{ID: "1", Name: "ordered", Input: json.RawMessage(`{}`)}
	resp := executeOneTool(context.Background(), tc, tools, nil, nil, nil, nil, events)

	if !resp.IsError {
		t.Fatal("expected error")
	}
	// CheckPermissions should run first and short-circuit
	if resp.Content != "Permission denied: denied" {
		t.Fatalf("unexpected content: %s", resp.Content)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 1 || order[0] != "permissions" {
		t.Fatalf("expected only permissions to run, got: %v", order)
	}
}

func TestValidators_RunAfterPreHooks(t *testing.T) {
	tools := NewToolRegistry()
	var receivedInput json.RawMessage
	tools.Register(ToolDefinition{
		Name:        "hooked",
		Description: "tool with hooks and validation",
		InputSchema: map[string]any{"type": "object"},
		ValidateInput: func(_ context.Context, input json.RawMessage) error {
			receivedInput = input
			return nil
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return "ok", nil
	})

	// Create hooks that modify input
	hooks := NewHooks()
	modifiedInput := json.RawMessage(`{"modified":true}`)
	hooks.AddPreHook("*", func(_ context.Context, _ HookContext) HookResult {
		return HookResult{
			Decision:      HookModify,
			ModifiedInput: modifiedInput,
		}
	})

	events := make(chan AgentEvent, 10)
	tc := ToolCall{ID: "1", Name: "hooked", Input: json.RawMessage(`{"original":true}`)}
	resp := executeOneTool(context.Background(), tc, tools, hooks, nil, nil, nil, events)

	if resp.IsError {
		t.Fatalf("unexpected error: %s", resp.Content)
	}
	// Validator should see the modified input from pre-hooks
	if string(receivedInput) != `{"modified":true}` {
		t.Fatalf("validator received wrong input: %s", string(receivedInput))
	}
}

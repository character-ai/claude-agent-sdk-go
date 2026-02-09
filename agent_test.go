package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHistoryToMessages(t *testing.T) {
	a := &Agent{tools: NewToolRegistry()}

	history := []ConversationMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there", ToolCalls: []ToolCall{
			{ID: "tc_1", Name: "search", Input: json.RawMessage(`{"q":"cats"}`)},
		}},
		{Role: "tool", ToolCallID: "tc_1", Content: "found cats"},
	}

	messages := a.historyToMessages(history)

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// First: user message
	userMsg, ok := messages[0].(UserMessage)
	if !ok {
		t.Fatalf("expected UserMessage, got %T", messages[0])
	}
	if len(userMsg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(userMsg.Content))
	}
	textBlock, ok := userMsg.Content[0].(TextBlock)
	if !ok || textBlock.Text != "Hello" {
		t.Fatalf("unexpected text block: %#v", userMsg.Content[0])
	}

	// Second: assistant message with text + tool use
	assistantMsg, ok := messages[1].(AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", messages[1])
	}
	if len(assistantMsg.Content) != 2 {
		t.Fatalf("expected 2 content blocks (text + tool use), got %d", len(assistantMsg.Content))
	}
	textBlock, ok = assistantMsg.Content[0].(TextBlock)
	if !ok || textBlock.Text != "Hi there" {
		t.Fatalf("unexpected text block: %#v", assistantMsg.Content[0])
	}
	toolUseBlock, ok := assistantMsg.Content[1].(ToolUseBlock)
	if !ok || toolUseBlock.Name != "search" {
		t.Fatalf("unexpected tool use block: %#v", assistantMsg.Content[1])
	}

	// Third: tool result as user message
	toolResultMsg, ok := messages[2].(UserMessage)
	if !ok {
		t.Fatalf("expected UserMessage for tool result, got %T", messages[2])
	}
	if len(toolResultMsg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(toolResultMsg.Content))
	}
	resultBlock, ok := toolResultMsg.Content[0].(ToolResultBlock)
	if !ok {
		t.Fatalf("expected ToolResultBlock, got %T", toolResultMsg.Content[0])
	}
	if resultBlock.ToolUseID != "tc_1" || resultBlock.Content != "found cats" {
		t.Fatalf("unexpected tool result: %#v", resultBlock)
	}
}

func TestHistoryToMessagesEmptyAssistant(t *testing.T) {
	a := &Agent{tools: NewToolRegistry()}

	history := []ConversationMessage{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: ""},
	}

	messages := a.historyToMessages(history)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Assistant message with no text should have no content blocks
	assistantMsg, ok := messages[1].(AssistantMessage)
	if !ok {
		t.Fatalf("expected AssistantMessage, got %T", messages[1])
	}
	if len(assistantMsg.Content) != 0 {
		t.Fatalf("expected 0 content blocks for empty assistant, got %d", len(assistantMsg.Content))
	}
}

func TestHistoryToMessagesUnknownRole(t *testing.T) {
	a := &Agent{tools: NewToolRegistry()}

	history := []ConversationMessage{
		{Role: "user", Content: "Hello"},
		{Role: "system", Content: "ignored"},
	}

	messages := a.historyToMessages(history)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message (system skipped), got %d", len(messages))
	}
}

// --- canUseTool integration tests ---

func TestCanUseToolDeny(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: ObjectSchema(map[string]any{"arg": StringParam("test arg")}, "arg"),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result", nil
	})

	agent := &Agent{
		tools: tools,
		canUseTool: func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) PermissionDecision {
			return PermissionDecision{Allow: false, Reason: "blocked by test"}
		},
	}

	events := make(chan AgentEvent, 10)
	toolCalls := []ToolCall{{ID: "tc_1", Name: "test_tool", Input: json.RawMessage(`{"arg":"val"}`)}}

	results := agent.executeTools(context.Background(), toolCalls, events)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].IsError {
		t.Fatal("expected error result when canUseTool denies")
	}
	if results[0].Content != "Tool execution denied: blocked by test" {
		t.Fatalf("unexpected error message: %s", results[0].Content)
	}
}

func TestCanUseToolAllow(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: ObjectSchema(map[string]any{"arg": StringParam("test arg")}, "arg"),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "success", nil
	})

	agent := &Agent{
		tools: tools,
		canUseTool: func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) PermissionDecision {
			return PermissionDecision{Allow: true}
		},
	}

	events := make(chan AgentEvent, 10)
	toolCalls := []ToolCall{{ID: "tc_1", Name: "test_tool", Input: json.RawMessage(`{"arg":"val"}`)}}

	results := agent.executeTools(context.Background(), toolCalls, events)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].IsError {
		t.Fatal("expected success result when canUseTool allows")
	}
	if results[0].Content != "success" {
		t.Fatalf("unexpected result: %s", results[0].Content)
	}
}

func TestCanUseToolModifyInput(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		var parsed struct {
			Arg string `json:"arg"`
		}
		_ = json.Unmarshal(input, &parsed)
		return "received: " + parsed.Arg, nil
	})

	agent := &Agent{
		tools: tools,
		canUseTool: func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) PermissionDecision {
			return PermissionDecision{
				Allow:         true,
				ModifiedInput: json.RawMessage(`{"arg":"modified"}`),
			}
		},
	}

	events := make(chan AgentEvent, 10)
	toolCalls := []ToolCall{{ID: "tc_1", Name: "test_tool", Input: json.RawMessage(`{"arg":"original"}`)}}

	results := agent.executeTools(context.Background(), toolCalls, events)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "received: modified" {
		t.Fatalf("expected modified input to be used, got: %s", results[0].Content)
	}
}

func TestCanUseToolNilAllowsExecution(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "success", nil
	})

	// canUseTool is nil — should proceed normally
	agent := &Agent{
		tools:      tools,
		canUseTool: nil,
	}

	events := make(chan AgentEvent, 10)
	toolCalls := []ToolCall{{ID: "tc_1", Name: "test_tool", Input: json.RawMessage(`{}`)}}

	results := agent.executeTools(context.Background(), toolCalls, events)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].IsError {
		t.Fatal("expected success when canUseTool is nil")
	}
}

func TestCanUseToolDenyDefaultReason(t *testing.T) {
	agent := &Agent{
		tools: NewToolRegistry(),
		canUseTool: func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) PermissionDecision {
			return PermissionDecision{Allow: false}
		},
	}
	agent.tools.Register(ToolDefinition{Name: "t", Description: "t"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "", nil
	})

	events := make(chan AgentEvent, 10)
	results := agent.executeTools(context.Background(), []ToolCall{{ID: "tc_1", Name: "t", Input: json.RawMessage(`{}`)}}, events)

	if results[0].Content != "Tool execution denied: permission denied" {
		t.Fatalf("expected default deny reason, got: %s", results[0].Content)
	}
}

func TestExecuteToolsNotFound(t *testing.T) {
	agent := &Agent{tools: NewToolRegistry()}

	events := make(chan AgentEvent, 10)
	results := agent.executeTools(context.Background(), []ToolCall{{ID: "tc_1", Name: "nonexistent", Input: json.RawMessage(`{}`)}}, events)

	if len(results) != 1 || !results[0].IsError {
		t.Fatal("expected error for nonexistent tool")
	}
	if results[0].Content != "Tool not found: nonexistent" {
		t.Fatalf("unexpected error: %s", results[0].Content)
	}
}

func TestExecuteToolsWithHooksDeny(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{Name: "blocked_tool", Description: "test"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "should not execute", nil
	})

	hooks := NewHooks()
	hooks.OnTool("blocked_tool").Before(func(ctx context.Context, hookCtx HookContext) HookResult {
		return DenyHook("hook blocked it")
	})

	agent := &Agent{tools: tools, hooks: hooks}

	events := make(chan AgentEvent, 10)
	results := agent.executeTools(context.Background(), []ToolCall{{ID: "tc_1", Name: "blocked_tool", Input: json.RawMessage(`{}`)}}, events)

	if !results[0].IsError {
		t.Fatal("expected error when hook denies")
	}
	if results[0].Content != "Tool execution denied: hook blocked it" {
		t.Fatalf("unexpected error: %s", results[0].Content)
	}
}

func TestAgentSendCancelledContext(t *testing.T) {
	agent := &Agent{client: &Client{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := agent.Send(ctx, "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

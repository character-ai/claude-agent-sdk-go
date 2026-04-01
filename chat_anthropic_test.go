package claudeagent

import (
	"encoding/json"
	"testing"
)

func TestConvertMessagesToAnthropic_User(t *testing.T) {
	msgs := []ChatMessage{
		{Role: ChatRoleUser, Content: "Hello"},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	// Should be a user message — verify role via JSON representation.
	data, _ := json.Marshal(result[0])
	if string(result[0].Role) != "user" {
		t.Errorf("expected user role, got: %s", data)
	}
}

func TestConvertMessagesToAnthropic_Assistant(t *testing.T) {
	msgs := []ChatMessage{
		{Role: ChatRoleAssistant, Content: "I'll help you."},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if string(result[0].Role) != "assistant" {
		t.Errorf("expected assistant role")
	}
}

func TestConvertMessagesToAnthropic_AssistantWithToolCalls(t *testing.T) {
	msgs := []ChatMessage{
		{
			Role:    ChatRoleAssistant,
			Content: "Let me search for that.",
			ToolCalls: []ToolCall{
				{ID: "tc_1", Name: "search", Input: json.RawMessage(`{"q":"test"}`)},
			},
		},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if string(result[0].Role) != "assistant" {
		t.Errorf("expected assistant role")
	}
	// Verify both text and tool use blocks are present.
	msg := result[0]
	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content blocks (text + tool_use), got %d", len(msg.Content))
	}
}

func TestConvertMessagesToAnthropic_ToolResult(t *testing.T) {
	msgs := []ChatMessage{
		{Role: ChatRoleTool, Content: "search result", ToolCallID: "tc_1"},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	// Tool results are wrapped in a user message.
	if string(result[0].Role) != "user" {
		t.Errorf("expected user role for tool result, got: %s", result[0].Role)
	}
}

func TestConvertMessagesToAnthropic_SystemSkipped(t *testing.T) {
	msgs := []ChatMessage{
		{Role: ChatRoleSystem, Content: "Be helpful."},
		{Role: ChatRoleUser, Content: "Hello"},
	}
	result := convertMessagesToAnthropic(msgs)
	// System message should be skipped (handled via params.System separately).
	if len(result) != 1 {
		t.Fatalf("expected 1 message (system skipped), got %d", len(result))
	}
}

func TestConvertToolsToAnthropic(t *testing.T) {
	defs := []ToolDefinition{
		{
			Name:        "search",
			Description: "Search the web",
			InputSchema: ObjectSchema(map[string]any{
				"query": StringParam("search query"),
			}, "query"),
		},
	}
	result := convertToolsToAnthropic(defs)
	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	if result[0].OfTool == nil {
		t.Fatal("expected OfTool to be set")
	}
	if result[0].OfTool.Name != "search" {
		t.Errorf("expected tool name 'search', got %s", result[0].OfTool.Name)
	}
}

func TestConvertToolsToAnthropic_Empty(t *testing.T) {
	result := convertToolsToAnthropic(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input")
	}
}

func TestAnthropicProvider_Name(t *testing.T) {
	p := &AnthropicProvider{}
	if p.Name() != "anthropic" {
		t.Errorf("expected 'anthropic', got %q", p.Name())
	}
}

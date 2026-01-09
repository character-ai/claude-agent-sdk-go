package claudeagent

import (
	"encoding/json"
	"testing"
)

func TestAssistantMessageUnmarshalContentBlocks(t *testing.T) {
	data := []byte(`{
		"id":"msg_123",
		"model":"claude",
		"content":[
			{"type":"text","text":"hello"},
			{"type":"tool_use","id":"tool_1","name":"search","input":{"q":"cats"}}
		]
	}`)

	var msg AssistantMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.Content))
	}

	textBlock, ok := msg.Content[0].(TextBlock)
	if !ok || textBlock.Text != "hello" {
		t.Fatalf("unexpected text block: %#v", msg.Content[0])
	}

	toolBlock, ok := msg.Content[1].(ToolUseBlock)
	if !ok || toolBlock.Name != "search" {
		t.Fatalf("unexpected tool block: %#v", msg.Content[1])
	}
}

func TestUserMessageUnmarshalContentBlocks(t *testing.T) {
	data := []byte(`{
		"content":[{"type":"text","text":"hi"}]
	}`)

	var msg UserMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	textBlock, ok := msg.Content[0].(TextBlock)
	if !ok || textBlock.Text != "hi" {
		t.Fatalf("unexpected text block: %#v", msg.Content[0])
	}
}

func TestToolResultBlockParsing(t *testing.T) {
	data := []byte(`{
		"content":[
			{"type":"tool_result","tool_use_id":"tool_123","content":"success","is_error":false}
		]
	}`)

	var msg UserMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	resultBlock, ok := msg.Content[0].(ToolResultBlock)
	if !ok {
		t.Fatalf("expected ToolResultBlock, got %T", msg.Content[0])
	}
	if resultBlock.ToolUseID != "tool_123" {
		t.Fatalf("unexpected tool_use_id: %s", resultBlock.ToolUseID)
	}
	if resultBlock.Content != "success" {
		t.Fatalf("unexpected content: %s", resultBlock.Content)
	}
	if resultBlock.IsError {
		t.Fatal("expected IsError to be false")
	}
}

func TestToolResultBlockType(t *testing.T) {
	block := ToolResultBlock{
		ToolUseID: "123",
		Content:   "result",
		IsError:   false,
	}

	if block.Type() != ContentTypeToolResult {
		t.Fatalf("unexpected type: %s", block.Type())
	}
}

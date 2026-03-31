package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegisterStructuredStoresHandler(t *testing.T) {
	reg := NewToolRegistry()
	def := ToolDefinition{
		Name:        "test_structured",
		Description: "a structured tool",
		InputSchema: ObjectSchema(map[string]any{"q": StringParam("query")}, "q"),
	}

	handler := func(ctx context.Context, input json.RawMessage) (string, *ToolResultMetadata, error) {
		return "ok", &ToolResultMetadata{SystemContext: "ctx"}, nil
	}
	reg.RegisterStructured(def, handler)

	// Verify the tool is stored with a structured handler
	tool, err := reg.Store().GetTool("test_structured")
	if err != nil {
		t.Fatalf("GetTool: %v", err)
	}
	if tool == nil {
		t.Fatal("tool not found")
	}
	if tool.StructuredHandler == nil {
		t.Fatal("expected StructuredHandler to be set")
	}
	if tool.Handler == nil {
		t.Fatal("expected Handler wrapper to be set")
	}
}

func TestExecuteStructuredReturnsMetadata(t *testing.T) {
	reg := NewToolRegistry()
	def := ToolDefinition{
		Name:        "meta_tool",
		Description: "returns metadata",
		InputSchema: ObjectSchema(map[string]any{"x": StringParam("x")}),
	}

	expectedMeta := &ToolResultMetadata{
		SystemContext:   "extra context",
		SuggestFollowUp: "do next thing",
		InjectMessages: []ConversationMessage{
			{Role: "user", Content: "injected"},
		},
	}

	reg.RegisterStructured(def, func(ctx context.Context, input json.RawMessage) (string, *ToolResultMetadata, error) {
		return "result", expectedMeta, nil
	})

	result, meta, err := reg.ExecuteStructured(context.Background(), "meta_tool", json.RawMessage(`{"x":"val"}`))
	if err != nil {
		t.Fatalf("ExecuteStructured: %v", err)
	}
	if result != "result" {
		t.Errorf("got result %q, want %q", result, "result")
	}
	if meta == nil {
		t.Fatal("expected metadata, got nil")
	}
	if meta.SystemContext != "extra context" {
		t.Errorf("got SystemContext %q, want %q", meta.SystemContext, "extra context")
	}
	if meta.SuggestFollowUp != "do next thing" {
		t.Errorf("got SuggestFollowUp %q, want %q", meta.SuggestFollowUp, "do next thing")
	}
	if len(meta.InjectMessages) != 1 || meta.InjectMessages[0].Content != "injected" {
		t.Errorf("unexpected InjectMessages: %+v", meta.InjectMessages)
	}
}

func TestExecuteStructuredNilMetadataForRegularHandler(t *testing.T) {
	reg := NewToolRegistry()
	def := ToolDefinition{
		Name:        "regular_tool",
		Description: "a regular tool",
		InputSchema: ObjectSchema(map[string]any{}),
	}

	reg.Register(def, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "plain result", nil
	})

	result, meta, err := reg.ExecuteStructured(context.Background(), "regular_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("ExecuteStructured: %v", err)
	}
	if result != "plain result" {
		t.Errorf("got result %q, want %q", result, "plain result")
	}
	if meta != nil {
		t.Errorf("expected nil metadata for regular handler, got %+v", meta)
	}
}

func TestRegisterStructuredFuncTypedInput(t *testing.T) {
	reg := NewToolRegistry()

	type MyInput struct {
		Name string `json:"name"`
	}

	def := ToolDefinition{
		Name:        "typed_structured",
		Description: "typed structured tool",
		InputSchema: ObjectSchema(map[string]any{"name": StringParam("name")}, "name"),
	}

	RegisterStructuredFunc(reg, def, func(ctx context.Context, input MyInput) (string, *ToolResultMetadata, error) {
		return "hello " + input.Name, &ToolResultMetadata{SuggestFollowUp: "greet again"}, nil
	})

	result, meta, err := reg.ExecuteStructured(context.Background(), "typed_structured", json.RawMessage(`{"name":"world"}`))
	if err != nil {
		t.Fatalf("ExecuteStructured: %v", err)
	}
	if result != "hello world" {
		t.Errorf("got result %q, want %q", result, "hello world")
	}
	if meta == nil || meta.SuggestFollowUp != "greet again" {
		t.Errorf("unexpected metadata: %+v", meta)
	}

	// Also verify the wrapper Handler works (backwards compat)
	plainResult, err := reg.Execute(context.Background(), "typed_structured", json.RawMessage(`{"name":"test"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if plainResult != "hello test" {
		t.Errorf("got plain result %q, want %q", plainResult, "hello test")
	}
}

func TestToolResponseMetadataNotSerialized(t *testing.T) {
	resp := ToolResponse{
		ToolUseID: "id-1",
		Content:   "content",
		IsError:   false,
		Metadata: &ToolResultMetadata{
			SystemContext: "should not appear in JSON",
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, ok := m["metadata"]; ok {
		t.Error("metadata should not be serialized to JSON")
	}
	if _, ok := m["Metadata"]; ok {
		t.Error("Metadata should not be serialized to JSON")
	}
}

func TestMetadataInjectMessagesProcessedInAgentHistory(t *testing.T) {
	// Unit test the inject logic by simulating what agent.go does:
	// after collecting tool results, it appends InjectMessages to history.

	toolResults := []ToolResponse{
		{
			ToolUseID: "tu-1",
			Content:   "tool output",
			Metadata: &ToolResultMetadata{
				InjectMessages: []ConversationMessage{
					{Role: "user", Content: "injected context 1"},
					{Role: "assistant", Content: "injected response"},
				},
			},
		},
		{
			ToolUseID: "tu-2",
			Content:   "plain output",
			// No metadata
		},
	}

	// Simulate agent.go logic
	var history []ConversationMessage
	for _, tr := range toolResults {
		history = append(history, ConversationMessage{
			Role:       "tool",
			ToolCallID: tr.ToolUseID,
			Content:    tr.Content,
		})
		if tr.Metadata != nil {
			history = append(history, tr.Metadata.InjectMessages...)
		}
	}

	// Verify: tool result for tu-1, then 2 injected, then tool result for tu-2
	if len(history) != 4 {
		t.Fatalf("expected 4 history entries, got %d", len(history))
	}
	if history[0].Role != "tool" || history[0].ToolCallID != "tu-1" {
		t.Errorf("entry 0: %+v", history[0])
	}
	if history[1].Role != "user" || history[1].Content != "injected context 1" {
		t.Errorf("entry 1: %+v", history[1])
	}
	if history[2].Role != "assistant" || history[2].Content != "injected response" {
		t.Errorf("entry 2: %+v", history[2])
	}
	if history[3].Role != "tool" || history[3].ToolCallID != "tu-2" {
		t.Errorf("entry 3: %+v", history[3])
	}
}

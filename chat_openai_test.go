package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockSSEResponse returns a mock SSE streaming response for testing.
func mockSSEResponse(chunks []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func sseChunk(role, content, finishReason string) string {
	type delta struct {
		Role    string `json:"role,omitempty"`
		Content string `json:"content,omitempty"`
	}
	type choice struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
	}
	c := chunk{Choices: []choice{{
		Delta:        delta{Role: role, Content: content},
		FinishReason: finishReason,
	}}}
	b, _ := json.Marshal(c)
	return string(b)
}

func TestOpenAICompatProvider_Name(t *testing.T) {
	p := NewOpenAICompatProvider(OpenAICompatConfig{})
	if p.Name() != "openai-compat" {
		t.Errorf("expected 'openai-compat', got %q", p.Name())
	}
	p2 := p.WithName("mistral")
	if p2.Name() != "mistral" {
		t.Errorf("expected 'mistral', got %q", p2.Name())
	}
}

func TestOpenAICompatProvider_TextResponse(t *testing.T) {
	chunks := []string{
		sseChunk("assistant", "", ""),
		sseChunk("", "Hello", ""),
		sseChunk("", " world", ""),
		sseChunk("", "", "stop"),
	}

	srv := httptest.NewServer(mockSSEResponse(chunks))
	defer srv.Close()

	p := NewOpenAICompatProvider(OpenAICompatConfig{
		BaseURL: srv.URL,
		Model:   "gpt-4o",
	})

	var deltas []string
	resp, err := p.Complete(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []ChatMessage{{Role: ChatRoleUser, Content: "Hello"}},
	}, func(e ChatStreamEvent) {
		if e.Type == ChatStreamContentDelta {
			deltas = append(deltas, e.Content)
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected 'end_turn', got %q", resp.StopReason)
	}
	if len(deltas) != 2 {
		t.Errorf("expected 2 deltas, got %d", len(deltas))
	}
}

func TestOpenAICompatProvider_ToolCall(t *testing.T) {
	type tcDelta struct {
		Index    int                `json:"index"`
		ID       string             `json:"id,omitempty"`
		Type     string             `json:"type,omitempty"`
		Function openAIFunctionCall `json:"function"`
	}
	type delta struct {
		ToolCalls []tcDelta `json:"tool_calls,omitempty"`
	}
	type choice struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
	}

	makeChunk := func(idx int, id, name, args, finish string) string {
		c := chunk{Choices: []choice{{
			Delta: delta{ToolCalls: []tcDelta{{
				Index:    idx,
				ID:       id,
				Type:     "function",
				Function: openAIFunctionCall{Name: name, Arguments: args},
			}}},
			FinishReason: finish,
		}}}
		b, _ := json.Marshal(c)
		return string(b)
	}

	chunks := []string{
		makeChunk(0, "call_123", "search", "", ""),
		makeChunk(0, "", "", `{"q":"test"}`, ""),
		makeChunk(0, "", "", "", "tool_calls"),
	}

	srv := httptest.NewServer(mockSSEResponse(chunks))
	defer srv.Close()

	p := NewOpenAICompatProvider(OpenAICompatConfig{BaseURL: srv.URL, Model: "gpt-4o"})

	var startEvents, endEvents int
	resp, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: ChatRoleUser, Content: "Search for cats"}},
	}, func(e ChatStreamEvent) {
		if e.Type == ChatStreamToolUseStart {
			startEvents++
		}
		if e.Type == ChatStreamToolUseEnd {
			endEvents++
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "search" {
		t.Errorf("expected 'search', got %q", resp.ToolCalls[0].Name)
	}
	if string(resp.ToolCalls[0].Input) != `{"q":"test"}` {
		t.Errorf("expected full args, got %q", string(resp.ToolCalls[0].Input))
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("expected 'tool_use', got %q", resp.StopReason)
	}
	if startEvents != 1 || endEvents != 1 {
		t.Errorf("expected 1 start and 1 end event, got %d/%d", startEvents, endEvents)
	}
}

func TestOpenAICompatProvider_HttpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error": "unauthorized"}`)
	}))
	defer srv.Close()

	p := NewOpenAICompatProvider(OpenAICompatConfig{BaseURL: srv.URL})
	_, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: ChatRoleUser, Content: "Hello"}},
	}, nil)

	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 in error, got: %v", err)
	}
}

func TestConvertMessages_SystemFromPrompt(t *testing.T) {
	p := &OpenAICompatProvider{}
	req := ChatRequest{
		SystemPrompt: "Be helpful.",
		Messages:     []ChatMessage{{Role: ChatRoleUser, Content: "Hello"}},
	}
	msgs := p.convertMessages(req)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected system first, got %s", msgs[0].Role)
	}
	if msgs[0].Content != "Be helpful." {
		t.Errorf("unexpected system content: %s", msgs[0].Content)
	}
}

func TestConvertMessages_SystemFromBlocks(t *testing.T) {
	p := &OpenAICompatProvider{}
	req := ChatRequest{
		SystemBlocks: []SystemPromptBlock{
			{Text: "First block."},
			{Text: "Second block."},
		},
		Messages: []ChatMessage{{Role: ChatRoleUser, Content: "Hello"}},
	}
	msgs := p.convertMessages(req)
	if msgs[0].Role != "system" {
		t.Errorf("expected system first")
	}
	if !strings.Contains(msgs[0].Content, "First block.") {
		t.Errorf("expected first block in content")
	}
	if !strings.Contains(msgs[0].Content, "Second block.") {
		t.Errorf("expected second block in content")
	}
}

func TestConvertMessages_ToolResult(t *testing.T) {
	p := &OpenAICompatProvider{}
	req := ChatRequest{
		Messages: []ChatMessage{
			{Role: ChatRoleUser, Content: "go"},
			{Role: ChatRoleAssistant, ToolCalls: []ToolCall{{ID: "tc_1", Name: "search"}}},
			{Role: ChatRoleTool, Content: "result", ToolCallID: "tc_1"},
			{Role: ChatRoleTool, Content: "error text", ToolCallID: "tc_2", IsError: true},
		},
	}
	msgs := p.convertMessages(req)
	// user + assistant + 2 tool results = 4
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[2].Role != "tool" || msgs[2].ToolCallID != "tc_1" {
		t.Errorf("expected tool result, got role=%s id=%s", msgs[2].Role, msgs[2].ToolCallID)
	}
	// IsError: content still passed through (OpenAI has no error flag)
	if msgs[3].Content != "error text" {
		t.Errorf("expected error content to pass through, got %q", msgs[3].Content)
	}
}

func TestOpenAICompatProvider_ParallelToolCallOrder(t *testing.T) {
	type tcDelta struct {
		Index    int                `json:"index"`
		ID       string             `json:"id,omitempty"`
		Function openAIFunctionCall `json:"function"`
	}
	type delta struct {
		ToolCalls []tcDelta `json:"tool_calls,omitempty"`
	}
	type choice struct {
		Delta        delta  `json:"delta"`
		FinishReason string `json:"finish_reason,omitempty"`
	}
	type chunk struct {
		Choices []choice `json:"choices"`
	}

	makeChunk := func(idx int, id, name, args, finish string) string {
		c := chunk{Choices: []choice{{
			Delta:        delta{ToolCalls: []tcDelta{{Index: idx, ID: id, Function: openAIFunctionCall{Name: name, Arguments: args}}}},
			FinishReason: finish,
		}}}
		b, _ := json.Marshal(c)
		return string(b)
	}

	// Two parallel tool calls: index 0 = "search", index 1 = "calc"
	chunks := []string{
		makeChunk(0, "id_0", "search", "", ""),
		makeChunk(1, "id_1", "calc", "", ""),
		makeChunk(0, "", "", `{"q":"x"}`, ""),
		makeChunk(1, "", "", `{"n":1}`, ""),
		makeChunk(0, "", "", "", "tool_calls"),
	}

	srv := httptest.NewServer(mockSSEResponse(chunks))
	defer srv.Close()

	p := NewOpenAICompatProvider(OpenAICompatConfig{BaseURL: srv.URL})
	resp, err := p.Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{{Role: ChatRoleUser, Content: "go"}},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	// Order must be deterministic: index 0 first, index 1 second.
	if resp.ToolCalls[0].Name != "search" {
		t.Errorf("expected 'search' at index 0, got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[1].Name != "calc" {
		t.Errorf("expected 'calc' at index 1, got %q", resp.ToolCalls[1].Name)
	}
}

func TestMapFinishReason(t *testing.T) {
	cases := map[string]string{
		"stop":       "end_turn",
		"tool_calls": "tool_use",
		"length":     "max_tokens",
		"":           "end_turn",
		"other":      "other",
	}
	for input, expected := range cases {
		if got := mapFinishReason(input); got != expected {
			t.Errorf("mapFinishReason(%q) = %q, want %q", input, got, expected)
		}
	}
}

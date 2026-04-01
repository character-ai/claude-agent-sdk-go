package claudeagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAICompatConfig configures an OpenAI-compatible chat completions provider.
type OpenAICompatConfig struct {
	// BaseURL is the API base URL (e.g., "https://api.openai.com/v1").
	BaseURL string
	// APIKey is the authentication key.
	APIKey string
	// Model is the model identifier (e.g., "gpt-4o", "mistral-large-latest").
	Model string
	// HTTPTimeout sets the HTTP client timeout. Defaults to 120 seconds.
	HTTPTimeout time.Duration
}

// OpenAICompatProvider implements LLMProvider for any /v1/chat/completions endpoint.
// Supports OpenAI, Mistral, DeepSeek, Qwen, and other compatible APIs.
type OpenAICompatProvider struct {
	cfg    OpenAICompatConfig
	client *http.Client
	name   string
}

// NewOpenAICompatProvider creates a provider for an OpenAI-compatible API.
func NewOpenAICompatProvider(cfg OpenAICompatConfig) *OpenAICompatProvider {
	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &OpenAICompatProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
		name:   "openai-compat",
	}
}

// Name returns the provider name.
func (p *OpenAICompatProvider) Name() string { return p.name }

// WithName returns a copy of the provider with a custom name (e.g., "mistral", "openai").
func (p *OpenAICompatProvider) WithName(name string) *OpenAICompatProvider {
	cp := *p
	cp.name = name
	return &cp
}

// Complete sends the request to the OpenAI-compatible chat completions endpoint.
func (p *OpenAICompatProvider) Complete(ctx context.Context, req ChatRequest, onEvent ChatStreamCallback) (ChatResponse, error) {
	body, err := p.buildRequestBody(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ChatResponse{}, fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	return p.parseSSEStream(resp.Body, onEvent)
}

// openAIChatRequest is the JSON body for /v1/chat/completions.
type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"` // "function"
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string            `json:"type"` // "function"
	Function openAIFunctionDef `json:"function"`
}

type openAIFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// buildRequestBody converts a ChatRequest to OpenAI JSON format.
func (p *OpenAICompatProvider) buildRequestBody(req ChatRequest) ([]byte, error) {
	messages := p.convertMessages(req)

	tools := make([]openAITool, 0, len(req.Tools))
	for _, def := range req.Tools {
		tools = append(tools, openAITool{
			Type: "function",
			Function: openAIFunctionDef{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.InputSchema,
			},
		})
	}

	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}

	body := openAIChatRequest{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		Stream:      true,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	return json.Marshal(body)
}

// convertMessages converts canonical ChatMessages to OpenAI message format.
// System prompt is prepended as a system message; tool results use "tool" role.
func (p *OpenAICompatProvider) convertMessages(req ChatRequest) []openAIMessage {
	var out []openAIMessage

	// System prompt: prefer plain string; SystemBlocks are concatenated.
	systemText := req.SystemPrompt
	if systemText == "" && len(req.SystemBlocks) > 0 {
		var sb strings.Builder
		for i, b := range req.SystemBlocks {
			if i > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(b.Text)
		}
		systemText = sb.String()
	}
	if systemText != "" {
		out = append(out, openAIMessage{Role: "system", Content: systemText})
	}

	for _, m := range req.Messages {
		switch m.Role {
		case ChatRoleSystem:
			// Already handled above; skip system messages in the history.
			continue
		case ChatRoleUser:
			out = append(out, openAIMessage{Role: "user", Content: m.Content})
		case ChatRoleAssistant:
			msg := openAIMessage{Role: "assistant", Content: m.Content}
			for _, tc := range m.ToolCalls {
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAIFunctionCall{
						Name:      tc.Name,
						Arguments: string(tc.Input),
					},
				})
			}
			out = append(out, msg)
		case ChatRoleTool:
			out = append(out, openAIMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			})
		}
	}
	return out
}

// openAIChunk is a single SSE delta from the streaming response.
type openAIChunk struct {
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Delta        openAIDelta `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type openAIDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   string                `json:"content,omitempty"`
	ToolCalls []openAIToolCallDelta `json:"tool_calls,omitempty"`
}

// openAIToolCallDelta includes the streaming index for parallel tool calls.
type openAIToolCallDelta struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// parseSSEStream reads the streaming response and accumulates into a ChatResponse.
func (p *OpenAICompatProvider) parseSSEStream(body io.Reader, onEvent ChatStreamCallback) (ChatResponse, error) {
	scanner := bufio.NewScanner(body)

	var content strings.Builder
	// toolCalls accumulates by index (parallel tool calls have an index field).
	toolCallsByIdx := map[int]*ToolCall{}
	toolCallArgs := map[int]*strings.Builder{}
	var finishReason string
	var usage ChatUsage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunks
		}

		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}

		// Text delta.
		if choice.Delta.Content != "" {
			content.WriteString(choice.Delta.Content)
			if onEvent != nil {
				onEvent(ChatStreamEvent{Type: ChatStreamContentDelta, Content: choice.Delta.Content})
			}
		}

		// Tool call deltas — each has an index for parallel tool calls.
		for _, tcDelta := range choice.Delta.ToolCalls {
			idx := tcDelta.Index
			if _, exists := toolCallsByIdx[idx]; !exists {
				tc := &ToolCall{ID: tcDelta.ID, Name: tcDelta.Function.Name}
				toolCallsByIdx[idx] = tc
				toolCallArgs[idx] = &strings.Builder{}
				if onEvent != nil {
					onEvent(ChatStreamEvent{Type: ChatStreamToolUseStart, ToolCall: tc})
				}
			} else {
				// Later deltas may fill in ID or name if missing from the first chunk.
				if tcDelta.ID != "" {
					toolCallsByIdx[idx].ID = tcDelta.ID
				}
				if tcDelta.Function.Name != "" {
					toolCallsByIdx[idx].Name = tcDelta.Function.Name
				}
			}
			if tcDelta.Function.Arguments != "" {
				_, _ = toolCallArgs[idx].WriteString(tcDelta.Function.Arguments)
				if onEvent != nil {
					onEvent(ChatStreamEvent{Type: ChatStreamToolUseDelta, Content: tcDelta.Function.Arguments})
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return ChatResponse{}, fmt.Errorf("stream read: %w", err)
	}

	// Finalize tool calls in index order to preserve the model's declared order.
	toolCalls := make([]ToolCall, 0, len(toolCallsByIdx))
	for i := 0; i < len(toolCallsByIdx); i++ {
		tc := toolCallsByIdx[i]
		tc.Input = json.RawMessage(toolCallArgs[i].String())
		completed := *tc // copy before emitting to avoid shared pointer
		toolCalls = append(toolCalls, completed)
		if onEvent != nil {
			onEvent(ChatStreamEvent{Type: ChatStreamToolUseEnd, ToolCall: &completed})
		}
	}

	return ChatResponse{
		Content:    content.String(),
		ToolCalls:  toolCalls,
		StopReason: mapFinishReason(finishReason),
		Usage:      usage,
	}, nil
}

// mapFinishReason normalizes OpenAI finish_reason to our StopReason conventions.
func mapFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "":
		return "end_turn"
	default:
		return reason
	}
}

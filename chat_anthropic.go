package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProviderConfig configures an Anthropic LLM provider.
type AnthropicProviderConfig struct {
	// APIKey is the Anthropic API key. Defaults to ANTHROPIC_API_KEY env var.
	APIKey string // #nosec G117 -- config field, not a hardcoded secret
}

// AnthropicProvider implements LLMProvider using the Anthropic Messages API.
// It supports prompt caching via SystemPromptBlocks, full streaming, and
// Anthropic-native token usage tracking.
type AnthropicProvider struct {
	client anthropic.Client
}

// NewAnthropicProvider creates an Anthropic provider.
func NewAnthropicProvider(cfg AnthropicProviderConfig) *AnthropicProvider {
	opts := []option.RequestOption{}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	return &AnthropicProvider{
		client: anthropic.NewClient(opts...),
	}
}

// Name returns "anthropic".
func (p *AnthropicProvider) Name() string { return "anthropic" }

// Complete sends the chat request to the Anthropic Messages API with streaming.
// onEvent is called for each content delta and tool use event as they arrive.
func (p *AnthropicProvider) Complete(ctx context.Context, req ChatRequest, onEvent ChatStreamCallback) (ChatResponse, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  convertMessagesToAnthropic(req.Messages),
	}

	// System prompt: structured blocks take precedence for cache control.
	if len(req.SystemBlocks) > 0 {
		blocks := make([]anthropic.TextBlockParam, len(req.SystemBlocks))
		for i, b := range req.SystemBlocks {
			blocks[i] = anthropic.TextBlockParam{Text: b.Text}
			if b.CacheControl != nil {
				blocks[i].CacheControl = anthropic.CacheControlEphemeralParam{}
			}
		}
		params.System = blocks
	} else if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.SystemPrompt}}
	}

	// Tools.
	if len(req.Tools) > 0 {
		params.Tools = convertToolsToAnthropic(req.Tools)
	}

	stream := p.client.Messages.NewStreaming(ctx, params)

	var (
		content         string
		toolCalls       []ToolCall
		currentToolID   string
		currentToolName string
		currentToolJSON string
		usage           ChatUsage
		stopReason      string
	)

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			switch cb := e.ContentBlock.AsAny().(type) {
			case anthropic.TextBlock:
				// Text content arrives via TextDelta events; nothing to do on block start.
				_ = cb
			case anthropic.ToolUseBlock:
				currentToolID = cb.ID
				currentToolName = cb.Name
				currentToolJSON = ""
				if onEvent != nil {
					tc := ToolCall{ID: cb.ID, Name: cb.Name}
					onEvent(ChatStreamEvent{Type: ChatStreamToolUseStart, ToolCall: &tc})
				}
			}

		case anthropic.ContentBlockDeltaEvent:
			switch d := e.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				content += d.Text
				if onEvent != nil {
					onEvent(ChatStreamEvent{Type: ChatStreamContentDelta, Content: d.Text})
				}
			case anthropic.InputJSONDelta:
				currentToolJSON += d.PartialJSON
				if onEvent != nil {
					onEvent(ChatStreamEvent{Type: ChatStreamToolUseDelta, Content: d.PartialJSON})
				}
			}

		case anthropic.ContentBlockStopEvent:
			if currentToolID != "" {
				tc := ToolCall{
					ID:    currentToolID,
					Name:  currentToolName,
					Input: json.RawMessage(currentToolJSON),
				}
				toolCalls = append(toolCalls, tc)
				if onEvent != nil {
					onEvent(ChatStreamEvent{Type: ChatStreamToolUseEnd, ToolCall: &tc})
				}
				currentToolID = ""
				currentToolName = ""
				currentToolJSON = ""
			}

		case anthropic.MessageStartEvent:
			usage.InputTokens = int(e.Message.Usage.InputTokens)
			usage.CacheCreationInputTokens = int(e.Message.Usage.CacheCreationInputTokens)
			usage.CacheReadInputTokens = int(e.Message.Usage.CacheReadInputTokens)

		case anthropic.MessageDeltaEvent:
			usage.OutputTokens = int(e.Usage.OutputTokens)
			stopReason = string(e.Delta.StopReason)
		}
	}

	if err := stream.Err(); err != nil {
		return ChatResponse{}, fmt.Errorf("stream error: %w", err)
	}

	// Detect truncated tool calls (stream ended mid-tool, no ContentBlockStopEvent).
	if currentToolID != "" {
		reason := stopReason
		if reason == "" {
			reason = "unknown"
		}
		return ChatResponse{}, fmt.Errorf(
			"output truncated: %s reached mid-tool-call (tool: %s)", reason, currentToolName)
	}

	// Normalize stop reason.
	if stopReason == "" {
		stopReason = "end_turn"
	}

	return ChatResponse{
		Content:    content,
		ToolCalls:  toolCalls,
		StopReason: stopReason,
		Usage:      usage,
	}, nil
}

// convertMessagesToAnthropic converts canonical ChatMessages to Anthropic SDK params.
func convertMessagesToAnthropic(msgs []ChatMessage) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case ChatRoleUser:
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))

		case ChatRoleAssistant:
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var inputData any
				_ = json.Unmarshal(tc.Input, &inputData)
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, inputData, tc.Name))
			}
			if len(blocks) > 0 {
				out = append(out, anthropic.NewAssistantMessage(blocks...))
			}

		case ChatRoleTool:
			out = append(out, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, m.IsError),
			))
		}
		// ChatRoleSystem is handled separately via params.System — skip here.
	}
	return out
}

// convertToolsToAnthropic converts ToolDefinitions to Anthropic SDK tool params.
func convertToolsToAnthropic(defs []ToolDefinition) []anthropic.ToolUnionParam {
	tools := make([]anthropic.ToolUnionParam, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        def.Name,
				Description: anthropic.String(def.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: def.InputSchema["properties"],
					ExtraFields: map[string]any{
						"type":     "object",
						"required": def.InputSchema["required"],
					},
				},
			},
		})
	}
	return tools
}

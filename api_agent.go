package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// APIAgent runs agentic loops using the Anthropic API directly.
// This is the pattern used by labs-service for custom tool flows.
type APIAgent struct {
	client     anthropic.Client
	tools      *ToolRegistry
	hooks      *Hooks
	model      string
	system     string
	maxTurns   int
	canUseTool CanUseToolFunc
	subagents  *SubagentConfig
}

// APIAgentConfig configures an API-based agent.
type APIAgentConfig struct {
	// Anthropic API key (defaults to ANTHROPIC_API_KEY env var)
	APIKey string

	// Model to use (defaults to claude-sonnet-4-20250514)
	Model string

	// System prompt
	SystemPrompt string

	// Custom tools
	Tools *ToolRegistry

	// Hooks for tool execution lifecycle
	Hooks *Hooks

	// Maximum turns before stopping (default: 10)
	MaxTurns int

	// CanUseTool is called before tool execution to get permission.
	// It is invoked before hooks.
	CanUseTool CanUseToolFunc

	// Subagents configures child agent definitions for the Task tool.
	Subagents *SubagentConfig
}

// NewAPIAgent creates an agent that uses the Anthropic API.
func NewAPIAgent(cfg APIAgentConfig) *APIAgent {
	opts := []option.RequestOption{}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}

	client := anthropic.NewClient(opts...)

	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-20250514"
	}
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = 10
	}

	tools := cfg.Tools
	if tools == nil {
		tools = NewToolRegistry()
	}

	a := &APIAgent{
		client:     client,
		tools:      tools,
		hooks:      cfg.Hooks,
		model:      cfg.Model,
		system:     cfg.SystemPrompt,
		maxTurns:   cfg.MaxTurns,
		canUseTool: cfg.CanUseTool,
		subagents:  cfg.Subagents,
	}

	// Register Task tool if subagents are configured
	if cfg.Subagents != nil {
		registerTaskTool(a.tools, cfg.Subagents, Options{
			Model:      cfg.Model,
			SystemPrompt: cfg.SystemPrompt,
		})
	}

	return a
}

// Run executes the agent loop and streams events.
func (a *APIAgent) Run(ctx context.Context, prompt string) (<-chan AgentEvent, error) {
	events := make(chan AgentEvent, 100)
	go a.runLoop(ctx, prompt, events)
	return events, nil
}

func (a *APIAgent) runLoop(ctx context.Context, prompt string, events chan<- AgentEvent) {
	defer close(events)

	// Build initial messages
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	// Convert tool definitions to Anthropic format
	tools := a.buildTools()

	for turn := 0; turn < a.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			events <- AgentEvent{Type: AgentEventError, Error: ctx.Err()}
			return
		default:
		}

		// Make streaming API call
		toolCalls, assistantBlocks, err := a.streamTurn(ctx, messages, tools, events)
		if err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// No tool calls = done
		if len(toolCalls) == 0 {
			events <- AgentEvent{Type: AgentEventComplete}
			return
		}

		// Add assistant message to history
		messages = append(messages, anthropic.NewAssistantMessage(assistantBlocks...))

		// Execute tools
		toolResults := a.executeTools(ctx, toolCalls, events)

		// Add tool results to messages
		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, tr := range toolResults {
			resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(
				tr.ToolUseID,
				tr.Content,
				tr.IsError,
			))
		}
		messages = append(messages, anthropic.NewUserMessage(resultBlocks...))

		events <- AgentEvent{Type: AgentEventTurnComplete}
	}

	events <- AgentEvent{
		Type:  AgentEventError,
		Error: fmt.Errorf("max turns (%d) reached", a.maxTurns),
	}
}

func (a *APIAgent) buildTools() []anthropic.ToolUnionParam {
	if a.tools == nil {
		return nil
	}

	defs := a.tools.Definitions()
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

func (a *APIAgent) streamTurn(
	ctx context.Context,
	messages []anthropic.MessageParam,
	tools []anthropic.ToolUnionParam,
	events chan<- AgentEvent,
) ([]ToolCall, []anthropic.ContentBlockParamUnion, error) {

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 4096,
		Messages:  messages,
	}

	if a.system != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: a.system},
		}
	}

	if len(tools) > 0 {
		params.Tools = tools
	}

	// Use streaming
	stream := a.client.Messages.NewStreaming(ctx, params)

	events <- AgentEvent{Type: AgentEventMessageStart}

	var (
		toolCalls       []ToolCall
		assistantBlocks []anthropic.ContentBlockParamUnion
		currentText     string
		currentToolID   string
		currentToolName string
		currentToolJSON string
	)

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			switch cb := e.ContentBlock.AsAny().(type) {
			case anthropic.TextBlock:
				currentText = cb.Text
			case anthropic.ToolUseBlock:
				currentToolID = cb.ID
				currentToolName = cb.Name
				currentToolJSON = ""
				events <- AgentEvent{
					Type: AgentEventToolUseStart,
					ToolCall: &ToolCall{
						ID:   cb.ID,
						Name: cb.Name,
					},
				}
			}

		case anthropic.ContentBlockDeltaEvent:
			switch d := e.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				currentText += d.Text
				events <- AgentEvent{
					Type:    AgentEventContentDelta,
					Content: d.Text,
				}
			case anthropic.InputJSONDelta:
				currentToolJSON += d.PartialJSON
				events <- AgentEvent{
					Type:    AgentEventToolUseDelta,
					Content: d.PartialJSON,
				}
			}

		case anthropic.ContentBlockStopEvent:
			if currentToolID != "" {
				// Finished a tool use block
				tc := ToolCall{
					ID:    currentToolID,
					Name:  currentToolName,
					Input: json.RawMessage(currentToolJSON),
				}
				toolCalls = append(toolCalls, tc)
				var inputData any
				_ = json.Unmarshal([]byte(currentToolJSON), &inputData)
				assistantBlocks = append(assistantBlocks, anthropic.NewToolUseBlock(
					currentToolID, inputData, currentToolName,
				))
				events <- AgentEvent{
					Type:     AgentEventToolUseEnd,
					ToolCall: &tc,
				}
				currentToolID = ""
				currentToolName = ""
				currentToolJSON = ""
			} else if currentText != "" {
				// Finished a text block
				assistantBlocks = append(assistantBlocks, anthropic.NewTextBlock(currentText))
				currentText = ""
			}
		}
	}

	if err := stream.Err(); err != nil {
		return nil, nil, fmt.Errorf("stream error: %w", err)
	}

	events <- AgentEvent{Type: AgentEventMessageEnd}

	return toolCalls, assistantBlocks, nil
}

func (a *APIAgent) executeTools(
	ctx context.Context,
	toolCalls []ToolCall,
	events chan<- AgentEvent,
) []ToolResponse {
	results := make([]ToolResponse, 0, len(toolCalls))

	for _, tc := range toolCalls {
		var response ToolResponse
		response.ToolUseID = tc.ID

		// Check canUseTool permission callback first
		currentInput := tc.Input
		if a.canUseTool != nil {
			decision := a.canUseTool(ctx, tc.Name, tc.ID, tc.Input)
			if !decision.Allow {
				reason := decision.Reason
				if reason == "" {
					reason = "permission denied"
				}
				response.Content = fmt.Sprintf("Tool execution denied: %s", reason)
				response.IsError = true
				events <- AgentEvent{
					Type:         AgentEventToolResult,
					ToolResponse: &response,
				}
				results = append(results, response)
				continue
			}
			if decision.ModifiedInput != nil {
				currentInput = decision.ModifiedInput
			}
		}

		// Run pre-tool-use hooks
		if a.hooks != nil {
			hookCtx := HookContext{
				ToolName:  tc.Name,
				ToolUseID: tc.ID,
				Input:     currentInput,
			}
			hookResult, _ := a.hooks.RunPreHooks(ctx, hookCtx)

			switch hookResult.Decision { //nolint:exhaustive // HookAllow is default, no action needed
			case HookDeny:
				response.Content = fmt.Sprintf("Tool execution denied: %s", hookResult.Reason)
				response.IsError = true
				events <- AgentEvent{
					Type:         AgentEventToolResult,
					ToolResponse: &response,
				}
				results = append(results, response)
				continue
			case HookModify:
				currentInput = hookResult.ModifiedInput
			}
		}

		// Execute the tool
		if a.tools == nil || !a.tools.Has(tc.Name) {
			response.Content = fmt.Sprintf("Tool not found: %s", tc.Name)
			response.IsError = true
		} else {
			result, err := a.tools.Execute(ctx, tc.Name, currentInput)
			if err != nil {
				response.Content = err.Error()
				response.IsError = true
			} else {
				response.Content = result
			}
		}

		// Run post-tool-use hooks
		if a.hooks != nil {
			hookCtx := HookContext{
				ToolName:  tc.Name,
				ToolUseID: tc.ID,
				Input:     currentInput,
			}
			_ = a.hooks.RunPostHooks(ctx, hookCtx, response.Content, response.IsError)
		}

		events <- AgentEvent{
			Type:         AgentEventToolResult,
			ToolResponse: &response,
		}

		results = append(results, response)
	}

	return results
}

// RunSync executes the agent and returns all text output.
func (a *APIAgent) RunSync(ctx context.Context, prompt string) (string, error) {
	events, err := a.Run(ctx, prompt)
	if err != nil {
		return "", err
	}

	var content string
	for event := range events {
		if event.Error != nil {
			return content, event.Error
		}
		content += event.Content
	}
	return content, nil
}

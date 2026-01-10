package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// AgentEvent represents events emitted during agent execution.
type AgentEvent struct {
	Type    AgentEventType
	Content string

	// For tool events
	ToolCall     *ToolCall
	ToolResponse *ToolResponse

	// For message events
	Message *AssistantMessage

	// For completion
	Result *ResultMessage
	Error  error
}

// AgentEventType categorizes agent events.
type AgentEventType string

const (
	AgentEventMessageStart AgentEventType = "message_start"
	AgentEventContentDelta AgentEventType = "content_delta"
	AgentEventMessageEnd   AgentEventType = "message_end"
	AgentEventToolUseStart AgentEventType = "tool_use_start"
	AgentEventToolUseDelta AgentEventType = "tool_use_delta"
	AgentEventToolUseEnd   AgentEventType = "tool_use_end"
	AgentEventToolResult   AgentEventType = "tool_result"
	AgentEventTurnComplete AgentEventType = "turn_complete"
	AgentEventError        AgentEventType = "error"
	AgentEventComplete     AgentEventType = "complete"
)

// Agent orchestrates Claude with custom tools in an agentic loop.
type Agent struct {
	client   *Client
	tools    *ToolRegistry
	hooks    *Hooks
	maxTurns int

	mu       sync.Mutex
	running  bool
	cancelFn context.CancelFunc
}

// AgentConfig configures an Agent.
type AgentConfig struct {
	// Base client options
	Options Options

	// Custom tools
	Tools *ToolRegistry

	// Hooks for tool execution lifecycle
	Hooks *Hooks

	// Maximum turns (LLM calls) before stopping. 0 = unlimited.
	MaxTurns int
}

// NewAgent creates an Agent with the given configuration.
func NewAgent(cfg AgentConfig) *Agent {
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = 10 // sensible default
	}
	return &Agent{
		client:   NewClient(cfg.Options),
		tools:    cfg.Tools,
		hooks:    cfg.Hooks,
		maxTurns: cfg.MaxTurns,
	}
}

// Run executes the agent loop with the given prompt.
// Returns a channel of AgentEvents for real-time streaming.
func (a *Agent) Run(ctx context.Context, prompt string) (<-chan AgentEvent, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil, fmt.Errorf("agent already running")
	}
	a.running = true

	ctx, cancel := context.WithCancel(ctx)
	a.cancelFn = cancel
	a.mu.Unlock()

	events := make(chan AgentEvent, 100)

	go a.runLoop(ctx, prompt, events)

	return events, nil
}

// Stop cancels the running agent.
func (a *Agent) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelFn != nil {
		a.cancelFn()
	}
}

// runLoop is the main agent execution loop.
func (a *Agent) runLoop(ctx context.Context, prompt string, events chan<- AgentEvent) {
	defer close(events)
	defer func() {
		a.mu.Lock()
		a.running = false
		a.mu.Unlock()
	}()

	// Build conversation history
	history := []ConversationMessage{
		{Role: "user", Content: prompt},
	}

	for turn := 0; turn < a.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			events <- AgentEvent{Type: AgentEventError, Error: ctx.Err()}
			return
		default:
		}

		// Stream response from Claude
		toolCalls, assistantContent, result, err := a.streamTurn(ctx, history, events)
		if err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// No tool calls = we're done
		if len(toolCalls) == 0 {
			events <- AgentEvent{
				Type:   AgentEventComplete,
				Result: result,
			}
			return
		}

		// Add assistant message to history
		history = append(history, ConversationMessage{
			Role:      "assistant",
			Content:   assistantContent,
			ToolCalls: toolCalls,
		})

		// Execute tools and collect results
		toolResults := a.executeTools(ctx, toolCalls, events)

		// Add tool results to history
		for _, tr := range toolResults {
			history = append(history, ConversationMessage{
				Role:       "tool",
				ToolCallID: tr.ToolUseID,
				Content:    tr.Content,
			})
		}

		events <- AgentEvent{Type: AgentEventTurnComplete}
	}

	// Max turns reached
	events <- AgentEvent{
		Type:  AgentEventError,
		Error: fmt.Errorf("max turns (%d) reached", a.maxTurns),
	}
}

// ConversationMessage represents a message in the conversation history.
type ConversationMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// streamTurn streams one LLM response and returns any tool calls.
func (a *Agent) streamTurn(
	ctx context.Context,
	history []ConversationMessage,
	events chan<- AgentEvent,
) ([]ToolCall, string, *ResultMessage, error) {

	// Convert history to prompt for now
	// TODO: Use proper message format via CLI stdin
	prompt := a.buildPrompt(history)

	cliEvents, err := a.client.Query(ctx, prompt)
	if err != nil {
		return nil, "", nil, err
	}

	var (
		toolCalls        []ToolCall
		assistantContent string
		currentToolCall  *ToolCall
		currentToolJSON  string
		result           *ResultMessage
	)

	events <- AgentEvent{Type: AgentEventMessageStart}

	for event := range cliEvents {
		if event.Error != nil {
			return nil, assistantContent, nil, event.Error
		}

		switch event.Type { //nolint:exhaustive // Only handling events we care about
		case EventContentBlockDelta:
			if event.Text != "" {
				assistantContent += event.Text
				events <- AgentEvent{
					Type:    AgentEventContentDelta,
					Content: event.Text,
				}
			}
			if event.ToolUseDelta != "" && currentToolCall != nil {
				currentToolJSON += event.ToolUseDelta
				events <- AgentEvent{
					Type:    AgentEventToolUseDelta,
					Content: event.ToolUseDelta,
				}
			}

		case EventContentBlockStart:
			if event.ToolUse != nil {
				currentToolCall = &ToolCall{
					ID:   event.ToolUse.ID,
					Name: event.ToolUse.Name,
				}
				currentToolJSON = ""
				if len(event.ToolUse.Input) > 0 {
					currentToolJSON = string(event.ToolUse.Input)
				}
				events <- AgentEvent{
					Type:     AgentEventToolUseStart,
					ToolCall: currentToolCall,
				}
			}

		case EventContentBlockStop:
			if currentToolCall != nil {
				if currentToolJSON != "" {
					currentToolCall.Input = json.RawMessage(currentToolJSON)
				}
				toolCalls = append(toolCalls, *currentToolCall)
				events <- AgentEvent{
					Type:     AgentEventToolUseEnd,
					ToolCall: currentToolCall,
				}
				currentToolCall = nil
				currentToolJSON = ""
			}

		case EventResult:
			result = event.Result
		}

		// Handle assistant messages with embedded tool calls
		if event.AssistantMessage != nil {
			for _, block := range event.AssistantMessage.Content {
				if tb, ok := block.(TextBlock); ok {
					assistantContent += tb.Text
				}
				if tu, ok := block.(ToolUseBlock); ok {
					tc := ToolCall(tu)
					toolCalls = append(toolCalls, tc)
					events <- AgentEvent{
						Type:     AgentEventToolUseStart,
						ToolCall: &tc,
					}
					events <- AgentEvent{
						Type:     AgentEventToolUseEnd,
						ToolCall: &tc,
					}
				}
			}
		}
	}

	events <- AgentEvent{Type: AgentEventMessageEnd}

	return toolCalls, assistantContent, result, nil
}

// executeTools runs all tool calls and returns results.
func (a *Agent) executeTools(
	ctx context.Context,
	toolCalls []ToolCall,
	events chan<- AgentEvent,
) []ToolResponse {
	results := make([]ToolResponse, 0, len(toolCalls))

	for _, tc := range toolCalls {
		var response ToolResponse
		response.ToolUseID = tc.ID

		// Run pre-tool-use hooks
		currentInput := tc.Input
		if a.hooks != nil {
			hookCtx := HookContext{
				ToolName:  tc.Name,
				ToolUseID: tc.ID,
				Input:     tc.Input,
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

// buildPrompt converts conversation history to a prompt string.
// TODO: Replace with proper message format via CLI stdin.
func (a *Agent) buildPrompt(history []ConversationMessage) string {
	var b strings.Builder
	for _, msg := range history {
		switch msg.Role {
		case "user":
			b.WriteString("User: ")
			b.WriteString(msg.Content)
			b.WriteString("\n")
		case "assistant":
			if msg.Content != "" {
				b.WriteString("Assistant: ")
				b.WriteString(msg.Content)
				b.WriteString("\n")
			}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					b.WriteString("Assistant tool call: ")
					b.WriteString(tc.Name)
					if len(tc.Input) > 0 {
						b.WriteString(" input=")
						b.WriteString(string(tc.Input))
					}
					b.WriteString("\n")
				}
			}
		case "tool":
			b.WriteString("Tool result (")
			b.WriteString(msg.ToolCallID)
			b.WriteString("): ")
			b.WriteString(msg.Content)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// RunSync executes the agent and collects all text output.
func (a *Agent) RunSync(ctx context.Context, prompt string) (string, error) {
	events, err := a.Run(ctx, prompt)
	if err != nil {
		return "", err
	}

	var content string
	for event := range events {
		if event.Error != nil {
			return content, event.Error
		}
		if event.Content != "" {
			content += event.Content
		}
	}
	return content, nil
}

// MarshalToolInput is a helper to marshal tool input to JSON.
func MarshalToolInput(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

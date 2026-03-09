package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
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

	// For subagent events - links to the parent's tool invocation
	ParentToolUseID string
	// SubagentName identifies which subagent produced this event
	SubagentName string

	// For turn_complete events - populated when a MetricsCollector is configured
	TurnMetrics *TurnMetrics
}

// AgentEventType categorizes agent events.
type AgentEventType string

const (
	AgentEventMessageStart   AgentEventType = "message_start"
	AgentEventContentDelta   AgentEventType = "content_delta"
	AgentEventMessageEnd     AgentEventType = "message_end"
	AgentEventToolUseStart   AgentEventType = "tool_use_start"
	AgentEventToolUseDelta   AgentEventType = "tool_use_delta"
	AgentEventToolUseEnd     AgentEventType = "tool_use_end"
	AgentEventToolResult     AgentEventType = "tool_result"
	AgentEventTurnComplete   AgentEventType = "turn_complete"
	AgentEventError          AgentEventType = "error"
	AgentEventComplete       AgentEventType = "complete"
	AgentEventSkillsSelected AgentEventType = "skills_selected"
)

// Agent orchestrates Claude with custom tools in an agentic loop.
type Agent struct {
	client         *Client
	tools          *ToolRegistry
	hooks          *Hooks
	maxTurns       int
	canUseTool     CanUseToolFunc
	subagents      *SubagentConfig
	skills         *SkillRegistry
	contextBuilder *ContextBuilder
	metrics        *MetricsCollector
	parallelTools  bool

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

	// CanUseTool is called before tool execution to get permission.
	// It is invoked before hooks.
	CanUseTool CanUseToolFunc

	// Subagents configures child agent definitions for the Task tool.
	Subagents *SubagentConfig

	// Skills provides skill-based tool organization with semantic lookup.
	Skills *SkillRegistry

	// ContextBuilder controls dynamic per-turn tool selection.
	// If nil, all registered tools are sent every turn (current behavior).
	ContextBuilder *ContextBuilder

	// Metrics collects per-turn and per-tool execution metrics.
	// If nil, no metrics are gathered.
	Metrics *MetricsCollector

	// ParallelTools enables concurrent execution of independent tool calls within a turn.
	// When true, all tool calls returned by the LLM in a single turn run in parallel.
	// Only enable this for tools with no inter-dependencies or shared mutable state.
	ParallelTools bool
}

// NewAgent creates an Agent with the given configuration.
func NewAgent(cfg AgentConfig) *Agent {
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = 10 // sensible default
	}

	tools := cfg.Tools
	if tools == nil {
		tools = NewToolRegistry()
	}

	a := &Agent{
		client:         NewClient(cfg.Options),
		tools:          tools,
		hooks:          cfg.Hooks,
		maxTurns:       cfg.MaxTurns,
		canUseTool:     cfg.CanUseTool,
		subagents:      cfg.Subagents,
		skills:         cfg.Skills,
		contextBuilder: cfg.ContextBuilder,
		metrics:        cfg.Metrics,
		parallelTools:  cfg.ParallelTools,
	}

	// Register Task tool if subagents are configured
	if cfg.Subagents != nil {
		registerTaskTool(a.tools, cfg.Subagents, cfg.Options, cfg.Hooks)
	}

	return a
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
		if a.metrics != nil {
			a.metrics.recordSessionEnd()
		}
		// Emit session end hook
		if a.hooks != nil {
			a.hooks.EmitEvent(ctx, HookEventData{
				Event:   HookSessionEnd,
				Message: "agent session ended",
			})
		}
	}()

	if a.metrics != nil {
		a.metrics.recordSessionStart()
	}

	// Emit session start hook
	if a.hooks != nil {
		a.hooks.EmitEvent(ctx, HookEventData{
			Event:   HookSessionStart,
			Message: "agent session started",
		})
	}

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

		// Stream response from Claude, tracking LLM latency
		llmStart := time.Now()
		toolCalls, assistantContent, result, err := a.streamTurn(ctx, history, events)
		llmLatency := time.Since(llmStart)
		if err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// No tool calls = we're done
		if len(toolCalls) == 0 {
			if result != nil && result.StopReason == "" {
				result.StopReason = "end_turn"
			}
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

		// Record turn metrics and emit with AgentEventTurnComplete
		var tm *TurnMetrics
		if a.metrics != nil {
			toolNames := make([]string, len(toolCalls))
			for i, tc := range toolCalls {
				toolNames[i] = tc.Name
			}
			recorded := TurnMetrics{
				TurnIndex:    turn,
				LLMLatency:   llmLatency,
				ToolsInvoked: toolNames,
			}
			a.metrics.recordTurn(recorded)
			tm = &recorded
		}
		events <- AgentEvent{Type: AgentEventTurnComplete, TurnMetrics: tm}
	}

	// Max turns reached
	if a.hooks != nil {
		a.hooks.EmitEvent(ctx, HookEventData{
			Event:   HookStop,
			Message: fmt.Sprintf("max turns (%d) reached", a.maxTurns),
		})
	}
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

	// Convert history to Messages for proper CLI communication
	messages := a.historyToMessages(history)
	cliEvents, err := a.client.QueryWithMessages(ctx, messages)
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
// When ParallelTools is enabled and there are multiple calls, they run concurrently.
func (a *Agent) executeTools(
	ctx context.Context,
	toolCalls []ToolCall,
	events chan<- AgentEvent,
) []ToolResponse {
	if a.parallelTools && len(toolCalls) > 1 {
		return runToolsParallel(ctx, toolCalls, a.tools, a.hooks, a.canUseTool, a.metrics, events)
	}
	return runToolsSequential(ctx, toolCalls, a.tools, a.hooks, a.canUseTool, a.metrics, events)
}

// historyToMessages converts conversation history to Message types for CLI communication.
func (a *Agent) historyToMessages(history []ConversationMessage) []Message {
	messages := make([]Message, 0, len(history))
	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, UserMessage{
				Content: []ContentBlock{TextBlock{Text: msg.Content}},
			})
		case "assistant":
			var blocks []ContentBlock
			if msg.Content != "" {
				blocks = append(blocks, TextBlock{Text: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				blocks = append(blocks, ToolUseBlock(tc))
			}
			messages = append(messages, AssistantMessage{Content: blocks})
		case "tool":
			messages = append(messages, UserMessage{
				Content: []ContentBlock{ToolResultBlock{
					ToolUseID: msg.ToolCallID,
					Content:   msg.Content,
				}},
			})
		}
	}
	return messages
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

// Close gracefully shuts down the agent, sending SIGINT then SIGKILL after timeout.
func (a *Agent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelFn != nil {
		a.cancelFn()
	}
	return a.client.Close()
}

// Send writes a follow-up message to the running agent's stdin.
// The ctx parameter is checked for cancellation before sending.
func (a *Agent) Send(ctx context.Context, message string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return a.client.Send(message)
	}
}

// RewindFiles rewinds file changes to a previous checkpoint.
// Requires EnableFileCheckpointing to be set in Options.
func (a *Agent) RewindFiles(ctx context.Context, userMessageID string) error {
	cm := &CheckpointManager{
		sessionID: a.client.opts.SessionID,
		cliPath:   a.client.opts.CLIPath,
		cwd:       a.client.opts.Cwd,
	}
	return cm.RewindFiles(ctx, userMessageID)
}

// MarshalToolInput is a helper to marshal tool input to JSON.
func MarshalToolInput(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

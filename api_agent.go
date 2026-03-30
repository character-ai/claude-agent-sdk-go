package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// APIAgent runs agentic loops using the Anthropic API directly.
// This is the pattern used by labs-service for custom tool flows.
type APIAgent struct {
	client         anthropic.Client
	tools          *ToolRegistry
	hooks          *Hooks
	model          string
	system         string
	maxTurns       int
	maxTokens      int
	canUseTool     CanUseToolFunc
	subagents      *SubagentConfig
	skills         *SkillRegistry
	contextBuilder *ContextBuilder
	metrics        *MetricsCollector
	parallelTools  bool
	retry          *RetryConfig
	budget         *BudgetConfig
	history        *HistoryConfig
	todoStore      *TodoStore
}

// APIAgentConfig configures an API-based agent.
type APIAgentConfig struct {
	// Anthropic API key (defaults to ANTHROPIC_API_KEY env var)
	APIKey string // #nosec G117 -- This is a config field, not a hardcoded secret

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

	// MaxTokens is the maximum number of tokens the model can generate per turn.
	// Defaults to 4096.
	MaxTokens int

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

	// Retry configures automatic retry behavior for tool execution failures.
	// Per-tool RetryConfig on ToolDefinition takes precedence over this global setting.
	Retry *RetryConfig

	// Budget sets resource limits (tokens, time) for the session.
	// The session stops with BudgetExceededError when any limit is hit.
	// Note: MaxCostUSD is not populated for APIAgent (use MaxTokens instead).
	Budget *BudgetConfig

	// History controls conversation history compaction before each LLM call.
	History *HistoryConfig

	// EnableTodos registers the write_todos tool, allowing the agent to
	// plan its work and track progress via a todo list. The host app
	// receives AgentEventTodosUpdated events when the list changes.
	EnableTodos bool

	// TodoStore is an optional pre-existing TodoStore to use. If nil and
	// EnableTodos is true, a new store is created automatically.
	TodoStore *TodoStore
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
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 4096
	}

	tools := cfg.Tools
	if tools == nil {
		tools = NewToolRegistry()
	}

	a := &APIAgent{
		client:         client,
		tools:          tools,
		hooks:          cfg.Hooks,
		model:          cfg.Model,
		system:         cfg.SystemPrompt,
		maxTurns:       cfg.MaxTurns,
		maxTokens:      cfg.MaxTokens,
		canUseTool:     cfg.CanUseTool,
		subagents:      cfg.Subagents,
		skills:         cfg.Skills,
		contextBuilder: cfg.ContextBuilder,
		metrics:        cfg.Metrics,
		parallelTools:  cfg.ParallelTools,
		retry:          cfg.Retry,
		budget:         cfg.Budget,
		history:        cfg.History,
	}

	// Register Task tool if subagents are configured
	if cfg.Subagents != nil {
		registerTaskTool(a.tools, cfg.Subagents, Options{
			Model:        cfg.Model,
			SystemPrompt: cfg.SystemPrompt,
		}, cfg.Hooks)
	}

	// Register todo tools if enabled
	if cfg.EnableTodos {
		a.todoStore = initTodoStore(a.tools, cfg.TodoStore)
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
	defer func() {
		if a.metrics != nil {
			a.metrics.recordSessionEnd()
		}
	}()

	if a.metrics != nil {
		a.metrics.recordSessionStart()
	}

	// Build initial messages
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
	}

	// Convert tool definitions to Anthropic format.
	// When context builder is configured, tools are rebuilt each turn based on latest context.
	lastQuery := prompt
	tools := a.buildToolsForQuery(ctx, lastQuery, events)

	budget := newBudgetTracker(a.budget)

	// Accumulate token usage across turns for final reporting.
	var totalInputTokens, totalOutputTokens int
	var totalCacheCreation, totalCacheRead int

	for turn := 0; turn < a.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			events <- AgentEvent{Type: AgentEventError, Error: ctx.Err()}
			return
		default:
		}

		// Check time budget before each turn
		if err := budget.check(); err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// Rebuild tools if context builder is configured (dynamic selection per turn).
		if a.contextBuilder != nil && turn > 0 {
			tools = a.buildToolsForQuery(ctx, lastQuery, events)
		}

		// Apply history compaction before sending to the LLM
		llmMessages := compactMessages(messages, a.history)

		// Make streaming API call, tracking LLM latency
		llmStart := time.Now()
		toolCalls, assistantBlocks, usage, err := a.streamTurn(ctx, llmMessages, tools, events)
		llmLatency := time.Since(llmStart)
		if err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// Accumulate token usage
		totalInputTokens += usage.InputTokens
		totalOutputTokens += usage.OutputTokens
		totalCacheCreation += usage.CacheCreationInputTokens
		totalCacheRead += usage.CacheReadInputTokens

		// Record token usage and check budget
		if err := budget.record(usage.InputTokens, usage.OutputTokens, 0); err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// No tool calls = done
		if len(toolCalls) == 0 {
			stopReason := usage.StopReason
			if stopReason == "" {
				stopReason = "end_turn"
			}
			events <- AgentEvent{
				Type:   AgentEventComplete,
				Result: buildAPIResult(turn+1, stopReason, totalInputTokens, totalOutputTokens, totalCacheCreation, totalCacheRead),
			}
			return
		}

		// Add assistant message to full history
		messages = append(messages, anthropic.NewAssistantMessage(assistantBlocks...))

		// Execute tools
		toolResults := a.executeTools(ctx, toolCalls, events)

		// Emit todos update if write_todos succeeded this turn
		emitTodoEvents(a.todoStore, toolCalls, toolResults, events)

		// Update lastQuery from tool results so context builder can adapt per turn.
		// Use the concatenation of tool result content as the next query context.
		var resultContext string
		var resultBlocks []anthropic.ContentBlockParamUnion
		for _, tr := range toolResults {
			resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(
				tr.ToolUseID,
				tr.Content,
				tr.IsError,
			))
			if !tr.IsError && tr.Content != "" {
				resultContext += tr.Content + " "
			}
		}
		if resultContext != "" {
			lastQuery = resultContext
		}

		messages = append(messages, anthropic.NewUserMessage(resultBlocks...))

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

	events <- AgentEvent{
		Type:   AgentEventError,
		Error:  fmt.Errorf("max turns (%d) reached", a.maxTurns),
		Result: buildAPIResult(a.maxTurns, "max_turns", totalInputTokens, totalOutputTokens, totalCacheCreation, totalCacheRead),
	}
}

// buildAPIResult constructs a ResultMessage with accumulated token usage.
func buildAPIResult(numTurns int, stopReason string, inputTokens, outputTokens, cacheCreation, cacheRead int) *ResultMessage {
	return &ResultMessage{
		Type:         "result",
		Subtype:      "success",
		NumTurns:     numTurns,
		StopReason:   stopReason,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Usage: &ResultUsage{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			CacheCreationInputTokens: cacheCreation,
			CacheReadInputTokens:     cacheRead,
		},
	}
}

func (a *APIAgent) buildToolsForQuery(ctx context.Context, query string, events chan<- AgentEvent) []anthropic.ToolUnionParam {
	if a.tools == nil {
		return nil
	}

	var defs []ToolDefinition
	if a.contextBuilder != nil && query != "" {
		defs = a.contextBuilder.SelectTools(ctx, query)
		events <- AgentEvent{
			Type:    AgentEventSkillsSelected,
			Content: fmt.Sprintf("selected %d tools for query", len(defs)),
		}
	} else {
		defs = a.tools.Definitions()
	}

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

// apiTurnUsage holds token counts and stop reason from a single streaming turn.
type apiTurnUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	StopReason               string
}

func (a *APIAgent) streamTurn(
	ctx context.Context,
	messages []anthropic.MessageParam,
	tools []anthropic.ToolUnionParam,
	events chan<- AgentEvent,
) ([]ToolCall, []anthropic.ContentBlockParamUnion, apiTurnUsage, error) {

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: int64(a.maxTokens),
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
		usage           apiTurnUsage
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

		case anthropic.MessageStartEvent:
			usage.InputTokens = int(e.Message.Usage.InputTokens)
			usage.CacheCreationInputTokens = int(e.Message.Usage.CacheCreationInputTokens)
			usage.CacheReadInputTokens = int(e.Message.Usage.CacheReadInputTokens)

		case anthropic.MessageDeltaEvent:
			usage.OutputTokens = int(e.Usage.OutputTokens)
			usage.StopReason = string(e.Delta.StopReason)
		}
	}

	if err := stream.Err(); err != nil {
		return nil, nil, apiTurnUsage{}, fmt.Errorf("stream error: %w", err)
	}

	// Check for truncated tool calls: if the stream ended with an open tool
	// block (no ContentBlockStopEvent received), the output was truncated
	// mid-tool-call — typically due to max_tokens.
	if currentToolID != "" {
		reason := usage.StopReason
		if reason == "" {
			reason = "unknown"
		}
		return nil, nil, usage, fmt.Errorf(
			"output truncated: %s reached mid-tool-call (tool: %s)", reason, currentToolName)
	}

	events <- AgentEvent{Type: AgentEventMessageEnd}

	return toolCalls, assistantBlocks, usage, nil
}

func (a *APIAgent) executeTools(
	ctx context.Context,
	toolCalls []ToolCall,
	events chan<- AgentEvent,
) []ToolResponse {
	if a.parallelTools && len(toolCalls) > 1 {
		return runToolsParallel(ctx, toolCalls, a.tools, a.hooks, a.canUseTool, a.retry, a.metrics, events)
	}
	return runToolsSequential(ctx, toolCalls, a.tools, a.hooks, a.canUseTool, a.retry, a.metrics, events)
}

// TodoStore returns the agent's TodoStore, or nil if todos are not enabled.
func (a *APIAgent) TodoStore() *TodoStore {
	return a.todoStore
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

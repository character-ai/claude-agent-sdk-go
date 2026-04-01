package claudeagent

import (
	"context"
	"fmt"
	"math"
	"time"
)

// MaxTokensRecovery configures automatic retry when output is truncated
// due to max_tokens limits. Only applies when stop_reason is "max_tokens"
// and there are no tool calls or mid-tool-call errors.
type MaxTokensRecovery struct {
	// ScaleFactor multiplies max_tokens on each retry. Default: 2.0.
	ScaleFactor float64
	// MaxRetries is the maximum number of recovery attempts. Default: 2.
	MaxRetries int
	// Ceiling is the absolute maximum for max_tokens. Default: 16384.
	Ceiling int
}

// withDefaults returns a copy with zero fields replaced by defaults.
func (r *MaxTokensRecovery) withDefaults() MaxTokensRecovery {
	out := *r
	if out.ScaleFactor == 0 {
		out.ScaleFactor = 2.0
	}
	if out.MaxRetries == 0 {
		out.MaxRetries = 2
	}
	if out.Ceiling == 0 {
		out.Ceiling = 16384
	}
	return out
}

// nextMaxTokens computes the next max_tokens value, capped at Ceiling.
func (r *MaxTokensRecovery) nextMaxTokens(current int) int {
	next := int(math.Ceil(float64(current) * r.ScaleFactor))
	if next > r.Ceiling {
		next = r.Ceiling
	}
	return next
}

// shouldRecoverMaxTokens reports whether a max_tokens recovery retry should be attempted.
func shouldRecoverMaxTokens(stopReason string, toolCalls []ToolCall, cfg *MaxTokensRecovery, attempt int) bool {
	if cfg == nil {
		return false
	}
	defaults := cfg.withDefaults()
	return stopReason == "max_tokens" && len(toolCalls) == 0 && attempt < defaults.MaxRetries
}

// FallbackModelConfig configures automatic model switching on persistent API errors.
type FallbackModelConfig struct {
	// Model is the fallback model identifier (e.g., "claude-haiku-4-5-20251001").
	Model string
	// AfterErrors is the number of consecutive errors before switching to fallback.
	// Default: 3.
	AfterErrors int
	// RevertAfter is the duration after which to try the primary model again.
	// Zero means stay on fallback for the rest of the session.
	RevertAfter time.Duration
}

// modelSelector manages primary/fallback model switching.
type modelSelector struct {
	primary        string
	fallback       *FallbackModelConfig
	consecutiveErr int
	switchedAt     time.Time
	usingFallback  bool
}

func newModelSelector(primary string, fallback *FallbackModelConfig) *modelSelector {
	return &modelSelector{primary: primary, fallback: fallback}
}

func (ms *modelSelector) currentModel() string {
	if ms.fallback == nil || !ms.usingFallback {
		return ms.primary
	}
	// Check if we should revert to primary.
	if ms.fallback.RevertAfter > 0 && time.Since(ms.switchedAt) >= ms.fallback.RevertAfter {
		ms.usingFallback = false
		ms.consecutiveErr = 0
		return ms.primary
	}
	return ms.fallback.Model
}

func (ms *modelSelector) recordError() {
	if ms.fallback == nil {
		return
	}
	ms.consecutiveErr++
	threshold := ms.fallback.AfterErrors
	if threshold == 0 {
		threshold = 3
	}
	if ms.consecutiveErr >= threshold && !ms.usingFallback {
		ms.usingFallback = true
		ms.switchedAt = time.Now()
	}
}

func (ms *modelSelector) recordSuccess() {
	ms.consecutiveErr = 0
	if ms.usingFallback {
		ms.usingFallback = false
	}
}

// APIAgent runs agentic loops using the Anthropic API directly.
// This is the pattern used by labs-service for custom tool flows.
type APIAgent struct {
	provider          LLMProvider
	tools             *ToolRegistry
	hooks             *Hooks
	modelSel          *modelSelector
	system            string
	systemBlocks      []SystemPromptBlock
	maxTurns          int
	maxTokens         int
	canUseTool        CanUseToolFunc
	subagents         *SubagentConfig
	skills            *SkillRegistry
	contextBuilder    *ContextBuilder
	metrics           *MetricsCollector
	parallelTools     bool
	retry             *RetryConfig
	budget            *BudgetConfig
	history           *HistoryConfig
	todoStore         *TodoStore
	maxTokensRecovery *MaxTokensRecovery
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

	// SystemPromptBlocks provides structured system prompt blocks with cache
	// control directives. When set, SystemPrompt is ignored.
	// Each block can have CacheControl set to enable Anthropic prompt caching.
	SystemPromptBlocks []SystemPromptBlock

	// EnableTodos registers the write_todos tool, allowing the agent to
	// plan its work and track progress via a todo list. The host app
	// receives AgentEventTodosUpdated events when the list changes.
	EnableTodos bool

	// TodoStore is an optional pre-existing TodoStore to use. If nil and
	// EnableTodos is true, a new store is created automatically.
	TodoStore *TodoStore

	// MaxTokensRecovery, if non-nil, enables automatic retry with increased
	// max_tokens when output is truncated.
	MaxTokensRecovery *MaxTokensRecovery

	// FallbackModel configures automatic model switching on persistent API errors.
	// When set, the agent switches to the fallback model after consecutive errors.
	FallbackModel *FallbackModelConfig

	// Provider overrides the default AnthropicProvider.
	// When set, APIKey is ignored (the provider manages its own credentials).
	// When nil, an AnthropicProvider is created from APIKey.
	Provider LLMProvider
}

// NewAPIAgent creates an agent that uses the Anthropic API (or a custom provider).
func NewAPIAgent(cfg APIAgentConfig) *APIAgent {
	// Only apply the Anthropic default model when no custom provider is given.
	// When Provider is set, the provider owns its model selection — passing a
	// hard-coded Anthropic model name to OpenAI/Mistral/etc. will cause a 404.
	if cfg.Model == "" && cfg.Provider == nil {
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

	// Determine system prompt representation: structured blocks take precedence.
	var systemStr string
	var systemBlocks []SystemPromptBlock
	if len(cfg.SystemPromptBlocks) > 0 {
		systemBlocks = cfg.SystemPromptBlocks
	} else if cfg.SystemPrompt != "" {
		systemStr = cfg.SystemPrompt
	}

	// Use the provided provider, or default to Anthropic.
	var provider LLMProvider
	if cfg.Provider != nil {
		provider = cfg.Provider
	} else {
		provider = NewAnthropicProvider(AnthropicProviderConfig{APIKey: cfg.APIKey})
	}

	a := &APIAgent{
		provider:          provider,
		tools:             tools,
		hooks:             cfg.Hooks,
		modelSel:          newModelSelector(cfg.Model, cfg.FallbackModel),
		system:            systemStr,
		systemBlocks:      systemBlocks,
		maxTurns:          cfg.MaxTurns,
		maxTokens:         cfg.MaxTokens,
		canUseTool:        cfg.CanUseTool,
		subagents:         cfg.Subagents,
		skills:            cfg.Skills,
		contextBuilder:    cfg.ContextBuilder,
		metrics:           cfg.Metrics,
		parallelTools:     cfg.ParallelTools,
		retry:             cfg.Retry,
		budget:            cfg.Budget,
		history:           cfg.History,
		maxTokensRecovery: cfg.MaxTokensRecovery,
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

	// Build initial chat history with canonical ChatMessage types.
	history := []ChatMessage{{Role: ChatRoleUser, Content: prompt}}

	// Select tools for the first turn.
	lastQuery := prompt
	toolDefs := a.selectTools(ctx, lastQuery, events)

	budget := newBudgetTracker(a.budget)

	var totalInputTokens, totalOutputTokens int
	var totalCacheCreation, totalCacheRead int

	for turn := 0; turn < a.maxTurns; turn++ {
		select {
		case <-ctx.Done():
			events <- AgentEvent{Type: AgentEventError, Error: ctx.Err()}
			return
		default:
		}

		if err := budget.check(); err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		// Rebuild tools if context builder is configured (dynamic selection per turn).
		if a.contextBuilder != nil && turn > 0 {
			toolDefs = a.selectTools(ctx, lastQuery, events)
		}

		// Compact history before sending to the LLM.
		llmHistory := compactChatHistory(ctx, history, a.history)

		// Build the provider request.
		req := ChatRequest{
			Model:        a.modelSel.currentModel(),
			Messages:     llmHistory,
			Tools:        toolDefs,
			SystemPrompt: a.system,
			SystemBlocks: a.systemBlocks,
			MaxTokens:    a.maxTokens,
		}

		// Translate streaming events to AgentEvents.
		onEvent := func(se ChatStreamEvent) {
			switch se.Type {
			case ChatStreamContentDelta:
				events <- AgentEvent{Type: AgentEventContentDelta, Content: se.Content}
			case ChatStreamToolUseStart:
				events <- AgentEvent{Type: AgentEventToolUseStart, ToolCall: se.ToolCall}
			case ChatStreamToolUseDelta:
				events <- AgentEvent{Type: AgentEventToolUseDelta, Content: se.Content}
			case ChatStreamToolUseEnd:
				events <- AgentEvent{Type: AgentEventToolUseEnd, ToolCall: se.ToolCall}
			}
		}

		// Call provider with max_tokens recovery retry loop.
		turnMaxTokens := a.maxTokens
		var resp ChatResponse
		var llmLatency time.Duration

		for attempt := 0; ; attempt++ {
			req.MaxTokens = turnMaxTokens
			events <- AgentEvent{Type: AgentEventMessageStart}
			llmStart := time.Now()
			var err error
			resp, err = a.provider.Complete(ctx, req, onEvent)
			llmLatency = time.Since(llmStart)

			if err != nil {
				a.modelSel.recordError()
				events <- AgentEvent{Type: AgentEventError, Error: err}
				return
			}
			events <- AgentEvent{Type: AgentEventMessageEnd}
			a.modelSel.recordSuccess()

			if shouldRecoverMaxTokens(resp.StopReason, resp.ToolCalls, a.maxTokensRecovery, attempt) {
				defaults := a.maxTokensRecovery.withDefaults()
				turnMaxTokens = defaults.nextMaxTokens(turnMaxTokens)
				continue
			}
			break
		}

		totalInputTokens += resp.Usage.InputTokens
		totalOutputTokens += resp.Usage.OutputTokens
		totalCacheCreation += resp.Usage.CacheCreationInputTokens
		totalCacheRead += resp.Usage.CacheReadInputTokens

		if err := budget.record(resp.Usage.InputTokens, resp.Usage.OutputTokens, 0); err != nil {
			events <- AgentEvent{Type: AgentEventError, Error: err}
			return
		}

		if len(resp.ToolCalls) == 0 {
			stopReason := resp.StopReason
			if stopReason == "" {
				stopReason = "end_turn"
			}
			events <- AgentEvent{
				Type:   AgentEventComplete,
				Result: buildAPIResult(turn+1, stopReason, totalInputTokens, totalOutputTokens, totalCacheCreation, totalCacheRead),
			}
			return
		}

		// Append assistant message with tool calls to history.
		history = append(history, ChatMessage{
			Role:      ChatRoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		toolResults := a.executeTools(ctx, resp.ToolCalls, events)

		emitTodoEvents(a.todoStore, resp.ToolCalls, toolResults, events)

		var resultContext string
		var injectedMessages []ConversationMessage
		for _, tr := range toolResults {
			history = append(history, ChatMessage{
				Role:       ChatRoleTool,
				Content:    tr.Content,
				ToolCallID: tr.ToolUseID,
				IsError:    tr.IsError,
			})
			if !tr.IsError && tr.Content != "" {
				resultContext += tr.Content + " "
			}
			if tr.Metadata != nil {
				injectedMessages = append(injectedMessages, tr.Metadata.InjectMessages...)
			}
		}
		if resultContext != "" {
			lastQuery = resultContext
		}

		// Inject any metadata messages from structured tool handlers.
		for _, msg := range injectedMessages {
			switch msg.Role {
			case "user":
				history = append(history, ChatMessage{Role: ChatRoleUser, Content: msg.Content})
			case "assistant":
				history = append(history, ChatMessage{Role: ChatRoleAssistant, Content: msg.Content})
			}
		}

		var tm *TurnMetrics
		if a.metrics != nil {
			toolNames := make([]string, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
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

// selectTools returns the tool definitions to send on a given turn.
// When a ContextBuilder is configured it does semantic selection; otherwise all tools.
func (a *APIAgent) selectTools(ctx context.Context, query string, events chan<- AgentEvent) []ToolDefinition {
	if a.tools == nil {
		return nil
	}
	if a.contextBuilder != nil && query != "" {
		defs := a.contextBuilder.SelectTools(ctx, query)
		events <- AgentEvent{
			Type:    AgentEventSkillsSelected,
			Content: fmt.Sprintf("selected %d tools for query", len(defs)),
		}
		return defs
	}
	return a.tools.Definitions()
}

// compactChatHistory trims history for the LLM using HistoryConfig.
// The full history is unchanged; only the slice sent to the provider is shortened.
func compactChatHistory(ctx context.Context, history []ChatMessage, cfg *HistoryConfig) []ChatMessage {
	if cfg == nil || cfg.MaxTurns == 0 {
		return history
	}
	if len(history) <= 1 {
		return history
	}
	rest := history[1:]
	// Each turn = 1 assistant message + N tool result messages.
	// Count assistant messages as a proxy for turns.
	var turns int
	var cutIdx int
	for i := len(rest) - 1; i >= 0; i-- {
		if rest[i].Role == ChatRoleAssistant {
			turns++
			if turns == cfg.MaxTurns {
				cutIdx = i
				break
			}
		}
	}
	if turns < cfg.MaxTurns {
		return history
	}

	// Optionally summarize dropped messages.
	dropped := history[1 : cutIdx+1]
	if cfg.Summarizer != nil && len(dropped) > cfg.SummarizeThreshold {
		// Convert ChatMessages to ConversationMessages for the summarizer.
		conv := make([]ConversationMessage, 0, len(dropped))
		for _, m := range dropped {
			conv = append(conv, ConversationMessage{
				Role:    string(m.Role),
				Content: m.Content,
			})
		}
		if summary, err := cfg.Summarizer(ctx, conv); err == nil && summary != "" {
			out := make([]ChatMessage, 0, 2+(len(history)-(cutIdx+1)))
			out = append(out, history[0], ChatMessage{
				Role:    ChatRoleUser,
				Content: "[Previous conversation summary]\n" + summary,
			})
			return append(out, history[cutIdx+1:]...)
		}
	}

	out := make([]ChatMessage, 0, 1+len(rest)-cutIdx)
	out = append(out, history[0])
	out = append(out, rest[cutIdx:]...)
	return out
}

func (a *APIAgent) executeTools(
	ctx context.Context,
	toolCalls []ToolCall,
	events chan<- AgentEvent,
) []ToolResponse {
	return runToolsSmart(ctx, toolCalls, a.tools, a.hooks, a.canUseTool, a.retry, a.metrics, events, a.parallelTools)
}

// TodoStore returns the agent's TodoStore, or nil if todos are not enabled.
func (a *APIAgent) TodoStore() *TodoStore {
	return a.todoStore
}

// SystemPromptBlock is a section of the system prompt with optional cache control.
type SystemPromptBlock struct {
	// Text is the content of this system prompt section.
	Text string
	// CacheControl, when non-nil, enables Anthropic prompt caching for this block.
	// Set to &CacheControl{Type: "ephemeral"} for standard caching behavior.
	CacheControl *CacheControl
}

// CacheControl configures prompt caching for a system prompt block.
type CacheControl struct {
	Type string // "ephemeral"
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

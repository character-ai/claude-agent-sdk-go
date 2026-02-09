package claudeagent

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"time"
)

// HookEvent represents the type of hook event.
type HookEvent string

const (
	// HookPreToolUse is called before a tool is executed.
	HookPreToolUse HookEvent = "PreToolUse"
	// HookPostToolUse is called after a tool is executed successfully.
	HookPostToolUse HookEvent = "PostToolUse"
	// HookPostToolUseFailure is called after a tool execution fails.
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"
	// HookStop is called when the agent stops.
	HookStop HookEvent = "Stop"
	// HookSubagentStart is called when a subagent begins execution.
	HookSubagentStart HookEvent = "SubagentStart"
	// HookSubagentStop is called when a subagent finishes execution.
	HookSubagentStop HookEvent = "SubagentStop"
	// HookNotification is called for general notifications.
	HookNotification HookEvent = "Notification"
	// HookSessionStart is called when a session begins.
	HookSessionStart HookEvent = "SessionStart"
	// HookSessionEnd is called when a session ends.
	HookSessionEnd HookEvent = "SessionEnd"
)

// HookDecision represents the decision made by a hook.
type HookDecision string

const (
	// HookAllow allows the tool execution to proceed.
	HookAllow HookDecision = "allow"
	// HookDeny blocks the tool execution.
	HookDeny HookDecision = "deny"
	// HookModify allows execution with modified input.
	HookModify HookDecision = "modify"
)

// HookContext provides context about the tool being executed.
type HookContext struct {
	// ToolName is the name of the tool being called.
	ToolName string
	// ToolUseID is the unique identifier for this tool invocation.
	ToolUseID string
	// Input is the raw JSON input to the tool.
	Input json.RawMessage
}

// HookEventData provides event-type-specific data for lifecycle hooks.
type HookEventData struct {
	// Event is the hook event type.
	Event HookEvent
	// SessionID is the current session identifier.
	SessionID string
	// SubagentName is the name of the subagent (for SubagentStart/Stop).
	SubagentName string
	// ToolName is the tool name (for tool-related events).
	ToolName string
	// ToolUseID is the tool invocation ID (for tool-related events).
	ToolUseID string
	// Error is the error message (for failure events).
	Error string
	// Message is a human-readable description of the event.
	Message string
}

// HookResult is the result returned by a hook handler.
type HookResult struct {
	// Decision determines whether to allow, deny, or modify the tool call.
	Decision HookDecision
	// Reason explains why the decision was made (used for deny).
	Reason string
	// ModifiedInput is the new input to use (only for HookModify).
	ModifiedInput json.RawMessage
	// Output is additional data to include in tool result (for PostToolUse).
	Output string
	// AdditionalContext is injected into the conversation as additional context.
	AdditionalContext string
	// SystemMessage is a system-level instruction to inject.
	SystemMessage string
	// Continue indicates whether execution should continue after this hook.
	Continue bool
	// SuppressOutput hides the tool's stdout from the conversation.
	SuppressOutput bool
}

// AllowHook returns a result that allows the tool execution.
func AllowHook() HookResult {
	return HookResult{Decision: HookAllow}
}

// DenyHook returns a result that denies the tool execution.
func DenyHook(reason string) HookResult {
	return HookResult{Decision: HookDeny, Reason: reason}
}

// ModifyHook returns a result that modifies the tool input.
func ModifyHook(newInput json.RawMessage) HookResult {
	return HookResult{Decision: HookModify, ModifiedInput: newInput}
}

// PreToolUseHook is a function called before tool execution.
type PreToolUseHook func(ctx context.Context, hookCtx HookContext) HookResult

// PostToolUseHook is a function called after tool execution.
type PostToolUseHook func(ctx context.Context, hookCtx HookContext, result string, isError bool) HookResult

// GenericHookHandler is a function called for lifecycle events.
type GenericHookHandler func(ctx context.Context, data HookEventData)

// HookMatcher matches tools by name pattern.
type HookMatcher struct {
	// Matcher is the tool name to match (exact match, "*" for all, or a regex pattern).
	Matcher string
	// IsRegex indicates the Matcher should be treated as a regular expression.
	IsRegex bool
	// Timeout is the maximum duration for hook execution. Zero means no timeout.
	Timeout time.Duration
	// PreHooks are called before tool execution.
	PreHooks []PreToolUseHook
	// PostHooks are called after tool execution.
	PostHooks []PostToolUseHook

	// compiled regex (lazily initialized)
	compiledOnce sync.Once
	compiledRe   *regexp.Regexp
}

// Matches returns true if the matcher applies to the given tool name.
func (h *HookMatcher) Matches(toolName string) bool {
	if h.Matcher == "*" {
		return true
	}
	if h.IsRegex {
		h.compiledOnce.Do(func() {
			h.compiledRe, _ = regexp.Compile(h.Matcher)
		})
		if h.compiledRe != nil {
			return h.compiledRe.MatchString(toolName)
		}
		return false
	}
	return h.Matcher == toolName
}

// Hooks configures hook handlers for tool execution.
type Hooks struct {
	// Matchers define which hooks apply to which tools.
	Matchers []HookMatcher
	// EventHandlers maps lifecycle events to generic handlers.
	EventHandlers map[HookEvent][]GenericHookHandler
}

// NewHooks creates a new Hooks configuration.
func NewHooks() *Hooks {
	return &Hooks{
		Matchers:      make([]HookMatcher, 0),
		EventHandlers: make(map[HookEvent][]GenericHookHandler),
	}
}

// AddPreHook adds a pre-tool-use hook for the specified tool pattern.
func (h *Hooks) AddPreHook(matcher string, hook PreToolUseHook) {
	for i := range h.Matchers {
		if h.Matchers[i].Matcher == matcher {
			h.Matchers[i].PreHooks = append(h.Matchers[i].PreHooks, hook)
			return
		}
	}
	h.Matchers = append(h.Matchers, HookMatcher{
		Matcher:  matcher,
		PreHooks: []PreToolUseHook{hook},
	})
}

// AddPostHook adds a post-tool-use hook for the specified tool pattern.
func (h *Hooks) AddPostHook(matcher string, hook PostToolUseHook) {
	for i := range h.Matchers {
		if h.Matchers[i].Matcher == matcher {
			h.Matchers[i].PostHooks = append(h.Matchers[i].PostHooks, hook)
			return
		}
	}
	h.Matchers = append(h.Matchers, HookMatcher{
		Matcher:   matcher,
		PostHooks: []PostToolUseHook{hook},
	})
}

// AddPreHookWithOptions adds a pre-hook with matcher options (regex, timeout).
func (h *Hooks) AddPreHookWithOptions(matcher string, isRegex bool, timeout time.Duration, hook PreToolUseHook) {
	for i := range h.Matchers {
		if h.Matchers[i].Matcher == matcher && h.Matchers[i].IsRegex == isRegex {
			h.Matchers[i].PreHooks = append(h.Matchers[i].PreHooks, hook)
			return
		}
	}
	h.Matchers = append(h.Matchers, HookMatcher{
		Matcher:  matcher,
		IsRegex:  isRegex,
		Timeout:  timeout,
		PreHooks: []PreToolUseHook{hook},
	})
}

// AddPostHookWithOptions adds a post-hook with matcher options (regex, timeout).
func (h *Hooks) AddPostHookWithOptions(matcher string, isRegex bool, timeout time.Duration, hook PostToolUseHook) {
	for i := range h.Matchers {
		if h.Matchers[i].Matcher == matcher && h.Matchers[i].IsRegex == isRegex {
			h.Matchers[i].PostHooks = append(h.Matchers[i].PostHooks, hook)
			return
		}
	}
	h.Matchers = append(h.Matchers, HookMatcher{
		Matcher:   matcher,
		IsRegex:   isRegex,
		Timeout:   timeout,
		PostHooks: []PostToolUseHook{hook},
	})
}

// OnEvent registers a handler for a lifecycle event.
func (h *Hooks) OnEvent(event HookEvent, handler GenericHookHandler) {
	if h.EventHandlers == nil {
		h.EventHandlers = make(map[HookEvent][]GenericHookHandler)
	}
	h.EventHandlers[event] = append(h.EventHandlers[event], handler)
}

// EmitEvent fires all handlers registered for the given event.
func (h *Hooks) EmitEvent(ctx context.Context, data HookEventData) {
	if h == nil || h.EventHandlers == nil {
		return
	}
	handlers, ok := h.EventHandlers[data.Event]
	if !ok {
		return
	}
	for _, handler := range handlers {
		handler(ctx, data)
	}
}

// RunPreHooks executes all matching pre-tool-use hooks.
// Returns the final decision and potentially modified input.
func (h *Hooks) RunPreHooks(ctx context.Context, hookCtx HookContext) (HookResult, error) {
	result := AllowHook()
	currentInput := hookCtx.Input

	for i := range h.Matchers {
		matcher := &h.Matchers[i]
		if !matcher.Matches(hookCtx.ToolName) {
			continue
		}

		for _, hook := range matcher.PreHooks {
			hookCtx.Input = currentInput

			var hookResult HookResult
			if matcher.Timeout > 0 {
				timeoutCtx, cancel := context.WithTimeout(ctx, matcher.Timeout)
				done := make(chan HookResult, 1)
				go func() {
					done <- hook(timeoutCtx, hookCtx)
				}()
				select {
				case hookResult = <-done:
				case <-timeoutCtx.Done():
					cancel()
					return DenyHook("hook timed out"), nil
				}
				cancel()
			} else {
				hookResult = hook(ctx, hookCtx)
			}

			switch hookResult.Decision {
			case HookDeny:
				return hookResult, nil
			case HookModify:
				currentInput = hookResult.ModifiedInput
				result = hookResult
			case HookAllow:
				// Continue to next hook
			}
		}
	}

	// Return final result with potentially modified input
	if result.Decision == HookModify {
		result.ModifiedInput = currentInput
	}
	return result, nil
}

// RunPostHooks executes all matching post-tool-use hooks.
func (h *Hooks) RunPostHooks(ctx context.Context, hookCtx HookContext, toolResult string, isError bool) error {
	for i := range h.Matchers {
		matcher := &h.Matchers[i]
		if !matcher.Matches(hookCtx.ToolName) {
			continue
		}

		for _, hook := range matcher.PostHooks {
			if matcher.Timeout > 0 {
				timeoutCtx, cancel := context.WithTimeout(ctx, matcher.Timeout)
				done := make(chan struct{}, 1)
				go func() {
					hook(timeoutCtx, hookCtx, toolResult, isError)
					done <- struct{}{}
				}()
				select {
				case <-done:
				case <-timeoutCtx.Done():
					// Timeout - continue to next hook
				}
				cancel()
			} else {
				hook(ctx, hookCtx, toolResult, isError)
			}
		}
	}

	// Emit failure event if tool errored
	if isError {
		h.EmitEvent(ctx, HookEventData{
			Event:     HookPostToolUseFailure,
			ToolName:  hookCtx.ToolName,
			ToolUseID: hookCtx.ToolUseID,
			Error:     toolResult,
		})
	}

	return nil
}

// OnTool is a convenience method to add hooks for a specific tool.
func (h *Hooks) OnTool(toolName string) *ToolHookBuilder {
	return &ToolHookBuilder{hooks: h, toolName: toolName}
}

// OnAllTools is a convenience method to add hooks for all tools.
func (h *Hooks) OnAllTools() *ToolHookBuilder {
	return &ToolHookBuilder{hooks: h, toolName: "*"}
}

// OnToolRegex is a convenience method to add hooks matching a regex pattern.
func (h *Hooks) OnToolRegex(pattern string) *ToolHookBuilder {
	return &ToolHookBuilder{hooks: h, toolName: pattern, isRegex: true}
}

// ToolHookBuilder provides a fluent API for adding hooks.
type ToolHookBuilder struct {
	hooks    *Hooks
	toolName string
	isRegex  bool
	timeout  time.Duration
}

// WithTimeout sets the timeout for hooks added by this builder.
func (b *ToolHookBuilder) WithTimeout(d time.Duration) *ToolHookBuilder {
	b.timeout = d
	return b
}

// Before adds a pre-tool-use hook.
func (b *ToolHookBuilder) Before(hook PreToolUseHook) *ToolHookBuilder {
	if b.isRegex || b.timeout > 0 {
		b.hooks.AddPreHookWithOptions(b.toolName, b.isRegex, b.timeout, hook)
	} else {
		b.hooks.AddPreHook(b.toolName, hook)
	}
	return b
}

// After adds a post-tool-use hook.
func (b *ToolHookBuilder) After(hook PostToolUseHook) *ToolHookBuilder {
	if b.isRegex || b.timeout > 0 {
		b.hooks.AddPostHookWithOptions(b.toolName, b.isRegex, b.timeout, hook)
	} else {
		b.hooks.AddPostHook(b.toolName, hook)
	}
	return b
}

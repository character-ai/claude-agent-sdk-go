package claudeagent

import (
	"context"
	"encoding/json"
)

// HookEvent represents the type of hook event.
type HookEvent string

const (
	// HookPreToolUse is called before a tool is executed.
	HookPreToolUse HookEvent = "PreToolUse"
	// HookPostToolUse is called after a tool is executed.
	HookPostToolUse HookEvent = "PostToolUse"
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

// HookMatcher matches tools by name pattern.
type HookMatcher struct {
	// Matcher is the tool name to match (exact match or "*" for all).
	Matcher string
	// PreHooks are called before tool execution.
	PreHooks []PreToolUseHook
	// PostHooks are called after tool execution.
	PostHooks []PostToolUseHook
}

// Matches returns true if the matcher applies to the given tool name.
func (h *HookMatcher) Matches(toolName string) bool {
	if h.Matcher == "*" {
		return true
	}
	return h.Matcher == toolName
}

// Hooks configures hook handlers for tool execution.
type Hooks struct {
	// Matchers define which hooks apply to which tools.
	Matchers []HookMatcher
}

// NewHooks creates a new Hooks configuration.
func NewHooks() *Hooks {
	return &Hooks{
		Matchers: make([]HookMatcher, 0),
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

// RunPreHooks executes all matching pre-tool-use hooks.
// Returns the final decision and potentially modified input.
func (h *Hooks) RunPreHooks(ctx context.Context, hookCtx HookContext) (HookResult, error) {
	result := AllowHook()
	currentInput := hookCtx.Input

	for _, matcher := range h.Matchers {
		if !matcher.Matches(hookCtx.ToolName) {
			continue
		}

		for _, hook := range matcher.PreHooks {
			hookCtx.Input = currentInput
			hookResult := hook(ctx, hookCtx)

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
	for _, matcher := range h.Matchers {
		if !matcher.Matches(hookCtx.ToolName) {
			continue
		}

		for _, hook := range matcher.PostHooks {
			hook(ctx, hookCtx, toolResult, isError)
		}
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

// ToolHookBuilder provides a fluent API for adding hooks.
type ToolHookBuilder struct {
	hooks    *Hooks
	toolName string
}

// Before adds a pre-tool-use hook.
func (b *ToolHookBuilder) Before(hook PreToolUseHook) *ToolHookBuilder {
	b.hooks.AddPreHook(b.toolName, hook)
	return b
}

// After adds a post-tool-use hook.
func (b *ToolHookBuilder) After(hook PostToolUseHook) *ToolHookBuilder {
	b.hooks.AddPostHook(b.toolName, hook)
	return b
}

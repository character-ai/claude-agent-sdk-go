package claudeagent

import (
	"context"
	"fmt"
	"sync"
)

// executeOneTool runs a single tool call through the full permission → hooks → execution → hooks pipeline.
// It is safe to call concurrently from multiple goroutines (events channel is goroutine-safe,
// metrics uses internal locking).
func executeOneTool(
	ctx context.Context,
	tc ToolCall,
	tools *ToolRegistry,
	hooks *Hooks,
	canUseTool CanUseToolFunc,
	retry *RetryConfig,
	metrics *MetricsCollector,
	events chan<- AgentEvent,
) ToolResponse {
	response := ToolResponse{ToolUseID: tc.ID}
	currentInput := tc.Input

	// Permission check
	if canUseTool != nil {
		decision := canUseTool(ctx, tc.Name, tc.ID, tc.Input)
		if !decision.Allow {
			reason := decision.Reason
			if reason == "" {
				reason = "permission denied"
			}
			response.Content = fmt.Sprintf("Tool execution denied: %s", reason)
			response.IsError = true
			events <- AgentEvent{Type: AgentEventToolResult, ToolResponse: &response}
			return response
		}
		if decision.ModifiedInput != nil {
			currentInput = decision.ModifiedInput
		}
	}

	// Pre-tool-use hooks
	if hooks != nil {
		hookCtx := HookContext{ToolName: tc.Name, ToolUseID: tc.ID, Input: currentInput}
		hookResult, _ := hooks.RunPreHooks(ctx, hookCtx)

		switch hookResult.Decision { //nolint:exhaustive // HookAllow is default, no action needed
		case HookDeny:
			response.Content = fmt.Sprintf("Tool execution denied: %s", hookResult.Reason)
			response.IsError = true
			events <- AgentEvent{Type: AgentEventToolResult, ToolResponse: &response}
			return response
		case HookModify:
			currentInput = hookResult.ModifiedInput
		}
	}

	// Tool-specific permission check
	if def := tools.GetToolDef(tc.Name); def != nil {
		if def.CheckPermissions != nil {
			if err := def.CheckPermissions(ctx, currentInput); err != nil {
				response.Content = fmt.Sprintf("Permission denied: %s", err.Error())
				response.IsError = true
				events <- AgentEvent{Type: AgentEventToolResult, ToolResponse: &response}
				return response
			}
		}
		if def.ValidateInput != nil {
			if err := def.ValidateInput(ctx, currentInput); err != nil {
				response.Content = fmt.Sprintf("Validation error: %s", err.Error())
				response.IsError = true
				events <- AgentEvent{Type: AgentEventToolResult, ToolResponse: &response}
				return response
			}
		}
	}

	// Execute
	if metrics != nil {
		metrics.recordToolStart(tc.ID)
	}

	if tools == nil || !tools.Has(tc.Name) {
		response.Content = fmt.Sprintf("Tool not found: %s", tc.Name)
		response.IsError = true
	} else {
		// Per-tool RetryConfig takes precedence over global
		rc := retry
		if perTool := tools.ToolRetryConfig(tc.Name); perTool != nil {
			rc = perTool
		}
		var meta *ToolResultMetadata
		result, err := executeWithRetry(ctx, rc, func() (string, error) {
			r, m, e := tools.ExecuteStructured(ctx, tc.Name, currentInput)
			meta = m
			return r, e
		})
		if err != nil {
			response.Content = err.Error()
			response.IsError = true
		} else {
			response.Content = result
			response.Metadata = meta
		}
	}

	if metrics != nil {
		metrics.recordToolEnd(tc.Name, tc.ID, response.IsError)
	}

	// Post-tool-use hooks
	if hooks != nil {
		hookCtx := HookContext{ToolName: tc.Name, ToolUseID: tc.ID, Input: currentInput}
		_ = hooks.RunPostHooks(ctx, hookCtx, response.Content, response.IsError)
	}

	events <- AgentEvent{Type: AgentEventToolResult, ToolResponse: &response}
	return response
}

// runToolsParallel executes all tool calls concurrently, preserving result order.
func runToolsParallel(
	ctx context.Context,
	toolCalls []ToolCall,
	tools *ToolRegistry,
	hooks *Hooks,
	canUseTool CanUseToolFunc,
	retry *RetryConfig,
	metrics *MetricsCollector,
	events chan<- AgentEvent,
) []ToolResponse {
	results := make([]ToolResponse, len(toolCalls))
	var wg sync.WaitGroup
	for i, tc := range toolCalls {
		wg.Add(1)
		go func(i int, tc ToolCall) {
			defer wg.Done()
			results[i] = executeOneTool(ctx, tc, tools, hooks, canUseTool, retry, metrics, events)
		}(i, tc)
	}
	wg.Wait()
	return results
}

// isSafeForConcurrency checks whether a tool call can run concurrently.
// A tool is safe if it has ConcurrencySafe=true in its annotations, or if
// it has no annotations and defaultParallel is true.
func isSafeForConcurrency(name string, tools *ToolRegistry, defaultParallel bool) bool {
	if tools == nil {
		return defaultParallel
	}
	ann := tools.ToolAnnotations(name)
	if ann == nil {
		return defaultParallel
	}
	return ann.ConcurrencySafe
}

// runToolsSmart partitions tool calls by concurrency safety and executes them accordingly.
// Tools with ConcurrencySafe=true run in parallel with each other.
// All other tools run sequentially.
// When defaultParallel is true, unannotated tools are treated as concurrency-safe.
// Results are returned in the original tool call order.
func runToolsSmart(
	ctx context.Context,
	toolCalls []ToolCall,
	tools *ToolRegistry,
	hooks *Hooks,
	canUseTool CanUseToolFunc,
	retry *RetryConfig,
	metrics *MetricsCollector,
	events chan<- AgentEvent,
	defaultParallel bool,
) []ToolResponse {
	// Fast path: single tool call needs no partitioning.
	if len(toolCalls) <= 1 {
		results := make([]ToolResponse, len(toolCalls))
		for i, tc := range toolCalls {
			results[i] = executeOneTool(ctx, tc, tools, hooks, canUseTool, retry, metrics, events)
		}
		return results
	}

	results := make([]ToolResponse, len(toolCalls))

	// Walk tool calls and group consecutive safe tools for parallel execution.
	i := 0
	for i < len(toolCalls) {
		if isSafeForConcurrency(toolCalls[i].Name, tools, defaultParallel) {
			// Collect consecutive safe tools.
			j := i
			for j < len(toolCalls) && isSafeForConcurrency(toolCalls[j].Name, tools, defaultParallel) {
				j++
			}
			// Run this batch in parallel.
			batch := toolCalls[i:j]
			batchResults := runToolsParallel(ctx, batch, tools, hooks, canUseTool, retry, metrics, events)
			copy(results[i:j], batchResults)
			i = j
		} else {
			// Unsafe tool: run sequentially.
			results[i] = executeOneTool(ctx, toolCalls[i], tools, hooks, canUseTool, retry, metrics, events)
			i++
		}
	}

	return results
}

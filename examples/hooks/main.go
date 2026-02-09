package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	claudeagent "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	// Create hooks with various patterns
	hooks := claudeagent.NewHooks()

	// Hook: Log all tool calls
	hooks.OnAllTools().Before(func(ctx context.Context, hookCtx claudeagent.HookContext) claudeagent.HookResult {
		fmt.Printf("[Hook] Tool called: %s (id=%s)\n", hookCtx.ToolName, hookCtx.ToolUseID)
		return claudeagent.AllowHook()
	})

	// Hook: Block dangerous tools
	hooks.OnTool("Bash").Before(func(ctx context.Context, hookCtx claudeagent.HookContext) claudeagent.HookResult {
		fmt.Printf("[Hook] Bash tool requested - checking safety...\n")
		// In a real app, inspect hookCtx.Input to decide
		return claudeagent.AllowHook()
	})

	// Hook: Regex match all MCP tools
	hooks.OnToolRegex(`^mcp__.*`).Before(func(ctx context.Context, hookCtx claudeagent.HookContext) claudeagent.HookResult {
		fmt.Printf("[Hook] MCP tool called: %s\n", hookCtx.ToolName)
		return claudeagent.AllowHook()
	})

	// Hook: Timeout protection for slow tools
	hooks.OnTool("slow_tool").WithTimeout(5 * time.Second).Before(func(ctx context.Context, hookCtx claudeagent.HookContext) claudeagent.HookResult {
		fmt.Println("[Hook] Checking slow tool with timeout protection")
		return claudeagent.AllowHook()
	})

	// Hook: Post-execution logging
	hooks.OnAllTools().After(func(ctx context.Context, hookCtx claudeagent.HookContext, result string, isError bool) claudeagent.HookResult {
		status := "success"
		if isError {
			status = "error"
		}
		fmt.Printf("[Hook] Tool %s completed: %s\n", hookCtx.ToolName, status)
		return claudeagent.AllowHook()
	})

	// Lifecycle event handlers
	hooks.OnEvent(claudeagent.HookSessionStart, func(ctx context.Context, data claudeagent.HookEventData) {
		fmt.Println("[Lifecycle] Session started")
	})

	hooks.OnEvent(claudeagent.HookSessionEnd, func(ctx context.Context, data claudeagent.HookEventData) {
		fmt.Println("[Lifecycle] Session ended")
	})

	hooks.OnEvent(claudeagent.HookPostToolUseFailure, func(ctx context.Context, data claudeagent.HookEventData) {
		fmt.Printf("[Lifecycle] Tool %s failed: %s\n", data.ToolName, data.Error)
	})

	hooks.OnEvent(claudeagent.HookStop, func(ctx context.Context, data claudeagent.HookEventData) {
		fmt.Printf("[Lifecycle] Agent stopped: %s\n", data.Message)
	})

	// Create agent with hooks and canUseTool callback
	agent := claudeagent.NewAgent(claudeagent.AgentConfig{
		Options: claudeagent.Options{
			Model:          "claude-sonnet-4-20250514",
			PermissionMode: claudeagent.PermissionAcceptEdits,
			MaxTurns:       5,
		},
		Hooks:    hooks,
		MaxTurns: 5,
		CanUseTool: func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) claudeagent.PermissionDecision {
			fmt.Printf("[Permission] Tool %s requesting permission\n", toolName)
			// Allow all tools in this example
			return claudeagent.PermissionDecision{Allow: true}
		},
	})

	events, err := agent.Run(context.Background(), "List files in the current directory")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for event := range events {
		switch event.Type {
		case claudeagent.AgentEventContentDelta:
			fmt.Print(event.Content)
		case claudeagent.AgentEventComplete:
			fmt.Println("\n--- Complete ---")
		case claudeagent.AgentEventError:
			fmt.Fprintf(os.Stderr, "Error: %v\n", event.Error)
		}
	}
}

// Sandbox example showing how to give an API agent code execution capabilities.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// Create sandbox with the process backend (for development).
	// In production, use claude.NewDockerBackend() for full isolation.
	sandbox := claude.NewSandboxRegistry(claude.SandboxConfig{
		Backend:          claude.NewProcessBackend(claude.ProcessBackendConfig{}),
		AllowedLanguages: []claude.Language{claude.LangPython, claude.LangBash},
		EnableSessions:   true,
		EnableCommands:   true,
		EnableFileIO:     true,
		Limits: claude.ResourceLimits{
			WallClockSec: 30,
			MemoryMB:     256,
		},
	})
	defer sandbox.Close()

	// Create API agent with sandbox tools.
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Model:        "claude-sonnet-4-20250514",
		SystemPrompt: sandbox.SystemPrompt(),
		Tools:        sandbox.Tools(),
		MaxTurns:     10,
	})

	prompt := "Write a Python script that generates the first 20 Fibonacci numbers and prints them. Then verify the result by running a bash command to count the output lines."
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	fmt.Println("Prompt:", prompt)
	fmt.Println()

	events, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}

	// Stream events.
	for event := range events {
		switch event.Type { //nolint:exhaustive // Only handling events we care about
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)
		case claude.AgentEventToolUseStart:
			fmt.Printf("\n[calling %s]\n", event.ToolCall.Name)
		case claude.AgentEventToolResult:
			fmt.Printf("[result: %s]\n", event.ToolResponse.Content)
		case claude.AgentEventError:
			fmt.Printf("Error: %v\n", event.Error)
		}
	}

	// Show execution history.
	fmt.Printf("\n\n=== %d Execution(s) ===\n", sandbox.ExecutionCount())
	for _, exec := range sandbox.Executions() {
		fmt.Printf("  %s: %s (%s) exit=%d duration=%s\n",
			exec.ID, exec.Language, exec.SessionID,
			exec.Result.ExitCode, exec.Result.Duration.Round(1e6))
	}
}

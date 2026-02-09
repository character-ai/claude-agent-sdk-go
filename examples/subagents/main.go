package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	claudeagent "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	// Create a tool registry for the researcher subagent
	researchTools := claudeagent.NewToolRegistry()
	claudeagent.RegisterFunc(researchTools, claudeagent.ToolDefinition{
		Name:        "search",
		Description: "Search for information on a topic",
		InputSchema: claudeagent.ObjectSchema(map[string]any{
			"query": claudeagent.StringParam("The search query"),
		}, "query"),
	}, func(ctx context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return fmt.Sprintf("Search results for '%s': [simulated results]", input.Query), nil
	})

	// Define subagents
	subagents := claudeagent.NewSubagentConfig()
	subagents.Add(&claudeagent.AgentDefinition{
		Name:        "researcher",
		Description: "Researches topics and returns summarized findings",
		Prompt:      "You are a research assistant. Use the search tool to find information and summarize it concisely.",
		Tools:       researchTools,
		Model:       "haiku",
		MaxTurns:    5,
	})
	subagents.Add(&claudeagent.AgentDefinition{
		Name:        "coder",
		Description: "Writes code based on specifications",
		Prompt:      "You are a coding assistant. Write clean, well-documented code.",
		Model:       "sonnet",
		MaxTurns:    3,
	})

	// Create the main agent with subagents
	agent := claudeagent.NewAgent(claudeagent.AgentConfig{
		Options: claudeagent.Options{
			Model:          "claude-sonnet-4-20250514",
			PermissionMode: claudeagent.PermissionAcceptEdits,
			MaxTurns:       10,
		},
		MaxTurns:  10,
		Subagents: subagents,
	})

	events, err := agent.Run(context.Background(), "Research the latest Go generics patterns and then write a simple example")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for event := range events {
		switch event.Type {
		case claudeagent.AgentEventContentDelta:
			fmt.Print(event.Content)
		case claudeagent.AgentEventToolUseStart:
			if event.ToolCall != nil {
				fmt.Printf("\n[Tool: %s]\n", event.ToolCall.Name)
				if event.ToolCall.Name == "Task" {
					var input struct {
						Description  string `json:"description"`
						SubagentName string `json:"subagent_name"`
					}
					if err := json.Unmarshal(event.ToolCall.Input, &input); err == nil {
						fmt.Printf("  Subagent: %s\n", input.SubagentName)
						fmt.Printf("  Task: %s\n", input.Description)
					}
				}
			}
		case claudeagent.AgentEventToolResult:
			if event.ToolResponse != nil {
				fmt.Printf("[Result: %s...]\n", truncate(event.ToolResponse.Content, 100))
			}
		case claudeagent.AgentEventComplete:
			fmt.Println("\n--- Complete ---")
		case claudeagent.AgentEventError:
			fmt.Fprintf(os.Stderr, "Error: %v\n", event.Error)
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Todos example showing how to enable the write_todos tool so the agent
// can plan and track its own work. The host app observes progress via
// AgentEventTodosUpdated events.
package main

import (
	"context"
	"fmt"
	"log"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// Create an API agent with todos enabled.
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		SystemPrompt: "You are a helpful assistant. Use the write_todos tool to plan your work before starting, then update each item's status as you progress.",
		MaxTurns:     10,
		EnableTodos:  true,
	})

	events, err := agent.Run(ctx, "Plan a 3-step approach to building a REST API in Go, then mark each step as completed as you explain it.")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		switch event.Type { //nolint:exhaustive // Only handling events we care about
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)

		case claude.AgentEventTodosUpdated:
			fmt.Println("\n--- Todo List Updated ---")
			for _, todo := range event.Todos {
				status := map[claude.TodoStatus]string{
					claude.TodoStatusPending:    "[ ]",
					claude.TodoStatusInProgress: "[~]",
					claude.TodoStatusCompleted:  "[x]",
				}[todo.Status]
				fmt.Printf("  %s %s (%s) %s\n", status, todo.ID, todo.Priority, todo.Description)
			}
			fmt.Println("-------------------------")

		case claude.AgentEventError:
			log.Printf("Error: %v", event.Error)

		case claude.AgentEventComplete:
			fmt.Println("\n=== Complete ===")
		}
	}

	// You can also read the final todo state directly from the store.
	if store := agent.TodoStore(); store != nil {
		final := store.List()
		fmt.Printf("\nFinal todo count: %d\n", len(final))
		for _, t := range final {
			fmt.Printf("  %s: %s [%s]\n", t.ID, t.Description, t.Status)
		}
	}
}

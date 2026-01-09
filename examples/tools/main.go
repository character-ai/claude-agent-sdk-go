// Tools example showing how to handle tool calls and results.
package main

import (
	"context"
	"fmt"
	"log"

	claude "github.com/character-tech/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// Enable specific tools with auto-accept for edits
	opts := claude.Options{
		AllowedTools:   []string{"Read", "Write", "Bash", "Glob"},
		PermissionMode: claude.PermissionAcceptEdits,
		MaxTurns:       5,
	}

	client := claude.NewClient(opts)

	events, err := client.Query(ctx, "Create a file called hello.txt with 'Hello from Claude!' in it, then read it back to confirm.")
	if err != nil {
		log.Fatal(err)
	}

	var currentTool string
	for event := range events {
		if event.Error != nil {
			log.Printf("Error: %v", event.Error)
			continue
		}

		switch {
		case event.ToolUse != nil:
			currentTool = event.ToolUse.Name
			fmt.Printf("\n> Using tool: %s (id: %s)\n", currentTool, event.ToolUse.ID)

		case event.ToolResult != nil:
			fmt.Printf("< Tool result (%d chars)\n", len(event.ToolResult.Content))
			if event.ToolResult.IsError {
				fmt.Printf("  Error: %s\n", event.ToolResult.Content)
			}

		case event.Text != "":
			fmt.Print(event.Text)

		case event.Result != nil:
			fmt.Printf("\n\n=== Complete ===\n")
			fmt.Printf("Session: %s\n", event.Result.SessionID)
			fmt.Printf("Cost: $%.4f\n", event.Result.Cost)
			fmt.Printf("Tokens: %d in / %d out\n", event.Result.InputTokens, event.Result.OutputTokens)
		}
	}
}

// Streaming example showing real-time event handling.
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

	opts := claude.Options{
		AllowedTools:   []string{"Read", "Bash"},
		PermissionMode: claude.PermissionAcceptEdits,
	}

	prompt := "What files are in the current directory? List them briefly."
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	events, err := claude.Query(ctx, prompt, opts)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Streaming response:")
	fmt.Println("---")

	for event := range events {
		if event.Error != nil {
			log.Printf("Error: %v", event.Error)
			continue
		}

		switch event.Type {
		case claude.EventContentBlockDelta:
			// Print text as it streams in
			fmt.Print(event.Text)

		case claude.EventContentBlockStart:
			if event.ToolUse != nil {
				fmt.Printf("\n[Tool: %s]\n", event.ToolUse.Name)
			}

		case claude.EventToolResult:
			if event.ToolResult != nil {
				fmt.Printf("[Result: %s...]\n", truncate(event.ToolResult.Content, 100))
			}

		case claude.EventResult:
			if event.Result != nil {
				fmt.Printf("\n---\nCost: $%.4f | Tokens: %d/%d | Turns: %d\n",
					event.Result.Cost,
					event.Result.InputTokens,
					event.Result.OutputTokens,
					event.Result.NumTurns)
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

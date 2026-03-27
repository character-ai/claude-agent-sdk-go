// Artifacts example showing how to generate HTML/JSX/text content with an API agent.
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

	// Create artifact registry — holds generated artifacts and provides tools
	artifacts := claude.NewArtifactRegistry()

	// Create API agent with artifact tools
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Model:        "claude-sonnet-4-20250514",
		SystemPrompt: artifacts.SystemPrompt(),
		Tools:        artifacts.Tools(),
		MaxTurns:     5,
	})

	prompt := "Create an interactive HTML page with a bouncing ball animation using canvas. Make it colorful."
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	fmt.Println("Prompt:", prompt)
	fmt.Println()

	events, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}

	// Stream events
	for event := range events {
		switch event.Type {
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)
		case claude.AgentEventToolUseStart:
			fmt.Printf("\n[calling %s]\n", event.ToolCall.Name)
		case claude.AgentEventToolResult:
			fmt.Printf("[%s]\n", event.ToolResponse.Content)
		case claude.AgentEventError:
			fmt.Printf("Error: %v\n", event.Error)
		}
	}

	// Write artifacts to files
	fmt.Printf("\n\n=== %d Artifact(s) Generated ===\n", artifacts.Count())
	for _, a := range artifacts.All() {
		ext := ".txt"
		switch a.Type {
		case claude.ArtifactHTML:
			ext = ".html"
		case claude.ArtifactJSX:
			ext = ".jsx"
		}
		filename := a.ID + ext
		if err := os.WriteFile(filename, []byte(a.Content), 0644); err != nil {
			fmt.Printf("Failed to write %s: %v\n", filename, err)
			continue
		}
		fmt.Printf("Wrote %s (%s, %d bytes) -> %s\n", a.Title, a.Type, len(a.Content), filename)
	}
}

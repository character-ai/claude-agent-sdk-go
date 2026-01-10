// Generation example showing agentic flows for image/video generation.
// This demonstrates the pattern used by labs-service for Jeeves/Recreate.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

// GenerateImageInput is the input schema for image generation.
type GenerateImageInput struct {
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Style       string `json:"style,omitempty"`
}

// GenerateVideoInput is the input schema for video generation.
type GenerateVideoInput struct {
	Prompt   string `json:"prompt"`
	ImageURL string `json:"image_url,omitempty"` // Optional base image
	Duration int    `json:"duration,omitempty"`  // Seconds
}

func main() {
	ctx := context.Background()

	// Create tool registry with generation tools
	tools := claude.NewToolRegistry()

	// Register image generation tool
	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "generate_image",
		Description: "Generate an image from a text prompt. Use this when you need to create visual content.",
		InputSchema: claude.ObjectSchema(map[string]any{
			"prompt":       claude.StringParam("Detailed description of the image to generate"),
			"aspect_ratio": claude.EnumParam("Aspect ratio", "1:1", "16:9", "9:16", "4:3"),
			"style":        claude.EnumParam("Visual style", "photorealistic", "anime", "illustration", "3d_render"),
		}, "prompt"),
	}, generateImage)

	// Register video generation tool
	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "generate_video",
		Description: "Generate a video from a text prompt, optionally using a base image URL. Use for animated content.",
		InputSchema: claude.ObjectSchema(map[string]any{
			"prompt":    claude.StringParam("Description of the video to generate"),
			"image_url": claude.StringParam("Optional URL of base image to animate"),
			"duration":  claude.IntParam("Video duration in seconds (5-30)"),
		}, "prompt"),
	}, generateVideo)

	// Create API-based agent with tools (like labs-service pattern)
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Model: "claude-sonnet-4-20250514",
		SystemPrompt: `You are a creative AI assistant that generates images and videos.
When asked to create visual content, use the appropriate generation tools.
For images, be descriptive and include style preferences.
For videos, consider whether a base image would help achieve better results.
Always explain what you're generating before calling the tool.`,
		Tools:    tools,
		MaxTurns: 5,
	})

	// Example prompt - Claude will decide which tools to use
	prompt := `Create an anime-style image of a samurai cat standing on a rooftop at sunset,
then create a short 5 second video of the scene with cherry blossoms falling.`

	fmt.Println("Starting agent with prompt:")
	fmt.Println(prompt)
	fmt.Println("\n--- Agent Execution ---")

	events, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}

	// Process events in real-time (like SSE to frontend)
	for event := range events {
		switch event.Type { //nolint:exhaustive // Only handling events we care about
		case claude.AgentEventMessageStart:
			fmt.Println("\n[Claude]")

		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)

		case claude.AgentEventToolUseStart:
			fmt.Printf("\n\n> Calling tool: %s\n", event.ToolCall.Name)

		case claude.AgentEventToolUseDelta:
			// Streaming tool input JSON (optional to display)

		case claude.AgentEventToolUseEnd:
			if len(event.ToolCall.Input) > 0 {
				var input map[string]any
				_ = json.Unmarshal(event.ToolCall.Input, &input)
				inputJSON, _ := json.MarshalIndent(input, "  ", "  ")
				fmt.Printf("  Input: %s\n", inputJSON)
			}

		case claude.AgentEventToolResult:
			if event.ToolResponse.IsError {
				fmt.Printf("< Error: %s\n", event.ToolResponse.Content)
			} else {
				fmt.Printf("< Result: %s\n", truncate(event.ToolResponse.Content, 200))
			}

		case claude.AgentEventTurnComplete:
			fmt.Println("\n[Continuing...]")

		case claude.AgentEventComplete:
			fmt.Println("\n\n--- Agent Complete ---")

		case claude.AgentEventError:
			fmt.Printf("\nError: %v\n", event.Error)
		}
	}
}

// Mock generation functions - replace with real API calls

func generateImage(ctx context.Context, input GenerateImageInput) (string, error) {
	// Simulate async generation with polling (like labs-service pattern)
	fmt.Printf("  [Generating image...]\n")
	time.Sleep(500 * time.Millisecond) // Simulate API call

	// Return mock result
	return fmt.Sprintf(`{"status":"complete","image_url":"https://cdn.example.com/img_%d.png","prompt":%q,"style":%q}`,
		time.Now().Unix(), input.Prompt, input.Style), nil
}

func generateVideo(ctx context.Context, input GenerateVideoInput) (string, error) {
	fmt.Printf("  [Generating video...]\n")
	time.Sleep(1 * time.Second) // Simulate longer video gen

	return fmt.Sprintf(`{"status":"complete","video_url":"https://cdn.example.com/vid_%d.mp4","prompt":%q,"duration":%d}`,
		time.Now().Unix(), input.Prompt, input.Duration), nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

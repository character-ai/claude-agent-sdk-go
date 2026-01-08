// Server example showing HTTP SSE endpoint for agentic generation.
// This mirrors the pattern used by labs-service for Jeeves.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	claude "github.com/character-tech/claude-agent-sdk-go"
)

func main() {
	// Create tool registry
	tools := createGenerationTools()

	// Create agent factory (new agent per request for isolation)
	createAgent := func() *claude.Agent {
		return claude.NewAgent(claude.AgentConfig{
			Options: claude.Options{
				PermissionMode: claude.PermissionAcceptEdits,
				SystemPrompt:   "You are a creative AI that generates images and videos on request.",
			},
			Tools:    tools,
			MaxTurns: 10,
		})
	}

	// SSE endpoint for agent interactions
	http.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		// Parse request
		var req struct {
			Prompt string `json:"prompt"`
		}

		if r.Method == "POST" {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
		} else {
			req.Prompt = r.URL.Query().Get("prompt")
		}

		if req.Prompt == "" {
			http.Error(w, "prompt required", http.StatusBadRequest)
			return
		}

		// Setup SSE
		sse, err := claude.NewSSEWriter(w)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Run agent
		agent := createAgent()
		events, err := agent.Run(r.Context(), req.Prompt)
		if err != nil {
			sse.WriteEvent("error", map[string]string{"error": err.Error()})
			return
		}

		// Stream events to client
		for event := range events {
			if err := sse.WriteAgentEvent(event); err != nil {
				log.Printf("SSE write error: %v", err)
				return
			}
		}

		sse.Close()
	})

	// Health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	addr := ":8080"
	log.Printf("Starting server on %s", addr)
	log.Printf("Try: curl -N 'http://localhost%s/api/generate?prompt=Generate%%20a%%20sunset%%20image'", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

// Tool definitions (same as generation example)

type GenerateImageInput struct {
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Style       string `json:"style,omitempty"`
}

type GenerateVideoInput struct {
	Prompt   string `json:"prompt"`
	ImageURL string `json:"image_url,omitempty"`
	Duration int    `json:"duration,omitempty"`
}

func createGenerationTools() *claude.ToolRegistry {
	tools := claude.NewToolRegistry()

	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "generate_image",
		Description: "Generate an image from a text prompt",
		InputSchema: claude.ObjectSchema(map[string]any{
			"prompt":       claude.StringParam("Image description"),
			"aspect_ratio": claude.EnumParam("Aspect ratio", "1:1", "16:9", "9:16"),
			"style":        claude.EnumParam("Style", "photorealistic", "anime", "illustration"),
		}, "prompt"),
	}, func(ctx context.Context, input GenerateImageInput) (string, error) {
		// Replace with real image generation API (NanoBanana, etc.)
		time.Sleep(500 * time.Millisecond)
		return fmt.Sprintf(`{"status":"complete","url":"https://cdn.example.com/img_%d.png","prompt":%q}`,
			time.Now().Unix(), input.Prompt), nil
	})

	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "generate_video",
		Description: "Generate a video from a text prompt",
		InputSchema: claude.ObjectSchema(map[string]any{
			"prompt":    claude.StringParam("Video description"),
			"image_url": claude.StringParam("Optional base image URL"),
			"duration":  claude.IntParam("Duration in seconds"),
		}, "prompt"),
	}, func(ctx context.Context, input GenerateVideoInput) (string, error) {
		// Replace with real video generation API (Veo, WaveSpeed, etc.)
		time.Sleep(1 * time.Second)
		return fmt.Sprintf(`{"status":"complete","url":"https://cdn.example.com/vid_%d.mp4","prompt":%q}`,
			time.Now().Unix(), input.Prompt), nil
	})

	return tools
}

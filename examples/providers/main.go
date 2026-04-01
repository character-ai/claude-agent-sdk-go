// Providers example showing how to run the same agent with Anthropic or OpenAI.
//
// Usage:
//
//	ANTHROPIC_API_KEY=sk-ant-... go run examples/providers/main.go anthropic
//	OPENAI_API_KEY=sk-...       go run examples/providers/main.go openai
//
// The agent uses a simple weather tool to demonstrate tool calling across providers.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run examples/providers/main.go <anthropic|openai>")
		os.Exit(1)
	}

	provider, err := buildProvider(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	// Register a simple weather tool so we can observe tool calling.
	tools := claude.NewToolRegistry()
	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "get_weather",
		Description: "Get the current weather for a city.",
		InputSchema: claude.ObjectSchema(map[string]any{
			"city": claude.StringParam("City name"),
		}, "city"),
		Annotations: &claude.ToolAnnotations{
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
	}, func(_ context.Context, input struct {
		City string `json:"city"`
	}) (string, error) {
		// Stub — returns fake data.
		return fmt.Sprintf(`{"city":%q,"temp_c":22,"condition":"sunny"}`, input.City), nil
	})

	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Provider: provider,
		Tools:    tools,
		MaxTurns: 5,
		SystemPrompt: "You are a helpful assistant. When asked about weather, " +
			"use the get_weather tool and present the result clearly.",
	})

	prompt := "What's the weather like in Tokyo and Paris?"
	if len(os.Args) > 2 {
		prompt = os.Args[2]
	}

	fmt.Printf("Provider : %s\n", provider.Name())
	fmt.Printf("Prompt   : %s\n\n", prompt)

	events, err := agent.Run(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		switch event.Type { //nolint:exhaustive
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)

		case claude.AgentEventToolUseStart:
			fmt.Printf("\n[tool: %s]\n", event.ToolCall.Name)

		case claude.AgentEventToolResult:
			var pretty any
			if json.Unmarshal([]byte(event.ToolResponse.Content), &pretty) == nil {
				b, _ := json.MarshalIndent(pretty, "  ", "  ")
				fmt.Printf("  %s\n", b)
			} else {
				fmt.Printf("  %s\n", event.ToolResponse.Content)
			}

		case claude.AgentEventComplete:
			r := event.Result
			fmt.Printf("\n\n--- done ---\n")
			fmt.Printf("turns: %d  stop: %s\n", r.NumTurns, r.StopReason)
			if r.InputTokens > 0 {
				fmt.Printf("tokens: %d in / %d out\n", r.InputTokens, r.OutputTokens)
			}

		case claude.AgentEventError:
			fmt.Printf("\nerror: %v\n", event.Error)
		}
	}
}

func buildProvider(name string) (claude.LLMProvider, error) {
	switch name {
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is not set")
		}
		return claude.NewAnthropicProvider(claude.AnthropicProviderConfig{APIKey: key}), nil

	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY is not set")
		}
		return claude.OpenAIProvider("gpt-4o"), nil

	default:
		return nil, fmt.Errorf("unknown provider %q — choose anthropic or openai", name)
	}
}

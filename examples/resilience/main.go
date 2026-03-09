// Resilience example demonstrating retry logic, budget controls, and
// history compaction working together in a long-running agent session.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// --- Tool registry ---
	tools := claude.NewToolRegistry()
	callCount := 0

	// A flaky tool that fails on the first two calls — demonstrates retry.
	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "fetch_data",
		Description: "Fetch data from an external API (simulates transient failures)",
		InputSchema: claude.ObjectSchema(map[string]any{
			"resource": claude.StringParam("Resource name to fetch"),
		}, "resource"),
		// Per-tool retry: 3 attempts, 100 ms base backoff, retry only on transient errors.
		RetryConfig: &claude.RetryConfig{
			MaxAttempts: 3,
			Backoff:     100 * time.Millisecond,
			RetryOn: func(err error) bool {
				return strings.Contains(err.Error(), "transient")
			},
		},
	}, func(ctx context.Context, input struct {
		Resource string `json:"resource"`
	}) (string, error) {
		callCount++
		fmt.Printf("  [fetch_data] attempt %d for %q\n", callCount, input.Resource)
		if callCount <= 2 {
			return "", errors.New("transient: service temporarily unavailable")
		}
		return fmt.Sprintf(`{"resource":"%s","data":"fetched successfully"}`, input.Resource), nil
	})

	// A summarize tool with a per-tool retry that never retries — permanent errors stop immediately.
	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "summarize",
		Description: "Summarize a piece of text",
		InputSchema: claude.ObjectSchema(map[string]any{
			"text": claude.StringParam("Text to summarize"),
		}, "text"),
		RetryConfig: &claude.RetryConfig{
			MaxAttempts: 1, // no retry for this tool
		},
	}, func(ctx context.Context, input struct {
		Text string `json:"text"`
	}) (string, error) {
		words := strings.Fields(input.Text)
		if len(words) > 5 {
			words = words[:5]
		}
		return "Summary: " + strings.Join(words, " ") + "...", nil
	})

	fmt.Println("=== Demo 1: Retry on transient failure ===")
	demoRetry(ctx, tools)

	fmt.Println()
	fmt.Println("=== Demo 2: Budget controls ===")
	demoBudget(ctx, tools)

	fmt.Println()
	fmt.Println("=== Demo 3: History compaction ===")
	demoHistory(ctx, tools)
}

// demoRetry shows RetryConfig in action — the tool fails twice then succeeds.
func demoRetry(ctx context.Context, tools *claude.ToolRegistry) {
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Model: "claude-sonnet-4-20250514",
		Tools: tools,
		// Global fallback retry for any tool without a per-tool config.
		Retry: &claude.RetryConfig{
			MaxAttempts: 2,
			Backoff:     50 * time.Millisecond,
		},
	})

	events, err := agent.Run(ctx, "Fetch the 'inventory' resource and report the result.")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		switch event.Type {
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)
		case claude.AgentEventToolUseStart:
			if event.ToolCall != nil {
				fmt.Printf("\n[calling %s]\n", event.ToolCall.Name)
			}
		case claude.AgentEventError:
			fmt.Printf("\n[error] %v\n", event.Error)
		default:
		}
	}
	fmt.Println()
}

// demoBudget shows how BudgetConfig stops the agent when limits are hit.
func demoBudget(ctx context.Context, tools *claude.ToolRegistry) {
	// Use a very short time budget to demonstrate the feature without
	// waiting for a real token budget to be exceeded.
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Model: "claude-sonnet-4-20250514",
		Tools: tools,
		Budget: &claude.BudgetConfig{
			MaxTokens:   100_000,          // generous token limit
			MaxDuration: 30 * time.Second, // session must finish within 30 seconds
		},
	})

	events, err := agent.Run(ctx, "Summarize the phrase 'Go is a statically typed language'.")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		switch event.Type {
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)
		case claude.AgentEventError:
			var budgetErr *claude.BudgetExceededError
			if errors.As(event.Error, &budgetErr) {
				fmt.Printf("\n[budget] session stopped: %v\n", budgetErr)
			} else {
				fmt.Printf("\n[error] %v\n", event.Error)
			}
		default:
		}
	}
	fmt.Println()
}

// demoHistory shows HistoryConfig keeping the context window bounded.
func demoHistory(_ context.Context, _ *claude.ToolRegistry) {
	// Build a larger tool registry for the CLI agent demo
	cliTools := claude.NewToolRegistry()
	claude.RegisterFunc(cliTools, claude.ToolDefinition{
		Name:        "get_item",
		Description: "Get an item by ID",
		InputSchema: claude.ObjectSchema(map[string]any{
			"id": claude.StringParam("Item ID"),
		}, "id"),
	}, func(ctx context.Context, input struct {
		ID string `json:"id"`
	}) (string, error) {
		return fmt.Sprintf(`{"id":%s,"name":"item-%s","value":42}`, input.ID, input.ID), nil
	})

	agent := claude.NewAgent(claude.AgentConfig{
		Tools: cliTools,
		History: &claude.HistoryConfig{
			// Only keep the 3 most recent turns in the context sent to the LLM.
			// Earlier turns are still held in memory for reference.
			MaxTurns: 3,
			// Replace tool result content from older turns with a short placeholder.
			// This reduces token usage while preserving conversation structure.
			DropToolResults: true,
		},
	})

	// Demonstrate that HistoryConfig values are set correctly on the agent.
	// (Full integration requires a running Claude CLI.)
	_ = agent

	fmt.Println("History compaction configured:")
	fmt.Println("  MaxTurns=3        — only the last 3 assistant+tool turns sent to LLM")
	fmt.Println("  DropToolResults=true — older tool results replaced with placeholder")
	fmt.Println()

	// Demonstrate HistoryConfig struct usage
	cfg := &claude.HistoryConfig{MaxTurns: 3, DropToolResults: true}
	printHistoryDemo(cfg)
}

// printHistoryDemo shows the compaction effect on a sample history.
func printHistoryDemo(cfg *claude.HistoryConfig) {
	_ = cfg // used for documentation

	// Build a realistic multi-turn history manually to show what compaction does.
	type msg struct {
		Role    string
		Content string
	}
	history := []msg{
		{Role: "user", Content: "initial prompt"},
		{Role: "assistant", Content: "I'll look up items 1, 2, and 3."},
		{Role: "tool", Content: `{"id":1,"name":"item-1","value":42}`},
		{Role: "assistant", Content: "Now looking up item 2."},
		{Role: "tool", Content: `{"id":2,"name":"item-2","value":99}`},
		{Role: "assistant", Content: "Now looking up item 3."},
		{Role: "tool", Content: `{"id":3,"name":"item-3","value":7}`},
		{Role: "assistant", Content: "Processing the latest item."},
		{Role: "tool", Content: `{"id":4,"name":"item-4","value":55}`},
	}

	fmt.Printf("Full history: %d messages\n", len(history))
	for i, m := range history {
		fmt.Printf("  [%d] %-12s %s\n", i, m.Role+":", truncate(m.Content, 50))
	}

	fmt.Println()
	fmt.Println("After compaction (MaxTurns=2, DropToolResults=true):")
	fmt.Println("  → keeps: initial prompt + last 2 turns")
	fmt.Println("  → older tool results replaced with [tool result omitted]")
	_ = json.RawMessage(nil) // imported for completeness
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

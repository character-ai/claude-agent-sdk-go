// Metrics example showing per-turn latency and per-tool execution stats,
// combined with parallel tool execution for faster multi-tool turns.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// --- Tool registry ---
	tools := claude.NewToolRegistry()

	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "fetch_weather",
		Description: "Get current weather for a city",
		InputSchema: claude.ObjectSchema(map[string]any{
			"city": claude.StringParam("City name"),
		}, "city"),
	}, func(ctx context.Context, input struct {
		City string `json:"city"`
	}) (string, error) {
		time.Sleep(50 * time.Millisecond) // simulate latency
		return fmt.Sprintf(`{"city":"%s","temp":"22°C","condition":"sunny"}`, input.City), nil
	})

	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "fetch_news",
		Description: "Get top headlines for a topic",
		InputSchema: claude.ObjectSchema(map[string]any{
			"topic": claude.StringParam("News topic"),
		}, "topic"),
	}, func(ctx context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		time.Sleep(80 * time.Millisecond) // simulate latency
		return fmt.Sprintf(`{"topic":"%s","headlines":["Story A","Story B"]}`, input.Topic), nil
	})

	claude.RegisterFunc(tools, claude.ToolDefinition{
		Name:        "lookup_stock",
		Description: "Look up a stock price by ticker symbol",
		InputSchema: claude.ObjectSchema(map[string]any{
			"ticker": claude.StringParam("Stock ticker symbol, e.g. AAPL"),
		}, "ticker"),
	}, func(ctx context.Context, input struct {
		Ticker string `json:"ticker"`
	}) (string, error) {
		time.Sleep(60 * time.Millisecond) // simulate latency
		return fmt.Sprintf(`{"ticker":"%s","price":"$192.50","change":"+1.2%%"}`, input.Ticker), nil
	})

	// --- Metrics collector ---
	mc := claude.NewMetricsCollector()

	// --- Agent with metrics + parallel tools ---
	agent := claude.NewAPIAgent(claude.APIAgentConfig{
		Model:    "claude-sonnet-4-20250514",
		Tools:    tools,
		MaxTurns: 5,

		// Collect per-turn LLM latency and per-tool execution stats
		Metrics: mc,

		// Run all tool calls within a turn concurrently.
		// Safe here because fetch_weather, fetch_news, and lookup_stock
		// are independent read-only operations.
		ParallelTools: true,
	})

	fmt.Println("Running agent...")
	fmt.Println()

	events, err := agent.Run(ctx, "Get the weather in Paris and New York, fetch news about AI, and look up the AAPL stock price. Summarize everything.")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events {
		switch event.Type {
		case claude.AgentEventContentDelta:
			fmt.Print(event.Content)

		case claude.AgentEventToolUseStart:
			if event.ToolCall != nil {
				fmt.Printf("\n[tool] calling %s...\n", event.ToolCall.Name)
			}

		case claude.AgentEventToolResult:
			if event.ToolResponse != nil {
				status := "ok"
				if event.ToolResponse.IsError {
					status = "error"
				}
				fmt.Printf("[tool] result (%s): %d chars\n", status, len(event.ToolResponse.Content))
			}

		case claude.AgentEventTurnComplete:
			// TurnMetrics is populated on every turn_complete when Metrics is configured
			if event.TurnMetrics != nil {
				tm := event.TurnMetrics
				fmt.Printf("\n[turn %d] LLM latency: %v | tools: %v\n",
					tm.TurnIndex, tm.LLMLatency.Round(time.Millisecond), tm.ToolsInvoked)
			}

		case claude.AgentEventError:
			log.Printf("error: %v", event.Error)
		}
	}

	// --- Print session summary ---
	snap := mc.Snapshot()
	fmt.Println()
	fmt.Println("=== Session Summary ===")
	fmt.Printf("Total duration:  %v\n", snap.SessionDuration.Round(time.Millisecond))
	fmt.Printf("Turns completed: %d\n", len(snap.Turns))
	fmt.Println()

	if len(snap.Turns) > 0 {
		fmt.Println("Per-turn LLM latency:")
		for _, t := range snap.Turns {
			fmt.Printf("  turn %d: %v  (tools: %v)\n",
				t.TurnIndex, t.LLMLatency.Round(time.Millisecond), t.ToolsInvoked)
		}
		fmt.Println()
	}

	if len(snap.ToolStats) > 0 {
		fmt.Println("Per-tool stats:")
		for name, s := range snap.ToolStats {
			fmt.Printf("  %-20s calls=%d  failures=%d  avg=%v  total=%v\n",
				name, s.Calls, s.Failures,
				s.AvgTime().Round(time.Millisecond),
				s.TotalTime.Round(time.Millisecond))
		}
	}

	// --- Demonstrate metrics without parallel tools for comparison ---
	fmt.Println()
	fmt.Println("=== Sequential comparison (single tool call) ===")

	mc2 := claude.NewMetricsCollector()
	agent2 := claude.NewAPIAgent(claude.APIAgentConfig{
		Model:   "claude-sonnet-4-20250514",
		Tools:   tools,
		Metrics: mc2,
		// ParallelTools: false (default)
	})

	events2, err := agent2.Run(ctx, "What is the weather in London?")
	if err != nil {
		log.Fatal(err)
	}

	for event := range events2 {
		if event.Type == claude.AgentEventContentDelta {
			fmt.Print(event.Content)
		}
	}

	snap2 := mc2.Snapshot()
	fmt.Println()
	fmt.Printf("Duration: %v\n", snap2.SessionDuration.Round(time.Millisecond))
	for name, s := range snap2.ToolStats {
		fmt.Printf("Tool %s: avg=%v\n", name, s.AvgTime().Round(time.Millisecond))
	}

	// --- Direct metrics access without running an agent ---
	fmt.Println()
	fmt.Println("=== Direct MetricsCollector usage ===")
	demonstrateMetricsAPI()
}

// demonstrateMetricsAPI shows how MetricsCollector works standalone
// (e.g. for testing or integrating with existing metric systems).
func demonstrateMetricsAPI() {
	mc := claude.NewMetricsCollector()

	mc.Snapshot() // safe to call before any session starts

	// Manually simulate what the agent does internally:
	// (in normal use you just read mc.Snapshot() after agent.Run)

	// Build a fake tool call to show JSON serialization
	input := json.RawMessage(`{"city":"Tokyo"}`)
	_ = input // input used by tool

	snap := mc.Snapshot()
	fmt.Printf("Empty snapshot: %d turns, %d tool stats, duration=%v\n",
		len(snap.Turns), len(snap.ToolStats), snap.SessionDuration)
}

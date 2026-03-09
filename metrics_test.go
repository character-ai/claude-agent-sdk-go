package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMetricsCollectorToolTracking(t *testing.T) {
	mc := NewMetricsCollector()
	mc.recordSessionStart()

	mc.recordToolStart("id1")
	time.Sleep(5 * time.Millisecond)
	mc.recordToolEnd("search", "id1", false)

	mc.recordToolStart("id2")
	time.Sleep(5 * time.Millisecond)
	mc.recordToolEnd("search", "id2", true)

	mc.recordToolStart("id3")
	time.Sleep(5 * time.Millisecond)
	mc.recordToolEnd("write", "id3", false)

	mc.recordSessionEnd()

	snap := mc.Snapshot()

	search, ok := snap.ToolStats["search"]
	if !ok {
		t.Fatal("expected search tool stats")
	}
	if search.Calls != 2 {
		t.Errorf("expected 2 search calls, got %d", search.Calls)
	}
	if search.Failures != 1 {
		t.Errorf("expected 1 search failure, got %d", search.Failures)
	}
	if search.TotalTime <= 0 {
		t.Error("expected positive total time for search")
	}
	if search.AvgTime() <= 0 {
		t.Error("expected positive avg time for search")
	}

	write, ok := snap.ToolStats["write"]
	if !ok {
		t.Fatal("expected write tool stats")
	}
	if write.Calls != 1 || write.Failures != 0 {
		t.Errorf("unexpected write stats: %+v", write)
	}

	if snap.SessionDuration <= 0 {
		t.Error("expected positive session duration")
	}
}

func TestMetricsCollectorTurnTracking(t *testing.T) {
	mc := NewMetricsCollector()

	mc.recordTurn(TurnMetrics{TurnIndex: 0, LLMLatency: 100 * time.Millisecond, ToolsInvoked: []string{"search"}})
	mc.recordTurn(TurnMetrics{TurnIndex: 1, LLMLatency: 200 * time.Millisecond, ToolsInvoked: []string{"write", "read"}})

	snap := mc.Snapshot()

	if len(snap.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(snap.Turns))
	}
	if snap.Turns[0].LLMLatency != 100*time.Millisecond {
		t.Errorf("unexpected turn 0 latency: %v", snap.Turns[0].LLMLatency)
	}
	if len(snap.Turns[1].ToolsInvoked) != 2 {
		t.Errorf("expected 2 tools in turn 1, got %d", len(snap.Turns[1].ToolsInvoked))
	}
}

func TestMetricsCollectorSnapshotIsACopy(t *testing.T) {
	mc := NewMetricsCollector()
	mc.recordTurn(TurnMetrics{TurnIndex: 0, LLMLatency: 50 * time.Millisecond})

	snap := mc.Snapshot()
	// Mutating the snapshot should not affect the collector
	snap.Turns[0].LLMLatency = 999 * time.Second

	snap2 := mc.Snapshot()
	if snap2.Turns[0].LLMLatency == 999*time.Second {
		t.Error("snapshot mutation affected collector state")
	}
}

func TestMetricsCollectorLiveSessionDuration(t *testing.T) {
	mc := NewMetricsCollector()
	mc.recordSessionStart()

	time.Sleep(10 * time.Millisecond)

	// Before session ends, duration should use time.Now()
	snap := mc.Snapshot()
	if snap.SessionDuration < 5*time.Millisecond {
		t.Errorf("expected session duration >= 5ms, got %v", snap.SessionDuration)
	}
}

func TestMetricsCollectorUnknownToolUseIDIgnored(t *testing.T) {
	mc := NewMetricsCollector()
	// recordToolEnd for an ID we never started — should not panic or create stats
	mc.recordToolEnd("search", "nonexistent-id", false)

	snap := mc.Snapshot()
	if len(snap.ToolStats) != 0 {
		t.Error("expected no tool stats for unknown ID")
	}
}

// TestAgentExecuteToolsWithMetrics verifies metrics are recorded during executeTools.
func TestAgentExecuteToolsWithMetrics(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{Name: "tool_a", Description: "a"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result_a", nil
	})
	tools.Register(ToolDefinition{Name: "tool_b", Description: "b"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result_b", nil
	})

	mc := NewMetricsCollector()
	agent := &Agent{tools: tools, metrics: mc}

	events := make(chan AgentEvent, 10)
	toolCalls := []ToolCall{
		{ID: "id1", Name: "tool_a", Input: json.RawMessage(`{}`)},
		{ID: "id2", Name: "tool_b", Input: json.RawMessage(`{}`)},
	}

	results := agent.executeTools(context.Background(), toolCalls, events)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	snap := mc.Snapshot()
	if snap.ToolStats["tool_a"] == nil || snap.ToolStats["tool_a"].Calls != 1 {
		t.Error("expected 1 call to tool_a")
	}
	if snap.ToolStats["tool_b"] == nil || snap.ToolStats["tool_b"].Calls != 1 {
		t.Error("expected 1 call to tool_b")
	}
}

// TestParallelToolExecution verifies parallel tools all run and results maintain order.
func TestParallelToolExecution(t *testing.T) {
	tools := NewToolRegistry()
	for _, name := range []string{"a", "b", "c"} {
		n := name // capture
		tools.Register(ToolDefinition{Name: n, Description: n}, func(ctx context.Context, input json.RawMessage) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "result_" + n, nil
		})
	}

	agent := &Agent{tools: tools, parallelTools: true}
	events := make(chan AgentEvent, 20)

	toolCalls := []ToolCall{
		{ID: "id1", Name: "a", Input: json.RawMessage(`{}`)},
		{ID: "id2", Name: "b", Input: json.RawMessage(`{}`)},
		{ID: "id3", Name: "c", Input: json.RawMessage(`{}`)},
	}

	start := time.Now()
	results := agent.executeTools(context.Background(), toolCalls, events)
	elapsed := time.Since(start)

	// Results must be in input order
	if results[0].Content != "result_a" || results[1].Content != "result_b" || results[2].Content != "result_c" {
		t.Errorf("results out of order: %v %v %v", results[0].Content, results[1].Content, results[2].Content)
	}

	// Parallel execution should be significantly faster than sequential (3×10ms)
	// We allow up to 25ms for scheduling overhead
	if elapsed > 25*time.Millisecond {
		t.Errorf("parallel execution too slow: %v (expected < 25ms for 3×10ms parallel tools)", elapsed)
	}

	close(events)
	count := 0
	for range events {
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 tool result events, got %d", count)
	}
}

// TestParallelToolsPreservesOrderOnMixedOutcomes ensures results are in input order even on errors.
func TestParallelToolsPreservesOrderOnMixedOutcomes(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{Name: "ok", Description: "ok"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "ok_result", nil
	})
	// "missing" is not registered — should error

	agent := &Agent{tools: tools, parallelTools: true}
	events := make(chan AgentEvent, 10)

	toolCalls := []ToolCall{
		{ID: "id1", Name: "ok", Input: json.RawMessage(`{}`)},
		{ID: "id2", Name: "missing", Input: json.RawMessage(`{}`)},
	}

	results := agent.executeTools(context.Background(), toolCalls, events)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].IsError || results[0].Content != "ok_result" {
		t.Errorf("first result should succeed: %+v", results[0])
	}
	if !results[1].IsError {
		t.Errorf("second result should be error for missing tool: %+v", results[1])
	}
}

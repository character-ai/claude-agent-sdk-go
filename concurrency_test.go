package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// helper to register a tool that records start time and sleeps for a duration.
func registerTimedTool(reg *ToolRegistry, name string, ann *ToolAnnotations, sleepDur time.Duration) {
	reg.Register(ToolDefinition{
		Name:        name,
		Description: name,
		InputSchema: ObjectSchema(nil),
		Annotations: ann,
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		time.Sleep(sleepDur)
		return name + ":done", nil
	})
}

func safeTool() *ToolAnnotations {
	return &ToolAnnotations{ConcurrencySafe: true}
}

func unsafeTool() *ToolAnnotations {
	return &ToolAnnotations{ConcurrencySafe: false}
}

func makeToolCalls(names ...string) []ToolCall {
	calls := make([]ToolCall, len(names))
	for i, n := range names {
		calls[i] = ToolCall{ID: fmt.Sprintf("tc_%s_%d", n, i), Name: n, Input: json.RawMessage(`{}`)}
	}
	return calls
}

func drainEvents(ch chan AgentEvent) {
	for range ch {
	}
}

func TestRunToolsSmart_SingleTool(t *testing.T) {
	reg := NewToolRegistry()
	registerTimedTool(reg, "only", safeTool(), 10*time.Millisecond)
	events := make(chan AgentEvent, 100)
	go drainEvents(events)

	results := runToolsSmart(context.Background(), makeToolCalls("only"), reg, nil, nil, nil, nil, events, false)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Content != "only:done" {
		t.Fatalf("unexpected content: %s", results[0].Content)
	}
}

func TestRunToolsSmart_SafeToolsRunParallel(t *testing.T) {
	reg := NewToolRegistry()
	sleepDur := 50 * time.Millisecond
	registerTimedTool(reg, "a", safeTool(), sleepDur)
	registerTimedTool(reg, "b", safeTool(), sleepDur)
	registerTimedTool(reg, "c", safeTool(), sleepDur)

	events := make(chan AgentEvent, 100)
	go drainEvents(events)

	start := time.Now()
	results := runToolsSmart(context.Background(), makeToolCalls("a", "b", "c"), reg, nil, nil, nil, nil, events, false)
	elapsed := time.Since(start)

	// Three 50ms tools in parallel should take ~50ms, not ~150ms.
	if elapsed > sleepDur*2 {
		t.Fatalf("expected parallel execution (~%v) but took %v", sleepDur, elapsed)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, name := range []string{"a", "b", "c"} {
		if results[i].Content != name+":done" {
			t.Fatalf("result[%d]: expected %s:done, got %s", i, name, results[i].Content)
		}
	}
}

func TestRunToolsSmart_UnsafeToolsRunSequentially(t *testing.T) {
	reg := NewToolRegistry()
	var counter int64
	var maxConcurrent int64
	var currentConcurrent int64

	for _, name := range []string{"x", "y", "z"} {
		n := name
		reg.Register(ToolDefinition{
			Name:        n,
			Description: n,
			InputSchema: ObjectSchema(nil),
			Annotations: unsafeTool(),
		}, func(_ context.Context, _ json.RawMessage) (string, error) {
			cur := atomic.AddInt64(&currentConcurrent, 1)
			// Track max concurrency seen.
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if cur <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, cur) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt64(&currentConcurrent, -1)
			atomic.AddInt64(&counter, 1)
			return n + ":done", nil
		})
	}

	events := make(chan AgentEvent, 100)
	go drainEvents(events)

	results := runToolsSmart(context.Background(), makeToolCalls("x", "y", "z"), reg, nil, nil, nil, nil, events, false)

	if atomic.LoadInt64(&maxConcurrent) > 1 {
		t.Fatalf("unsafe tools should not run concurrently, max concurrent was %d", atomic.LoadInt64(&maxConcurrent))
	}
	if atomic.LoadInt64(&counter) != 3 {
		t.Fatalf("expected 3 executions, got %d", atomic.LoadInt64(&counter))
	}
	for i, name := range []string{"x", "y", "z"} {
		if results[i].Content != name+":done" {
			t.Fatalf("result[%d]: expected %s:done, got %s", i, name, results[i].Content)
		}
	}
}

func TestRunToolsSmart_DefaultParallelTrue(t *testing.T) {
	reg := NewToolRegistry()
	sleepDur := 50 * time.Millisecond
	// Register tools WITHOUT annotations.
	for _, name := range []string{"p", "q"} {
		registerTimedTool(reg, name, nil, sleepDur)
	}

	events := make(chan AgentEvent, 100)
	go drainEvents(events)

	start := time.Now()
	results := runToolsSmart(context.Background(), makeToolCalls("p", "q"), reg, nil, nil, nil, nil, events, true)
	elapsed := time.Since(start)

	// defaultParallel=true => unannotated tools run in parallel.
	if elapsed > sleepDur*2 {
		t.Fatalf("expected parallel execution (~%v) but took %v", sleepDur, elapsed)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRunToolsSmart_DefaultParallelFalse(t *testing.T) {
	reg := NewToolRegistry()
	sleepDur := 30 * time.Millisecond
	// Register tools WITHOUT annotations.
	for _, name := range []string{"s", "t"} {
		registerTimedTool(reg, name, nil, sleepDur)
	}

	events := make(chan AgentEvent, 100)
	go drainEvents(events)

	start := time.Now()
	results := runToolsSmart(context.Background(), makeToolCalls("s", "t"), reg, nil, nil, nil, nil, events, false)
	elapsed := time.Since(start)

	// defaultParallel=false => unannotated tools run sequentially.
	if elapsed < sleepDur*2 {
		t.Fatalf("expected sequential execution (~%v) but took %v", sleepDur*2, elapsed)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRunToolsSmart_MixedSafeUnsafe_OrderPreserved(t *testing.T) {
	reg := NewToolRegistry()
	sleepDur := 20 * time.Millisecond

	// safe, safe, unsafe, safe, unsafe
	registerTimedTool(reg, "s1", safeTool(), sleepDur)
	registerTimedTool(reg, "s2", safeTool(), sleepDur)
	registerTimedTool(reg, "u1", unsafeTool(), sleepDur)
	registerTimedTool(reg, "s3", safeTool(), sleepDur)
	registerTimedTool(reg, "u2", unsafeTool(), sleepDur)

	events := make(chan AgentEvent, 100)
	go drainEvents(events)

	calls := makeToolCalls("s1", "s2", "u1", "s3", "u2")
	results := runToolsSmart(context.Background(), calls, reg, nil, nil, nil, nil, events, false)

	expected := []string{"s1:done", "s2:done", "u1:done", "s3:done", "u2:done"}
	if len(results) != len(expected) {
		t.Fatalf("expected %d results, got %d", len(expected), len(results))
	}
	for i, want := range expected {
		if results[i].Content != want {
			t.Errorf("result[%d]: expected %q, got %q", i, want, results[i].Content)
		}
		// Verify tool use IDs match original order.
		if results[i].ToolUseID != calls[i].ID {
			t.Errorf("result[%d]: expected tool_use_id %q, got %q", i, calls[i].ID, results[i].ToolUseID)
		}
	}
}

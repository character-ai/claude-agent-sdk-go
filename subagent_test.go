package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSubagentConfig(t *testing.T) {
	sc := NewSubagentConfig()

	def := &AgentDefinition{
		Name:        "researcher",
		Description: "Researches topics",
		Prompt:      "You are a research assistant.",
		Model:       "sonnet",
		MaxTurns:    5,
	}

	sc.Add(def)

	if len(sc.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(sc.Agents))
	}

	got, ok := sc.Agents["researcher"]
	if !ok {
		t.Fatal("expected 'researcher' agent")
	}
	if got.Description != "Researches topics" {
		t.Fatalf("unexpected description: %s", got.Description)
	}
}

func TestSubagentConfigMultiple(t *testing.T) {
	sc := NewSubagentConfig()

	sc.Add(&AgentDefinition{
		Name:        "coder",
		Description: "Writes code",
		Model:       "opus",
	})
	sc.Add(&AgentDefinition{
		Name:        "reviewer",
		Description: "Reviews code",
		Model:       "haiku",
	})

	if len(sc.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(sc.Agents))
	}
}

func TestTaskToolRegistration(t *testing.T) {
	sc := NewSubagentConfig()
	sc.Add(&AgentDefinition{
		Name:        "helper",
		Description: "Helps with tasks",
		Model:       "inherit",
	})

	registry := NewToolRegistry()
	registerTaskTool(registry, sc, Options{})

	if !registry.Has("Task") {
		t.Fatal("expected Task tool to be registered")
	}

	defs := registry.Definitions()
	found := false
	for _, d := range defs {
		if d.Name == "Task" {
			found = true
			if d.Description == "" {
				t.Fatal("Task tool should have a description")
			}
		}
	}
	if !found {
		t.Fatal("Task definition not found in registry")
	}
}

func TestTaskToolInputParsing(t *testing.T) {
	input := taskToolInput{
		Description:  "Do something",
		SubagentName: "helper",
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed taskToolInput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if parsed.Description != "Do something" {
		t.Fatalf("unexpected description: %s", parsed.Description)
	}
	if parsed.SubagentName != "helper" {
		t.Fatalf("unexpected subagent name: %s", parsed.SubagentName)
	}
}

func TestTaskToolNotFoundSubagent(t *testing.T) {
	sc := NewSubagentConfig()
	sc.Add(&AgentDefinition{
		Name:        "helper",
		Description: "Helps with tasks",
	})

	registry := NewToolRegistry()
	registerTaskTool(registry, sc, Options{})

	// Try to invoke with a non-existent subagent
	input := json.RawMessage(`{"description": "test", "subagent_name": "nonexistent"}`)
	_, err := registry.Execute(context.Background(), "Task", input)
	if err == nil {
		t.Fatal("expected error for non-existent subagent")
	}
}

func TestAgentDefinitionWithTools(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(ToolDefinition{
		Name:        "custom_tool",
		Description: "A custom tool",
		InputSchema: ObjectSchema(map[string]any{
			"query": StringParam("search query"),
		}, "query"),
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result", nil
	})

	def := &AgentDefinition{
		Name:        "searcher",
		Description: "Searches things",
		Tools:       tools,
		MaxTurns:    3,
	}

	if def.Tools == nil {
		t.Fatal("expected tools to be set")
	}
	if !def.Tools.Has("custom_tool") {
		t.Fatal("expected custom_tool to be registered")
	}
}

func TestAgentDefinitionWithHooks(t *testing.T) {
	hooks := NewHooks()
	var started, stopped bool

	hooks.OnEvent(HookSubagentStart, func(ctx context.Context, data HookEventData) {
		started = true
	})
	hooks.OnEvent(HookSubagentStop, func(ctx context.Context, data HookEventData) {
		stopped = true
	})

	def := &AgentDefinition{
		Name:        "monitored",
		Description: "Monitored agent",
		Hooks:       hooks,
	}

	// Simulate subagent lifecycle hooks
	def.Hooks.EmitEvent(context.Background(), HookEventData{
		Event:        HookSubagentStart,
		SubagentName: def.Name,
	})

	if !started {
		t.Fatal("expected SubagentStart hook to fire")
	}

	def.Hooks.EmitEvent(context.Background(), HookEventData{
		Event:        HookSubagentStop,
		SubagentName: def.Name,
	})

	if !stopped {
		t.Fatal("expected SubagentStop hook to fire")
	}
}

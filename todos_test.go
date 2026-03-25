package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTodoStoreWriteAndList(t *testing.T) {
	store := NewTodoStore()

	items := []TodoItem{
		{ID: "1", Description: "First task", Status: TodoStatusPending, Priority: TodoPriorityHigh},
		{ID: "2", Description: "Second task", Status: TodoStatusInProgress, Priority: TodoPriorityMedium},
	}
	store.Write(items)

	got := store.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if got[0].ID != "1" || got[1].ID != "2" {
		t.Fatalf("unexpected IDs: %q, %q", got[0].ID, got[1].ID)
	}
}

func TestTodoStoreListReturnsCopy(t *testing.T) {
	store := NewTodoStore()
	store.Write([]TodoItem{
		{ID: "1", Description: "task", Status: TodoStatusPending, Priority: TodoPriorityLow},
	})

	list := store.List()
	list[0].Description = "mutated"

	original := store.List()
	if original[0].Description == "mutated" {
		t.Fatal("List() should return a copy, not a reference")
	}
}

func TestTodoStoreWriteReplaces(t *testing.T) {
	store := NewTodoStore()
	store.Write([]TodoItem{
		{ID: "1", Description: "old", Status: TodoStatusPending, Priority: TodoPriorityHigh},
	})
	store.Write([]TodoItem{
		{ID: "2", Description: "new", Status: TodoStatusCompleted, Priority: TodoPriorityLow},
	})

	got := store.List()
	if len(got) != 1 {
		t.Fatalf("expected 1 item after replace, got %d", len(got))
	}
	if got[0].ID != "2" {
		t.Fatalf("expected ID '2', got %q", got[0].ID)
	}
}

func TestTodoStoreEmpty(t *testing.T) {
	store := NewTodoStore()
	got := store.List()
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d items", len(got))
	}
}

func TestTodosToolDefinition(t *testing.T) {
	def := TodosToolDefinition()
	if def.Name != TodoToolName {
		t.Fatalf("expected name %q, got %q", TodoToolName, def.Name)
	}
	if def.InputSchema == nil {
		t.Fatal("expected non-nil InputSchema")
	}
	props, ok := def.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}
	if _, ok := props["todos"]; !ok {
		t.Fatal("expected 'todos' property in schema")
	}
}

func TestRegisterTodosToolHandler(t *testing.T) {
	store := NewTodoStore()
	registry := NewToolRegistry()

	var emittedItems []TodoItem
	RegisterTodosTool(registry, store, func(items []TodoItem) {
		emittedItems = items
	})

	if !registry.Has(TodoToolName) {
		t.Fatal("write_todos tool not registered")
	}

	input := writeTodosInput{
		Todos: []TodoItem{
			{ID: "a", Description: "Do something", Status: TodoStatusPending, Priority: TodoPriorityHigh},
			{ID: "b", Description: "Do another thing", Status: TodoStatusInProgress, Priority: TodoPriorityMedium},
			{ID: "c", Description: "Done thing", Status: TodoStatusCompleted, Priority: TodoPriorityLow},
		},
	}
	raw, _ := json.Marshal(input)

	result, err := registry.Execute(context.Background(), "write_todos", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Check store was updated
	items := store.List()
	if len(items) != 3 {
		t.Fatalf("expected 3 items in store, got %d", len(items))
	}

	// Check event was emitted
	if len(emittedItems) != 3 {
		t.Fatalf("expected 3 emitted items, got %d", len(emittedItems))
	}
}

func TestTodosToolValidation(t *testing.T) {
	store := NewTodoStore()
	registry := NewToolRegistry()
	RegisterTodosTool(registry, store, nil)

	tests := []struct {
		name  string
		input writeTodosInput
	}{
		{
			name: "missing ID",
			input: writeTodosInput{
				Todos: []TodoItem{{Description: "task", Status: TodoStatusPending, Priority: TodoPriorityHigh}},
			},
		},
		{
			name: "missing description",
			input: writeTodosInput{
				Todos: []TodoItem{{ID: "1", Status: TodoStatusPending, Priority: TodoPriorityHigh}},
			},
		},
		{
			name: "invalid status",
			input: writeTodosInput{
				Todos: []TodoItem{{ID: "1", Description: "task", Status: "bad", Priority: TodoPriorityHigh}},
			},
		},
		{
			name: "invalid priority",
			input: writeTodosInput{
				Todos: []TodoItem{{ID: "1", Description: "task", Status: TodoStatusPending, Priority: "bad"}},
			},
		},
		{
			name: "duplicate ID",
			input: writeTodosInput{
				Todos: []TodoItem{
					{ID: "1", Description: "first", Status: TodoStatusPending, Priority: TodoPriorityHigh},
					{ID: "1", Description: "second", Status: TodoStatusPending, Priority: TodoPriorityLow},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _ := json.Marshal(tt.input)
			_, err := registry.Execute(context.Background(), "write_todos", raw)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestTodosToolEmptyList(t *testing.T) {
	store := NewTodoStore()
	registry := NewToolRegistry()
	RegisterTodosTool(registry, store, nil)

	// Pre-populate
	store.Write([]TodoItem{
		{ID: "1", Description: "task", Status: TodoStatusPending, Priority: TodoPriorityHigh},
	})

	// Clear with empty list
	raw, _ := json.Marshal(writeTodosInput{Todos: []TodoItem{}})
	result, err := registry.Execute(context.Background(), "write_todos", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Todo list cleared." {
		t.Fatalf("expected 'Todo list cleared.', got %q", result)
	}
	if len(store.List()) != 0 {
		t.Fatal("expected empty store after clearing")
	}
}

func TestFormatTodoSummary(t *testing.T) {
	tests := []struct {
		name     string
		items    []TodoItem
		expected string
	}{
		{
			name:     "empty",
			items:    nil,
			expected: "Todo list cleared.",
		},
		{
			name: "mixed",
			items: []TodoItem{
				{Status: TodoStatusPending},
				{Status: TodoStatusInProgress},
				{Status: TodoStatusCompleted},
				{Status: TodoStatusCompleted},
			},
			expected: "Updated 4 todos, 1 pending, 1 in progress, 2 completed.",
		},
		{
			name: "all pending",
			items: []TodoItem{
				{Status: TodoStatusPending},
				{Status: TodoStatusPending},
			},
			expected: "Updated 2 todos, 2 pending.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTodoSummary(tt.items)
			if got != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestAgentConfigEnableTodos(t *testing.T) {
	agent := NewAgent(AgentConfig{
		EnableTodos: true,
	})

	if agent.todoStore == nil {
		t.Fatal("expected todoStore to be created when EnableTodos is true")
	}
	if !agent.tools.Has(TodoToolName) {
		t.Fatal("expected write_todos tool to be registered")
	}
	if agent.TodoStore() == nil {
		t.Fatal("expected TodoStore() to return non-nil")
	}
}

func TestAgentConfigDisabledTodos(t *testing.T) {
	agent := NewAgent(AgentConfig{})

	if agent.todoStore != nil {
		t.Fatal("expected todoStore to be nil when EnableTodos is false")
	}
	if agent.tools.Has(TodoToolName) {
		t.Fatal("expected write_todos tool to not be registered")
	}
	if agent.TodoStore() != nil {
		t.Fatal("expected TodoStore() to return nil")
	}
}

func TestAgentConfigSharedTodoStore(t *testing.T) {
	shared := NewTodoStore()
	shared.Write([]TodoItem{
		{ID: "pre", Description: "pre-existing", Status: TodoStatusPending, Priority: TodoPriorityHigh},
	})

	agent := NewAgent(AgentConfig{
		EnableTodos: true,
		TodoStore:   shared,
	})

	if agent.todoStore != shared {
		t.Fatal("expected agent to use the shared TodoStore")
	}
	items := agent.TodoStore().List()
	if len(items) != 1 || items[0].ID != "pre" {
		t.Fatal("expected to see pre-existing items from shared store")
	}
}

func TestAPIAgentConfigEnableTodos(t *testing.T) {
	agent := NewAPIAgent(APIAgentConfig{
		EnableTodos: true,
	})

	if agent.todoStore == nil {
		t.Fatal("expected todoStore to be created when EnableTodos is true")
	}
	if !agent.tools.Has(TodoToolName) {
		t.Fatal("expected write_todos tool to be registered")
	}
	if agent.TodoStore() == nil {
		t.Fatal("expected TodoStore() to return non-nil")
	}
}

func TestAPIAgentConfigDisabledTodos(t *testing.T) {
	agent := NewAPIAgent(APIAgentConfig{})

	if agent.todoStore != nil {
		t.Fatal("expected todoStore to be nil when EnableTodos is false")
	}
	if agent.tools.Has(TodoToolName) {
		t.Fatal("expected write_todos tool to not be registered")
	}
	if agent.TodoStore() != nil {
		t.Fatal("expected TodoStore() to return nil")
	}
}

func TestAPIAgentConfigSharedTodoStore(t *testing.T) {
	shared := NewTodoStore()
	shared.Write([]TodoItem{
		{ID: "pre", Description: "pre-existing", Status: TodoStatusPending, Priority: TodoPriorityHigh},
	})

	agent := NewAPIAgent(APIAgentConfig{
		EnableTodos: true,
		TodoStore:   shared,
	})

	if agent.todoStore != shared {
		t.Fatal("expected agent to use the shared TodoStore")
	}
	items := agent.TodoStore().List()
	if len(items) != 1 || items[0].ID != "pre" {
		t.Fatal("expected to see pre-existing items from shared store")
	}
}

func TestTodoToolNameConst(t *testing.T) {
	def := TodosToolDefinition()
	if def.Name != TodoToolName {
		t.Fatalf("TodosToolDefinition().Name = %q, want %q", def.Name, TodoToolName)
	}
}

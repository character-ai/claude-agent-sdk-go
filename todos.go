package claudeagent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TodoToolName is the registered name of the write_todos tool.
const TodoToolName = "write_todos"

// TodoStatus represents the state of a todo item.
type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
)

// TodoPriority represents the urgency of a todo item.
type TodoPriority string

const (
	TodoPriorityHigh   TodoPriority = "high"
	TodoPriorityMedium TodoPriority = "medium"
	TodoPriorityLow    TodoPriority = "low"
)

// TodoItem represents a single tracked task the agent is working on.
type TodoItem struct {
	ID          string       `json:"id"`
	Description string       `json:"description"`
	Status      TodoStatus   `json:"status"`
	Priority    TodoPriority `json:"priority"`
	ParentID    string       `json:"parent_id,omitempty"`
}

// TodoStore provides thread-safe storage for todo items.
// It is intentionally separate from the memdb Store since todos are
// a simple ordered list managed atomically via write_todos.
type TodoStore struct {
	mu    sync.RWMutex
	items []TodoItem
}

// NewTodoStore creates an empty TodoStore.
func NewTodoStore() *TodoStore {
	return &TodoStore{}
}

// Write replaces all todos atomically.
func (s *TodoStore) Write(items []TodoItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make([]TodoItem, len(items))
	copy(s.items, items)
}

// List returns a snapshot of all todos.
func (s *TodoStore) List() []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TodoItem, len(s.items))
	copy(out, s.items)
	return out
}

// writeTodosInput is the JSON input for the write_todos tool.
type writeTodosInput struct {
	Todos []TodoItem `json:"todos"`
}

// TodosToolDefinition returns the ToolDefinition for the write_todos tool.
func TodosToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: TodoToolName,
		Description: `Write and manage a todo list to plan and track your work. ` +
			`Each call replaces the entire list. Use this to break down complex tasks, ` +
			`track progress, and organize subtasks. ` +
			`Update status as you work: pending → in_progress → completed.`,
		InputSchema: ObjectSchema(
			map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "The complete todo list (replaces any existing list)",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":          StringParam("Unique identifier for the todo"),
							"description": StringParam("What needs to be done"),
							"status":      EnumParam("Current status", "pending", "in_progress", "completed"),
							"priority":    EnumParam("Priority level", "high", "medium", "low"),
							"parent_id":   StringParam("ID of parent todo for subtasks (optional)"),
						},
						"required": []string{"id", "description", "status", "priority"},
					},
				},
			},
			"todos",
		),
	}
}

// RegisterTodosTool registers the write_todos tool on the given registry,
// backed by the provided TodoStore. The emitEvent callback is called after
// each write so the agent loop can emit AgentEventTodosUpdated.
func RegisterTodosTool(registry *ToolRegistry, store *TodoStore, emitEvent func([]TodoItem)) {
	RegisterFunc(registry, TodosToolDefinition(), func(_ context.Context, input writeTodosInput) (string, error) {
		// Validate items
		seen := make(map[string]bool, len(input.Todos))
		for i, item := range input.Todos {
			if item.ID == "" {
				return "", fmt.Errorf("todo at index %d: id is required", i)
			}
			if seen[item.ID] {
				return "", fmt.Errorf("todo at index %d: duplicate id %q", i, item.ID)
			}
			seen[item.ID] = true
			if item.Description == "" {
				return "", fmt.Errorf("todo %q: description is required", item.ID)
			}
			switch item.Status {
			case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted:
			default:
				return "", fmt.Errorf("todo %q: invalid status %q", item.ID, item.Status)
			}
			switch item.Priority {
			case TodoPriorityHigh, TodoPriorityMedium, TodoPriorityLow:
			default:
				return "", fmt.Errorf("todo %q: invalid priority %q", item.ID, item.Priority)
			}
		}

		store.Write(input.Todos)

		if emitEvent != nil {
			emitEvent(store.List())
		}

		return formatTodoSummary(input.Todos), nil
	})
}

// formatTodoSummary returns a human-readable summary of the todo list.
func formatTodoSummary(items []TodoItem) string {
	if len(items) == 0 {
		return "Todo list cleared."
	}

	var pending, inProgress, completed int
	for _, item := range items {
		switch item.Status {
		case TodoStatusPending:
			pending++
		case TodoStatusInProgress:
			inProgress++
		case TodoStatusCompleted:
			completed++
		}
	}

	parts := []string{fmt.Sprintf("Updated %d todos", len(items))}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d in progress", inProgress))
	}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completed))
	}
	return strings.Join(parts, ", ") + "."
}


package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// TodoToolName is the registered name of the write_todos tool.
const TodoToolName = "write_todos"

// ReadTodosToolName is the registered name of the read_todos tool.
const ReadTodosToolName = "read_todos"

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

// ReadTodosToolDefinition returns the ToolDefinition for the read_todos tool.
func ReadTodosToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name: ReadTodosToolName,
		Description: `Read the current todo list. Use this to refresh your knowledge of ` +
			`pending work, especially after long conversations where earlier context ` +
			`may have been compressed.`,
		InputSchema: ObjectSchema(map[string]any{}), // no required fields
	}
}

// RegisterTodosTools registers both write_todos and read_todos tools on the
// given registry, backed by the provided TodoStore.
func RegisterTodosTools(registry *ToolRegistry, store *TodoStore) {
	// write_todos
	RegisterFunc(registry, TodosToolDefinition(), func(_ context.Context, input writeTodosInput) (string, error) {
		if err := validateTodos(input.Todos); err != nil {
			return "", err
		}
		store.Write(input.Todos)
		return formatTodoSummary(input.Todos), nil
	})

	// read_todos
	registry.Register(ReadTodosToolDefinition(), func(_ context.Context, _ json.RawMessage) (string, error) {
		items := store.List()
		if len(items) == 0 {
			return "No todos.", nil
		}
		data, err := json.Marshal(items)
		if err != nil {
			return "", fmt.Errorf("marshal todos: %w", err)
		}
		return string(data), nil
	})
}

// validateTodos checks all items for required fields, valid enums,
// duplicate IDs, and dangling parent_id references.
func validateTodos(items []TodoItem) error {
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		if item.ID == "" {
			return fmt.Errorf("todo at index %d: id is required", i)
		}
		if seen[item.ID] {
			return fmt.Errorf("todo at index %d: duplicate id %q", i, item.ID)
		}
		seen[item.ID] = true
		if item.Description == "" {
			return fmt.Errorf("todo %q: description is required", item.ID)
		}
		switch item.Status {
		case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted:
		default:
			return fmt.Errorf("todo %q: invalid status %q", item.ID, item.Status)
		}
		switch item.Priority {
		case TodoPriorityHigh, TodoPriorityMedium, TodoPriorityLow:
		default:
			return fmt.Errorf("todo %q: invalid priority %q", item.ID, item.Priority)
		}
	}
	// Validate parent_id references after all IDs are collected.
	for _, item := range items {
		if item.ParentID != "" && !seen[item.ParentID] {
			return fmt.Errorf("todo %q: parent_id %q does not reference an existing todo", item.ID, item.ParentID)
		}
	}
	return nil
}

// initTodoStore initializes the TodoStore and registers todo tools on the
// given registry. Returns the store. This is the shared init path for both
// Agent and APIAgent.
func initTodoStore(registry *ToolRegistry, existing *TodoStore) *TodoStore {
	ts := existing
	if ts == nil {
		ts = NewTodoStore()
	}
	RegisterTodosTools(registry, ts)
	return ts
}

// emitTodoEvents sends an AgentEventTodosUpdated if write_todos succeeded
// this turn. Uses a single pass over toolResults with a pre-built success set.
func emitTodoEvents(
	todoStore *TodoStore,
	toolCalls []ToolCall,
	toolResults []ToolResponse,
	events chan<- AgentEvent,
) {
	if todoStore == nil {
		return
	}

	// Build set of successful tool-use IDs in one pass.
	successIDs := make(map[string]bool, len(toolResults))
	for _, tr := range toolResults {
		if !tr.IsError {
			successIDs[tr.ToolUseID] = true
		}
	}

	// Check if write_todos succeeded.
	for _, tc := range toolCalls {
		if tc.Name == TodoToolName && successIDs[tc.ID] {
			events <- AgentEvent{
				Type:  AgentEventTodosUpdated,
				Todos: todoStore.List(),
			}
			return
		}
	}
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

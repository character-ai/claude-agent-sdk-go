package claudeagent

import (
	"context"
	"encoding/json"
)

// ToolDefinition describes a tool that can be called by Claude.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolCall represents a tool invocation from Claude.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResponse is the result of executing a tool.
type ToolResponse struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// ToolHandler is a function that executes a tool and returns a result.
type ToolHandler func(ctx context.Context, input json.RawMessage) (string, error)

// ToolRegistry maps tool names to their handlers.
// Internally backed by a Store for indexed lookups and thread-safe access.
type ToolRegistry struct {
	store *Store
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		store: NewStore(),
	}
}

// NewToolRegistryWithStore creates a tool registry sharing the given store.
func NewToolRegistryWithStore(store *Store) *ToolRegistry {
	return &ToolRegistry{store: store}
}

// Store returns the underlying store for cross-component queries.
func (r *ToolRegistry) Store() *Store {
	return r.store
}

// Register adds a tool to the registry with Source="native".
func (r *ToolRegistry) Register(def ToolDefinition, handler ToolHandler) {
	r.RegisterWithSource(def, handler, "native", nil)
}

// RegisterWithSource adds a tool with an explicit source and tags.
func (r *ToolRegistry) RegisterWithSource(def ToolDefinition, handler ToolHandler, source string, tags []string) {
	_ = r.store.InsertTool(&StoredTool{
		ToolDefinition: def,
		Source:         source,
		Tags:           tags,
		Handler:        handler,
	})
}

// RegisterFunc is a convenience method for registering a tool with a typed handler.
func RegisterFunc[T any](r *ToolRegistry, def ToolDefinition, handler func(ctx context.Context, input T) (string, error)) {
	r.Register(def, func(ctx context.Context, raw json.RawMessage) (string, error) {
		var input T
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", err
		}
		return handler(ctx, input)
	})
}

// Definitions returns all registered tool definitions.
func (r *ToolRegistry) Definitions() []ToolDefinition {
	tools, err := r.store.ListTools()
	if err != nil {
		return nil
	}
	defs := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, t.ToolDefinition)
	}
	return defs
}

// Execute runs a tool by name with the given input.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	tool, err := r.store.GetTool(name)
	if err != nil || tool == nil {
		return "", &ToolNotFoundError{Name: name}
	}
	if tool.Handler == nil {
		return "", &ToolNotFoundError{Name: name}
	}
	return tool.Handler(ctx, input)
}

// Has checks if a tool is registered.
func (r *ToolRegistry) Has(name string) bool {
	tool, err := r.store.GetTool(name)
	return err == nil && tool != nil
}

// GetHandler returns the handler for the given tool name.
func (r *ToolRegistry) GetHandler(name string) (ToolHandler, bool) {
	tool, err := r.store.GetTool(name)
	if err != nil || tool == nil {
		return nil, false
	}
	return tool.Handler, tool.Handler != nil
}

// Merge adds all tools from another registry.
func (r *ToolRegistry) Merge(other *ToolRegistry) {
	if other == nil {
		return
	}
	tools, err := other.store.ListTools()
	if err != nil {
		return
	}
	for _, t := range tools {
		_ = r.store.InsertTool(t)
	}
}

// Remove deletes a tool from the registry by name.
func (r *ToolRegistry) Remove(name string) {
	_ = r.store.DeleteTool(name)
}

// ToolsByTag returns tool definitions matching the given tag.
func (r *ToolRegistry) ToolsByTag(tag string) []ToolDefinition {
	tools, err := r.store.ListToolsByTag(tag)
	if err != nil {
		return nil
	}
	defs := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, t.ToolDefinition)
	}
	return defs
}

// ToolNotFoundError indicates a tool was not found in the registry.
type ToolNotFoundError struct {
	Name string
}

func (e *ToolNotFoundError) Error() string {
	return "tool not found: " + e.Name
}

// Common tool input schema helpers

// StringParam creates a string parameter schema.
func StringParam(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

// IntParam creates an integer parameter schema.
func IntParam(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
	}
}

// BoolParam creates a boolean parameter schema.
func BoolParam(description string) map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": description,
	}
}

// EnumParam creates an enum parameter schema.
func EnumParam(description string, values ...string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
		"enum":        values,
	}
}

// ObjectSchema creates an object schema for tool input.
func ObjectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

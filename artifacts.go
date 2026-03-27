package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ArtifactType represents the kind of artifact content.
type ArtifactType string

const (
	ArtifactHTML ArtifactType = "html"
	ArtifactJSX  ArtifactType = "jsx"
	ArtifactText ArtifactType = "text"
)

// Artifact represents a generated content artifact.
type Artifact struct {
	ID        string       `json:"id"`
	Type      ArtifactType `json:"type"`
	Title     string       `json:"title"`
	Content   string       `json:"content"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Version   int          `json:"version"`
}

// ArtifactRegistry manages artifacts and provides tools for agent use.
type ArtifactRegistry struct {
	mu        sync.RWMutex
	artifacts map[string]*Artifact
	order     []string // insertion order
	nextID    int
}

// NewArtifactRegistry creates a new artifact registry.
func NewArtifactRegistry() *ArtifactRegistry {
	return &ArtifactRegistry{
		artifacts: make(map[string]*Artifact),
	}
}

// Tools returns a ToolRegistry with create_artifact and update_artifact tools.
func (r *ArtifactRegistry) Tools() *ToolRegistry {
	tools := NewToolRegistry()

	tools.Register(ToolDefinition{
		Name:        "create_artifact",
		Description: "Create a new artifact. Use this to generate HTML pages, JSX components, or text content. Each artifact is a self-contained piece of content.",
		InputSchema: ObjectSchema(map[string]any{
			"type":    EnumParam("The artifact type", "html", "jsx", "text"),
			"title":   StringParam("A short, descriptive title for the artifact"),
			"content": StringParam("The full content of the artifact"),
		}, "type", "title", "content"),
	}, r.handleCreate)

	tools.Register(ToolDefinition{
		Name:        "update_artifact",
		Description: "Update an existing artifact by replacing its content entirely. Use when the user asks to modify a previously created artifact.",
		InputSchema: ObjectSchema(map[string]any{
			"id":      StringParam("The artifact ID to update"),
			"title":   StringParam("Updated title (optional, keeps existing if omitted)"),
			"content": StringParam("The new full content of the artifact"),
		}, "id", "content"),
	}, r.handleUpdate)

	return tools
}

type createInput struct {
	Type    ArtifactType `json:"type"`
	Title   string       `json:"title"`
	Content string       `json:"content"`
}

type updateInput struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content"`
}

func (r *ArtifactRegistry) handleCreate(_ context.Context, raw json.RawMessage) (string, error) {
	var input createInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	switch input.Type {
	case ArtifactHTML, ArtifactJSX, ArtifactText:
	default:
		return "", fmt.Errorf("unsupported artifact type: %q (must be html, jsx, or text)", input.Type)
	}

	if input.Title == "" {
		return "", fmt.Errorf("title is required")
	}
	if input.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	r.mu.Lock()
	r.nextID++
	id := fmt.Sprintf("artifact_%d", r.nextID)
	now := time.Now()
	a := &Artifact{
		ID:        id,
		Type:      input.Type,
		Title:     input.Title,
		Content:   input.Content,
		CreatedAt: now,
		UpdatedAt: now,
		Version:   1,
	}
	r.artifacts[id] = a
	r.order = append(r.order, id)
	r.mu.Unlock()

	return fmt.Sprintf("Created artifact %q (id: %s, type: %s, %d bytes)", a.Title, id, input.Type, len(input.Content)), nil
}

func (r *ArtifactRegistry) handleUpdate(_ context.Context, raw json.RawMessage) (string, error) {
	var input updateInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if input.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	r.mu.Lock()
	a, ok := r.artifacts[input.ID]
	if !ok {
		r.mu.Unlock()
		return "", fmt.Errorf("artifact not found: %s", input.ID)
	}
	a.Content = input.Content
	a.UpdatedAt = time.Now()
	a.Version++
	if input.Title != "" {
		a.Title = input.Title
	}
	r.mu.Unlock()

	return fmt.Sprintf("Updated artifact %q (id: %s, version: %d, %d bytes)", a.Title, a.ID, a.Version, len(a.Content)), nil
}

// Get returns an artifact by ID.
func (r *ArtifactRegistry) Get(id string) (*Artifact, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.artifacts[id]
	if !ok {
		return nil, false
	}
	copy := *a
	return &copy, true
}

// All returns all artifacts in creation order.
func (r *ArtifactRegistry) All() []Artifact {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Artifact, 0, len(r.order))
	for _, id := range r.order {
		if a, ok := r.artifacts[id]; ok {
			result = append(result, *a)
		}
	}
	return result
}

// Count returns the number of artifacts.
func (r *ArtifactRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.artifacts)
}

// SystemPrompt returns a system prompt snippet that instructs the agent on artifact usage.
func (r *ArtifactRegistry) SystemPrompt() string {
	return artifactSystemPrompt
}

const artifactSystemPrompt = `You have access to an artifact system for generating self-contained content. Use artifacts for:

- **HTML**: Interactive web pages, visualizations, dashboards, forms. Write complete, self-contained HTML with inline CSS and JavaScript. Use modern HTML5, CSS3, and vanilla JS. You may include CDN links for libraries like Chart.js, D3, Three.js, or Tailwind CSS.
- **JSX**: React components. Write a single default-exported component. You may use Tailwind classes. Assume React and ReactDOM are available.
- **Text**: Markdown, JSON, YAML, config files, prose, or any plain text content.

Guidelines:
- Create an artifact when the user asks you to generate, build, create, or write something that produces a tangible output.
- Each artifact should be complete and self-contained — it must work on its own.
- For HTML artifacts, always include <!DOCTYPE html> and make the page visually polished.
- When updating an artifact, provide the complete new content — not a partial diff.
- Prefer one well-crafted artifact over multiple fragments.`

package claudeagent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// DeferredTool represents a tool whose full definition is not loaded into the
// active registry until explicitly requested via ToolSearch.
type DeferredTool struct {
	Name        string
	Description string // Brief one-line description
	SearchHint  string // Keywords for search matching
}

// DeferredToolLoader resolves a deferred tool name into its full definition and handler.
type DeferredToolLoader func(ctx context.Context, name string) (*ToolDefinition, ToolHandler, error)

// DeferredToolRegistry holds tools that are lazily loaded on demand.
type DeferredToolRegistry struct {
	mu     sync.RWMutex
	tools  map[string]*DeferredTool
	loader DeferredToolLoader
	loaded map[string]bool // tracks which tools have been loaded
}

// NewDeferredToolRegistry creates a new DeferredToolRegistry with the given loader.
func NewDeferredToolRegistry(loader DeferredToolLoader) *DeferredToolRegistry {
	return &DeferredToolRegistry{
		tools:  make(map[string]*DeferredTool),
		loader: loader,
		loaded: make(map[string]bool),
	}
}

// Add registers a deferred tool for discovery.
func (d *DeferredToolRegistry) Add(tool DeferredTool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tools[tool.Name] = &tool
}

// Search finds deferred tools matching the query string. Uses simple
// keyword matching against Name, Description, and SearchHint.
// Returns up to maxResults matches, sorted by relevance.
func (d *DeferredToolRegistry) Search(query string, maxResults int) []DeferredTool {
	if maxResults <= 0 {
		maxResults = 5
	}

	queryWords := tokenizeQuery(query)
	if len(queryWords) == 0 {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	type scored struct {
		tool  DeferredTool
		score int
	}

	var results []scored
	for _, t := range d.tools {
		corpus := strings.ToLower(t.Name + " " + t.Description + " " + t.SearchHint)
		score := 0
		for _, w := range queryWords {
			if strings.Contains(corpus, w) {
				score++
			}
		}
		if score > 0 {
			results = append(results, scored{tool: *t, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].tool.Name < results[j].tool.Name
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	out := make([]DeferredTool, len(results))
	for i, r := range results {
		out[i] = r.tool
	}
	return out
}

// Load resolves a deferred tool and registers it in the target registry.
// Returns an error if the tool is not found or the loader fails.
func (d *DeferredToolRegistry) Load(ctx context.Context, name string, into *ToolRegistry) error {
	d.mu.RLock()
	_, exists := d.tools[name]
	d.mu.RUnlock()

	if !exists {
		return fmt.Errorf("deferred tool not found: %s", name)
	}

	def, handler, err := d.loader(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to load deferred tool %s: %w", name, err)
	}

	into.Register(*def, handler)

	d.mu.Lock()
	d.loaded[name] = true
	d.mu.Unlock()

	return nil
}

// All returns all registered deferred tools.
func (d *DeferredToolRegistry) All() []DeferredTool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := make([]DeferredTool, 0, len(d.tools))
	for _, t := range d.tools {
		out = append(out, *t)
	}
	return out
}

// IsLoaded returns true if the named tool has been loaded.
func (d *DeferredToolRegistry) IsLoaded(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.loaded[name]
}

// RegisterToolSearchTool registers a "ToolSearch" tool in the given registry
// that allows the model to discover and load deferred tools on demand.
func RegisterToolSearchTool(registry *ToolRegistry, deferred *DeferredToolRegistry) {
	def := ToolDefinition{
		Name:        "ToolSearch",
		Description: "Search for and load additional tools by keyword. Returns matching tool names and descriptions.",
		InputSchema: ObjectSchema(map[string]any{
			"query":       StringParam("Keywords to search for tools"),
			"max_results": IntParam("Maximum number of results to return (default: 5)"),
		}, "query"),
	}

	type toolSearchInput struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}

	RegisterFunc(registry, def, func(ctx context.Context, input toolSearchInput) (string, error) {
		maxResults := input.MaxResults
		if maxResults <= 0 {
			maxResults = 5
		}

		matches := deferred.Search(input.Query, maxResults)
		if len(matches) == 0 {
			return fmt.Sprintf("No matching tools found for: %s", input.Query), nil
		}

		var loaded []string
		for _, m := range matches {
			if err := deferred.Load(ctx, m.Name, registry); err != nil {
				continue
			}
			loaded = append(loaded, fmt.Sprintf("- %s: %s", m.Name, m.Description))
		}

		if len(loaded) == 0 {
			return fmt.Sprintf("No matching tools found for: %s", input.Query), nil
		}

		return fmt.Sprintf("Loaded %d tools:\n%s", len(loaded), strings.Join(loaded, "\n")), nil
	})
}

// tokenizeQuery splits a string into lowercase words for deferred tool search.
// This is intentionally separate from the tokenize function in search.go.
func tokenizeQuery(s string) []string {
	return strings.Fields(strings.ToLower(s))
}

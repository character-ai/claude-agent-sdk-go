package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func testLoader(_ context.Context, name string) (*ToolDefinition, ToolHandler, error) {
	def := &ToolDefinition{
		Name:        name,
		Description: "Full definition of " + name,
		InputSchema: ObjectSchema(map[string]any{
			"input": StringParam("test input"),
		}),
	}
	handler := func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result from " + name, nil
	}
	return def, handler, nil
}

func failingLoader(_ context.Context, name string) (*ToolDefinition, ToolHandler, error) {
	return nil, nil, fmt.Errorf("loader error for %s", name)
}

func newTestDeferred() *DeferredToolRegistry {
	d := NewDeferredToolRegistry(testLoader)
	d.Add(DeferredTool{Name: "FileSearch", Description: "Search files by pattern", SearchHint: "glob find"})
	d.Add(DeferredTool{Name: "CodeGrep", Description: "Search code with regex", SearchHint: "grep ripgrep content"})
	d.Add(DeferredTool{Name: "GitBlame", Description: "Show git blame for a file", SearchHint: "git history author"})
	d.Add(DeferredTool{Name: "DatabaseQuery", Description: "Run SQL queries", SearchHint: "sql database postgres"})
	d.Add(DeferredTool{Name: "HTTPClient", Description: "Make HTTP requests", SearchHint: "http rest api curl fetch"})
	return d
}

func TestDeferredAddAndSearch(t *testing.T) {
	d := newTestDeferred()

	results := d.Search("search", 10)
	if len(results) == 0 {
		t.Fatal("expected results for 'search'")
	}

	// Both FileSearch and CodeGrep should match "search"
	names := make(map[string]bool)
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["FileSearch"] {
		t.Error("expected FileSearch in results")
	}
	if !names["CodeGrep"] {
		t.Error("expected CodeGrep in results")
	}
}

func TestDeferredSearchNoResults(t *testing.T) {
	d := newTestDeferred()
	results := d.Search("quantum entanglement", 10)
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestDeferredSearchMaxResults(t *testing.T) {
	d := newTestDeferred()
	// Query that matches multiple tools
	results := d.Search("search file code git sql http", 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestDeferredLoad(t *testing.T) {
	d := newTestDeferred()
	registry := NewToolRegistry()

	err := d.Load(context.Background(), "FileSearch", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !registry.Has("FileSearch") {
		t.Error("expected FileSearch to be registered")
	}

	result, err := registry.Execute(context.Background(), "FileSearch", json.RawMessage(`{"input":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result from FileSearch" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestDeferredLoadUnknown(t *testing.T) {
	d := newTestDeferred()
	registry := NewToolRegistry()

	err := d.Load(context.Background(), "NonExistent", registry)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDeferredIsLoaded(t *testing.T) {
	d := newTestDeferred()
	registry := NewToolRegistry()

	if d.IsLoaded("FileSearch") {
		t.Error("FileSearch should not be loaded yet")
	}

	err := d.Load(context.Background(), "FileSearch", registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !d.IsLoaded("FileSearch") {
		t.Error("FileSearch should be loaded")
	}

	if d.IsLoaded("CodeGrep") {
		t.Error("CodeGrep should not be loaded")
	}
}

func TestRegisterToolSearchTool(t *testing.T) {
	d := newTestDeferred()
	registry := NewToolRegistry()

	RegisterToolSearchTool(registry, d)

	if !registry.Has("ToolSearch") {
		t.Fatal("ToolSearch tool not registered")
	}

	// Execute with a query
	input := json.RawMessage(`{"query": "file search glob", "max_results": 3}`)
	result, err := registry.Execute(context.Background(), "ToolSearch", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// FileSearch should now be in the registry
	if !registry.Has("FileSearch") {
		t.Error("FileSearch should have been loaded into registry")
	}
}

func TestToolSearchEndToEnd(t *testing.T) {
	d := newTestDeferred()
	registry := NewToolRegistry()

	RegisterToolSearchTool(registry, d)

	// Verify tools are not yet in registry
	if registry.Has("DatabaseQuery") {
		t.Fatal("DatabaseQuery should not be registered yet")
	}

	// Search for database tools
	input := json.RawMessage(`{"query": "sql database"}`)
	result, err := registry.Execute(context.Background(), "ToolSearch", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !registry.Has("DatabaseQuery") {
		t.Error("DatabaseQuery should have been loaded into registry")
	}

	// Verify we can now execute the loaded tool
	dbResult, err := registry.Execute(context.Background(), "DatabaseQuery", json.RawMessage(`{"input":"SELECT 1"}`))
	if err != nil {
		t.Fatalf("unexpected error executing loaded tool: %v", err)
	}
	if dbResult != "result from DatabaseQuery" {
		t.Errorf("unexpected result: %s", dbResult)
	}

	// Verify IsLoaded tracks correctly
	if !d.IsLoaded("DatabaseQuery") {
		t.Error("DatabaseQuery should be marked as loaded")
	}

	// Verify the result message format
	if result == "" || result == fmt.Sprintf("No matching tools found for: %s", "sql database") {
		t.Error("expected successful load message")
	}
}

func TestToolSearchNoMatches(t *testing.T) {
	d := newTestDeferred()
	registry := NewToolRegistry()

	RegisterToolSearchTool(registry, d)

	input := json.RawMessage(`{"query": "quantum physics"}`)
	result, err := registry.Execute(context.Background(), "ToolSearch", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "No matching tools found for: quantum physics"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestDeferredLoadFailingLoader(t *testing.T) {
	d := NewDeferredToolRegistry(failingLoader)
	d.Add(DeferredTool{Name: "BadTool", Description: "A tool that fails to load"})

	registry := NewToolRegistry()
	err := d.Load(context.Background(), "BadTool", registry)
	if err == nil {
		t.Fatal("expected error from failing loader")
	}

	if d.IsLoaded("BadTool") {
		t.Error("BadTool should not be marked as loaded after failure")
	}
}

func TestDeferredAll(t *testing.T) {
	d := newTestDeferred()
	all := d.All()
	if len(all) != 5 {
		t.Errorf("expected 5 tools, got %d", len(all))
	}
}

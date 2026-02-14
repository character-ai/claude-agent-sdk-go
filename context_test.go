package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestContextBuilderFallbackNoIndex(t *testing.T) {
	store := NewStore()
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tool1"},
		Source:         "native",
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tool2"},
		Source:         "native",
	})

	cb := NewContextBuilder(store)
	defs := cb.SelectTools(context.Background(), "some query")
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools (fallback), got %d", len(defs))
	}
}

func TestContextBuilderFallbackEmptyQuery(t *testing.T) {
	store := NewStore()
	idx := NewBM25Index()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tool1"},
		Source:         "native",
	})

	cb := NewContextBuilder(store, WithIndex(idx))
	defs := cb.SelectTools(context.Background(), "")
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool (fallback on empty query), got %d", len(defs))
	}
}

func TestContextBuilderSelectToolsWithIndex(t *testing.T) {
	store := NewStore()
	idx := NewBM25Index()

	// Register two skills.
	webSkill := &StoredSkill{
		Name:        "web-research",
		Description: "Search and fetch web pages",
		Tags:        []string{"web"},
		Tools: []ToolDefinition{
			{Name: "web_search", Description: "Search the web"},
			{Name: "fetch_page", Description: "Fetch a web page"},
		},
		Handlers: map[string]ToolHandler{
			"web_search": func(ctx context.Context, input json.RawMessage) (string, error) { return "", nil },
			"fetch_page": func(ctx context.Context, input json.RawMessage) (string, error) { return "", nil },
		},
	}
	codeSkill := &StoredSkill{
		Name:        "code-gen",
		Description: "Generate and write code",
		Tags:        []string{"code"},
		Tools: []ToolDefinition{
			{Name: "write_code", Description: "Write code"},
		},
	}

	_ = store.InsertSkill(webSkill)
	_ = store.InsertSkill(codeSkill)

	// Insert tools into store.
	for _, def := range webSkill.Tools {
		_ = store.InsertTool(&StoredTool{
			ToolDefinition: def,
			Source:         "skill:web-research",
			Tags:           []string{"web"},
		})
	}
	for _, def := range codeSkill.Tools {
		_ = store.InsertTool(&StoredTool{
			ToolDefinition: def,
			Source:         "skill:code-gen",
			Tags:           []string{"code"},
		})
	}

	// Index skills.
	_ = idx.Index("web-research", "Search and fetch web pages", []string{"web"})
	_ = idx.Index("code-gen", "Generate and write code", []string{"code"})

	cb := NewContextBuilder(store, WithIndex(idx), WithMaxTools(20))

	// Query for web-related tools.
	defs := cb.SelectTools(context.Background(), "search the web for information")
	if len(defs) == 0 {
		t.Fatal("expected at least 1 tool")
	}

	// web_search should be in results.
	found := false
	for _, d := range defs {
		if d.Name == "web_search" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'web_search' to be in selected tools")
	}
}

func TestContextBuilderMaxTools(t *testing.T) {
	store := NewStore()
	idx := NewBM25Index()

	skill := &StoredSkill{
		Name:        "big-skill",
		Description: "A skill with many tools",
		Tools:       make([]ToolDefinition, 30),
	}
	for i := range 30 {
		name := "tool_" + string(rune('a'+i%26))
		skill.Tools[i] = ToolDefinition{Name: name, Description: "tool " + name}
	}
	_ = store.InsertSkill(skill)
	for _, def := range skill.Tools {
		_ = store.InsertTool(&StoredTool{ToolDefinition: def, Source: "skill:big-skill"})
	}
	_ = idx.Index("big-skill", "A skill with many tools", nil)

	cb := NewContextBuilder(store, WithIndex(idx), WithMaxTools(5))
	defs := cb.SelectTools(context.Background(), "tools")
	if len(defs) > 5 {
		t.Fatalf("expected at most 5 tools, got %d", len(defs))
	}
}

func TestContextBuilderSelectSkills(t *testing.T) {
	store := NewStore()
	idx := NewBM25Index()

	_ = store.InsertSkill(&StoredSkill{Name: "web", Description: "Web tools"})
	_ = store.InsertSkill(&StoredSkill{Name: "code", Description: "Code tools"})
	_ = idx.Index("web", "Web tools", nil)
	_ = idx.Index("code", "Code tools", nil)

	cb := NewContextBuilder(store, WithIndex(idx))
	skills := cb.SelectSkills(context.Background(), "web", 1)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "web" {
		t.Fatalf("expected 'web' skill, got %q", skills[0].Name)
	}
}

func TestContextBuilderSelectSkillsNoIndex(t *testing.T) {
	store := NewStore()

	_ = store.InsertSkill(&StoredSkill{Name: "s1"})
	_ = store.InsertSkill(&StoredSkill{Name: "s2"})

	cb := NewContextBuilder(store)
	skills := cb.SelectSkills(context.Background(), "anything", 10)
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills (fallback), got %d", len(skills))
	}
}

func TestContextBuilderWithDependencies(t *testing.T) {
	store := NewStore()
	idx := NewBM25Index()

	// Base skill.
	_ = store.InsertSkill(&StoredSkill{
		Name:        "text-processing",
		Description: "Text processing utilities",
		Tools: []ToolDefinition{
			{Name: "tokenize", Description: "Tokenize text"},
		},
	})

	// Dependent skill.
	_ = store.InsertSkill(&StoredSkill{
		Name:         "web-research",
		Description:  "Search and analyze web content",
		Dependencies: []string{"text-processing"},
		Tools: []ToolDefinition{
			{Name: "web_search", Description: "Search the web"},
		},
	})

	// Store tools.
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tokenize"},
		Source:         "skill:text-processing",
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "web_search"},
		Source:         "skill:web-research",
	})

	_ = idx.Index("web-research", "Search and analyze web content", nil)
	_ = idx.Index("text-processing", "Text processing utilities", nil)

	cb := NewContextBuilder(store, WithIndex(idx))
	defs := cb.SelectTools(context.Background(), "search web content")

	// Should include both web_search (direct) and tokenize (dependency).
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
	}

	if !names["web_search"] {
		t.Fatal("expected web_search in results")
	}
	if !names["tokenize"] {
		t.Fatal("expected tokenize from dependency in results")
	}
}

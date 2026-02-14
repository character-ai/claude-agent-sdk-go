package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSkillRegistryRegisterAndGet(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	tools := NewToolRegistryWithStore(NewStore())
	tools.Register(ToolDefinition{
		Name:        "web_search",
		Description: "Search the web",
	}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "results", nil
	})

	skill := Skill{
		Name:        "web-research",
		Description: "Research things on the web",
		Tags:        []string{"web", "research"},
		Category:    "research",
	}

	if err := sr.Register(skill, tools); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, err := sr.Get("web-research")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected skill, got nil")
	}
	if got.Name != "web-research" {
		t.Fatalf("expected name 'web-research', got %q", got.Name)
	}
	if got.Category != "research" {
		t.Fatalf("expected category 'research', got %q", got.Category)
	}
}

func TestSkillRegistryToolsStoredWithSource(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	tools := NewToolRegistryWithStore(NewStore())
	tools.Register(ToolDefinition{Name: "fetch_page"}, nil)

	err := sr.Register(Skill{
		Name: "web",
		Tags: []string{"web"},
	}, tools)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	skillTools, err := store.ListToolsBySource("skill:web")
	if err != nil {
		t.Fatalf("ListToolsBySource failed: %v", err)
	}
	if len(skillTools) != 1 {
		t.Fatalf("expected 1 tool with source 'skill:web', got %d", len(skillTools))
	}
	if skillTools[0].Name != "fetch_page" {
		t.Fatalf("expected tool name 'fetch_page', got %q", skillTools[0].Name)
	}
}

func TestSkillRegistryRemove(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	tools := NewToolRegistryWithStore(NewStore())
	tools.Register(ToolDefinition{Name: "tool_a"}, nil)
	tools.Register(ToolDefinition{Name: "tool_b"}, nil)

	_ = sr.Register(Skill{Name: "removable"}, tools)

	if err := sr.Remove("removable"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	got, _ := sr.Get("removable")
	if got != nil {
		t.Fatal("expected skill to be removed")
	}

	// Tools should also be removed.
	remaining, _ := store.ListToolsBySource("skill:removable")
	if len(remaining) != 0 {
		t.Fatalf("expected 0 tools after removal, got %d", len(remaining))
	}
}

func TestSkillRegistryByTag(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	_ = sr.Register(Skill{Name: "s1", Tags: []string{"alpha"}}, nil)
	_ = sr.Register(Skill{Name: "s2", Tags: []string{"alpha", "beta"}}, nil)
	_ = sr.Register(Skill{Name: "s3", Tags: []string{"beta"}}, nil)

	alpha, _ := sr.ByTag("alpha")
	if len(alpha) != 2 {
		t.Fatalf("expected 2 alpha-tagged skills, got %d", len(alpha))
	}

	beta, _ := sr.ByTag("beta")
	if len(beta) != 2 {
		t.Fatalf("expected 2 beta-tagged skills, got %d", len(beta))
	}
}

func TestSkillRegistryByCategory(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	_ = sr.Register(Skill{Name: "s1", Category: "research"}, nil)
	_ = sr.Register(Skill{Name: "s2", Category: "coding"}, nil)
	_ = sr.Register(Skill{Name: "s3", Category: "research"}, nil)

	research, _ := sr.ByCategory("research")
	if len(research) != 2 {
		t.Fatalf("expected 2 research skills, got %d", len(research))
	}
}

func TestSkillRegistryAll(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	_ = sr.Register(Skill{Name: "a"}, nil)
	_ = sr.Register(Skill{Name: "b"}, nil)

	all, _ := sr.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(all))
	}
}

func TestSkillRegistryResolve(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	// Create base skill.
	baseTools := NewToolRegistryWithStore(NewStore())
	baseTools.Register(ToolDefinition{Name: "tokenize"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "tokens", nil
	})
	_ = sr.Register(Skill{Name: "text-processing"}, baseTools)

	// Create dependent skill.
	webTools := NewToolRegistryWithStore(NewStore())
	webTools.Register(ToolDefinition{Name: "web_search"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "results", nil
	})
	_ = sr.Register(Skill{
		Name:         "web-research",
		Dependencies: []string{"text-processing"},
	}, webTools)

	// Resolve web-research — should include text-processing tools too.
	resolved, err := sr.Resolve("web-research")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	defs := resolved.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools after resolve, got %d", len(defs))
	}

	if !resolved.Has("tokenize") {
		t.Fatal("expected resolved registry to have 'tokenize' from dependency")
	}
	if !resolved.Has("web_search") {
		t.Fatal("expected resolved registry to have 'web_search'")
	}
}

func TestSkillRegistryResolveNotFound(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	_, err := sr.Resolve("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestSkillRegistryResolveCyclicDeps(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	// Create two skills that depend on each other.
	_ = sr.Register(Skill{Name: "a", Dependencies: []string{"b"}}, nil)
	_ = sr.Register(Skill{Name: "b", Dependencies: []string{"a"}}, nil)

	// Should not hang — resolved map prevents infinite recursion.
	_, err := sr.Resolve("a")
	if err != nil {
		t.Fatalf("Resolve should handle cyclic deps, got error: %v", err)
	}
}

func TestSkillRegistryRegisterNilTools(t *testing.T) {
	store := NewStore()
	sr := NewSkillRegistry(store)

	// Should not panic.
	err := sr.Register(Skill{Name: "no-tools", Description: "A skill with no tools"}, nil)
	if err != nil {
		t.Fatalf("Register with nil tools should succeed: %v", err)
	}

	got, _ := sr.Get("no-tools")
	if got == nil {
		t.Fatal("expected skill to be stored")
	}
}

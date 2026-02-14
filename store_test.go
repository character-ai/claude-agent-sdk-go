package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestStoreInsertAndGetTool(t *testing.T) {
	store := NewStore()

	tool := &StoredTool{
		ToolDefinition: ToolDefinition{
			Name:        "search",
			Description: "Search the web",
			InputSchema: map[string]any{"type": "object"},
		},
		Source: "native",
		Tags:   []string{"web", "search"},
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "result", nil
		},
	}

	if err := store.InsertTool(tool); err != nil {
		t.Fatalf("InsertTool failed: %v", err)
	}

	got, err := store.GetTool("search")
	if err != nil {
		t.Fatalf("GetTool failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected tool, got nil")
	}
	if got.Name != "search" {
		t.Fatalf("expected name 'search', got %q", got.Name)
	}
	if got.Source != "native" {
		t.Fatalf("expected source 'native', got %q", got.Source)
	}
}

func TestStoreGetToolNotFound(t *testing.T) {
	store := NewStore()
	got, err := store.GetTool("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent tool")
	}
}

func TestStoreDeleteTool(t *testing.T) {
	store := NewStore()

	tool := &StoredTool{
		ToolDefinition: ToolDefinition{Name: "temp"},
		Source:         "native",
	}
	_ = store.InsertTool(tool)

	if err := store.DeleteTool("temp"); err != nil {
		t.Fatalf("DeleteTool failed: %v", err)
	}

	got, _ := store.GetTool("temp")
	if got != nil {
		t.Fatal("expected tool to be deleted")
	}
}

func TestStoreDeleteToolNotFound(t *testing.T) {
	store := NewStore()
	// Deleting a nonexistent tool should not error.
	if err := store.DeleteTool("ghost"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreListTools(t *testing.T) {
	store := NewStore()

	for _, name := range []string{"a", "b", "c"} {
		_ = store.InsertTool(&StoredTool{
			ToolDefinition: ToolDefinition{Name: name},
			Source:         "native",
		})
	}

	tools, err := store.ListTools()
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
}

func TestStoreListToolsBySource(t *testing.T) {
	store := NewStore()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "native_tool"},
		Source:         "native",
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "mcp_tool"},
		Source:         "mcp:server1",
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "skill_tool"},
		Source:         "skill:research",
	})

	native, _ := store.ListToolsBySource("native")
	if len(native) != 1 {
		t.Fatalf("expected 1 native tool, got %d", len(native))
	}

	mcp, _ := store.ListToolsBySource("mcp:server1")
	if len(mcp) != 1 {
		t.Fatalf("expected 1 mcp tool, got %d", len(mcp))
	}
}

func TestStoreListToolsByTag(t *testing.T) {
	store := NewStore()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "web_search"},
		Source:         "native",
		Tags:           []string{"web", "search"},
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "file_search"},
		Source:         "native",
		Tags:           []string{"file", "search"},
	})

	searchTools, _ := store.ListToolsByTag("search")
	if len(searchTools) != 2 {
		t.Fatalf("expected 2 search-tagged tools, got %d", len(searchTools))
	}

	webTools, _ := store.ListToolsByTag("web")
	if len(webTools) != 1 {
		t.Fatalf("expected 1 web-tagged tool, got %d", len(webTools))
	}
}

func TestStoreInsertToolUpsert(t *testing.T) {
	store := NewStore()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tool1", Description: "v1"},
		Source:         "native",
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tool1", Description: "v2"},
		Source:         "native",
	})

	got, _ := store.GetTool("tool1")
	if got.Description != "v2" {
		t.Fatalf("expected upsert to update description to 'v2', got %q", got.Description)
	}

	tools, _ := store.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool after upsert, got %d", len(tools))
	}
}

// --- Skill operations ---

func TestStoreInsertAndGetSkill(t *testing.T) {
	store := NewStore()

	skill := &StoredSkill{
		Name:        "web-research",
		Description: "Research things on the web",
		Tags:        []string{"web", "research"},
		Category:    "research",
	}

	if err := store.InsertSkill(skill); err != nil {
		t.Fatalf("InsertSkill failed: %v", err)
	}

	got, err := store.GetSkill("web-research")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected skill, got nil")
	}
	if got.Category != "research" {
		t.Fatalf("expected category 'research', got %q", got.Category)
	}
}

func TestStoreDeleteSkill(t *testing.T) {
	store := NewStore()

	_ = store.InsertSkill(&StoredSkill{Name: "temp-skill"})
	if err := store.DeleteSkill("temp-skill"); err != nil {
		t.Fatalf("DeleteSkill failed: %v", err)
	}

	got, _ := store.GetSkill("temp-skill")
	if got != nil {
		t.Fatal("expected skill to be deleted")
	}
}

func TestStoreListSkills(t *testing.T) {
	store := NewStore()

	_ = store.InsertSkill(&StoredSkill{Name: "a", Category: "cat1"})
	_ = store.InsertSkill(&StoredSkill{Name: "b", Category: "cat1"})
	_ = store.InsertSkill(&StoredSkill{Name: "c", Category: "cat2"})

	all, _ := store.ListSkills()
	if len(all) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(all))
	}

	cat1, _ := store.ListSkillsByCategory("cat1")
	if len(cat1) != 2 {
		t.Fatalf("expected 2 cat1 skills, got %d", len(cat1))
	}
}

func TestStoreListSkillsByTag(t *testing.T) {
	store := NewStore()

	_ = store.InsertSkill(&StoredSkill{Name: "s1", Tags: []string{"alpha", "beta"}})
	_ = store.InsertSkill(&StoredSkill{Name: "s2", Tags: []string{"beta", "gamma"}})

	beta, _ := store.ListSkillsByTag("beta")
	if len(beta) != 2 {
		t.Fatalf("expected 2 beta-tagged skills, got %d", len(beta))
	}

	gamma, _ := store.ListSkillsByTag("gamma")
	if len(gamma) != 1 {
		t.Fatalf("expected 1 gamma-tagged skill, got %d", len(gamma))
	}
}

// --- Hook operations ---

func TestStoreInsertAndListHooks(t *testing.T) {
	store := NewStore()

	hook := &StoredHook{
		Pattern: "test_tool",
	}
	if err := store.InsertHook(hook); err != nil {
		t.Fatalf("InsertHook failed: %v", err)
	}

	// InsertHook should NOT mutate caller's struct (copies internally).
	if hook.ID != "" {
		t.Fatal("expected caller struct to remain unmutated")
	}

	hooks, err := store.ListHooks()
	if err != nil {
		t.Fatalf("ListHooks failed: %v", err)
	}
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].ID == "" {
		t.Fatal("expected stored hook to have auto-generated ID")
	}
}

func TestStoreDeleteHook(t *testing.T) {
	store := NewStore()

	hook := &StoredHook{Pattern: "delete_me"}
	_ = store.InsertHook(hook)
	id := hook.ID

	if err := store.DeleteHook(id); err != nil {
		t.Fatalf("DeleteHook failed: %v", err)
	}

	got, _ := store.GetHook(id)
	if got != nil {
		t.Fatal("expected hook to be deleted")
	}
}

func TestStoreListHooksByPattern(t *testing.T) {
	store := NewStore()

	_ = store.InsertHook(&StoredHook{Pattern: "tool_a"})
	_ = store.InsertHook(&StoredHook{Pattern: "tool_a"})
	_ = store.InsertHook(&StoredHook{Pattern: "tool_b"})

	hooks, _ := store.ListHooksByPattern("tool_a")
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks for pattern 'tool_a', got %d", len(hooks))
	}
}

// --- Snapshot ---

func TestStoreSnapshot(t *testing.T) {
	store := NewStore()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "snap_tool"},
		Source:         "native",
		Tags:           []string{"snap"},
	})
	_ = store.InsertSkill(&StoredSkill{
		Name:     "snap_skill",
		Category: "testing",
	})

	snap := store.Snapshot()

	tools, _ := snap.Tools()
	if len(tools) != 1 {
		t.Fatalf("snapshot expected 1 tool, got %d", len(tools))
	}

	skills, _ := snap.Skills()
	if len(skills) != 1 {
		t.Fatalf("snapshot expected 1 skill, got %d", len(skills))
	}

	// Snapshot isolation: insert after snapshot should not be visible.
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "new_tool"},
		Source:         "native",
	})

	tools2, _ := snap.Tools()
	if len(tools2) != 1 {
		t.Fatalf("snapshot should be isolated, expected 1 tool, got %d", len(tools2))
	}
}

func TestStoreSnapshotBySource(t *testing.T) {
	store := NewStore()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "t1"},
		Source:         "native",
	})
	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "t2"},
		Source:         "skill:web",
	})

	snap := store.Snapshot()

	native, _ := snap.ToolsBySource("native")
	if len(native) != 1 {
		t.Fatalf("expected 1 native tool in snapshot, got %d", len(native))
	}

	skill, _ := snap.ToolsBySource("skill:web")
	if len(skill) != 1 {
		t.Fatalf("expected 1 skill tool in snapshot, got %d", len(skill))
	}
}

func TestStoreSnapshotByTag(t *testing.T) {
	store := NewStore()

	_ = store.InsertTool(&StoredTool{
		ToolDefinition: ToolDefinition{Name: "tagged"},
		Source:         "native",
		Tags:           []string{"important"},
	})

	snap := store.Snapshot()
	tagged, _ := snap.ToolsByTag("important")
	if len(tagged) != 1 {
		t.Fatalf("expected 1 tagged tool, got %d", len(tagged))
	}
}

func TestStoreSnapshotSkillsByCategory(t *testing.T) {
	store := NewStore()

	_ = store.InsertSkill(&StoredSkill{Name: "s1", Category: "research"})
	_ = store.InsertSkill(&StoredSkill{Name: "s2", Category: "coding"})

	snap := store.Snapshot()
	research, _ := snap.SkillsByCategory("research")
	if len(research) != 1 {
		t.Fatalf("expected 1 research skill, got %d", len(research))
	}
}

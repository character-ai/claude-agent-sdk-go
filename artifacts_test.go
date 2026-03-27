package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestArtifactRegistry_CreateAndGet(t *testing.T) {
	reg := NewArtifactRegistry()
	tools := reg.Tools()

	// Verify tools are registered
	if !tools.Has("create_artifact") {
		t.Fatal("expected create_artifact tool")
	}
	if !tools.Has("update_artifact") {
		t.Fatal("expected update_artifact tool")
	}

	ctx := context.Background()

	// Create an HTML artifact
	input, _ := json.Marshal(map[string]string{
		"type":    "html",
		"title":   "My Page",
		"content": "<!DOCTYPE html><html><body><h1>Hello</h1></body></html>",
	})

	result, err := tools.Execute(ctx, "create_artifact", input)
	if err != nil {
		t.Fatalf("create_artifact failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Verify it was stored
	if reg.Count() != 1 {
		t.Fatalf("expected 1 artifact, got %d", reg.Count())
	}

	artifacts := reg.All()
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	a := artifacts[0]
	if a.ID != "artifact_1" {
		t.Errorf("expected id artifact_1, got %s", a.ID)
	}
	if a.Type != ArtifactHTML {
		t.Errorf("expected type html, got %s", a.Type)
	}
	if a.Title != "My Page" {
		t.Errorf("expected title 'My Page', got %s", a.Title)
	}
	if a.Version != 1 {
		t.Errorf("expected version 1, got %d", a.Version)
	}

	// Get by ID
	got, ok := reg.Get("artifact_1")
	if !ok {
		t.Fatal("expected to find artifact_1")
	}
	if got.Title != "My Page" {
		t.Errorf("expected title 'My Page', got %s", got.Title)
	}

	// Get missing
	_, ok = reg.Get("artifact_999")
	if ok {
		t.Fatal("expected artifact_999 to be missing")
	}
}

func TestArtifactRegistry_Update(t *testing.T) {
	reg := NewArtifactRegistry()
	tools := reg.Tools()
	ctx := context.Background()

	// Create
	input, _ := json.Marshal(map[string]string{
		"type":    "text",
		"title":   "Notes",
		"content": "version 1",
	})
	_, err := tools.Execute(ctx, "create_artifact", input)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Update content only
	updateInput, _ := json.Marshal(map[string]string{
		"id":      "artifact_1",
		"content": "version 2",
	})
	_, err = tools.Execute(ctx, "update_artifact", updateInput)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	a, _ := reg.Get("artifact_1")
	if a.Content != "version 2" {
		t.Errorf("expected 'version 2', got %q", a.Content)
	}
	if a.Title != "Notes" {
		t.Errorf("title should be unchanged, got %q", a.Title)
	}
	if a.Version != 2 {
		t.Errorf("expected version 2, got %d", a.Version)
	}

	// Update with new title
	updateInput2, _ := json.Marshal(map[string]string{
		"id":      "artifact_1",
		"title":   "Updated Notes",
		"content": "version 3",
	})
	_, err = tools.Execute(ctx, "update_artifact", updateInput2)
	if err != nil {
		t.Fatalf("update with title failed: %v", err)
	}

	a, _ = reg.Get("artifact_1")
	if a.Title != "Updated Notes" {
		t.Errorf("expected 'Updated Notes', got %q", a.Title)
	}
	if a.Version != 3 {
		t.Errorf("expected version 3, got %d", a.Version)
	}
}

func TestArtifactRegistry_UpdateNotFound(t *testing.T) {
	reg := NewArtifactRegistry()
	tools := reg.Tools()
	ctx := context.Background()

	input, _ := json.Marshal(map[string]string{
		"id":      "nonexistent",
		"content": "hello",
	})
	_, err := tools.Execute(ctx, "update_artifact", input)
	if err == nil {
		t.Fatal("expected error for nonexistent artifact")
	}
}

func TestArtifactRegistry_ValidationErrors(t *testing.T) {
	reg := NewArtifactRegistry()
	tools := reg.Tools()
	ctx := context.Background()

	tests := []struct {
		name  string
		input map[string]string
	}{
		{"missing type", map[string]string{"type": "invalid", "title": "t", "content": "c"}},
		{"missing title", map[string]string{"type": "html", "title": "", "content": "c"}},
		{"missing content", map[string]string{"type": "html", "title": "t", "content": ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(tt.input)
			_, err := tools.Execute(ctx, "create_artifact", input)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestArtifactRegistry_AllTypes(t *testing.T) {
	reg := NewArtifactRegistry()
	tools := reg.Tools()
	ctx := context.Background()

	for _, typ := range []string{"html", "jsx", "text"} {
		input, _ := json.Marshal(map[string]string{
			"type":    typ,
			"title":   typ + " artifact",
			"content": "content for " + typ,
		})
		_, err := tools.Execute(ctx, "create_artifact", input)
		if err != nil {
			t.Fatalf("failed to create %s artifact: %v", typ, err)
		}
	}

	if reg.Count() != 3 {
		t.Fatalf("expected 3 artifacts, got %d", reg.Count())
	}

	// Verify order
	all := reg.All()
	if all[0].Type != ArtifactHTML || all[1].Type != ArtifactJSX || all[2].Type != ArtifactText {
		t.Error("artifacts not in creation order")
	}
}

func TestArtifactRegistry_SystemPrompt(t *testing.T) {
	reg := NewArtifactRegistry()
	prompt := reg.SystemPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty system prompt")
	}
}

func TestArtifactRegistry_MultipleCreatesOrdering(t *testing.T) {
	reg := NewArtifactRegistry()
	tools := reg.Tools()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		input, _ := json.Marshal(map[string]string{
			"type":    "text",
			"title":   "Item",
			"content": "content",
		})
		_, _ = tools.Execute(ctx, "create_artifact", input)
	}

	all := reg.All()
	for i, a := range all {
		expectedID := fmt.Sprintf("artifact_%d", i+1)
		if a.ID != expectedID {
			t.Errorf("index %d: expected %s, got %s", i, expectedID, a.ID)
		}
	}
}

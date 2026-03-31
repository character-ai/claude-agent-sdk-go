package claudeagent

import (
	"testing"
)

func TestSystemPromptBlocksOverridesSystemPrompt(t *testing.T) {
	blocks := []SystemPromptBlock{
		{Text: "block one"},
		{Text: "block two", CacheControl: &CacheControl{Type: "ephemeral"}},
	}

	agent := NewAPIAgent(APIAgentConfig{
		APIKey:             "test-key",
		SystemPrompt:       "should be ignored",
		SystemPromptBlocks: blocks,
	})

	if agent.system != "" {
		t.Errorf("expected system to be empty when SystemPromptBlocks is set, got %q", agent.system)
	}
	if len(agent.systemBlocks) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(agent.systemBlocks))
	}
	if agent.systemBlocks[0].Text != "block one" {
		t.Errorf("unexpected block[0] text: %q", agent.systemBlocks[0].Text)
	}
	if agent.systemBlocks[1].CacheControl == nil {
		t.Error("expected block[1] to have CacheControl set")
	}
}

func TestSystemPromptBackwardCompat(t *testing.T) {
	agent := NewAPIAgent(APIAgentConfig{
		APIKey:       "test-key",
		SystemPrompt: "hello system",
	})

	if agent.system != "hello system" {
		t.Errorf("expected system %q, got %q", "hello system", agent.system)
	}
	if len(agent.systemBlocks) != 0 {
		t.Errorf("expected no system blocks, got %d", len(agent.systemBlocks))
	}
}

func TestSystemPromptBlockConstruction(t *testing.T) {
	b := SystemPromptBlock{
		Text:         "cached section",
		CacheControl: &CacheControl{Type: "ephemeral"},
	}

	if b.Text != "cached section" {
		t.Errorf("unexpected text: %q", b.Text)
	}
	if b.CacheControl == nil || b.CacheControl.Type != "ephemeral" {
		t.Error("expected cache control with type ephemeral")
	}

	plain := SystemPromptBlock{Text: "plain section"}
	if plain.CacheControl != nil {
		t.Error("expected nil cache control for plain block")
	}
}

func TestEmptyBlocksUsesSystemPrompt(t *testing.T) {
	agent := NewAPIAgent(APIAgentConfig{
		APIKey:             "test-key",
		SystemPrompt:       "fallback",
		SystemPromptBlocks: []SystemPromptBlock{},
	})

	// Empty slice (not nil) should still fall back to SystemPrompt
	if agent.system != "fallback" {
		t.Errorf("expected system %q with empty blocks, got %q", "fallback", agent.system)
	}
}

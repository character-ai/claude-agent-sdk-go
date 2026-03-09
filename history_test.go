package claudeagent

import (
	"testing"
)

func makeHistory(pairs int) []ConversationMessage {
	// initial user prompt + pairs of (assistant + tool-result)
	h := []ConversationMessage{{Role: "user", Content: "initial"}}
	for i := 0; i < pairs; i++ {
		h = append(h, ConversationMessage{Role: "assistant", Content: "reply", ToolCalls: []ToolCall{{ID: "tc", Name: "t"}}})
		h = append(h, ConversationMessage{Role: "tool", ToolCallID: "tc", Content: "result"})
	}
	return h
}

func TestCompactHistoryNilConfig(t *testing.T) {
	h := makeHistory(5)
	got := compactHistory(h, nil)
	if len(got) != len(h) {
		t.Errorf("nil config: expected %d messages, got %d", len(h), len(got))
	}
}

func TestCompactHistoryZeroMaxTurns(t *testing.T) {
	h := makeHistory(5)
	got := compactHistory(h, &HistoryConfig{MaxTurns: 0})
	if len(got) != len(h) {
		t.Errorf("MaxTurns=0: expected %d messages unchanged, got %d", len(h), len(got))
	}
}

func TestCompactHistoryMaxTurnsRollingWindow(t *testing.T) {
	h := makeHistory(5) // 1 initial + 5*2 = 11 messages total

	got := compactHistory(h, &HistoryConfig{MaxTurns: 2})

	// Should be: initial + last 2 turns = 1 + 4 = 5 messages
	want := 5
	if len(got) != want {
		t.Errorf("MaxTurns=2: expected %d messages, got %d", want, len(got))
	}

	// First message is always the initial prompt
	if got[0].Role != "user" || got[0].Content != "initial" {
		t.Errorf("first message should be initial user prompt, got: %+v", got[0])
	}

	// All returned messages should be either assistant or tool (the last 2 turns)
	for _, msg := range got[1:] {
		if msg.Role != "assistant" && msg.Role != "tool" {
			t.Errorf("expected assistant/tool role in compacted history, got: %s", msg.Role)
		}
	}
}

func TestCompactHistoryPreservesAllWhenUnderLimit(t *testing.T) {
	h := makeHistory(2) // 1 + 4 = 5 messages
	got := compactHistory(h, &HistoryConfig{MaxTurns: 5})
	if len(got) != len(h) {
		t.Errorf("under limit: expected all %d messages preserved, got %d", len(h), len(got))
	}
}

func TestCompactHistoryDropToolResults(t *testing.T) {
	h := makeHistory(3) // 1 + 6 = 7 messages

	got := compactHistory(h, &HistoryConfig{MaxTurns: 2, DropToolResults: true})

	// MaxTurns=2: keep last 2 turns (4 messages) + initial = 5
	// But turn 0 (the now-dropped oldest) is gone.
	// The remaining 2 turns: only the oldest of those 2 gets DropToolResults.
	// turn index 0 of kept turns (= originally turn 1) → old → tool result dropped
	// turn index 1 of kept turns (= originally turn 2) → latest → tool result kept

	if len(got) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(got))
	}

	// Find tool results — older turn's result should be omitted, latest kept
	toolResults := []ConversationMessage{}
	for _, m := range got {
		if m.Role == "tool" {
			toolResults = append(toolResults, m)
		}
	}
	if len(toolResults) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(toolResults))
	}
	if toolResults[0].Content != "[tool result omitted]" {
		t.Errorf("older tool result should be omitted, got: %q", toolResults[0].Content)
	}
	if toolResults[1].Content == "[tool result omitted]" {
		t.Errorf("latest tool result should NOT be omitted")
	}
}

func TestCompactHistoryOriginalUnmodified(t *testing.T) {
	h := makeHistory(5)
	original := make([]ConversationMessage, len(h))
	copy(original, h)

	compactHistory(h, &HistoryConfig{MaxTurns: 2, DropToolResults: true})

	for i, msg := range h {
		if msg.Role != original[i].Role || msg.Content != original[i].Content {
			t.Errorf("compactHistory modified original slice at index %d", i)
		}
	}
}

func TestCompactHistorySingleMessage(t *testing.T) {
	h := []ConversationMessage{{Role: "user", Content: "hi"}}
	got := compactHistory(h, &HistoryConfig{MaxTurns: 1})
	if len(got) != 1 {
		t.Errorf("single message: expected 1, got %d", len(got))
	}
}

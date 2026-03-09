package claudeagent

import "github.com/anthropics/anthropic-sdk-go"

// HistoryConfig controls conversation history compaction applied before each
// LLM call. This prevents the context window from growing unboundedly in
// long multi-turn sessions.
type HistoryConfig struct {
	// MaxTurns keeps only the last N assistant+tool turns in the history sent
	// to the LLM. The initial user prompt is always preserved. 0 = unlimited.
	// A "turn" is one assistant response and all its tool result messages.
	MaxTurns int

	// DropToolResults replaces tool-result content from older turns with a
	// short placeholder, reducing token usage while preserving conversation
	// structure. Has no effect unless MaxTurns > 0 and older turns exist.
	// Not supported for APIAgent (use MaxTurns only).
	DropToolResults bool
}

// compactHistory returns a compacted view of the CLI agent's ConversationMessage
// history. The original slice is never modified.
func compactHistory(history []ConversationMessage, cfg *HistoryConfig) []ConversationMessage {
	if cfg == nil || (cfg.MaxTurns == 0 && !cfg.DropToolResults) {
		return history
	}
	if len(history) <= 1 {
		return history
	}

	initial := history[0]
	rest := history[1:]

	// Group rest into turns: each turn starts at an assistant message and
	// includes all consecutive tool-result messages that follow it.
	type span struct{ start, end int }
	var turns []span
	for i := 0; i < len(rest); {
		if rest[i].Role != "assistant" {
			i++
			continue
		}
		s := span{start: i}
		i++
		for i < len(rest) && rest[i].Role == "tool" {
			i++
		}
		s.end = i
		turns = append(turns, s)
	}

	// Apply rolling window: keep only the last MaxTurns turns.
	if cfg.MaxTurns > 0 && len(turns) > cfg.MaxTurns {
		turns = turns[len(turns)-cfg.MaxTurns:]
	}

	// Rebuild the compacted history.
	out := make([]ConversationMessage, 0, len(history))
	out = append(out, initial)
	for i, t := range turns {
		isOldTurn := i < len(turns)-1 // all but the most recent turn are "old"
		for _, msg := range rest[t.start:t.end] {
			if isOldTurn && cfg.DropToolResults && msg.Role == "tool" {
				out = append(out, ConversationMessage{
					Role:       msg.Role,
					ToolCallID: msg.ToolCallID,
					Content:    "[tool result omitted]",
				})
			} else {
				out = append(out, msg)
			}
		}
	}
	return out
}

// compactMessages returns a compacted view of the API agent's message list.
// Only MaxTurns is applied; DropToolResults is not supported for APIAgent
// because modifying anthropic.MessageParam tool-result blocks while keeping
// the tool-use IDs valid is non-trivial and best handled via MaxTurns alone.
func compactMessages(messages []anthropic.MessageParam, cfg *HistoryConfig) []anthropic.MessageParam {
	if cfg == nil || cfg.MaxTurns == 0 {
		return messages
	}
	if len(messages) <= 1 {
		return messages
	}

	// Layout:
	//   messages[0]           = initial user prompt (always kept)
	//   messages[1], [2], ... = alternating AssistantMessage / UserMessage(tool results)
	// Each turn = 1 assistant + 1 user pair = 2 messages.
	rest := messages[1:]
	maxRest := cfg.MaxTurns * 2
	if len(rest) <= maxRest {
		return messages
	}

	out := make([]anthropic.MessageParam, 0, 1+maxRest)
	out = append(out, messages[0])
	out = append(out, rest[len(rest)-maxRest:]...)
	return out
}

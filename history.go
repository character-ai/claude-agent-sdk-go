package claudeagent

import (
	"context"
	"fmt"
)

// HistorySummarizer summarizes a slice of conversation messages into a single
// summary string. The caller provides the implementation (e.g., calling an LLM).
type HistorySummarizer func(ctx context.Context, messages []ConversationMessage) (string, error)

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

	// Summarizer, if set, is called to produce a summary of turns that would
	// otherwise be dropped when MaxTurns is exceeded. The summary is prepended
	// as a system-context user message after the initial prompt.
	Summarizer HistorySummarizer

	// SummarizeThreshold is the number of excess turns before summarization
	// triggers. For example, if MaxTurns=10 and SummarizeThreshold=5, the
	// summarizer is called when history reaches 15 turns. Default: 0.
	SummarizeThreshold int
}

// compactHistory returns a compacted view of the CLI agent's ConversationMessage
// history. The original slice is never modified.
func compactHistory(ctx context.Context, history []ConversationMessage, cfg *HistoryConfig) []ConversationMessage {
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

	// Determine which turns to drop and which to keep.
	var droppedTurns []span
	if cfg.MaxTurns > 0 && len(turns) > cfg.MaxTurns {
		droppedTurns = turns[:len(turns)-cfg.MaxTurns]
		turns = turns[len(turns)-cfg.MaxTurns:]
	}

	// Optionally summarize dropped turns.
	var summaryMsg *ConversationMessage
	if len(droppedTurns) > 0 && cfg.Summarizer != nil && len(droppedTurns) >= cfg.SummarizeThreshold {
		// Collect dropped messages for the summarizer.
		var dropped []ConversationMessage
		for _, t := range droppedTurns {
			dropped = append(dropped, rest[t.start:t.end]...)
		}
		summary, err := cfg.Summarizer(ctx, dropped)
		if err == nil {
			summaryMsg = &ConversationMessage{
				Role:    "user",
				Content: fmt.Sprintf("[Previous conversation summary]\n%s", summary),
			}
		}
		// On error, fall back to drop behavior (no summary prepended).
	}

	// Rebuild the compacted history.
	out := make([]ConversationMessage, 0, len(history))
	out = append(out, initial)
	if summaryMsg != nil {
		out = append(out, *summaryMsg)
	}
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

package claudeagent

import "strings"

// SystemReminder represents mid-conversation context to inject via XML tags.
type SystemReminder struct {
	Content string
}

// FormatReminder wraps content in <system-reminder> XML tags.
func FormatReminder(r SystemReminder) string {
	return "<system-reminder>\n" + r.Content + "\n</system-reminder>"
}

// FormatReminders formats multiple reminders into a single string.
func FormatReminders(reminders []SystemReminder) string {
	parts := make([]string, len(reminders))
	for i, r := range reminders {
		parts[i] = FormatReminder(r)
	}
	return strings.Join(parts, "\n")
}

// InjectReminders appends formatted reminders to the last user message in the history.
// If there are no user messages, the reminders are prepended as a new user message.
// Returns a new slice without mutating the input.
func InjectReminders(history []ConversationMessage, reminders []SystemReminder) []ConversationMessage {
	if len(reminders) == 0 {
		// Return a copy to avoid aliasing, but content is unchanged.
		result := make([]ConversationMessage, len(history))
		copy(result, history)
		return result
	}

	formatted := FormatReminders(reminders)

	// Make a deep-enough copy of the slice.
	result := make([]ConversationMessage, len(history))
	copy(result, history)

	// Find the last user message.
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role == "user" {
			result[i].Content = result[i].Content + "\n" + formatted
			return result
		}
	}

	// No user message found — prepend one.
	msg := ConversationMessage{
		Role:    "user",
		Content: formatted,
	}
	return append([]ConversationMessage{msg}, result...)
}

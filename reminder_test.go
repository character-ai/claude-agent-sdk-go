package claudeagent

import (
	"strings"
	"testing"
)

func TestFormatReminder(t *testing.T) {
	r := SystemReminder{Content: "Hello world"}
	got := FormatReminder(r)
	want := "<system-reminder>\nHello world\n</system-reminder>"
	if got != want {
		t.Errorf("FormatReminder() = %q, want %q", got, want)
	}
}

func TestFormatReminders(t *testing.T) {
	reminders := []SystemReminder{
		{Content: "First"},
		{Content: "Second"},
	}
	got := FormatReminders(reminders)
	if !strings.Contains(got, "<system-reminder>\nFirst\n</system-reminder>") {
		t.Error("missing first reminder")
	}
	if !strings.Contains(got, "<system-reminder>\nSecond\n</system-reminder>") {
		t.Error("missing second reminder")
	}
	// They should be joined by a newline.
	parts := strings.Split(got, "\n")
	// First reminder ends at line 3 (0-indexed: 0,1,2), then newline separator, then second starts.
	// Total: "<system-reminder>\nFirst\n</system-reminder>\n<system-reminder>\nSecond\n</system-reminder>"
	if len(parts) != 6 {
		t.Errorf("expected 6 lines, got %d: %q", len(parts), got)
	}
}

func TestInjectReminders_AppendsToLastUserMessage(t *testing.T) {
	history := []ConversationMessage{
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer"},
		{Role: "user", Content: "Second question"},
	}
	reminders := []SystemReminder{{Content: "context info"}}

	result := InjectReminders(history, reminders)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	// Last user message (index 2) should have the reminder appended.
	if !strings.Contains(result[2].Content, "Second question") {
		t.Error("original content missing from last user message")
	}
	if !strings.Contains(result[2].Content, "<system-reminder>") {
		t.Error("reminder not appended to last user message")
	}
	// First user message should be unchanged.
	if strings.Contains(result[0].Content, "<system-reminder>") {
		t.Error("first user message should not be modified")
	}
}

func TestInjectReminders_NoUserMessages(t *testing.T) {
	history := []ConversationMessage{
		{Role: "assistant", Content: "Hello"},
	}
	reminders := []SystemReminder{{Content: "important context"}}

	result := InjectReminders(history, reminders)

	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected first message to be user, got %q", result[0].Role)
	}
	if !strings.Contains(result[0].Content, "<system-reminder>") {
		t.Error("prepended message should contain reminder")
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected second message to be assistant, got %q", result[1].Role)
	}
}

func TestInjectReminders_EmptyReminders(t *testing.T) {
	history := []ConversationMessage{
		{Role: "user", Content: "Hello"},
	}

	result := InjectReminders(history, nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content != "Hello" {
		t.Errorf("content should be unchanged, got %q", result[0].Content)
	}
}

func TestInjectReminders_DoesNotMutateInput(t *testing.T) {
	history := []ConversationMessage{
		{Role: "user", Content: "Original"},
		{Role: "assistant", Content: "Response"},
		{Role: "user", Content: "Follow-up"},
	}
	reminders := []SystemReminder{{Content: "injected"}}

	_ = InjectReminders(history, reminders)

	// Original slice should be untouched.
	if history[2].Content != "Follow-up" {
		t.Errorf("input was mutated: got %q, want %q", history[2].Content, "Follow-up")
	}
	if strings.Contains(history[0].Content, "<system-reminder>") {
		t.Error("first message in original history was mutated")
	}
}

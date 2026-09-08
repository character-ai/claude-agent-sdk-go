package claudeagent

import (
	"errors"
	"testing"
)

func TestCollectQueryEventsKeepsResultOnError(t *testing.T) {
	events := make(chan Event, 2)
	events <- Event{Text: "partial"}
	events <- Event{
		Result: &ResultMessage{Cost: 1.25, InputTokens: 9, OutputTokens: 3, TerminalReason: "max_turns", IsError: true},
		Error:  NewResultError(&ResultMessage{TerminalReason: "max_turns", IsError: true}),
	}
	close(events)

	text, result, err := collectQueryEvents(events)
	if text != "partial" {
		t.Fatalf("expected accumulated text, got %q", text)
	}
	if err == nil {
		t.Fatal("expected error result")
	}
	if result == nil {
		t.Fatal("expected structured result to be preserved")
	}
	if result.Cost != 1.25 || result.InputTokens != 9 || result.TerminalReason != "max_turns" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCollectQueryEventsSuccess(t *testing.T) {
	events := make(chan Event, 1)
	events <- Event{Text: "ok", Result: &ResultMessage{Cost: 0.1}}
	close(events)

	text, result, err := collectQueryEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	if text != "ok" || result == nil || result.Cost != 0.1 {
		t.Fatalf("unexpected success collection: %q %+v", text, result)
	}
}

func TestCollectQueryEventsBareError(t *testing.T) {
	events := make(chan Event, 1)
	events <- Event{Error: errors.New("cli missing")}
	close(events)

	_, result, err := collectQueryEvents(events)
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Fatalf("expected no result, got %+v", result)
	}
}

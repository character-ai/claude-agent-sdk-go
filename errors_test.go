package claudeagent

import (
	"strings"
	"testing"
)

func TestNewResultErrorFromResultMessage(t *testing.T) {
	status := 529
	r := &ResultMessage{
		Subtype:        "success",
		Result:         "API Error: overloaded",
		Errors:         []string{"overloaded"},
		APIErrorStatus: &status,
		TerminalReason: "api_error",
		SessionID:      "sess-1",
	}

	err := NewResultError(r)

	if err.Subtype != "success" {
		t.Fatalf("expected subtype 'success', got %q", err.Subtype)
	}
	if err.TerminalReason != "api_error" {
		t.Fatalf("expected terminal reason 'api_error', got %q", err.TerminalReason)
	}
	if err.APIErrorStatus == nil || *err.APIErrorStatus != 529 {
		t.Fatalf("expected api error status 529, got %v", err.APIErrorStatus)
	}
	if err.SessionID != "sess-1" {
		t.Fatalf("expected session id 'sess-1', got %q", err.SessionID)
	}
	if !strings.Contains(err.Error(), "API Error: overloaded") {
		t.Fatalf("expected error message to include result text, got %q", err.Error())
	}
}

func TestNewResultErrorNil(t *testing.T) {
	err := NewResultError(nil)
	if err == nil {
		t.Fatal("expected non-nil ResultError even for nil input")
	}
	if err.Error() == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestNewResultErrorFallsBackToErrorsList(t *testing.T) {
	r := &ResultMessage{Subtype: "error_max_turns", Errors: []string{"turn limit hit"}}
	err := NewResultError(r)

	if !strings.Contains(err.Error(), "turn limit hit") {
		t.Fatalf("expected error message to include errors list, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "error_max_turns") {
		t.Fatalf("expected error message to include subtype, got %q", err.Error())
	}
}

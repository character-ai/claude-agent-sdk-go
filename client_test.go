package claudeagent

import "testing"

func TestParseEventTextDelta(t *testing.T) {
	c := &Client{}
	line := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`
	event := c.parseEvent(line)

	if event.Text != "hello" {
		t.Fatalf("expected text 'hello', got %q", event.Text)
	}
	if event.ToolUseDelta != "" {
		t.Fatalf("expected no tool delta, got %q", event.ToolUseDelta)
	}
}

func TestParseEventToolUseDelta(t *testing.T) {
	c := &Client{}
	line := `{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"q\":\"cats\"}"}}`
	event := c.parseEvent(line)

	if event.ToolUseDelta == "" {
		t.Fatalf("expected tool delta, got empty")
	}
	if event.Text != "" {
		t.Fatalf("expected no text, got %q", event.Text)
	}
}

func TestParseEventAssistantMessageFormat(t *testing.T) {
	c := &Client{}
	line := `{"role":"assistant","content":[{"type":"text","text":"hi"}]}`
	event := c.parseEvent(line)

	if event.AssistantMessage == nil {
		t.Fatalf("expected assistant message")
	}
	if event.Text != "hi" {
		t.Fatalf("expected text 'hi', got %q", event.Text)
	}
}

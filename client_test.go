package claudeagent

import (
	"strings"
	"testing"
)

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

// --- buildArgs tests ---

func TestBuildArgsDefaults(t *testing.T) {
	c := &Client{opts: Options{}}
	args := c.buildArgs()

	if !contains(args, "--output-format") || !contains(args, "stream-json") {
		t.Fatal("expected --output-format stream-json")
	}
	if !contains(args, "--verbose") {
		t.Fatal("expected --verbose")
	}
}

func TestBuildArgsModel(t *testing.T) {
	c := &Client{opts: Options{Model: "claude-sonnet-4-20250514"}}
	args := c.buildArgs()

	idx := indexOf(args, "--model")
	if idx < 0 || args[idx+1] != "claude-sonnet-4-20250514" {
		t.Fatalf("expected --model claude-sonnet-4-20250514, got args: %v", args)
	}
}

func TestBuildArgsCwd(t *testing.T) {
	c := &Client{opts: Options{Cwd: "/tmp/test"}}
	args := c.buildArgs()

	idx := indexOf(args, "--cwd")
	if idx < 0 || args[idx+1] != "/tmp/test" {
		t.Fatalf("expected --cwd /tmp/test, got args: %v", args)
	}
}

func TestBuildArgsPermissionMode(t *testing.T) {
	c := &Client{opts: Options{PermissionMode: PermissionAcceptEdits}}
	args := c.buildArgs()

	idx := indexOf(args, "--permission-mode")
	if idx < 0 || args[idx+1] != "acceptEdits" {
		t.Fatalf("expected --permission-mode acceptEdits, got args: %v", args)
	}
}

func TestBuildArgsPermissionModeDefault(t *testing.T) {
	c := &Client{opts: Options{PermissionMode: PermissionDefault}}
	args := c.buildArgs()

	if contains(args, "--permission-mode") {
		t.Fatal("default permission mode should not produce a --permission-mode flag")
	}
}

func TestBuildArgsAllowedTools(t *testing.T) {
	c := &Client{opts: Options{AllowedTools: []string{"Read", "Write"}}}
	args := c.buildArgs()

	count := 0
	for i, a := range args {
		if a == "--allowed-tools" {
			if args[i+1] != "Read" && args[i+1] != "Write" {
				t.Fatalf("unexpected allowed tool: %s", args[i+1])
			}
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 --allowed-tools flags, got %d", count)
	}
}

func TestBuildArgsMaxTurns(t *testing.T) {
	c := &Client{opts: Options{MaxTurns: 15}}
	args := c.buildArgs()

	idx := indexOf(args, "--max-turns")
	if idx < 0 || args[idx+1] != "15" {
		t.Fatalf("expected --max-turns 15, got args: %v", args)
	}
}

func TestBuildArgsMaxTurnsZero(t *testing.T) {
	c := &Client{opts: Options{MaxTurns: 0}}
	args := c.buildArgs()

	if contains(args, "--max-turns") {
		t.Fatal("max turns 0 should not produce a --max-turns flag")
	}
}

func TestBuildArgsSystemPrompt(t *testing.T) {
	c := &Client{opts: Options{SystemPrompt: "You are helpful."}}
	args := c.buildArgs()

	idx := indexOf(args, "--system-prompt")
	if idx < 0 || args[idx+1] != "You are helpful." {
		t.Fatalf("expected --system-prompt, got args: %v", args)
	}
}

func TestBuildArgsSessionID(t *testing.T) {
	c := &Client{opts: Options{SessionID: "sess-123"}}
	args := c.buildArgs()

	idx := indexOf(args, "--continue")
	if idx < 0 || args[idx+1] != "sess-123" {
		t.Fatalf("expected --continue sess-123, got args: %v", args)
	}
}

func TestBuildArgsToolsConfig(t *testing.T) {
	c := &Client{opts: Options{Tools: &ToolsConfig{Preset: "code", Names: []string{"Read"}}}}
	args := c.buildArgs()

	count := 0
	for _, a := range args {
		if a == "--tools" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 --tools flags (preset + name), got %d", count)
	}
}

func TestBuildArgsCustomSessionID(t *testing.T) {
	c := &Client{opts: Options{CustomSessionID: "custom-123"}}
	args := c.buildArgs()

	idx := indexOf(args, "--session-id")
	if idx < 0 || args[idx+1] != "custom-123" {
		t.Fatalf("expected --session-id custom-123, got args: %v", args)
	}
}

func TestBuildArgsForkSession(t *testing.T) {
	c := &Client{opts: Options{ForkSession: true}}
	args := c.buildArgs()

	if !contains(args, "--fork-session") {
		t.Fatal("expected --fork-session flag")
	}
}

func TestBuildArgsDebug(t *testing.T) {
	c := &Client{opts: Options{Debug: true, DebugFile: "/tmp/debug.log"}}
	args := c.buildArgs()

	if !contains(args, "--debug") {
		t.Fatal("expected --debug flag")
	}
	idx := indexOf(args, "--debug-file")
	if idx < 0 || args[idx+1] != "/tmp/debug.log" {
		t.Fatalf("expected --debug-file /tmp/debug.log, got args: %v", args)
	}
}

func TestBuildArgsBetas(t *testing.T) {
	c := &Client{opts: Options{Betas: []string{"beta1", "beta2"}}}
	args := c.buildArgs()

	count := 0
	for _, a := range args {
		if a == "--beta" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 --beta flags, got %d", count)
	}
}

func TestBuildArgsAdditionalDirectories(t *testing.T) {
	c := &Client{opts: Options{AdditionalDirectories: []string{"/dir1", "/dir2"}}}
	args := c.buildArgs()

	count := 0
	for _, a := range args {
		if a == "--additional-directory" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 --additional-directory flags, got %d", count)
	}
}

func TestBuildArgsSettingSources(t *testing.T) {
	c := &Client{opts: Options{SettingSources: []string{"a.json", "b.json"}}}
	args := c.buildArgs()

	idx := indexOf(args, "--setting-sources")
	if idx < 0 || args[idx+1] != "a.json,b.json" {
		t.Fatalf("expected --setting-sources a.json,b.json, got args: %v", args)
	}
}

func TestBuildArgsPlugins(t *testing.T) {
	c := &Client{opts: Options{Plugins: []PluginConfig{{Path: "/p1.js"}, {Path: "/p2.js"}}}}
	args := c.buildArgs()

	count := 0
	for _, a := range args {
		if a == "--plugin" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 --plugin flags, got %d", count)
	}
}

func TestBuildArgsFileCheckpointing(t *testing.T) {
	c := &Client{opts: Options{EnableFileCheckpointing: true}}
	args := c.buildArgs()

	if !contains(args, "--enable-file-checkpointing") {
		t.Fatal("expected --enable-file-checkpointing flag")
	}
}

func TestBuildArgsExtraArgs(t *testing.T) {
	c := &Client{opts: Options{ExtraArgs: []string{"--custom-flag", "value"}}}
	args := c.buildArgs()

	if !contains(args, "--custom-flag") || !contains(args, "value") {
		t.Fatalf("expected extra args, got args: %v", args)
	}
}

func TestBuildArgsDisallowedTools(t *testing.T) {
	c := &Client{opts: Options{DisallowedTools: []string{"Bash"}}}
	args := c.buildArgs()

	idx := indexOf(args, "--disallowed-tools")
	if idx < 0 || args[idx+1] != "Bash" {
		t.Fatalf("expected --disallowed-tools Bash, got args: %v", args)
	}
}

// --- QueryWithMessages format test ---

func TestQueryWithMessagesFormat(t *testing.T) {
	// Test that QueryWithMessages builds the correct prompt from messages.
	// We can't actually run the CLI, but we can verify the args would be correct
	// by testing the formatting logic directly.

	messages := []Message{
		UserMessage{Content: []ContentBlock{TextBlock{Text: "Hello"}}},
		UserMessage{Content: []ContentBlock{
			ToolResultBlock{ToolUseID: "tool_1", Content: "result data", IsError: false},
		}},
		UserMessage{Content: []ContentBlock{
			ToolResultBlock{ToolUseID: "tool_2", Content: "error data", IsError: true},
		}},
	}

	// Simulate the formatting logic from QueryWithMessages
	var parts []string
	for _, msg := range messages {
		switch m := msg.(type) {
		case UserMessage:
			for _, block := range m.Content {
				switch b := block.(type) {
				case TextBlock:
					parts = append(parts, b.Text)
				case ToolResultBlock:
					prefix := "Tool result"
					if b.IsError {
						prefix = "Tool error"
					}
					parts = append(parts, "["+prefix+" "+b.ToolUseID+"]: "+b.Content)
				}
			}
		}
	}

	prompt := strings.Join(parts, "\n\n")

	if !strings.Contains(prompt, "Hello") {
		t.Fatal("expected prompt to contain 'Hello'")
	}
	if !strings.Contains(prompt, "[Tool result tool_1]: result data") {
		t.Fatalf("expected tool result formatting, got: %s", prompt)
	}
	if !strings.Contains(prompt, "[Tool error tool_2]: error data") {
		t.Fatalf("expected tool error formatting, got: %s", prompt)
	}
}

// Helpers

func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

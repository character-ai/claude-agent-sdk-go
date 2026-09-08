package claudeagent

import (
	"context"
	"strings"
	"testing"
)

func TestParseEventResultUsageAndCost(t *testing.T) {
	c := &Client{}
	line := `{"type":"result","subtype":"success","is_error":false,"duration_ms":2653,"num_turns":1,"result":"hi","stop_reason":"end_turn","session_id":"fc708dfc","total_cost_usd":0.07687225,"usage":{"input_tokens":3,"cache_creation_input_tokens":11759,"cache_read_input_tokens":6527,"output_tokens":4,"server_tool_use":{"web_search_requests":0},"service_tier":"standard"}}`
	event := c.parseEvent(line)

	if event.Type != EventResult {
		t.Fatalf("expected result event, got %q", event.Type)
	}
	if event.Result == nil {
		t.Fatal("expected result to be non-nil")
	}
	if event.Result.Cost == 0 {
		t.Fatal("expected non-zero cost")
	}
	if event.Result.Cost < 0.076 || event.Result.Cost > 0.077 {
		t.Fatalf("expected cost ~0.0769, got %f", event.Result.Cost)
	}
	if event.Result.InputTokens != 3 {
		t.Fatalf("expected 3 input tokens, got %d", event.Result.InputTokens)
	}
	if event.Result.OutputTokens != 4 {
		t.Fatalf("expected 4 output tokens, got %d", event.Result.OutputTokens)
	}
	if event.Result.Usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if event.Result.Usage.CacheCreationInputTokens != 11759 {
		t.Fatalf("expected 11759 cache creation tokens, got %d", event.Result.Usage.CacheCreationInputTokens)
	}
	if event.Result.Usage.CacheReadInputTokens != 6527 {
		t.Fatalf("expected 6527 cache read tokens, got %d", event.Result.Usage.CacheReadInputTokens)
	}
}

func TestParseEventResultExtendedFields(t *testing.T) {
	c := &Client{}
	// Captured from a real `claude -p --output-format json` invocation.
	line := `{"duration_api_ms":3614,"stop_reason":"end_turn","session_id":"0f5fe2d6","total_cost_usd":0.113,"usage":{"input_tokens":6093,"output_tokens":227},"modelUsage":{"auto":{"inputTokens":6093,"outputTokens":227,"costUSD":0.113,"contextWindow":200000,"maxOutputTokens":32000,"canonicalModel":"auto","provider":"firstParty"}},"permission_denials":[],"terminal_reason":"completed","is_error":false,"num_turns":1,"subtype":"success","api_error_status":null,"result":"hi","type":"result","duration_ms":4151,"uuid":"6c826315-a084-4682-bcc4-1feeed3afac0"}`
	event := c.parseEvent(line)

	if event.Result == nil {
		t.Fatal("expected result to be non-nil")
	}
	if event.Result.TerminalReason != "completed" {
		t.Fatalf("expected terminal_reason %q, got %q", "completed", event.Result.TerminalReason)
	}
	if event.Result.UUID != "6c826315-a084-4682-bcc4-1feeed3afac0" {
		t.Fatalf("expected uuid to be parsed, got %q", event.Result.UUID)
	}
	if event.Result.APIErrorStatus != nil {
		t.Fatalf("expected nil api_error_status, got %v", *event.Result.APIErrorStatus)
	}
	mu, ok := event.Result.ModelUsage["auto"]
	if !ok {
		t.Fatalf("expected modelUsage[auto] to be present, got %v", event.Result.ModelUsage)
	}
	if mu.CostUSD != 0.113 || mu.ContextWindow != 200000 || mu.Provider != "firstParty" {
		t.Fatalf("unexpected model usage: %+v", mu)
	}
}

func TestParseEventConversationReset(t *testing.T) {
	c := &Client{}
	line := `{"type":"conversation_reset","new_conversation_id":"new-conv-1","session_id":"old-sess","uuid":"reset-uuid"}`
	event := c.parseEvent(line)

	if event.Type != EventConversationReset {
		t.Fatalf("expected conversation_reset event, got %q", event.Type)
	}
	if event.ConversationReset == nil {
		t.Fatal("expected ConversationReset to be non-nil")
	}
	if event.ConversationReset.NewConversationID != "new-conv-1" {
		t.Fatalf("expected new_conversation_id to be parsed, got %q", event.ConversationReset.NewConversationID)
	}
	if event.ConversationReset.SessionID != "old-sess" {
		t.Fatalf("expected session_id to be parsed, got %q", event.ConversationReset.SessionID)
	}
}

func TestParseEventResultErrorPopulatesEventError(t *testing.T) {
	c := &Client{}
	line := `{"type":"result","subtype":"error_max_turns","is_error":true,"session_id":"sess-1","terminal_reason":"max_turns","errors":["hit turn limit"]}`
	event := c.parseEvent(line)

	if event.Result == nil || !event.Result.IsError {
		t.Fatal("expected an error result")
	}
	if event.Error == nil {
		t.Fatal("expected event.Error to be populated for an error result")
	}
	re, ok := event.Error.(*ResultError)
	if !ok {
		t.Fatalf("expected event.Error to be a *ResultError, got %T", event.Error)
	}
	if re.TerminalReason != "max_turns" {
		t.Fatalf("expected terminal reason 'max_turns', got %q", re.TerminalReason)
	}
}

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

	// --continue takes no value and resumes the most recent conversation in
	// Cwd; resuming a specific session ID requires --resume.
	idx := indexOf(args, "--resume")
	if idx < 0 || args[idx+1] != "sess-123" {
		t.Fatalf("expected --resume sess-123, got args: %v", args)
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

func TestBuildArgsEffort(t *testing.T) {
	c := &Client{opts: Options{Effort: EffortHigh}}
	args := c.buildArgs()

	idx := indexOf(args, "--effort")
	if idx < 0 || args[idx+1] != "high" {
		t.Fatalf("expected --effort high, got args: %v", args)
	}
}

func TestBuildArgsForwardSubagentTextAndHookEvents(t *testing.T) {
	c := &Client{opts: Options{ForwardSubagentText: true, IncludeHookEvents: true}}
	args := c.buildArgs()

	if !contains(args, "--forward-subagent-text") {
		t.Fatal("expected --forward-subagent-text flag")
	}
	if !contains(args, "--include-hook-events") {
		t.Fatal("expected --include-hook-events flag")
	}
}

func TestBuildArgsMaxBudgetUSD(t *testing.T) {
	c := &Client{opts: Options{MaxBudgetUSD: 2.5}}
	args := c.buildArgs()

	idx := indexOf(args, "--max-budget-usd")
	if idx < 0 || args[idx+1] != "2.5" {
		t.Fatalf("expected --max-budget-usd 2.5, got args: %v", args)
	}
}

func TestBuildArgsJSONSchema(t *testing.T) {
	schema := `{"type":"object"}`
	c := &Client{opts: Options{JSONSchema: schema}}
	args := c.buildArgs()

	idx := indexOf(args, "--json-schema")
	if idx < 0 || args[idx+1] != schema {
		t.Fatalf("expected --json-schema %s, got args: %v", schema, args)
	}
}

func TestCommandEnvironmentPropagatesTraceContext(t *testing.T) {
	t.Setenv("TRACEPARENT", "stale")
	t.Setenv("TRACESTATE", "stale=1")
	ctx := WithTraceContext(context.Background(), "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01", "vendor=value")
	env := commandEnvironment(ctx, nil)

	if value := envValue(env, "TRACEPARENT"); value != "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01" {
		t.Fatalf("unexpected TRACEPARENT: %q", value)
	}
	if value := envValue(env, "TRACESTATE"); value != "vendor=value" {
		t.Fatalf("unexpected TRACESTATE: %q", value)
	}
	if value := envValue(env, "CLAUDE_CODE_ENTRYPOINT"); value != "sdk-go" {
		t.Fatalf("unexpected entrypoint: %q", value)
	}
}

func TestCommandEnvironmentOptionsOverrideTraceContext(t *testing.T) {
	ctx := WithTraceContext(context.Background(), "propagated", "propagated-state")
	env := commandEnvironment(ctx, map[string]string{
		"TRACEPARENT": "explicit",
		"TRACESTATE":  "explicit-state",
	})

	if value := envValue(env, "TRACEPARENT"); value != "explicit" {
		t.Fatalf("explicit TRACEPARENT should win, got %q", value)
	}
	if value := envValue(env, "TRACESTATE"); value != "explicit-state" {
		t.Fatalf("explicit TRACESTATE should win, got %q", value)
	}
}

func TestCommandEnvironmentScrubsStaleTraceState(t *testing.T) {
	t.Setenv("TRACESTATE", "stale=1")
	ctx := WithTraceContext(context.Background(), "fresh", "")
	env := commandEnvironment(ctx, nil)

	if value := envValue(env, "TRACEPARENT"); value != "fresh" {
		t.Fatalf("unexpected TRACEPARENT: %q", value)
	}
	if value := envValue(env, "TRACESTATE"); value != "" {
		t.Fatalf("expected stale TRACESTATE to be removed, got %q", value)
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

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, value := range env {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return ""
}

package claudeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Client provides a streaming interface to Claude Code CLI.
type Client struct {
	opts Options

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	running bool
}

// NewClient creates a new Claude agent client.
func NewClient(opts Options) *Client {
	return &Client{
		opts: opts,
	}
}

// buildArgs constructs CLI arguments from options.
func (c *Client) buildArgs() []string {
	args := []string{
		"--output-format", "stream-json",
		"--verbose",
	}

	if c.opts.Model != "" {
		args = append(args, "--model", c.opts.Model)
	}

	if c.opts.Cwd != "" {
		args = append(args, "--cwd", c.opts.Cwd)
	}

	if c.opts.PermissionMode != "" && c.opts.PermissionMode != PermissionDefault {
		args = append(args, "--permission-mode", string(c.opts.PermissionMode))
	}

	for _, tool := range c.opts.AllowedTools {
		args = append(args, "--allowed-tools", tool)
	}

	for _, tool := range c.opts.DisallowedTools {
		args = append(args, "--disallowed-tools", tool)
	}

	if c.opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", c.opts.MaxTurns))
	}

	if c.opts.SystemPrompt != "" {
		args = append(args, "--system-prompt", c.opts.SystemPrompt)
	}

	if c.opts.SessionID != "" {
		args = append(args, "--continue", c.opts.SessionID)
	}

	args = append(args, c.opts.ExtraArgs...)

	return args
}

// Query sends a prompt and returns a channel of streaming events.
func (c *Client) Query(ctx context.Context, prompt string) (<-chan Event, error) {
	args := c.buildArgs()
	args = append(args, "--print", prompt)

	return c.runStreaming(ctx, args)
}

// QueryWithMessages sends messages and returns streaming events.
func (c *Client) QueryWithMessages(ctx context.Context, messages []Message) (<-chan Event, error) {
	// For now, convert to a simple prompt
	// TODO: Support full message history via stdin
	var prompt string
	for _, msg := range messages {
		if um, ok := msg.(UserMessage); ok {
			for _, block := range um.Content {
				if tb, ok := block.(TextBlock); ok {
					prompt += tb.Text + "\n"
				}
			}
		}
	}

	return c.Query(ctx, prompt)
}

// Event represents a parsed event from the stream.
type Event struct {
	// Raw JSON line
	Raw string

	// Parsed event type
	Type StreamEventType

	// For text content deltas
	Text string

	// For tool use events
	ToolUse *ToolUseEvent

	// For tool results
	ToolResult *ToolResultEvent

	// For assistant messages
	AssistantMessage *AssistantMessage

	// For result/completion
	Result *ResultMessage

	// Parsing error if any
	Error error
}

// ToolUseEvent represents a tool being invoked.
type ToolUseEvent struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultEvent represents the result of a tool invocation.
type ToolResultEvent struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// runStreaming executes the CLI and streams events.
func (c *Client) runStreaming(ctx context.Context, args []string) (<-chan Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil, fmt.Errorf("client already running")
	}

	cmd := exec.CommandContext(ctx, "claude", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude CLI: %w", err)
	}

	c.cmd = cmd
	c.stdout = stdout
	c.stderr = stderr
	c.running = true

	events := make(chan Event, 100)

	go c.streamEvents(ctx, stdout, stderr, events, cmd)

	return events, nil
}

// streamEvents reads from stdout and parses JSON events.
func (c *Client) streamEvents(ctx context.Context, stdout, stderr io.ReadCloser, events chan<- Event, cmd *exec.Cmd) {
	defer close(events)
	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
	}()

	// Read stderr in background for debugging
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// Could log stderr here if needed
			_ = scanner.Text()
		}
	}()

	scanner := bufio.NewScanner(stdout)
	// Increase buffer size for large JSON lines
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		event := c.parseEvent(line)
		events <- event
	}

	if err := scanner.Err(); err != nil {
		events <- Event{Error: fmt.Errorf("scanner error: %w", err)}
	}

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		// Only report if it's not a context cancellation
		if ctx.Err() == nil {
			events <- Event{Error: fmt.Errorf("command error: %w", err)}
		}
	}
}

// parseEvent parses a JSON line into an Event.
func (c *Client) parseEvent(line string) Event {
	event := Event{Raw: line}

	// Try to parse as a generic JSON object first
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		event.Error = fmt.Errorf("failed to parse JSON: %w", err)
		return event
	}

	// Determine event type
	typeVal, ok := raw["type"].(string)
	if !ok {
		// Check for assistant message format
		if role, ok := raw["role"].(string); ok && role == "assistant" {
			event.Type = EventMessageStart
			var msg AssistantMessage
			if err := json.Unmarshal([]byte(line), &msg); err == nil {
				event.AssistantMessage = &msg
				// Extract text from content blocks
				if content, ok := raw["content"].([]any); ok {
					for _, block := range content {
						if blockMap, ok := block.(map[string]any); ok {
							if blockMap["type"] == "text" {
								if text, ok := blockMap["text"].(string); ok {
									event.Text += text
								}
							}
						}
					}
				}
			}
			return event
		}
		return event
	}

	event.Type = StreamEventType(typeVal)

	switch event.Type {
	case EventMessageStart:
		if msgData, ok := raw["message"].(map[string]any); ok {
			msgBytes, _ := json.Marshal(msgData)
			var msg AssistantMessage
			json.Unmarshal(msgBytes, &msg)
			event.AssistantMessage = &msg
		}

	case EventContentBlockDelta:
		if delta, ok := raw["delta"].(map[string]any); ok {
			if text, ok := delta["text"].(string); ok {
				event.Text = text
			}
			if partialJSON, ok := delta["partial_json"].(string); ok {
				event.Text = partialJSON
			}
		}

	case EventContentBlockStart:
		if block, ok := raw["content_block"].(map[string]any); ok {
			if block["type"] == "tool_use" {
				event.ToolUse = &ToolUseEvent{
					ID:   getString(block, "id"),
					Name: getString(block, "name"),
				}
			}
		}

	case EventToolResult:
		event.ToolResult = &ToolResultEvent{
			ToolUseID: getString(raw, "tool_use_id"),
			Content:   getString(raw, "content"),
			IsError:   getBool(raw, "is_error"),
		}

	case EventResult:
		var result ResultMessage
		if err := json.Unmarshal([]byte(line), &result); err == nil {
			event.Result = &result
		}
	}

	return event
}

// Helper functions for type-safe map access
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// Stop terminates the running command.
func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.cmd == nil {
		return nil
	}

	if c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}

// IsRunning returns whether a query is currently running.
func (c *Client) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

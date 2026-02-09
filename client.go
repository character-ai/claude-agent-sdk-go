package claudeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
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
	done    chan struct{} // closed when streamEvents finishes
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

	if c.opts.Tools != nil {
		if c.opts.Tools.Preset != "" {
			args = append(args, "--tools", c.opts.Tools.Preset)
		}
		for _, name := range c.opts.Tools.Names {
			args = append(args, "--tools", name)
		}
	}

	if c.opts.CustomSessionID != "" {
		args = append(args, "--session-id", c.opts.CustomSessionID)
	}

	if c.opts.ForkSession {
		args = append(args, "--fork-session")
	}

	if c.opts.Debug {
		args = append(args, "--debug")
	}

	if c.opts.DebugFile != "" {
		args = append(args, "--debug-file", c.opts.DebugFile)
	}

	for _, beta := range c.opts.Betas {
		args = append(args, "--beta", beta)
	}

	for _, dir := range c.opts.AdditionalDirectories {
		args = append(args, "--additional-directory", dir)
	}

	if len(c.opts.SettingSources) > 0 {
		args = append(args, "--setting-sources", strings.Join(c.opts.SettingSources, ","))
	}

	for _, plugin := range c.opts.Plugins {
		if plugin.Path != "" {
			args = append(args, "--plugin", plugin.Path)
		}
	}

	if c.opts.EnableFileCheckpointing {
		args = append(args, "--enable-file-checkpointing")
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
// Messages are formatted into a single prompt for the CLI.
// For proper multi-turn conversation support with external tool execution,
// use APIAgent which communicates directly with the Anthropic API.
func (c *Client) QueryWithMessages(ctx context.Context, messages []Message) (<-chan Event, error) {
	args := c.buildArgs()

	// Build a combined prompt from all messages.
	// The CLI doesn't natively support receiving pre-built conversation history,
	// so we format user content and tool results into a single prompt.
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
					parts = append(parts, fmt.Sprintf("[%s %s]: %s", prefix, b.ToolUseID, b.Content))
				}
			}
		case AssistantMessage:
			// Skip assistant messages — the CLI manages its own context
		}
	}

	prompt := strings.Join(parts, "\n\n")
	if prompt == "" {
		prompt = "continue"
	}

	args = append(args, "--print", prompt)
	return c.runStreaming(ctx, args)
}

// Event represents a parsed event from the stream.
type Event struct {
	// Raw JSON line
	Raw string

	// Parsed event type
	Type StreamEventType

	// For text content deltas
	Text string

	// For tool input JSON deltas
	ToolUseDelta string

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
		return nil, ErrAlreadyRunning
	}

	cliPath := c.opts.CLIPath
	if cliPath == "" {
		cliPath = "claude"
	}

	if _, err := exec.LookPath(cliPath); err != nil {
		return nil, ErrCLINotFound
	}

	cmd := exec.CommandContext(ctx, cliPath, args...) // #nosec G204 -- cliPath is intentionally configurable

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

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
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.running = true
	c.done = make(chan struct{})

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
		done := c.done
		c.mu.Unlock()
		if done != nil {
			close(done)
		}
	}()

	stderrCh := make(chan string, 1)
	go func() {
		defer close(stderrCh)
		data, _ := io.ReadAll(stderr)
		stderrCh <- string(data)
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
			stderrOutput := <-stderrCh
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode := exitErr.ExitCode()
				events <- Event{Error: &ProcessError{ExitCode: exitCode, Stderr: stderrOutput}}
				return
			}
			events <- Event{Error: fmt.Errorf("command error: %w", err)}
		}
		return
	}

	<-stderrCh
}

// parseEvent parses a JSON line into an Event.
func (c *Client) parseEvent(line string) Event {
	event := Event{Raw: line}

	// Try to parse as a generic JSON object first
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		event.Error = &JSONDecodeError{Line: line, Err: err}
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
				event.Text = extractTextFromContent(msg.Content)
			}
			return event
		}
		return event
	}

	event.Type = StreamEventType(typeVal)

	switch event.Type { //nolint:exhaustive // Only handling events we care about
	case EventMessageStart, EventAssistant:
		if msgData, ok := raw["message"].(map[string]any); ok {
			msgBytes, _ := json.Marshal(msgData)
			var msg AssistantMessage
			if err := json.Unmarshal(msgBytes, &msg); err == nil {
				event.AssistantMessage = &msg
				event.Text = extractTextFromContent(msg.Content)
			}
		}

	case EventContentBlockDelta:
		if delta, ok := raw["delta"].(map[string]any); ok {
			if deltaType, ok := delta["type"].(string); ok && deltaType == "input_json_delta" {
				if partialJSON, ok := delta["partial_json"].(string); ok {
					event.ToolUseDelta = partialJSON
				}
				break
			}
			if text, ok := delta["text"].(string); ok {
				event.Text = text
			} else if partialJSON, ok := delta["partial_json"].(string); ok {
				event.ToolUseDelta = partialJSON
			}
		}

	case EventContentBlockStart:
		if block, ok := raw["content_block"].(map[string]any); ok {
			if block["type"] == "tool_use" {
				event.ToolUse = &ToolUseEvent{
					ID:   getString(block, "id"),
					Name: getString(block, "name"),
					Input: func() json.RawMessage {
						if input, ok := block["input"]; ok {
							if rawInput, err := json.Marshal(input); err == nil {
								return rawInput
							}
						}
						return nil
					}(),
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
			// Populate convenience fields from Usage
			if result.Usage != nil {
				result.InputTokens = result.Usage.InputTokens
				result.OutputTokens = result.Usage.OutputTokens
			}
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

func extractTextFromContent(blocks []ContentBlock) string {
	var text string
	for _, block := range blocks {
		if tb, ok := block.(TextBlock); ok {
			text += tb.Text
		}
	}
	return text
}

// Stop terminates the running command immediately with SIGKILL.
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

// Close gracefully shuts down the running command.
// It sends SIGINT first, then SIGKILL after a 5-second timeout.
func (c *Client) Close() error {
	c.mu.Lock()
	if !c.running || c.cmd == nil || c.cmd.Process == nil {
		c.mu.Unlock()
		return nil
	}
	proc := c.cmd.Process
	done := c.done
	c.mu.Unlock()

	// Send SIGINT for graceful shutdown
	if err := proc.Signal(syscall.SIGINT); err != nil {
		// Process may have already exited
		return nil
	}

	// Wait for streamEvents to finish (which calls cmd.Wait internally),
	// or force kill after 5 seconds.
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		// Force kill after timeout
		return proc.Kill()
	}
}

// Send writes data to the running process's stdin.
// This can be used to send follow-up messages to a running query.
func (c *Client) Send(data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running || c.stdin == nil {
		return ErrNotRunning
	}

	_, err := fmt.Fprintln(c.stdin, data)
	return err
}

// IsRunning returns whether a query is currently running.
func (c *Client) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

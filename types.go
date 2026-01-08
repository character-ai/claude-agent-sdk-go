// Package claudeagent provides a Go SDK for interacting with Claude Code CLI.
package claudeagent

import (
	"encoding/json"
)

// MessageRole represents the role of a message sender.
type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// ContentBlockType represents the type of content block.
type ContentBlockType string

const (
	ContentTypeText    ContentBlockType = "text"
	ContentTypeToolUse ContentBlockType = "tool_use"
)

// StreamEventType represents the type of streaming event.
type StreamEventType string

const (
	EventMessageStart      StreamEventType = "message_start"
	EventContentBlockStart StreamEventType = "content_block_start"
	EventContentBlockDelta StreamEventType = "content_block_delta"
	EventContentBlockStop  StreamEventType = "content_block_stop"
	EventMessageDelta      StreamEventType = "message_delta"
	EventMessageStop       StreamEventType = "message_stop"
	EventToolUseStart      StreamEventType = "tool_use_start"
	EventToolUseDelta      StreamEventType = "tool_use_delta"
	EventToolUseEnd        StreamEventType = "tool_use_end"
	EventToolResult        StreamEventType = "tool_result"
	EventResult            StreamEventType = "result"
)

// PermissionMode controls how tool permissions are handled.
type PermissionMode string

const (
	PermissionDefault         PermissionMode = "default"
	PermissionAcceptEdits     PermissionMode = "acceptEdits"
	PermissionPlan            PermissionMode = "plan"
	PermissionBypassAll       PermissionMode = "bypassPermissions"
)

// ContentBlock represents a block of content in a message.
type ContentBlock interface {
	contentBlock()
	Type() ContentBlockType
}

// TextBlock represents a text content block.
type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) contentBlock()              {}
func (TextBlock) Type() ContentBlockType     { return ContentTypeText }

// ToolUseBlock represents a tool use content block.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (ToolUseBlock) contentBlock()          {}
func (ToolUseBlock) Type() ContentBlockType { return ContentTypeToolUse }

// ToolResultBlock represents a tool result.
type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// Message represents a conversation message.
type Message interface {
	message()
	Role() MessageRole
}

// UserMessage represents a message from the user.
type UserMessage struct {
	Content []ContentBlock `json:"content"`
}

func (UserMessage) message()        {}
func (UserMessage) Role() MessageRole { return RoleUser }

// AssistantMessage represents a message from Claude.
type AssistantMessage struct {
	ID           string         `json:"id,omitempty"`
	Model        string         `json:"model,omitempty"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
}

func (AssistantMessage) message()        {}
func (AssistantMessage) Role() MessageRole { return RoleAssistant }

// StreamEvent represents a streaming event from Claude.
type StreamEvent struct {
	Type  StreamEventType `json:"type"`
	Index int             `json:"index,omitempty"`

	// For message_start
	Message *AssistantMessage `json:"message,omitempty"`

	// For content_block_start/delta
	ContentBlock *RawContentBlock `json:"content_block,omitempty"`
	Delta        *StreamDelta     `json:"delta,omitempty"`
}

// RawContentBlock is used for parsing content blocks from JSON.
type RawContentBlock struct {
	Type  ContentBlockType `json:"type"`
	ID    string           `json:"id,omitempty"`
	Name  string           `json:"name,omitempty"`
	Text  string           `json:"text,omitempty"`
	Input json.RawMessage  `json:"input,omitempty"`
}

// StreamDelta represents delta content in a stream event.
type StreamDelta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// ResultMessage contains the final result of a query.
type ResultMessage struct {
	Type           string  `json:"type"`
	Subtype        string  `json:"subtype,omitempty"`
	Cost           float64 `json:"cost_usd,omitempty"`
	InputTokens    int     `json:"input_tokens,omitempty"`
	OutputTokens   int     `json:"output_tokens,omitempty"`
	Duration       float64 `json:"duration_ms,omitempty"`
	SessionID      string  `json:"session_id,omitempty"`
	IsError        bool    `json:"is_error,omitempty"`
	NumTurns       int     `json:"num_turns,omitempty"`
}

// Options configures the Claude agent behavior.
type Options struct {
	// Working directory for the agent
	Cwd string

	// Model to use (e.g., "claude-sonnet-4-20250514")
	Model string

	// Permission mode for tool execution
	PermissionMode PermissionMode

	// Allowed tools (e.g., ["Read", "Write", "Bash"])
	AllowedTools []string

	// Disallowed tools
	DisallowedTools []string

	// Maximum turns before stopping
	MaxTurns int

	// System prompt override
	SystemPrompt string

	// Continue from a previous session
	SessionID string

	// Additional CLI arguments
	ExtraArgs []string
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		PermissionMode: PermissionDefault,
		MaxTurns:       0, // unlimited
	}
}

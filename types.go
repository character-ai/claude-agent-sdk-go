// Package claudeagent provides a Go SDK for interacting with Claude Code CLI.
package claudeagent

import (
	"context"
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
	ContentTypeText       ContentBlockType = "text"
	ContentTypeToolUse    ContentBlockType = "tool_use"
	ContentTypeToolResult ContentBlockType = "tool_result"
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
	EventAssistant         StreamEventType = "assistant"
	EventSystem            StreamEventType = "system"
	EventConversationReset StreamEventType = "conversation_reset"
)

// PermissionMode controls how tool permissions are handled.
type PermissionMode string

const (
	PermissionDefault     PermissionMode = "default"
	PermissionAcceptEdits PermissionMode = "acceptEdits"
	PermissionPlan        PermissionMode = "plan"
	PermissionBypassAll   PermissionMode = "bypassPermissions"
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

func (TextBlock) contentBlock()          {}
func (TextBlock) Type() ContentBlockType { return ContentTypeText }

// ToolUseBlock represents a tool use content block.
type ToolUseBlock struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (ToolUseBlock) contentBlock()          {}
func (ToolUseBlock) Type() ContentBlockType { return ContentTypeToolUse }

// ToolResultBlock represents a tool result content block.
type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

func (ToolResultBlock) contentBlock()          {}
func (ToolResultBlock) Type() ContentBlockType { return ContentTypeToolResult }

// Message represents a conversation message.
type Message interface {
	message()
	Role() MessageRole
}

// UserMessage represents a message from the user.
type UserMessage struct {
	Content []ContentBlock `json:"content"`
	// Origin describes the provenance of this message (nil for a message the
	// caller sent directly). Populated for injected turns such as background
	// task notifications or peer/channel messages.
	Origin *MessageOrigin `json:"origin,omitempty"`
}

func (UserMessage) message()          {}
func (UserMessage) Role() MessageRole { return RoleUser }

// UnmarshalJSON decodes user message content blocks based on "type".
func (m *UserMessage) UnmarshalJSON(data []byte) error {
	type wire struct {
		Content []json.RawMessage `json:"content"`
		Origin  *MessageOrigin    `json:"origin,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.Content = parseContentBlocks(w.Content)
	m.Origin = w.Origin
	return nil
}

// MessageOriginKind identifies the provenance of a message. Values other
// than the ones documented here may be emitted by newer CLI versions —
// treat anything unrecognized as "not human".
type MessageOriginKind string

const (
	OriginHuman            MessageOriginKind = "human"
	OriginChannel          MessageOriginKind = "channel"
	OriginPeer             MessageOriginKind = "peer"
	OriginTaskNotification MessageOriginKind = "task-notification"
	OriginCoordinator      MessageOriginKind = "coordinator"
	OriginUnclassified     MessageOriginKind = "unclassified"
	OriginObserver         MessageOriginKind = "observer"
	OriginAutoContinuation MessageOriginKind = "auto-continuation"
	OriginObserverActivity MessageOriginKind = "observer-activity"
)

// MessageOrigin describes where an injected message came from — e.g. a
// background task notification, a peer session, or an MCP server channel.
type MessageOrigin struct {
	// Kind discriminates the origin. See MessageOriginKind.
	Kind MessageOriginKind `json:"kind"`
	// Subkind further qualifies Kind (e.g. "scheduled-trigger" or
	// "peer-send-message" when Kind is "task-notification").
	Subkind string `json:"subkind,omitempty"`
	// Server is the MCP server name (Kind == "channel").
	Server string `json:"server,omitempty"`
	// From is the sender address (Kind == "peer" or "observer"). Sender-asserted —
	// use for reply routing/display, never as proof of identity.
	From string `json:"from,omitempty"`
	// Name is the sender's display name (Kind == "peer"), already sanitized by the CLI.
	Name string `json:"name,omitempty"`
	// FromSession is the sender's host-openable session id, if provided (Kind == "peer").
	FromSession string `json:"fromSession,omitempty"`
	// SenderTaskID is the task id of the in-process background subagent that sent
	// this message (Kind == "peer" or "observer"; absent for cross-session peers).
	SenderTaskID string `json:"senderTaskId,omitempty"`
	// Body is the decoded message body with the peer envelope stripped (Kind == "peer").
	Body string `json:"body,omitempty"`
}

// AssistantMessage represents a message from Claude.
type AssistantMessage struct {
	ID           string         `json:"id,omitempty"`
	Model        string         `json:"model,omitempty"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason,omitempty"`
	StopSequence string         `json:"stop_sequence,omitempty"`
}

func (AssistantMessage) message()          {}
func (AssistantMessage) Role() MessageRole { return RoleAssistant }

// UnmarshalJSON decodes assistant message content blocks based on "type".
func (m *AssistantMessage) UnmarshalJSON(data []byte) error {
	type wire struct {
		ID           string            `json:"id,omitempty"`
		Model        string            `json:"model,omitempty"`
		Content      []json.RawMessage `json:"content"`
		StopReason   string            `json:"stop_reason,omitempty"`
		StopSequence string            `json:"stop_sequence,omitempty"`
	}
	var w wire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.ID = w.ID
	m.Model = w.Model
	m.Content = parseContentBlocks(w.Content)
	m.StopReason = w.StopReason
	m.StopSequence = w.StopSequence
	return nil
}

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

// ResultUsage contains token usage information.
type ResultUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ResultMessage contains the final result of a query.
type ResultMessage struct {
	Type         string       `json:"type"`
	Subtype      string       `json:"subtype,omitempty"`
	Cost         float64      `json:"total_cost_usd,omitempty"`
	Usage        *ResultUsage `json:"usage,omitempty"`
	InputTokens  int          `json:"-"` // Populated from Usage
	OutputTokens int          `json:"-"` // Populated from Usage
	Duration     float64      `json:"duration_ms,omitempty"`
	SessionID    string       `json:"session_id,omitempty"`
	IsError      bool         `json:"is_error,omitempty"`
	NumTurns     int          `json:"num_turns,omitempty"`
	Result       string       `json:"result,omitempty"`
	StopReason   string       `json:"stop_reason,omitempty"`

	// UUID uniquely identifies this result message.
	UUID string `json:"uuid,omitempty"`
	// TerminalReason explains why the query loop terminated (e.g. "completed",
	// "max_turns", "aborted_streaming", "aborted_tools"). Empty when the CLI
	// did not report one (older CLI versions, or a result that bypassed the
	// query loop such as a local slash command).
	TerminalReason string `json:"terminal_reason,omitempty"`
	// ModelUsage breaks down token usage and cost per model, keyed by the
	// model string the CLI billed against. Passed through verbatim from the
	// CLI's "modelUsage" field.
	ModelUsage map[string]ModelUsage `json:"modelUsage,omitempty"`
	// PermissionDenials lists tool calls that were denied during this turn.
	PermissionDenials []json.RawMessage `json:"permission_denials,omitempty"`
	// DeferredToolUse is set when a PreToolUse hook returned a "defer"
	// decision: the run stopped and this carries the deferred tool call so
	// the caller can inspect it and decide whether to resume.
	DeferredToolUse *DeferredToolUse `json:"deferred_tool_use,omitempty"`
	// Errors lists non-fatal error messages accumulated during the turn.
	Errors []string `json:"errors,omitempty"`
	// APIErrorStatus is the HTTP status code (e.g. 429, 500, 529) of the
	// failing API call when IsError is true and Subtype is "success"; nil
	// otherwise. Safe to log (no message content).
	APIErrorStatus *int `json:"api_error_status,omitempty"`
	// StructuredOutput holds the JSON result validated against Options.JSONSchema,
	// when one was configured.
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	// Origin describes the provenance of the user message that triggered this
	// turn — nil for a directly-sent prompt. Lets a streaming-input consumer
	// distinguish its own turns from injected ones (e.g. task notifications).
	Origin *MessageOrigin `json:"origin,omitempty"`
}

// EffortLevel controls how much extended-thinking effort Claude spends on a turn.
type EffortLevel string

const (
	EffortLow    EffortLevel = "low"
	EffortMedium EffortLevel = "medium"
	EffortHigh   EffortLevel = "high"
	EffortXHigh  EffortLevel = "xhigh"
	EffortMax    EffortLevel = "max"
)

// ModelUsage is a per-model token usage and cost breakdown reported on a
// ResultMessage. Field names mirror the CLI's own camelCase "modelUsage" shape.
type ModelUsage struct {
	InputTokens              int     `json:"inputTokens,omitempty"`
	OutputTokens             int     `json:"outputTokens,omitempty"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens,omitempty"`
	WebSearchRequests        int     `json:"webSearchRequests,omitempty"`
	CostUSD                  float64 `json:"costUSD,omitempty"`
	ContextWindow            int     `json:"contextWindow,omitempty"`
	MaxOutputTokens          int     `json:"maxOutputTokens,omitempty"`
	// CanonicalModel is the model id used for the pricing lookup (e.g.
	// "claude-opus-4-7"); may differ from the raw model string this entry is
	// keyed by (provider-specific ids, aliases).
	CanonicalModel string `json:"canonicalModel,omitempty"`
	// Provider is the API provider that served this model ("firstParty",
	// "bedrock", "vertex", "foundry", "anthropicAws", "anthropicGoogleCloud",
	// "mantle", "gateway").
	Provider string `json:"provider,omitempty"`
}

// DeferredToolUse is a tool call deferred by a PreToolUse hook returning a
// "defer" decision.
type DeferredToolUse struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ConversationResetMessage is emitted when the session's conversation is
// replaced without ending the connection — e.g. after "/clear" or any other
// flow that discards the transcript mid-session.
//
// In streaming input mode a single connection can carry many user turns, and
// a reset clears the conversation history *and* zeroes the running totals
// reported on subsequent ResultMessages (e.g. Cost). If you accumulate those
// totals across a long-lived session, snapshot them when this message arrives.
type ConversationResetMessage struct {
	// NewConversationID is an opaque identifier for the fresh conversation, for
	// UIs to key an empty transcript on (and discard any cached session
	// title). This is NOT the SessionID of subsequent messages.
	NewConversationID string `json:"new_conversation_id,omitempty"`
	// UUID uniquely identifies this message.
	UUID string `json:"uuid,omitempty"`
	// SessionID is the ID of the session that was reset (the outgoing
	// session; messages after the reset carry a new session ID).
	SessionID string `json:"session_id,omitempty"`
}

// PermissionDecision represents the result of a canUseTool callback.
type PermissionDecision struct {
	// Allow indicates whether tool execution is permitted.
	Allow bool
	// Reason explains the decision.
	Reason string
	// ModifiedInput optionally replaces the tool input.
	ModifiedInput json.RawMessage
}

// CanUseToolFunc is a callback invoked before tool execution to get permission.
type CanUseToolFunc func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) PermissionDecision

// ToolsConfig specifies which built-in tools are available.
type ToolsConfig struct {
	// Preset is a named tool preset (e.g., "default", "code", "all").
	Preset string
	// Names lists specific tool names to enable.
	Names []string
}

// PluginConfig describes a plugin to load.
type PluginConfig struct {
	// Type is the plugin type (e.g., "file").
	Type string
	// Path is the file path for file-type plugins.
	Path string
}

// Options configures the Claude agent behavior.
type Options struct {
	// Working directory for the agent
	Cwd string

	// Path to the Claude CLI executable (defaults to "claude" in PATH)
	CLIPath string

	// Env adds or overrides environment variables for the Claude CLI process.
	// Inherited variables remain available. Explicit TRACEPARENT/TRACESTATE
	// values take precedence over trace context propagated from Query's context.
	Env map[string]string

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

	// MCP servers for tool integration (in-process and external)
	MCPServers *MCPServers

	// Tools specifies which built-in tools are available (different from AllowedTools filter).
	Tools *ToolsConfig

	// CustomSessionID sets an explicit session ID instead of auto-generating one.
	CustomSessionID string

	// ForkSession creates a fork of the session specified by SessionID.
	ForkSession bool

	// Debug enables debug logging.
	Debug bool

	// DebugFile writes debug output to a file instead of stderr.
	DebugFile string

	// Betas enables beta features by name.
	Betas []string

	// AdditionalDirectories adds extra directories to the agent context.
	AdditionalDirectories []string

	// SettingSources specifies additional settings source files.
	SettingSources []string

	// Plugins configures plugins to load.
	Plugins []PluginConfig

	// EnableFileCheckpointing enables file checkpointing for session rewind.
	EnableFileCheckpointing bool

	// Effort controls how much extended-thinking effort Claude spends
	// (low, medium, high, xhigh, max). Maps to the CLI's --effort flag.
	Effort EffortLevel

	// ForwardSubagentText forwards subagent text and thinking blocks as
	// assistant/user messages with parent_tool_use_id set. Only takes effect
	// with the CLI's stream-json output format.
	ForwardSubagentText bool

	// IncludeHookEvents includes all hook lifecycle events in the CLI's
	// output stream (stream-json output format only).
	IncludeHookEvents bool

	// MaxBudgetUSD stops the CLI's own turn loop once cumulative API spend
	// exceeds this amount. Unlike BudgetConfig.MaxCostUSD (enforced client-side
	// across Query calls), this is enforced by the CLI itself within a single
	// --print invocation.
	MaxBudgetUSD float64

	// JSONSchema requests structured output validated against this JSON
	// Schema string; the validated result is returned on
	// ResultMessage.StructuredOutput.
	JSONSchema string
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		PermissionMode: PermissionDefault,
		MaxTurns:       0, // unlimited
	}
}

func parseContentBlocks(rawBlocks []json.RawMessage) []ContentBlock {
	blocks := make([]ContentBlock, 0, len(rawBlocks))
	for _, raw := range rawBlocks {
		var meta struct {
			Type ContentBlockType `json:"type"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		switch meta.Type {
		case ContentTypeText:
			var tb TextBlock
			if err := json.Unmarshal(raw, &tb); err == nil {
				blocks = append(blocks, tb)
			}
		case ContentTypeToolUse:
			var tb ToolUseBlock
			if err := json.Unmarshal(raw, &tb); err == nil {
				blocks = append(blocks, tb)
			}
		case ContentTypeToolResult:
			var tb ToolResultBlock
			if err := json.Unmarshal(raw, &tb); err == nil {
				blocks = append(blocks, tb)
			}
		}
	}
	return blocks
}

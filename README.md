# Claude Agent SDK for Go

A Go SDK for building agentic applications powered by Claude Code CLI.

## Installation

```bash
go get github.com/character-ai/claude-agent-sdk-go
```

**Prerequisites:**
- [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) must be installed and authenticated (for `Client` and `Agent`)
- `ANTHROPIC_API_KEY` environment variable must be set (for `APIAgent`)

## Quick Start

### Simple Query

```go
package main

import (
    "context"
    "fmt"
    "log"

    claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
    ctx := context.Background()

    text, result, err := claude.QuerySync(ctx, "What is 2 + 2?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Response:", text)
    fmt.Printf("Cost: $%.4f\n", result.Cost)
}
```

### Streaming Response

```go
events, err := claude.Query(ctx, "Explain quantum computing", claude.Options{
    MaxTurns: 5,
})
if err != nil {
    log.Fatal(err)
}

for event := range events {
    if event.Text != "" {
        fmt.Print(event.Text)
    }
}
```

### With Tools

```go
opts := claude.Options{
    AllowedTools:   []string{"Read", "Write", "Bash"},
    PermissionMode: claude.PermissionAcceptEdits,
    MaxTurns:       10,
}

client := claude.NewClient(opts)
events, err := client.Query(ctx, "Create a hello.txt file")
```

### Custom Tools (API Agent)

The `APIAgent` calls the Anthropic API directly (no CLI needed). It reads `ANTHROPIC_API_KEY` from the environment by default, or you can pass it explicitly via `APIAgentConfig.APIKey`.

```go
tools := claude.NewToolRegistry()

claude.RegisterFunc(tools, claude.ToolDefinition{
    Name:        "generate_image",
    Description: "Generate an image from a text prompt",
    InputSchema: claude.ObjectSchema(map[string]any{
        "prompt": claude.StringParam("Image description"),
        "style":  claude.EnumParam("Style", "photo", "anime", "illustration"),
    }, "prompt"),
}, func(ctx context.Context, input struct {
    Prompt string `json:"prompt"`
    Style  string `json:"style"`
}) (string, error) {
    // Your image generation logic
    return `{"url": "https://example.com/image.png"}`, nil
})

agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Model:        "claude-sonnet-4-20250514",
    SystemPrompt: "You are a creative AI assistant.",
    Tools:        tools,
    MaxTurns:     5,
})

events, err := agent.Run(ctx, "Generate a sunset image")
```

### HTTP Server with SSE

```go
http.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
    sse, _ := claude.NewSSEWriter(w)

    agent := claude.NewAgent(claude.AgentConfig{
        Options: claude.Options{
            PermissionMode: claude.PermissionAcceptEdits,
        },
        Tools:    tools,
        MaxTurns: 10,
    })

    events, _ := agent.Run(r.Context(), prompt)

    for event := range events {
        sse.WriteAgentEvent(event)
    }
    sse.Close()
})
```

## Permission Callback (CanUseTool)

The `CanUseToolFunc` callback gives you interactive control over tool execution. It is called **before** hooks, allowing you to approve, deny, or modify tool invocations programmatically.

```go
agent := claude.NewAgent(claude.AgentConfig{
    Options: claude.Options{
        PermissionMode: claude.PermissionAcceptEdits,
    },
    Tools:    tools,
    MaxTurns: 10,
    CanUseTool: func(ctx context.Context, toolName, toolUseID string, input json.RawMessage) claude.PermissionDecision {
        // Deny all Bash commands
        if toolName == "Bash" {
            return claude.PermissionDecision{
                Allow:  false,
                Reason: "Bash is not allowed in this environment",
            }
        }

        // Modify input for specific tools
        if toolName == "Write" {
            // Example: inject a safety header into the input
            return claude.PermissionDecision{
                Allow:         true,
                ModifiedInput: input, // optionally modify input here
            }
        }

        // Allow everything else
        return claude.PermissionDecision{Allow: true}
    },
})
```

The `CanUseToolFunc` is also available on `APIAgentConfig` for API-based agents.

## Hooks System

Hooks allow you to intercept and control tool execution. This is useful for:
- Blocking dangerous operations
- Modifying tool inputs
- Logging and auditing
- Implementing custom permission logic
- Monitoring lifecycle events

### PreToolUse Hooks

```go
hooks := claude.NewHooks()

// Block specific tools
hooks.OnTool("Bash").Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    var input struct {
        Command string `json:"command"`
    }
    json.Unmarshal(hc.Input, &input)

    // Block rm commands
    if strings.Contains(input.Command, "rm -rf") {
        return claude.DenyHook("Destructive commands are not allowed")
    }
    return claude.AllowHook()
})

// Log all tool executions
hooks.OnAllTools().Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    log.Printf("Tool called: %s", hc.ToolName)
    return claude.AllowHook()
})

// Modify tool input
hooks.OnTool("Write").Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    // Example: modify the input before execution
    return claude.ModifyHook(hc.Input) // optionally transform hc.Input here
})

agent := claude.NewAgent(claude.AgentConfig{
    Hooks: hooks,
    Tools: tools,
})
```

### PostToolUse Hooks

```go
hooks.OnAllTools().After(func(ctx context.Context, hc claude.HookContext, result string, isError bool) claude.HookResult {
    if isError {
        log.Printf("Tool %s failed: %s", hc.ToolName, result)
    }
    return claude.AllowHook()
})
```

### Regex Matchers

Match tools by regular expression pattern instead of exact name:

```go
// Match all MCP tools
hooks.OnToolRegex(`^mcp__.*`).Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    log.Printf("MCP tool called: %s", hc.ToolName)
    return claude.AllowHook()
})

// Match tools by prefix pattern
hooks.OnToolRegex(`^(Read|Write|Edit)$`).Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    log.Printf("File operation: %s", hc.ToolName)
    return claude.AllowHook()
})
```

### Hook Timeouts

Protect against slow hooks with timeout support:

```go
hooks.OnTool("external_api").WithTimeout(5 * time.Second).Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    // If this takes longer than 5 seconds, the hook is denied with "hook timed out"
    // Perform validation logic here (e.g., call an external service)
    log.Printf("Validating tool input for %s", hc.ToolName)
    return claude.AllowHook()
})
```

### Enhanced Hook Results

Hook results support additional fields for richer control:

```go
hooks.OnTool("search").Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
    return claude.HookResult{
        Decision:          claude.HookAllow,
        AdditionalContext: "Remember to cite sources",  // Injected into conversation
        SystemMessage:     "Be concise in responses",   // System-level instruction
        Continue:          true,                         // Continue execution
        SuppressOutput:    false,                        // Show tool output
    }
})
```

### Lifecycle Event Handlers

Monitor agent lifecycle events beyond tool execution:

```go
hooks := claude.NewHooks()

hooks.OnEvent(claude.HookSessionStart, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Session started: %s", data.SessionID)
})

hooks.OnEvent(claude.HookSessionEnd, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Session ended: %s", data.Message)
})

hooks.OnEvent(claude.HookStop, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Agent stopped: %s", data.Message)
})

hooks.OnEvent(claude.HookPostToolUseFailure, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Tool %s failed: %s", data.ToolName, data.Error)
})

hooks.OnEvent(claude.HookSubagentStart, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Subagent %s started", data.SubagentName)
})

hooks.OnEvent(claude.HookSubagentStop, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Subagent %s stopped", data.SubagentName)
})

hooks.OnEvent(claude.HookNotification, func(ctx context.Context, data claude.HookEventData) {
    log.Printf("Notification: %s", data.Message)
})
```

**Available Hook Events:**

| Event | Description |
|-------|-------------|
| `HookPreToolUse` | Before a tool is executed |
| `HookPostToolUse` | After a tool is executed successfully |
| `HookPostToolUseFailure` | After a tool execution fails |
| `HookStop` | When the agent stops (e.g., max turns reached) |
| `HookSubagentStart` | When a subagent begins execution |
| `HookSubagentStop` | When a subagent finishes execution |
| `HookNotification` | General notifications |
| `HookSessionStart` | When a session begins |
| `HookSessionEnd` | When a session ends |

## Subagents

Subagents allow you to define specialized child agents that can be invoked via a `Task` tool. When you configure subagents, a `Task` tool is automatically registered that Claude can use to delegate work.

```go
// Create tools for the researcher subagent
researchTools := claude.NewToolRegistry()
claude.RegisterFunc(researchTools, claude.ToolDefinition{
    Name:        "search",
    Description: "Search for information",
    InputSchema: claude.ObjectSchema(map[string]any{
        "query": claude.StringParam("Search query"),
    }, "query"),
}, func(ctx context.Context, input struct{ Query string `json:"query"` }) (string, error) {
    return fmt.Sprintf("Results for: %s", input.Query), nil
})

// Define subagents
subagents := claude.NewSubagentConfig()

subagents.Add(&claude.AgentDefinition{
    Name:        "researcher",
    Description: "Researches topics and returns summarized findings",
    Prompt:      "You are a research assistant. Use the search tool to find information.",
    Tools:       researchTools,
    Model:       "haiku",      // Use a faster model for research
    MaxTurns:    5,
})

subagents.Add(&claude.AgentDefinition{
    Name:        "coder",
    Description: "Writes code based on specifications",
    Prompt:      "You are a coding assistant. Write clean, documented code.",
    Model:       "sonnet",
    MaxTurns:    10,
})

// Create the main agent with subagents
agent := claude.NewAgent(claude.AgentConfig{
    Options: claude.Options{
        Model:          "claude-sonnet-4-20250514",
        PermissionMode: claude.PermissionAcceptEdits,
    },
    MaxTurns:  10,
    Subagents: subagents,
})

// Claude can now use the Task tool to delegate to subagents:
// Task(description="Research Go generics patterns", subagent_name="researcher")
events, err := agent.Run(ctx, "Research Go generics and write an example")
```

### AgentDefinition Fields

| Field | Type | Description |
|-------|------|-------------|
| `Name` | `string` | Unique identifier for the subagent |
| `Description` | `string` | Explains what the subagent does (shown to Claude) |
| `Prompt` | `string` | System prompt for the subagent |
| `Tools` | `*ToolRegistry` | Tool registry for the subagent |
| `Model` | `string` | Model to use: `"sonnet"`, `"opus"`, `"haiku"`, or `"inherit"` (parent's model) |
| `MaxTurns` | `int` | Max turns for the subagent (default: 10) |
| `Hooks` | `*Hooks` | Lifecycle hooks for the subagent |

### Subagent Events

Agent events include subagent metadata when events originate from a child agent:

```go
for event := range events {
    if event.SubagentName != "" {
        fmt.Printf("[%s] ", event.SubagentName)
    }
    if event.Content != "" {
        fmt.Print(event.Content)
    }
}
```

## File Checkpointing

Enable file checkpointing to rewind file changes to previous states:

```go
agent := claude.NewAgent(claude.AgentConfig{
    Options: claude.Options{
        EnableFileCheckpointing: true,
    },
    MaxTurns: 10,
})

events, _ := agent.Run(ctx, "Refactor the codebase")

// Later, rewind file changes to a specific message
err := agent.RewindFiles(ctx, "msg_01234567")
```

You can also use `CheckpointManager` directly:

```go
cm := claude.NewCheckpointManager(sessionID, "", ".")
err := cm.RewindFiles(ctx, userMessageID)
```

## Unified Store

All tools, skills, and hooks are stored in a unified `go-memdb`-backed store with indexed lookups and thread-safe access. Components can share a store for cross-cutting queries.

```go
// Create a shared store.
store := claude.NewStore()

// Create registries that share the store.
tools := claude.NewToolRegistryWithStore(store)
hooks := claude.NewHooksWithStore(store)
skills := claude.NewSkillRegistry(store)

// Query tools by source or tag.
mcpTools, _ := store.ListToolsBySource("mcp:my-server")
webTools, _ := store.ListToolsByTag("web")

// Point-in-time snapshot for consistent reads.
snap := store.Snapshot()
defer snap.Close()
allTools, _ := snap.Tools()
allSkills, _ := snap.Skills()
```

## Skills

Skills are composable capability bundles — groups of related tools with rich metadata (tags, categories, dependencies, examples). They enable semantic tool selection and modular agent composition.

### Registering Skills

```go
store := claude.NewStore()
skillReg := claude.NewSkillRegistry(store)

// Create tools for the skill.
webTools := claude.NewToolRegistry()
claude.RegisterFunc(webTools, claude.ToolDefinition{
    Name:        "web_search",
    Description: "Search the web for information",
    InputSchema: claude.ObjectSchema(map[string]any{
        "query": claude.StringParam("The search query"),
    }, "query"),
}, func(ctx context.Context, input struct {
    Query string `json:"query"`
}) (string, error) {
    return fmt.Sprintf("Results for: %s", input.Query), nil
})

// Register the skill with its tools.
err := skillReg.Register(claude.Skill{
    Name:        "web-research",
    Description: "Search the web and fetch page content",
    Tags:        []string{"web", "search", "research"},
    Category:    "research",
    Examples: []claude.SkillExample{
        {Query: "Find the latest news about Go", ToolsUsed: []string{"web_search"}},
    },
}, webTools)
```

### Querying Skills

```go
// By tag.
webSkills, _ := skillReg.ByTag("web")

// By category.
researchSkills, _ := skillReg.ByCategory("research")

// All skills.
all, _ := skillReg.All()
```

### Dependency Resolution

Skills can declare dependencies. `Resolve` performs transitive resolution with cycle detection:

```go
// "web-research" depends on "text-processing".
skillReg.Register(claude.Skill{
    Name:         "web-research",
    Dependencies: []string{"text-processing"},
}, webTools)

// Resolve returns a ToolRegistry with tools from both skills.
resolved, _ := skillReg.Resolve("web-research")
```

## BM25 Search

The SDK includes a zero-dependency BM25 keyword search index for semantic skill/tool selection:

```go
index := claude.NewBM25Index()

// Index skills by description + tags.
_ = index.Index("web-research", "Search the web and fetch page content", []string{"web", "search"})
_ = index.Index("math", "Perform mathematical calculations", []string{"math", "calculation"})

// Search returns ranked results.
results := index.Search("search the web for information", 3)
for _, r := range results {
    fmt.Printf("  %s (score: %.3f)\n", r.ID, r.Score)
}
```

You can also implement the `SkillIndex` interface for custom search backends (e.g., vector search).

## Context Builder (Dynamic Tool Selection)

The `ContextBuilder` selects relevant tools per turn using BM25 search over skill descriptions. This keeps the tool list small and focused, improving model performance.

```go
store := claude.NewStore()
index := claude.NewBM25Index()
skillReg := claude.NewSkillRegistry(store)

// ... register skills and index them ...

cb := claude.NewContextBuilder(store, claude.WithIndex(index), claude.WithMaxTools(10))

// Use with APIAgent for dynamic per-turn tool selection.
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Tools:          claude.NewToolRegistryWithStore(store),
    Skills:         skillReg,
    ContextBuilder: cb,
    SystemPrompt:   "You are a helpful assistant.",
})
```

When `ContextBuilder` is configured:
- Each turn, tools are selected based on the current query context
- Dependencies are resolved transitively with decaying relevance scores
- Falls back to all tools if no index is configured or query is empty

## Graceful Shutdown

The SDK provides both immediate and graceful shutdown methods:

```go
agent := claude.NewAgent(cfg)
events, _ := agent.Run(ctx, prompt)

// Graceful shutdown: sends SIGINT, waits up to 5s, then SIGKILL
err := agent.Close()

// Immediate shutdown: cancels context
agent.Stop()
```

For the low-level client:

```go
client := claude.NewClient(opts)
events, _ := client.Query(ctx, prompt)

// Graceful: SIGINT → wait 5s → SIGKILL
client.Close()

// Immediate: SIGKILL
client.Stop()
```

## Streaming Input

Send follow-up messages to a running agent via stdin:

```go
client := claude.NewClient(opts)
events, _ := client.Query(ctx, "Start a conversation")

// Send additional input while the query is running
client.Send("Here is some additional context")

// Agent-level wrapper
agent := claude.NewAgent(cfg)
events, _ := agent.Run(ctx, "Start working")
agent.Send(ctx, "Also consider edge cases")
```

## MCP Server Integration

The SDK supports Model Context Protocol (MCP) servers for custom tool integration.

### In-Process MCP Server

```go
// Create an in-process MCP server
server := claude.NewSDKMCPServer("my-tools", "1.0.0")

// Add a tool with typed input
type GreetInput struct {
    Name string `json:"name"`
}

claude.AddToolFunc(server, claude.MCPTool{
    Name:        "greet",
    Description: "Greet a user by name",
    InputSchema: claude.ObjectSchema(map[string]any{
        "name": claude.StringParam("The user's name"),
    }, "name"),
}, func(ctx context.Context, args GreetInput) (string, error) {
    return fmt.Sprintf("Hello, %s!", args.Name), nil
})

// Add another tool
type CalcInput struct {
    A  float64 `json:"a"`
    B  float64 `json:"b"`
    Op string  `json:"op"`
}

claude.AddToolFunc(server, claude.MCPTool{
    Name:        "calculate",
    Description: "Perform math operations",
    InputSchema: claude.ObjectSchema(map[string]any{
        "a":  claude.IntParam("First number"),
        "b":  claude.IntParam("Second number"),
        "op": claude.EnumParam("Operation", "add", "subtract", "multiply"),
    }, "a", "b", "op"),
}, func(ctx context.Context, args CalcInput) (string, error) {
    var result float64
    switch args.Op {
    case "add":
        result = args.A + args.B
    case "subtract":
        result = args.A - args.B
    case "multiply":
        result = args.A * args.B
    }
    return fmt.Sprintf("%.2f", result), nil
})

// Configure MCP servers
mcpServers := claude.NewMCPServers()
mcpServers.AddInProcess("tools", server)

// Convert to tool registry for use with Agent
registry := claude.NewMCPToolRegistry(mcpServers).ToToolRegistry()

// Tools are named: mcp__<server>__<tool>
// e.g., mcp__tools__greet, mcp__tools__calculate
```

### MCP Tool Annotations

Provide hints about tool behavior using annotations:

```go
server.AddTool(claude.MCPTool{
    Name:        "read_file",
    Description: "Read a file from disk",
    InputSchema: schema,
    Annotations: &claude.MCPToolAnnotations{
        ReadOnlyHint:    true,  // Only reads data, no side effects
        DestructiveHint: false, // Does not cause irreversible changes
        IdempotentHint:  true,  // Same input always produces same result
        OpenWorldHint:   true,  // Interacts with the filesystem
    },
}, handler)
```

### Combining MCP Tools with Custom Tools

```go
// Create custom tools
customTools := claude.NewToolRegistry()
claude.RegisterFunc(customTools, claude.ToolDefinition{
    Name: "custom_tool",
    // ...
}, handler)

// Create MCP tools
mcpRegistry := claude.NewMCPToolRegistry(mcpServers).ToToolRegistry()

// Merge all tools
allTools := claude.MergeToolRegistries(customTools, mcpRegistry)

agent := claude.NewAgent(claude.AgentConfig{
    Tools: allTools,
})
```

### MCP Server Status

```go
server := claude.NewSDKMCPServer("tools", "1.0.0")
// ... add tools ...

fmt.Println(server.ToolCount()) // Number of registered tools
fmt.Println(server.HasTool("greet")) // Check if a tool exists
```

## Configuration

### Options

| Field | Type | Description |
|-------|------|-------------|
| `Cwd` | `string` | Working directory for the agent |
| `CLIPath` | `string` | Path to Claude CLI (defaults to "claude" in PATH) |
| `Model` | `string` | Model to use (e.g., "claude-sonnet-4-20250514") |
| `PermissionMode` | `PermissionMode` | Tool permission handling |
| `AllowedTools` | `[]string` | List of allowed tools (filter) |
| `DisallowedTools` | `[]string` | List of disallowed tools (filter) |
| `MaxTurns` | `int` | Maximum conversation turns |
| `SystemPrompt` | `string` | System prompt override |
| `SessionID` | `string` | Continue from previous session |
| `MCPServers` | `*MCPServers` | MCP server configuration |
| `ExtraArgs` | `[]string` | Additional CLI arguments |
| `Tools` | `*ToolsConfig` | Built-in tool specification (preset or explicit names) |
| `CustomSessionID` | `string` | Explicit session ID instead of auto-generated |
| `ForkSession` | `bool` | Fork the session specified by SessionID |
| `Debug` | `bool` | Enable debug logging |
| `DebugFile` | `string` | Write debug output to file instead of stderr |
| `Betas` | `[]string` | Enable beta features by name |
| `AdditionalDirectories` | `[]string` | Extra directories to add to agent context |
| `SettingSources` | `[]string` | Additional settings source files |
| `Plugins` | `[]PluginConfig` | Plugins to load |
| `EnableFileCheckpointing` | `bool` | Enable file checkpointing for rewind |

### AgentConfig

| Field | Type | Description |
|-------|------|-------------|
| `Options` | `Options` | Base client options |
| `Tools` | `*ToolRegistry` | Custom tool registry |
| `Hooks` | `*Hooks` | Hook handlers for tool lifecycle |
| `MaxTurns` | `int` | Max turns (default: 10) |
| `CanUseTool` | `CanUseToolFunc` | Permission callback (called before hooks) |
| `Subagents` | `*SubagentConfig` | Subagent definitions for Task tool |
| `Skills` | `*SkillRegistry` | Skill-based tool organization |
| `ContextBuilder` | `*ContextBuilder` | Dynamic per-turn tool selection |

### APIAgentConfig

| Field | Type | Description |
|-------|------|-------------|
| `APIKey` | `string` | Anthropic API key (defaults to `ANTHROPIC_API_KEY` env var) |
| `Model` | `string` | Model to use (default: claude-sonnet-4-20250514) |
| `SystemPrompt` | `string` | System prompt |
| `Tools` | `*ToolRegistry` | Custom tool registry |
| `Hooks` | `*Hooks` | Hook handlers for tool lifecycle |
| `MaxTurns` | `int` | Max turns (default: 10) |
| `CanUseTool` | `CanUseToolFunc` | Permission callback (called before hooks) |
| `Subagents` | `*SubagentConfig` | Subagent definitions for Task tool |
| `Skills` | `*SkillRegistry` | Skill-based tool organization |
| `ContextBuilder` | `*ContextBuilder` | Dynamic per-turn tool selection |

### Built-in Tool Control

Use `ToolsConfig` to specify exactly which built-in tools are available (different from `AllowedTools`/`DisallowedTools` which are filters):

```go
opts := claude.Options{
    Tools: &claude.ToolsConfig{
        Preset: "code",                           // Use a preset
        Names:  []string{"Read", "Write", "Bash"}, // Or specify individual tools
    },
}
```

### Plugins

Load plugins from files:

```go
opts := claude.Options{
    Plugins: []claude.PluginConfig{
        {Type: "file", Path: "/path/to/plugin.js"},
    },
}
```

### Permission Modes

- `PermissionDefault` - Default permission handling
- `PermissionAcceptEdits` - Auto-accept file edits
- `PermissionPlan` - Plan mode only
- `PermissionBypassAll` - Bypass all permissions (use with caution)

## Error Handling

```go
import "errors"

events, err := claude.Query(ctx, "Hello")
if err != nil {
    if errors.Is(err, claude.ErrCLINotFound) {
        log.Fatal("Claude CLI not installed")
    }
    if errors.Is(err, claude.ErrAlreadyRunning) {
        log.Fatal("Client already running")
    }
    log.Fatal(err)
}

for event := range events {
    if event.Error != nil {
        var procErr *claude.ProcessError
        if errors.As(event.Error, &procErr) {
            log.Printf("Process failed (exit %d): %s", procErr.ExitCode, procErr.Stderr)
        }
        var jsonErr *claude.JSONDecodeError
        if errors.As(event.Error, &jsonErr) {
            log.Printf("JSON decode error: %v", jsonErr.Err)
        }
    }
}
```

### Error Types

| Error | Description |
|-------|-------------|
| `ErrCLINotFound` | Claude CLI not found in PATH |
| `ErrAlreadyRunning` | Client is already processing a query |
| `ErrNotRunning` | No query in progress |
| `ProcessError` | CLI process exited with error (has `ExitCode`, `Stderr`) |
| `JSONDecodeError` | Failed to parse JSON response |
| `ToolNotFoundError` | Tool not found in registry |

## Event Types

When streaming, you'll receive events of these types:

### Client Events

- `EventMessageStart` - New assistant message starting
- `EventContentBlockStart` - Content block (text or tool use) starting
- `EventContentBlockDelta` - Incremental content update
- `EventContentBlockStop` - Content block finished
- `EventToolResult` - Result from tool execution
- `EventResult` - Final result with cost and token counts

### Agent Events

- `AgentEventMessageStart` - New message starting
- `AgentEventContentDelta` - Text content delta
- `AgentEventMessageEnd` - Message finished
- `AgentEventToolUseStart` - Tool invocation starting
- `AgentEventToolUseDelta` - Tool input streaming
- `AgentEventToolUseEnd` - Tool invocation complete
- `AgentEventToolResult` - Tool execution result
- `AgentEventTurnComplete` - Turn finished (tool results sent back)
- `AgentEventComplete` - Agent finished (includes `Result` with `StopReason`)
- `AgentEventError` - Error occurred

## Examples

See the [examples](./examples) directory:

- [simple](./examples/simple) - Basic synchronous query
- [streaming](./examples/streaming) - Real-time event streaming
- [tools](./examples/tools) - Working with built-in tools
- [generation](./examples/generation) - Custom tools for image/video generation (API agent)
- [hooks](./examples/hooks) - Hook patterns: regex, timeout, lifecycle events, permissions
- [subagents](./examples/subagents) - Subagent definitions with the Task tool
- [skills](./examples/skills) - Skills, BM25 search, context builder, and dynamic tool selection

---

## Python SDK Parity

This Go SDK aims for feature parity with the [official Python Claude Agent SDK](https://github.com/anthropics/claude-agent-sdk-python).

### Feature Comparison

| Feature | Python SDK | Go SDK | Notes |
|---------|------------|--------|-------|
| **Core API** |
| Simple query function | `query()` | `Query()` / `QuerySync()` | |
| Streaming responses | `AsyncIterator` | `<-chan Event` | Go-idiomatic channels |
| Stateful client | `ClaudeSDKClient` | `Client` | |
| **Agents** |
| CLI-based agent | via ClaudeSDKClient | `Agent` | |
| Direct API agent | - | `APIAgent` | Go-only feature |
| Subagents | `SubagentConfig` | `SubagentConfig` | Auto-registers Task tool |
| **Tools** |
| Built-in tools | Read, Write, Bash | Read, Write, Bash | |
| Built-in tool control | `tools` | `ToolsConfig` | Preset or explicit names |
| Custom tools | `@tool` decorator | `RegisterFunc` | Type-safe generics |
| Tool registry | implicit | `ToolRegistry` | Explicit registry |
| **Permissions** |
| Permission callback | `can_use_tool` | `CanUseToolFunc` | Called before hooks |
| Permission modes | `permission_mode` | `PermissionMode` | |
| **MCP Integration** |
| In-process MCP servers | `create_sdk_mcp_server` | `NewSDKMCPServer` | |
| External MCP servers | stdio config | `MCPServerConfig` | |
| Tool naming | `mcp__server__tool` | `mcp__server__tool` | Same convention |
| Tool annotations | `MCPToolAnnotations` | `MCPToolAnnotations` | Behavior hints |
| **Hooks** |
| PreToolUse | `HookMatcher` | `hooks.OnTool().Before()` | Fluent API |
| PostToolUse | - | `hooks.OnTool().After()` | |
| Hook decisions | allow/deny | `AllowHook`/`DenyHook`/`ModifyHook` | |
| Wildcard matching | `"*"` | `OnAllTools()` | |
| Regex matching | regex matchers | `OnToolRegex()` | |
| Hook timeout | timeout config | `WithTimeout()` | |
| Lifecycle events | event handlers | `OnEvent()` / `EmitEvent()` | 9 event types |
| Enhanced results | additional fields | `AdditionalContext`, `SuppressOutput`, etc. | |
| **Sessions** |
| Session continuation | `session_id` | `SessionID` | |
| Custom session ID | `custom_session_id` | `CustomSessionID` | |
| Session forking | `fork_session` | `ForkSession` | |
| File checkpointing | `enable_file_checkpointing` | `EnableFileCheckpointing` | |
| File rewind | `rewind_files()` | `RewindFiles()` | |
| **Configuration** |
| Working directory | `cwd` | `Cwd` | |
| CLI path override | `cli_path` | `CLIPath` | |
| System prompt | `system_prompt` | `SystemPrompt` | |
| Max turns | `max_turns` | `MaxTurns` | |
| Debug mode | `debug` | `Debug` / `DebugFile` | |
| Betas | `betas` | `Betas` | |
| Additional directories | `additional_directories` | `AdditionalDirectories` | |
| Setting sources | `setting_sources` | `SettingSources` | |
| Plugins | `plugins` | `Plugins` | |
| **Lifecycle** |
| Graceful shutdown | `close()` | `Close()` | SIGINT → SIGKILL |
| Streaming input | `send()` | `Send()` | Stdin pipe |
| Stop reason | `stop_reason` | `StopReason` | In ResultMessage |
| **Error Handling** |
| CLI not found | `CLINotFoundError` | `ErrCLINotFound` | |
| Process error | `ProcessError` | `ProcessError` | |
| JSON decode error | `CLIJSONDecodeError` | `JSONDecodeError` | |
| Connection error | `CLIConnectionError` | - | Not implemented |
| **Content Types** |
| TextBlock | ✓ | ✓ | |
| ToolUseBlock | ✓ | ✓ | |
| ToolResultBlock | ✓ | ✓ | |
| **Skills & Context** |
| Skill registry | - | `SkillRegistry` | Go-only: composable capability bundles |
| BM25 search | - | `BM25Index` | Go-only: zero-dependency keyword search |
| Context builder | - | `ContextBuilder` | Go-only: dynamic per-turn tool selection |
| Unified store | - | `Store` | Go-only: go-memdb backed indexed storage |
| **Extras** |
| SSE HTTP helpers | - | `SSEWriter` | Go-only feature |
| HTTP handler | - | `AgentHTTPHandler` | Go-only feature |

### Go-Specific Features

Features available in the Go SDK but not in Python:

1. **Direct API Agent** (`APIAgent`) - Bypasses CLI and calls Anthropic API directly
2. **SSE Helpers** - Built-in Server-Sent Events support for HTTP streaming
3. **HTTP Handler** - Ready-to-use HTTP handler for agent endpoints
4. **Type-Safe Tool Registration** - Generics-based `RegisterFunc[T]`
5. **Skills & Context Builder** - Composable capability bundles with BM25-based dynamic tool selection
6. **Unified Store** - `go-memdb`-backed indexed storage for tools, skills, and hooks

### Python-Specific Features

Features in the Python SDK not yet in Go:

1. **CLIConnectionError** - Specific error type for connection issues

---

## License

MIT

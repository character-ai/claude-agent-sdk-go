# Claude Agent SDK for Go

A Go SDK for building agentic applications powered by Claude Code CLI.

## Installation

```bash
go get github.com/character-ai/claude-agent-sdk-go
```

**Prerequisite:** [Claude Code CLI](https://docs.anthropic.com/en/docs/claude-code) must be installed and authenticated.

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

```go
tools := claude.NewToolRegistry()

claude.RegisterFunc(tools, claude.ToolDefinition{
    Name:        "generate_image",
    Description: "Generate an image from a text prompt",
    InputSchema: claude.ObjectSchema(map[string]any{
        "prompt": claude.StringParam("Image description"),
        "style":  claude.EnumParam("Style", "photo", "anime", "illustration"),
    }, "prompt"),
}, func(ctx context.Context, input ImageInput) (string, error) {
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

## Hooks System

Hooks allow you to intercept and control tool execution. This is useful for:
- Blocking dangerous operations
- Modifying tool inputs
- Logging and auditing
- Implementing custom permission logic

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
    // Add a header to all written files
    modified := addHeaderToContent(hc.Input)
    return claude.ModifyHook(modified)
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

## Configuration

### Options

| Field | Type | Description |
|-------|------|-------------|
| `Cwd` | `string` | Working directory for the agent |
| `CLIPath` | `string` | Path to Claude CLI (defaults to "claude" in PATH) |
| `Model` | `string` | Model to use (e.g., "claude-sonnet-4-20250514") |
| `PermissionMode` | `PermissionMode` | Tool permission handling |
| `AllowedTools` | `[]string` | List of allowed tools |
| `DisallowedTools` | `[]string` | List of disallowed tools |
| `MaxTurns` | `int` | Maximum conversation turns |
| `SystemPrompt` | `string` | System prompt override |
| `SessionID` | `string` | Continue from previous session |
| `MCPServers` | `*MCPServers` | MCP server configuration |
| `ExtraArgs` | `[]string` | Additional CLI arguments |

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

- `EventMessageStart` - New assistant message starting
- `EventContentBlockStart` - Content block (text or tool use) starting
- `EventContentBlockDelta` - Incremental content update
- `EventContentBlockStop` - Content block finished
- `EventToolResult` - Result from tool execution
- `EventResult` - Final result with cost and token counts

## Examples

See the [examples](./examples) directory:

- [simple](./examples/simple) - Basic synchronous query
- [streaming](./examples/streaming) - Real-time event streaming
- [tools](./examples/tools) - Working with built-in tools
- [generation](./examples/generation) - Custom tools for image/video generation
- [server](./examples/server) - HTTP server with SSE endpoint

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
| **Tools** |
| Built-in tools | Read, Write, Bash | Read, Write, Bash | |
| Custom tools | `@tool` decorator | `RegisterFunc` | Type-safe generics |
| Tool registry | implicit | `ToolRegistry` | Explicit registry |
| **MCP Integration** |
| In-process MCP servers | `create_sdk_mcp_server` | `NewSDKMCPServer` | |
| External MCP servers | stdio config | `MCPServerConfig` | |
| Tool naming | `mcp__server__tool` | `mcp__server__tool` | Same convention |
| **Hooks** |
| PreToolUse | `HookMatcher` | `hooks.OnTool().Before()` | Fluent API |
| PostToolUse | - | `hooks.OnTool().After()` | |
| Hook decisions | allow/deny | `AllowHook`/`DenyHook`/`ModifyHook` | |
| Wildcard matching | `"*"` | `OnAllTools()` | |
| **Configuration** |
| Working directory | `cwd` | `Cwd` | |
| CLI path override | `cli_path` | `CLIPath` | |
| System prompt | `system_prompt` | `SystemPrompt` | |
| Max turns | `max_turns` | `MaxTurns` | |
| Permission modes | `permission_mode` | `PermissionMode` | |
| Allowed tools | `allowed_tools` | `AllowedTools` | |
| Session continuation | - | `SessionID` | Go-only feature |
| **Error Handling** |
| CLI not found | `CLINotFoundError` | `ErrCLINotFound` | |
| Process error | `ProcessError` | `ProcessError` | |
| JSON decode error | `CLIJSONDecodeError` | `JSONDecodeError` | |
| Connection error | `CLIConnectionError` | - | Not implemented |
| **Content Types** |
| TextBlock | | | |
| ToolUseBlock | | | |
| ToolResultBlock | | | |
| **Extras** |
| SSE HTTP helpers | - | `SSEWriter` | Go-only feature |
| HTTP handler | - | `AgentHTTPHandler` | Go-only feature |

### Go-Specific Features

Features available in the Go SDK but not in Python:

1. **Direct API Agent** (`APIAgent`) - Bypasses CLI and calls Anthropic API directly
2. **SSE Helpers** - Built-in Server-Sent Events support for HTTP streaming
3. **HTTP Handler** - Ready-to-use HTTP handler for agent endpoints
4. **Session Continuation** - Resume previous sessions via `SessionID`
5. **Type-Safe Tool Registration** - Generics-based `RegisterFunc[T]`

### Python-Specific Features

Features in the Python SDK not yet in Go:

1. **CLIConnectionError** - Specific error type for connection issues
2. **Bidirectional conversation** - `query()` then `receive_response()` pattern

---

## License

MIT

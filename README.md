# Claude Agent SDK for Go

A Go SDK for building agentic applications powered by Claude Code CLI.

## Installation

```bash
go get github.com/character-tech/claude-agent-sdk-go
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

    claude "github.com/character-tech/claude-agent-sdk-go"
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

## Configuration

### Options

| Field | Type | Description |
|-------|------|-------------|
| `Cwd` | `string` | Working directory for the agent |
| `Model` | `string` | Model to use (e.g., "claude-sonnet-4-20250514") |
| `PermissionMode` | `PermissionMode` | Tool permission handling |
| `AllowedTools` | `[]string` | List of allowed tools |
| `DisallowedTools` | `[]string` | List of disallowed tools |
| `MaxTurns` | `int` | Maximum conversation turns |
| `SystemPrompt` | `string` | System prompt override |
| `SessionID` | `string` | Continue from previous session |

### Permission Modes

- `PermissionDefault` - Default permission handling
- `PermissionAcceptEdits` - Auto-accept file edits
- `PermissionPlan` - Plan mode only
- `PermissionBypassAll` - Bypass all permissions (use with caution)

## Examples

See the [examples](./examples) directory:

- [simple](./examples/simple) - Basic synchronous query
- [streaming](./examples/streaming) - Real-time event streaming
- [tools](./examples/tools) - Working with built-in tools
- [generation](./examples/generation) - Custom tools for image/video generation
- [server](./examples/server) - HTTP server with SSE endpoint

## Event Types

When streaming, you'll receive events of these types:

- `EventMessageStart` - New assistant message starting
- `EventContentBlockStart` - Content block (text or tool use) starting
- `EventContentBlockDelta` - Incremental content update
- `EventToolResult` - Result from tool execution
- `EventResult` - Final result with cost and token counts

## License

MIT

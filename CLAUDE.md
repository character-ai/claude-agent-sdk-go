# CLAUDE.md

This file provides guidance for Claude Code when working on this repository.

## Project Overview

This is a Go SDK for building agentic applications that interact with Claude Code CLI. It provides:

- Streaming client for Claude Code CLI
- Custom tool registration and execution
- API-based agent for direct Anthropic API usage
- SSE helpers for HTTP server integration

## Package Structure

```
.
├── types.go         # Core types: Messages, Events, Options, ContentBlocks, PermissionDecision
├── client.go        # CLI client: spawns claude process, parses streaming JSON, Close/Send
├── query.go         # Convenience functions: Query, QuerySync, CollectText
├── agent.go         # CLI-based agent with tool support, canUseTool, subagents
├── api_agent.go     # Direct API agent (uses anthropic-sdk-go)
├── tools.go         # Tool registry and schema helpers
├── hooks.go         # Hook system: matchers, regex, timeout, lifecycle events
├── subagent.go      # Subagent definitions, SubagentConfig, Task tool registration
├── checkpoint.go    # CheckpointManager for file rewind
├── mcp.go           # MCP server interface, SDKMCPServer, tool annotations
├── sse.go           # SSE writer for HTTP streaming
├── errors.go        # Error types
└── examples/        # Usage examples
```

## Key Patterns

### CLI Client
The `Client` wraps the Claude Code CLI, executing it with `--output-format stream-json` and parsing newline-delimited JSON events.

### Tool Registration
Tools are registered with `RegisterFunc` which uses generics for type-safe input parsing:
```go
claude.RegisterFunc(tools, definition, func(ctx context.Context, input MyInput) (string, error) {
    // handler
})
```

### Event Streaming
All queries return `<-chan Event` for real-time processing. Events include text deltas, tool calls, tool results, and final results.

## Build Commands

```bash
go build ./...           # Build all packages
go test ./...            # Run tests
go vet ./...             # Run linter
go run examples/simple/main.go  # Run example
```

## Dependencies

- `github.com/anthropics/anthropic-sdk-go` - For API-based agent
- `github.com/tidwall/gjson` - JSON parsing (transitive)

## Code Conventions

- Package name is `claudeagent`
- Exported types use PascalCase
- Keep functions focused; prefer composition over inheritance
- Use context.Context for cancellation
- Return channels for streaming operations

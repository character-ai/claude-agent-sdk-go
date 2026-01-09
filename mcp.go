package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// MCPServer represents an MCP (Model Context Protocol) server.
type MCPServer interface {
	// Name returns the server name.
	Name() string
	// Version returns the server version.
	Version() string
	// ListTools returns available tools.
	ListTools() []MCPTool
	// CallTool executes a tool and returns the result.
	CallTool(ctx context.Context, name string, args json.RawMessage) (MCPToolResult, error)
}

// MCPTool describes a tool available on an MCP server.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// MCPToolResult is the result of calling an MCP tool.
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPContent represents content in an MCP response.
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// TextContent creates a text content block.
func TextContent(text string) MCPContent {
	return MCPContent{Type: "text", Text: text}
}

// SDKMCPServer is an in-process MCP server implementation.
// This is the Go equivalent of Python's create_sdk_mcp_server.
type SDKMCPServer struct {
	name     string
	version  string
	tools    []MCPTool
	handlers map[string]MCPToolHandler

	mu sync.RWMutex
}

// MCPToolHandler is a function that handles MCP tool calls.
type MCPToolHandler func(ctx context.Context, args json.RawMessage) (MCPToolResult, error)

// NewSDKMCPServer creates a new in-process MCP server.
func NewSDKMCPServer(name, version string) *SDKMCPServer {
	return &SDKMCPServer{
		name:     name,
		version:  version,
		tools:    make([]MCPTool, 0),
		handlers: make(map[string]MCPToolHandler),
	}
}

// Name returns the server name.
func (s *SDKMCPServer) Name() string {
	return s.name
}

// Version returns the server version.
func (s *SDKMCPServer) Version() string {
	return s.version
}

// ListTools returns all registered tools.
func (s *SDKMCPServer) ListTools() []MCPTool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tools
}

// CallTool executes the named tool.
func (s *SDKMCPServer) CallTool(ctx context.Context, name string, args json.RawMessage) (MCPToolResult, error) {
	s.mu.RLock()
	handler, ok := s.handlers[name]
	s.mu.RUnlock()

	if !ok {
		return MCPToolResult{
			Content: []MCPContent{TextContent(fmt.Sprintf("Tool not found: %s", name))},
			IsError: true,
		}, nil
	}

	return handler(ctx, args)
}

// AddTool registers a tool with the server.
func (s *SDKMCPServer) AddTool(tool MCPTool, handler MCPToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, tool)
	s.handlers[tool.Name] = handler
}

// AddToolFunc registers a typed tool handler.
func AddToolFunc[T any](s *SDKMCPServer, tool MCPTool, handler func(ctx context.Context, args T) (string, error)) {
	s.AddTool(tool, func(ctx context.Context, raw json.RawMessage) (MCPToolResult, error) {
		var args T
		if err := json.Unmarshal(raw, &args); err != nil {
			return MCPToolResult{
				Content: []MCPContent{TextContent(fmt.Sprintf("Invalid arguments: %v", err))},
				IsError: true,
			}, nil
		}
		result, err := handler(ctx, args)
		if err != nil {
			return MCPToolResult{
				Content: []MCPContent{TextContent(err.Error())},
				IsError: true,
			}, nil
		}
		return MCPToolResult{
			Content: []MCPContent{TextContent(result)},
		}, nil
	})
}

// MCPServerConfig describes an external MCP server configuration.
type MCPServerConfig struct {
	// Type is the transport type ("stdio", "sse", etc.)
	Type string `json:"type"`
	// Command is the command to run for stdio servers.
	Command string `json:"command,omitempty"`
	// Args are arguments for the command.
	Args []string `json:"args,omitempty"`
	// URL is the URL for SSE/HTTP servers.
	URL string `json:"url,omitempty"`
	// Env is environment variables for the server.
	Env map[string]string `json:"env,omitempty"`
}

// MCPServers holds both in-process and external MCP servers.
type MCPServers struct {
	// InProcess maps server names to SDK MCP servers.
	InProcess map[string]*SDKMCPServer
	// External maps server names to external server configs.
	External map[string]MCPServerConfig
}

// NewMCPServers creates a new MCP servers configuration.
func NewMCPServers() *MCPServers {
	return &MCPServers{
		InProcess: make(map[string]*SDKMCPServer),
		External:  make(map[string]MCPServerConfig),
	}
}

// AddInProcess adds an in-process SDK MCP server.
func (m *MCPServers) AddInProcess(name string, server *SDKMCPServer) {
	m.InProcess[name] = server
}

// AddExternal adds an external MCP server configuration.
func (m *MCPServers) AddExternal(name string, config MCPServerConfig) {
	m.External[name] = config
}

// GetToolName returns the full tool name for an MCP tool (mcp__serverName__toolName).
func GetToolName(serverName, toolName string) string {
	return fmt.Sprintf("mcp__%s__%s", serverName, toolName)
}

// MCPToolRegistry wraps MCP servers and provides a unified tool interface.
type MCPToolRegistry struct {
	servers *MCPServers
}

// NewMCPToolRegistry creates a registry from MCP servers.
func NewMCPToolRegistry(servers *MCPServers) *MCPToolRegistry {
	return &MCPToolRegistry{servers: servers}
}

// ToToolRegistry converts MCP servers to a standard ToolRegistry.
// Tool names are prefixed with "mcp__serverName__".
func (r *MCPToolRegistry) ToToolRegistry() *ToolRegistry {
	registry := NewToolRegistry()

	if r.servers == nil {
		return registry
	}

	// Add in-process server tools
	for serverName, server := range r.servers.InProcess {
		for _, tool := range server.ListTools() {
			fullName := GetToolName(serverName, tool.Name)
			def := ToolDefinition{
				Name:        fullName,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
			}

			// Capture variables for closure
			srv := server
			toolName := tool.Name
			registry.Register(def, func(ctx context.Context, input json.RawMessage) (string, error) {
				result, err := srv.CallTool(ctx, toolName, input)
				if err != nil {
					return "", err
				}
				if result.IsError && len(result.Content) > 0 {
					return "", fmt.Errorf("%s", result.Content[0].Text)
				}
				if len(result.Content) > 0 {
					return result.Content[0].Text, nil
				}
				return "", nil
			})
		}
	}

	return registry
}

// MergeToolRegistries combines multiple tool registries into one.
func MergeToolRegistries(registries ...*ToolRegistry) *ToolRegistry {
	merged := NewToolRegistry()
	for _, r := range registries {
		merged.Merge(r)
	}
	return merged
}

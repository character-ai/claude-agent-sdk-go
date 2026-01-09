package claudeagent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSDKMCPServer(t *testing.T) {
	server := NewSDKMCPServer("test-server", "1.0.0")

	if server.Name() != "test-server" {
		t.Fatalf("unexpected name: %s", server.Name())
	}
	if server.Version() != "1.0.0" {
		t.Fatalf("unexpected version: %s", server.Version())
	}
}

func TestSDKMCPServerAddTool(t *testing.T) {
	server := NewSDKMCPServer("test", "1.0")

	server.AddTool(MCPTool{
		Name:        "greet",
		Description: "Greet a user",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
			"required": []string{"name"},
		},
	}, func(ctx context.Context, args json.RawMessage) (MCPToolResult, error) {
		var input struct {
			Name string `json:"name"`
		}
		json.Unmarshal(args, &input)
		return MCPToolResult{
			Content: []MCPContent{TextContent("Hello, " + input.Name + "!")},
		}, nil
	})

	tools := server.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "greet" {
		t.Fatalf("unexpected tool name: %s", tools[0].Name)
	}
}

func TestSDKMCPServerCallTool(t *testing.T) {
	server := NewSDKMCPServer("test", "1.0")

	server.AddTool(MCPTool{
		Name:        "echo",
		Description: "Echo input",
	}, func(ctx context.Context, args json.RawMessage) (MCPToolResult, error) {
		var input struct {
			Message string `json:"message"`
		}
		json.Unmarshal(args, &input)
		return MCPToolResult{
			Content: []MCPContent{TextContent(input.Message)},
		}, nil
	})

	result, err := server.CallTool(context.Background(), "echo", json.RawMessage(`{"message":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "test" {
		t.Fatalf("unexpected text: %s", result.Content[0].Text)
	}
}

func TestSDKMCPServerToolNotFound(t *testing.T) {
	server := NewSDKMCPServer("test", "1.0")

	result, err := server.CallTool(context.Background(), "nonexistent", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestAddToolFunc(t *testing.T) {
	type GreetInput struct {
		Name string `json:"name"`
	}

	server := NewSDKMCPServer("test", "1.0")
	AddToolFunc(server, MCPTool{
		Name:        "greet",
		Description: "Greet a user",
	}, func(ctx context.Context, args GreetInput) (string, error) {
		return "Hello, " + args.Name + "!", nil
	})

	result, err := server.CallTool(context.Background(), "greet", json.RawMessage(`{"name":"World"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content[0].Text != "Hello, World!" {
		t.Fatalf("unexpected result: %s", result.Content[0].Text)
	}
}

func TestMCPServers(t *testing.T) {
	servers := NewMCPServers()

	server1 := NewSDKMCPServer("server1", "1.0")
	server1.AddTool(MCPTool{Name: "tool1", Description: "Tool 1"}, func(ctx context.Context, args json.RawMessage) (MCPToolResult, error) {
		return MCPToolResult{Content: []MCPContent{TextContent("result1")}}, nil
	})

	server2 := NewSDKMCPServer("server2", "1.0")
	server2.AddTool(MCPTool{Name: "tool2", Description: "Tool 2"}, func(ctx context.Context, args json.RawMessage) (MCPToolResult, error) {
		return MCPToolResult{Content: []MCPContent{TextContent("result2")}}, nil
	})

	servers.AddInProcess("s1", server1)
	servers.AddInProcess("s2", server2)

	if len(servers.InProcess) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers.InProcess))
	}
}

func TestMCPToolRegistry(t *testing.T) {
	servers := NewMCPServers()

	server := NewSDKMCPServer("myserver", "1.0")
	server.AddTool(MCPTool{
		Name:        "mytool",
		Description: "My tool",
		InputSchema: ObjectSchema(map[string]any{
			"input": StringParam("The input"),
		}, "input"),
	}, func(ctx context.Context, args json.RawMessage) (MCPToolResult, error) {
		return MCPToolResult{Content: []MCPContent{TextContent("executed")}}, nil
	})

	servers.AddInProcess("myserver", server)

	mcpRegistry := NewMCPToolRegistry(servers)
	toolRegistry := mcpRegistry.ToToolRegistry()

	// Check tool was registered with correct name
	fullName := GetToolName("myserver", "mytool")
	if !toolRegistry.Has(fullName) {
		t.Fatalf("tool %s not found", fullName)
	}

	// Execute the tool
	result, err := toolRegistry.Execute(context.Background(), fullName, json.RawMessage(`{"input":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "executed" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestGetToolName(t *testing.T) {
	name := GetToolName("server", "tool")
	if name != "mcp__server__tool" {
		t.Fatalf("unexpected tool name: %s", name)
	}
}

func TestMergeToolRegistries(t *testing.T) {
	reg1 := NewToolRegistry()
	reg1.Register(ToolDefinition{Name: "tool1", Description: "Tool 1"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result1", nil
	})

	reg2 := NewToolRegistry()
	reg2.Register(ToolDefinition{Name: "tool2", Description: "Tool 2"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result2", nil
	})

	merged := MergeToolRegistries(reg1, reg2)

	if !merged.Has("tool1") {
		t.Fatal("tool1 not found")
	}
	if !merged.Has("tool2") {
		t.Fatal("tool2 not found")
	}
	if len(merged.Definitions()) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(merged.Definitions()))
	}
}

func TestMergeToolRegistriesWithNil(t *testing.T) {
	reg1 := NewToolRegistry()
	reg1.Register(ToolDefinition{Name: "tool1", Description: "Tool 1"}, func(ctx context.Context, input json.RawMessage) (string, error) {
		return "result1", nil
	})

	// Should not panic with nil
	merged := MergeToolRegistries(reg1, nil)
	if !merged.Has("tool1") {
		t.Fatal("tool1 not found")
	}
}

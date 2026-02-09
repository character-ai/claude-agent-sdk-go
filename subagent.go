package claudeagent

import (
	"context"
	"encoding/json"
	"fmt"
)

// AgentDefinition describes a named subagent that can be invoked via the Task tool.
type AgentDefinition struct {
	// Name uniquely identifies this subagent.
	Name string
	// Description explains what this subagent does (shown to Claude).
	Description string
	// Prompt is the system prompt for the subagent.
	Prompt string
	// Tools is the tool registry for the subagent.
	Tools *ToolRegistry
	// Model specifies the model to use ("sonnet", "opus", "haiku", or "inherit").
	// "inherit" or empty uses the parent agent's model.
	Model string
	// MaxTurns limits the number of turns for the subagent. 0 = use default.
	MaxTurns int
	// Hooks configures lifecycle hooks for the subagent.
	Hooks *Hooks
}

// SubagentConfig holds the set of available subagent definitions.
type SubagentConfig struct {
	// Agents maps subagent names to their definitions.
	Agents map[string]*AgentDefinition
}

// NewSubagentConfig creates a new subagent configuration.
func NewSubagentConfig() *SubagentConfig {
	return &SubagentConfig{
		Agents: make(map[string]*AgentDefinition),
	}
}

// Add registers an agent definition.
func (sc *SubagentConfig) Add(def *AgentDefinition) {
	sc.Agents[def.Name] = def
}

// taskToolInput is the input schema for the auto-registered Task tool.
type taskToolInput struct {
	Description  string `json:"description"`
	SubagentName string `json:"subagent_name"`
}

// registerTaskTool adds the "Task" tool to the registry, which dispatches to subagents.
func registerTaskTool(registry *ToolRegistry, subagents *SubagentConfig, parentOpts Options) {
	// Build description listing available subagents
	desc := "Launch a subagent to handle a task. Available subagents:\n"
	for name, def := range subagents.Agents {
		desc += fmt.Sprintf("- %s: %s\n", name, def.Description)
	}

	def := ToolDefinition{
		Name:        "Task",
		Description: desc,
		InputSchema: ObjectSchema(
			map[string]any{
				"description":   StringParam("The task description for the subagent to perform"),
				"subagent_name": StringParam("The name of the subagent to use"),
			},
			"description", "subagent_name",
		),
	}

	registry.Register(def, func(ctx context.Context, input json.RawMessage) (string, error) {
		var ti taskToolInput
		if err := json.Unmarshal(input, &ti); err != nil {
			return "", fmt.Errorf("invalid Task input: %w", err)
		}

		agentDef, ok := subagents.Agents[ti.SubagentName]
		if !ok {
			return "", fmt.Errorf("subagent not found: %s", ti.SubagentName)
		}

		return runSubagent(ctx, agentDef, ti.Description, parentOpts)
	})
}

// runSubagent creates and runs a child agent with the given definition.
func runSubagent(ctx context.Context, def *AgentDefinition, task string, parentOpts Options) (string, error) {
	// Determine model - use parent's if "inherit" or empty
	model := def.Model
	if model == "" || model == "inherit" {
		model = parentOpts.Model
	}

	// Resolve model shorthand
	switch model {
	case "sonnet":
		model = "claude-sonnet-4-20250514"
	case "opus":
		model = "claude-opus-4-20250514"
	case "haiku":
		model = "claude-haiku-4-5-20251001"
	}

	maxTurns := def.MaxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}

	// Build the child prompt
	prompt := task
	if def.Prompt != "" {
		prompt = def.Prompt + "\n\n" + task
	}

	// Create child agent config
	childOpts := Options{
		Cwd:            parentOpts.Cwd,
		CLIPath:        parentOpts.CLIPath,
		Model:          model,
		PermissionMode: parentOpts.PermissionMode,
		SystemPrompt:   def.Prompt,
		MaxTurns:       maxTurns,
	}

	child := NewAgent(AgentConfig{
		Options:  childOpts,
		Tools:    def.Tools,
		Hooks:    def.Hooks,
		MaxTurns: maxTurns,
	})

	// Emit SubagentStart hook via parent hooks if available
	if def.Hooks != nil {
		def.Hooks.EmitEvent(ctx, HookEventData{
			Event:        HookSubagentStart,
			SubagentName: def.Name,
			Message:      task,
		})
	}

	result, err := child.RunSync(ctx, prompt)

	// Emit SubagentStop hook
	if def.Hooks != nil {
		def.Hooks.EmitEvent(ctx, HookEventData{
			Event:        HookSubagentStop,
			SubagentName: def.Name,
			Message:      result,
		})
	}

	if err != nil {
		return "", fmt.Errorf("subagent %s failed: %w", def.Name, err)
	}

	return result, nil
}

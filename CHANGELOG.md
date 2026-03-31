# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added

#### Artifact System (`ArtifactRegistry`)

An in-memory artifact system that lets agents generate self-contained HTML, JSX, or text content — similar to Claude.ai's artifacts.

- **Typed artifacts** — `html`, `jsx`, or `text` with validation
- **Two tools** — `create_artifact` and `update_artifact`, auto-registered via `Tools()`
- **System prompt** — `SystemPrompt()` returns instructions that teach the agent when/how to produce artifacts
- **Versioned** — each update increments the version; `UpdatedAt` tracks last modification
- **Thread-safe** — safe for concurrent access; tools registry is cached after first `Tools()` call
- **Ordered** — `All()` returns artifacts in creation order

```go
artifacts := claude.NewArtifactRegistry()

agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Tools:        artifacts.Tools(),
    SystemPrompt: artifacts.SystemPrompt(),
})

events, _ := agent.Run(ctx, "Create an interactive dashboard")
for range events {} // drain

for _, a := range artifacts.All() {
    // a.ID, a.Type ("html"|"jsx"|"text"), a.Title, a.Content
}
```

New types: `Artifact`, `ArtifactType`, `ArtifactRegistry`.
New constants: `ArtifactHTML`, `ArtifactJSX`, `ArtifactText`.

#### Todo Tracking (`EnableTodos` / `TodoStore`)

A built-in `write_todos` tool that lets the agent plan and track its own work.
The host app receives `AgentEventTodosUpdated` events whenever the list changes.

- **Opt-in** — set `EnableTodos: true` on `AgentConfig` or `APIAgentConfig`
- **Idempotent writes** — the tool replaces the entire list on each call, avoiding partial-update bugs
- **Shared store** — pass a `TodoStore` to share todo state across parent and child agents
- **Validation** — rejects items with missing fields, invalid status/priority, duplicate IDs, or dangling `parent_id` references
- **Live events** — `AgentEventTodosUpdated` carries the full `[]TodoItem` snapshot
- **Read tool** — `read_todos` lets the agent refresh its view of pending work after history compaction

Configure via `AgentConfig.EnableTodos` or `APIAgentConfig.EnableTodos`:

```go
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    EnableTodos: true,
    // ...
})

events, _ := agent.Run(ctx, prompt)
for event := range events {
    if event.Type == claude.AgentEventTodosUpdated {
        for _, todo := range event.Todos {
            fmt.Printf("[%s] %s\n", todo.Status, todo.Description)
        }
    }
}
```

New types: `TodoItem`, `TodoStatus`, `TodoPriority`, `TodoStore`.
New field on `AgentEvent`: `Todos []TodoItem`.
New event type: `AgentEventTodosUpdated`.
New config fields: `EnableTodos bool`, `TodoStore *TodoStore` (on both `AgentConfig` and `APIAgentConfig`).
New accessor: `Agent.TodoStore()` / `APIAgent.TodoStore()`.
New constants: `TodoToolName`, `ReadTodosToolName`.
New helpers: `RegisterTodosTools`, `initTodoStore`, `emitTodoEvents`, `validateTodos`.

#### Metrics Collection (`MetricsCollector`)

A new `MetricsCollector` type that gathers per-turn and per-tool execution metrics with zero overhead when not configured.

- **Session duration** — wall-clock time from session start to end
- **Per-turn `TurnMetrics`** — LLM latency (time waiting for the model), turn index, and list of tools invoked
- **Per-tool `ToolStats`** — call count, failure count, total and average execution time

Configure via `AgentConfig.Metrics` or `APIAgentConfig.Metrics`:

```go
mc := claude.NewMetricsCollector()
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Metrics: mc,
    // ...
})
events, _ := agent.Run(ctx, prompt)
for range events {} // drain

snap := mc.Snapshot() // thread-safe, copy-on-read
fmt.Println(snap.SessionDuration)
fmt.Println(snap.Turns[0].LLMLatency)
fmt.Println(snap.ToolStats["search"].AvgTime())
```

`TurnMetrics` is also emitted live on every `AgentEventTurnComplete` event as `event.TurnMetrics`, so consumers see per-turn data in the event stream without polling.

New types: `MetricsCollector`, `LoopMetrics`, `TurnMetrics`, `ToolStats`.
New field on `AgentEvent`: `TurnMetrics *TurnMetrics`.

#### Parallel Tool Execution (`ParallelTools`)

When the LLM returns multiple tool calls in one turn, they can now run concurrently instead of sequentially.

Configure via `AgentConfig.ParallelTools` or `APIAgentConfig.ParallelTools`:

```go
agent := claude.NewAgent(claude.AgentConfig{
    ParallelTools: true,
    // ...
})
```

- Results are always returned **in input order** regardless of goroutine completion order.
- Only enable for tools with **no inter-dependencies** — tools that share mutable state or must run in sequence should keep the default `false`.
- For a single tool call in a turn, no goroutine is spawned (no overhead).

#### Shared `executeOneTool` helper (`execute.go`)

Internal refactor: both `Agent` and `APIAgent` now share a single `executeOneTool` function that runs the full permission → pre-hooks → execution → metrics → post-hooks pipeline. Eliminates duplicated logic between the two agent types.

#### Retry Logic (`retry.go`)

`RetryConfig` adds automatic retry with exponential backoff to tool execution.
Attach globally via `AgentConfig.Retry` / `APIAgentConfig.Retry`, or per-tool
via `ToolDefinition.RetryConfig` — per-tool takes precedence.

```go
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Retry: &claude.RetryConfig{
        MaxAttempts: 3,
        Backoff:     500 * time.Millisecond,
        RetryOn: func(err error) bool {
            return strings.Contains(err.Error(), "rate limit")
        },
    },
})
```

- `MaxAttempts`: total attempts including the first; 0 or 1 = no retry
- `Backoff`: base wait before first retry; doubles each attempt
- `RetryOn`: predicate to filter retryable errors; nil = retry on any error
- Context cancellation is respected between retry sleeps

New type: `RetryConfig`.
New field on `ToolDefinition`: `RetryConfig *RetryConfig` (json:"-").
New method on `ToolRegistry`: `ToolRetryConfig(name string) *RetryConfig`.
New package-level helper: `executeWithRetry`.

#### Budget Controls (`budget.go`)

`BudgetConfig` stops the session with a `*BudgetExceededError` when any resource
limit is exceeded. All three limits are independent; zero values are unlimited.

```go
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Budget: &claude.BudgetConfig{
        MaxTokens:   50_000,
        MaxCostUSD:  0.50,             // CLI Agent only
        MaxDuration: 2 * time.Minute,
    },
})
```

- `MaxTokens`: cumulative input+output tokens. Works for both agents.
  `APIAgent` captures usage from `MessageStartEvent` / `MessageDeltaEvent`.
- `MaxCostUSD`: cumulative USD cost via `ResultMessage.Cost`; CLI `Agent` only.
- `MaxDuration`: wall-clock time since session start; both agents.

Budget is checked at the **start** of each turn (time limit) and **after** each
LLM call (token and cost limits). The session emits `AgentEventError` with a
`*BudgetExceededError` when stopped.

New types: `BudgetConfig`, `BudgetExceededError`.

#### History Compaction (`history.go`)

`HistoryConfig` trims the conversation history sent to the LLM on each turn,
preventing unbounded context-window growth. The full history is always kept
in memory — only the LLM's view is compacted.

```go
agent := claude.NewAgent(claude.AgentConfig{
    History: &claude.HistoryConfig{
        MaxTurns:        5,    // keep last 5 assistant+tool turns
        DropToolResults: true, // replace old tool results with placeholder
    },
})
```

- `MaxTurns`: rolling window on turn count; initial prompt always preserved.
  Works for both `Agent` (`compactHistory`) and `APIAgent` (`compactMessages`).
- `DropToolResults`: replaces tool-result content in older turns with
  `[tool result omitted]`. CLI `Agent` only (use `MaxTurns` for `APIAgent`).

New type: `HistoryConfig`.
New helpers: `compactHistory([]ConversationMessage, *HistoryConfig)`,
`compactMessages([]anthropic.MessageParam, *HistoryConfig)`.

#### Tool Annotations (`ToolAnnotations`)

Safety and behavior metadata for tools, enabling smarter harness decisions without changing tool execution.

- **`ReadOnly`** — tool does not modify state
- **`Destructive`** — tool makes hard-to-reverse changes
- **`ConcurrencySafe`** — tool can run in parallel with other safe tools
- **`SearchHint`** — keyword phrase for deferred tool discovery

```go
tools.Register(claude.ToolDefinition{
    Name: "web_search",
    Annotations: &claude.ToolAnnotations{
        ReadOnly:        true,
        ConcurrencySafe: true,
        SearchHint:      "search the web for information",
    },
    // ...
}, handler)
```

New type: `ToolAnnotations`. New field on `ToolDefinition`: `Annotations *ToolAnnotations`.
New methods on `ToolRegistry`: `IsConcurrencySafe(name)`, `ToolAnnotations(name)`.

#### Per-Tool Concurrency (`runToolsSmart`)

Replaces the global `ParallelTools` bool with per-tool concurrency decisions based on `ToolAnnotations.ConcurrencySafe`.

- Tools annotated `ConcurrencySafe: true` run in parallel with each other
- Unannotated tools fall back to the global `ParallelTools` setting
- Unsafe tools act as barriers — sequential execution with exclusive access
- Results always returned in original call order

The global `ParallelTools` field remains as the default for unannotated tools.

#### Three-Stage Tool Validation

Two new optional callbacks on `ToolDefinition` that run before the handler:

- **`CheckPermissions`** — tool-specific permission check (after pre-hooks, before validation)
- **`ValidateInput`** — input validation (after permission check, before execution)

Both are `json:"-"` and nil by default. The execution pipeline is now: CanUseTool → PreHooks → CheckPermissions → ValidateInput → Execute → PostHooks.

```go
claude.RegisterFunc(tools, claude.ToolDefinition{
    Name: "delete_file",
    CheckPermissions: func(ctx context.Context, input json.RawMessage) error {
        // check user has delete access
    },
    ValidateInput: func(ctx context.Context, input json.RawMessage) error {
        // validate path is safe
    },
}, handler)
```

New types: `ToolValidator`, `ToolPermissionCheck`.
New method on `ToolRegistry`: `GetToolDef(name)`.

#### Structured Tool Results (`ToolResultMetadata`)

Tool handlers can now return metadata alongside string content, enabling tools to inject messages, provide system context, or suggest follow-up actions.

```go
tools.RegisterStructured(def, func(ctx context.Context, input json.RawMessage) (string, *claude.ToolResultMetadata, error) {
    return "result", &claude.ToolResultMetadata{
        InjectMessages: []claude.ConversationMessage{{Role: "user", Content: "extra context"}},
        SystemContext:  "user is authenticated",
    }, nil
})
```

- `InjectMessages` are appended to history after tool results
- `Metadata` field on `ToolResponse` carries metadata (not serialized to JSON)
- `RegisterStructuredFunc[T]` provides type-safe generic registration
- `ExecuteStructured` returns both content and metadata

New types: `ToolResultMetadata`, `StructuredToolHandler`.
New methods: `RegisterStructured`, `RegisterStructuredFunc`, `ExecuteStructured`.

#### System Prompt Cache Boundary (`SystemPromptBlock`)

Support for Anthropic's prompt caching via structured system prompt blocks on `APIAgent`.

```go
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    SystemPromptBlocks: []claude.SystemPromptBlock{
        {Text: staticInstructions, CacheControl: &claude.CacheControl{Type: "ephemeral"}},
        {Text: dynamicContext}, // not cached
    },
})
```

When `SystemPromptBlocks` is set, `SystemPrompt` is ignored. Each block can have `CacheControl` set independently, enabling cache boundaries between static and dynamic content.

New types: `SystemPromptBlock`, `CacheControl`.

#### Max-Tokens Recovery (`MaxTokensRecovery`)

Automatic retry with increased `max_tokens` when the API truncates output (stop reason `max_tokens` with no tool calls).

```go
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    MaxTokens: 4096,
    MaxTokensRecovery: &claude.MaxTokensRecovery{
        ScaleFactor: 2.0,  // double max_tokens each retry
        MaxRetries:  2,    // up to 2 retries
        Ceiling:     16384,
    },
})
```

Distinct from the mid-tool-call truncation error (which returns `AgentEventError`). This recovery is for legitimate end-of-turn truncation. APIAgent only.

New type: `MaxTokensRecovery`.

#### Fallback Model (`FallbackModelConfig`)

Automatic model switching on persistent API errors for `APIAgent`.

```go
agent := claude.NewAPIAgent(claude.APIAgentConfig{
    Model: "claude-sonnet-4-20250514",
    FallbackModel: &claude.FallbackModelConfig{
        Model:       "claude-haiku-4-5-20251001",
        AfterErrors: 3,
        RevertAfter: 5 * time.Minute,
    },
})
```

- Switches to fallback after `AfterErrors` consecutive errors (default: 3)
- Reverts to primary on success or after `RevertAfter` duration
- `RevertAfter: 0` means stay on fallback for the rest of the session

New type: `FallbackModelConfig`.

#### System Reminders (`SystemReminder`)

Utility for injecting `<system-reminder>` tagged content into conversation history mid-conversation.

```go
reminders := []claude.SystemReminder{
    {Content: "The user prefers concise answers"},
}
history = claude.InjectReminders(history, reminders)
```

Pure utility — no agent loop modifications required. Callers inject via hooks or direct history manipulation.

New type: `SystemReminder`.
New functions: `FormatReminder`, `FormatReminders`, `InjectReminders`.

#### History Summarization (`HistorySummarizer`)

Extends history compaction with an optional summarization stage. When turns exceed `MaxTurns`, dropped turns can be summarized instead of silently discarded.

```go
agent := claude.NewAgent(claude.AgentConfig{
    History: &claude.HistoryConfig{
        MaxTurns: 10,
        Summarizer: func(ctx context.Context, msgs []claude.ConversationMessage) (string, error) {
            // Call an LLM to summarize the dropped turns
            return summary, nil
        },
    },
})
```

- Summary prepended as `[Previous conversation summary]` user message
- `SummarizeThreshold` controls how many excess turns trigger summarization
- Summarizer errors fall back silently to the existing drop behavior

New type: `HistorySummarizer`.

#### Deferred Tool Loading (`DeferredToolRegistry`)

ToolSearch pattern for large tool registries — tools are not loaded into the active context until the model explicitly requests them.

```go
deferred := claude.NewDeferredToolRegistry(func(ctx context.Context, name string) (*claude.ToolDefinition, claude.ToolHandler, error) {
    // resolve tool definition from config, database, etc.
})
deferred.Add(claude.DeferredTool{Name: "rare_tool", Description: "...", SearchHint: "..."})

claude.RegisterToolSearchTool(tools, deferred)
```

- Registers a `ToolSearch` tool that the model invokes to discover and load tools on demand
- Simple keyword matching against name, description, and search hint
- Loaded tools are registered into the active `ToolRegistry` automatically

New types: `DeferredTool`, `DeferredToolLoader`, `DeferredToolRegistry`.
New function: `RegisterToolSearchTool`.

### Changed

- `ToolDefinition` gains three new fields: `Annotations *ToolAnnotations`, `ValidateInput ToolValidator`, `CheckPermissions ToolPermissionCheck`. All nil by default.
- `ToolResponse` gains `Metadata *ToolResultMetadata` (json:"-", nil unless tool returns structured results).
- `AgentConfig` gains optional fields: `Metrics`, `ParallelTools`, `Retry`, `Budget`, `History`, `EnableTodos`, `TodoStore`. Zero values preserve existing behavior.
- `APIAgentConfig` gains the same fields plus `SystemPromptBlocks`, `MaxTokensRecovery`, `FallbackModel`.
- `APIAgent` replaces internal `model string` with `modelSelector` state machine for fallback support.
- `AgentEvent` gains `TurnMetrics *TurnMetrics` and `Todos []TodoItem` optional fields.
- `HistoryConfig` gains `Summarizer HistorySummarizer` and `SummarizeThreshold int`.
- `compactHistory` and `compactMessages` now accept `context.Context` as first parameter.
- Tool execution pipeline extended: CanUseTool → PreHooks → CheckPermissions → ValidateInput → Execute → PostHooks.
- `executeTools` in both agents now uses `runToolsSmart` for per-tool concurrency decisions.
- `runToolsSequential` removed (replaced by `runToolsSmart`).

---

## Prior to changelog

This project did not maintain a changelog before this entry. See the [git log](https://github.com/character-ai/claude-agent-sdk-go/commits/main) for full history.

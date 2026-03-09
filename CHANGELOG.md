# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added

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

### Changed

- `AgentConfig` gains five new optional fields: `Metrics`, `ParallelTools`, `Retry`, `Budget`, `History`. Zero values preserve existing behavior.
- `APIAgentConfig` gains the same five fields (`Budget.MaxCostUSD` is a no-op for APIAgent).
- `AgentEvent` gains one new optional field: `TurnMetrics *TurnMetrics`. Nil unless `MetricsCollector` is configured.
- `AgentEventTurnComplete` now carries `TurnMetrics` when a collector is active.
- `APIAgent.streamTurn` now returns an `apiTurnUsage` value (unexported) so
  token counts are available to the budget tracker.
- `ToolDefinition` gains `RetryConfig *RetryConfig` (not serialised to JSON).

---

## Prior to changelog

This project did not maintain a changelog before this entry. See the [git log](https://github.com/character-ai/claude-agent-sdk-go/commits/main) for full history.

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

### Changed

- `AgentConfig` gains two new optional fields: `Metrics *MetricsCollector` and `ParallelTools bool`. Zero values preserve existing behavior.
- `APIAgentConfig` gains the same two fields.
- `AgentEvent` gains one new optional field: `TurnMetrics *TurnMetrics`. Nil unless `MetricsCollector` is configured.
- `AgentEventTurnComplete` now carries `TurnMetrics` when a collector is active.

---

## Prior to changelog

This project did not maintain a changelog before this entry. See the [git log](https://github.com/character-ai/claude-agent-sdk-go/commits/main) for full history.

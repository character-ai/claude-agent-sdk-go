# Architecture Design: Unified Store, Skills, and Semantic Context

## Problem Statement

The SDK uses 6 independent ad-hoc storage structures:

| Component | Storage | Thread Safety | Removal | Indexing |
|-----------|---------|---------------|---------|----------|
| ToolRegistry | slice + map | None | No | O(n) scan |
| Hooks.Matchers | slice | None | No | O(n) scan per tool call |
| Hooks.EventHandlers | map | None | No | Direct map lookup |
| SubagentConfig | map | None | No | Direct map lookup |
| SDKMCPServer | slice + map | sync.RWMutex | No | O(n) scan |
| MCPServers | 2 maps | None | No | Direct map lookup |

This creates several issues:
- **No thread safety** — concurrent tool registration races
- **No removal** — tools/hooks can only be added, never removed
- **No cross-component queries** — "which tools came from skill X?" requires manual tracking
- **No semantic search** — all tools sent every turn regardless of relevance

## Why go-memdb

| Approach | ACID Txns | Indexes | Memory | Deps | Complexity |
|----------|-----------|---------|--------|------|------------|
| Raw maps + sync.Mutex | No | Manual | Low | 0 | High (error-prone) |
| sync.Map | No | None | Low | 0 | Medium |
| **go-memdb** | **Yes** | **Built-in** | **Medium** | **1** | **Low** |
| Bleve/full-text | Yes | Full-text | High | 10+ | Medium |
| SQLite | Yes | SQL indexes | Medium | CGo | High |

go-memdb provides:
- **Immutable radix trees** — lock-free concurrent reads via snapshots
- **Multi-field indexing** — query tools by name, source, or tags in O(log n)
- **ACID transactions** — consistent reads across multiple tables
- **Zero CGo** — pure Go, compiles everywhere
- **Single dependency** — `hashicorp/go-memdb` (+ its `immutable` dep)

## Skills Concept

Inspired by Semantic Kernel (Microsoft) and LangGraph tool groups, a **Skill** is a composable capability bundle:

```
Skill: "web-research"
├── Tags: ["search", "web", "scraping"]
├── Category: "research"
├── Tools:
│   ├── web_search (search the web)
│   ├── fetch_page (fetch and parse a URL)
│   └── summarize  (summarize content)
├── Dependencies: ["text-processing"]
└── Examples:
    └── "Find the latest pricing for product X"
```

Skills enable:
- **Grouping** — related tools packaged together
- **Discovery** — find capabilities by tag or category
- **Dependency resolution** — skill A depends on skill B's tools
- **Selective loading** — only activate skills relevant to the task

## BM25 for Tool Selection

Why BM25 over vector/embedding search:
- **Zero dependencies** — ~150 lines of Go, no model downloads
- **Deterministic** — same query always returns same results
- **Fast** — no inference latency, pure term matching
- **Sufficient** — tool descriptions are short, keyword-rich text
- **Composable** — easily combined with tag-based filtering

The BM25 implementation uses standard parameters (k1=1.2, b=0.75) with simple whitespace+punctuation tokenization. For most SDK use cases (10-100 tools), this provides excellent selection without the overhead of embedding models.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                    Agent Loop                         │
│  ┌──────────────────────────────────────────────┐    │
│  │           ContextBuilder                      │    │
│  │  query → BM25 search → skill resolution →     │    │
│  │  tool selection → ToolDefinition[]            │    │
│  └──────────────┬───────────────────────────────┘    │
│                 │                                      │
│  ┌──────────────▼───────────────────────────────┐    │
│  │              Store (go-memdb)                  │    │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐        │    │
│  │  │  tools  │ │ skills  │ │  hooks  │        │    │
│  │  │ (name)  │ │ (name)  │ │  (id)   │        │    │
│  │  │ (source)│ │ (categ) │ │(pattern)│        │    │
│  │  │ (tags)  │ │ (tags)  │ │ (event) │        │    │
│  │  └─────────┘ └─────────┘ └─────────┘        │    │
│  └──────────────────────────────────────────────┘    │
│                                                        │
│  ┌──────────────────┐  ┌──────────────────────┐      │
│  │  ToolRegistry    │  │  SkillRegistry       │      │
│  │  (reads/writes   │  │  (reads/writes       │      │
│  │   tools table)   │  │   skills+tools)      │      │
│  └──────────────────┘  └──────────────────────┘      │
└──────────────────────────────────────────────────────┘
```

### Data Flow

1. **Registration**: `SkillRegistry.Register(skill, tools)` writes to both `skills` and `tools` tables
2. **Indexing**: `BM25Index` indexes skill/tool descriptions on registration
3. **Selection**: Each turn, `ContextBuilder.SelectTools(query)` finds relevant skills via BM25
4. **Resolution**: Selected skills' dependencies are resolved, tools collected
5. **Execution**: Only selected tools sent to the LLM; all tools remain available for execution

### Backwards Compatibility

- `ToolRegistry` API unchanged — `Register`, `Execute`, `Has`, `Definitions`, `Merge` all work identically
- `Hooks` API unchanged — `AddPreHook`, `RunPreHooks`, `EmitEvent` all work identically
- `AgentConfig` and `APIAgentConfig` gain optional `Skills` and `ContextBuilder` fields
- When `ContextBuilder` is nil, all tools are sent every turn (current behavior)

// Skills example demonstrating skill-based tool organization with semantic lookup.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	claude "github.com/character-ai/claude-agent-sdk-go"
)

func main() {
	// Create a shared store for all components.
	store := claude.NewStore()

	// Create a skill registry backed by the shared store.
	skillReg := claude.NewSkillRegistry(store)

	// Create hooks that share the same store.
	hooks := claude.NewHooksWithStore(store)
	hooks.OnAllTools().Before(func(ctx context.Context, hc claude.HookContext) claude.HookResult {
		fmt.Printf("  [hook] tool called: %s\n", hc.ToolName)
		return claude.AllowHook()
	})

	// Create a BM25 index for semantic tool selection.
	index := claude.NewBM25Index()

	// --- Register "text-processing" skill (base dependency) ---
	textTools := claude.NewToolRegistry()
	claude.RegisterFunc(textTools, claude.ToolDefinition{
		Name:        "tokenize",
		Description: "Tokenize text into words",
		InputSchema: claude.ObjectSchema(map[string]any{
			"text": claude.StringParam("The text to tokenize"),
		}, "text"),
	}, func(ctx context.Context, input struct {
		Text string `json:"text"`
	}) (string, error) {
		return fmt.Sprintf("Tokens: %v", input.Text), nil
	})

	claude.RegisterFunc(textTools, claude.ToolDefinition{
		Name:        "summarize",
		Description: "Summarize a piece of text",
		InputSchema: claude.ObjectSchema(map[string]any{
			"text": claude.StringParam("The text to summarize"),
		}, "text"),
	}, func(ctx context.Context, input struct {
		Text string `json:"text"`
	}) (string, error) {
		if len(input.Text) > 100 {
			return input.Text[:100] + "...", nil
		}
		return input.Text, nil
	})

	if err := skillReg.Register(claude.Skill{
		Name:        "text-processing",
		Description: "Process, analyze, and summarize text content",
		Tags:        []string{"text", "nlp", "summarize"},
		Category:    "processing",
	}, textTools); err != nil {
		log.Fatal(err)
	}
	_ = index.Index("text-processing", "Process, analyze, and summarize text content", []string{"text", "nlp", "summarize"})

	// --- Register "web-research" skill (depends on text-processing) ---
	webTools := claude.NewToolRegistry()
	claude.RegisterFunc(webTools, claude.ToolDefinition{
		Name:        "web_search",
		Description: "Search the web for information on a topic",
		InputSchema: claude.ObjectSchema(map[string]any{
			"query": claude.StringParam("The search query"),
		}, "query"),
	}, func(ctx context.Context, input struct {
		Query string `json:"query"`
	}) (string, error) {
		return fmt.Sprintf("Search results for: %s\n1. Example result 1\n2. Example result 2", input.Query), nil
	})

	claude.RegisterFunc(webTools, claude.ToolDefinition{
		Name:        "fetch_page",
		Description: "Fetch and extract text content from a URL",
		InputSchema: claude.ObjectSchema(map[string]any{
			"url": claude.StringParam("The URL to fetch"),
		}, "url"),
	}, func(ctx context.Context, input struct {
		URL string `json:"url"`
	}) (string, error) {
		return fmt.Sprintf("Page content from %s: [example content]", input.URL), nil
	})

	if err := skillReg.Register(claude.Skill{
		Name:         "web-research",
		Description:  "Search the web and fetch page content for research",
		Tags:         []string{"web", "search", "research"},
		Category:     "research",
		Dependencies: []string{"text-processing"}, // depends on text-processing
		Examples: []claude.SkillExample{
			{Query: "Find the latest news about Go", ToolsUsed: []string{"web_search", "fetch_page"}},
		},
	}, webTools); err != nil {
		log.Fatal(err)
	}
	_ = index.Index("web-research", "Search the web and fetch page content for research", []string{"web", "search", "research"})

	// --- Register "math" skill ---
	mathTools := claude.NewToolRegistry()
	claude.RegisterFunc(mathTools, claude.ToolDefinition{
		Name:        "calculate",
		Description: "Evaluate a mathematical expression",
		InputSchema: claude.ObjectSchema(map[string]any{
			"expression": claude.StringParam("The expression to evaluate"),
		}, "expression"),
	}, func(ctx context.Context, input struct {
		Expression string `json:"expression"`
	}) (string, error) {
		return fmt.Sprintf("Result of %s = 42", input.Expression), nil
	})

	if err := skillReg.Register(claude.Skill{
		Name:        "math",
		Description: "Perform mathematical calculations and evaluations",
		Tags:        []string{"math", "calculation"},
		Category:    "computation",
	}, mathTools); err != nil {
		log.Fatal(err)
	}
	_ = index.Index("math", "Perform mathematical calculations and evaluations", []string{"math", "calculation"})

	// --- Demo: Query the skill registry ---
	fmt.Println("=== All Registered Skills ===")
	allSkills, _ := skillReg.All()
	for _, s := range allSkills {
		fmt.Printf("  - %s (%s): %s\n", s.Name, s.Category, s.Description)
	}

	fmt.Println("\n=== Skills by Category 'research' ===")
	research, _ := skillReg.ByCategory("research")
	for _, s := range research {
		fmt.Printf("  - %s\n", s.Name)
	}

	fmt.Println("\n=== Skills by Tag 'web' ===")
	web, _ := skillReg.ByTag("web")
	for _, s := range web {
		fmt.Printf("  - %s\n", s.Name)
	}

	// --- Demo: Shared store queries ---
	fmt.Println("\n=== Shared Store: tools by source ===")
	webSkillTools, _ := store.ListToolsBySource("skill:web-research")
	for _, t := range webSkillTools {
		fmt.Printf("  - %s (source: %s)\n", t.Name, t.Source)
	}

	fmt.Println("\n=== Shared Store: snapshot ===")
	snap := store.Snapshot()
	snapTools, _ := snap.Tools()
	fmt.Printf("  Tools in snapshot: %d\n", len(snapTools))
	snapSkills, _ := snap.Skills()
	fmt.Printf("  Skills in snapshot: %d\n", len(snapSkills))
	snap.Close()

	// --- Demo: BM25 search ---
	fmt.Println("\n=== BM25 Search: 'search web for information' ===")
	results := index.Search("search web for information", 3)
	for _, r := range results {
		fmt.Printf("  - %s (score: %.3f)\n", r.ID, r.Score)
	}

	// --- Demo: Resolve dependencies (transitive) ---
	fmt.Println("\n=== Resolve 'web-research' skill tools (includes text-processing dep) ===")
	resolved, err := skillReg.Resolve("web-research")
	if err != nil {
		log.Fatal(err)
	}
	for _, def := range resolved.Definitions() {
		fmt.Printf("  - %s: %s\n", def.Name, def.Description)
	}

	// --- Demo: Context builder ---
	fmt.Println("\n=== Context Builder: select tools for 'search the web' ===")
	cb := claude.NewContextBuilder(store, claude.WithIndex(index), claude.WithMaxTools(5))
	selected := cb.SelectTools(context.Background(), "search the web")
	for _, def := range selected {
		fmt.Printf("  - %s: %s\n", def.Name, def.Description)
	}

	fmt.Println("\n=== Context Builder: select skills for 'calculate math' ===")
	skills := cb.SelectSkills(context.Background(), "calculate math", 2)
	for _, s := range skills {
		fmt.Printf("  - %s: %s\n", s.Name, s.Description)
	}

	// --- Demo: Use with APIAgent (requires API key) ---
	fmt.Println("\n=== APIAgent Configuration (not running - needs API key) ===")

	// All tools are already in the shared store from skill registration.
	allTools := claude.NewToolRegistryWithStore(store)

	fmt.Printf("  Total tools available: %d\n", len(allTools.Definitions()))
	fmt.Println("  Context builder configured with BM25 index")
	fmt.Println("  Agent would dynamically select tools per turn based on query")

	// To actually run the agent (requires ANTHROPIC_API_KEY):
	//
	// agent := claude.NewAPIAgent(claude.APIAgentConfig{
	//     Tools:          allTools,
	//     Hooks:          hooks,
	//     Skills:         skillReg,
	//     ContextBuilder: cb,
	//     SystemPrompt:   "You are a helpful assistant with web research, math, and text processing capabilities.",
	// })
	// events, _ := agent.Run(context.Background(), "Search the web for Go 1.23 release notes and summarize them")
	// for event := range events {
	//     if event.Content != "" {
	//         fmt.Print(event.Content)
	//     }
	// }

	_ = json.Marshal // silence import
	_ = hooks        // used in commented agent config above
}

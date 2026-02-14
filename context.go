package claudeagent

import "context"

// ContextBuilder provides dynamic per-turn tool selection using BM25 search.
type ContextBuilder struct {
	store    *Store
	index    SkillIndex
	maxTools int
}

// ContextOption configures a ContextBuilder.
type ContextOption func(*ContextBuilder)

// WithMaxTools sets the maximum number of tools to return from SelectTools.
func WithMaxTools(n int) ContextOption {
	return func(cb *ContextBuilder) {
		cb.maxTools = n
	}
}

// WithIndex sets the search index for semantic tool selection.
func WithIndex(index SkillIndex) ContextOption {
	return func(cb *ContextBuilder) {
		cb.index = index
	}
}

// NewContextBuilder creates a ContextBuilder with the given store and options.
func NewContextBuilder(store *Store, opts ...ContextOption) *ContextBuilder {
	cb := &ContextBuilder{
		store:    store,
		maxTools: 20,
	}
	for _, opt := range opts {
		opt(cb)
	}
	return cb
}

// SelectTools returns the tool definitions most relevant to the query.
// Falls back to all tools if no index is configured or the query is empty.
func (cb *ContextBuilder) SelectTools(_ context.Context, query string) []ToolDefinition {
	// Fallback: return all tools.
	if cb.index == nil || query == "" {
		tools, err := cb.store.ListTools()
		if err != nil {
			return nil
		}
		defs := make([]ToolDefinition, 0, len(tools))
		for _, t := range tools {
			defs = append(defs, t.ToolDefinition)
		}
		return defs
	}

	// 1. Search skills by query.
	skillResults := cb.index.Search(query, 10)
	if len(skillResults) == 0 {
		// No matches — fall back to all tools.
		return cb.allToolDefs()
	}

	// 2. Collect skill names from results.
	skillNames := make([]string, 0, len(skillResults))
	for _, sr := range skillResults {
		skillNames = append(skillNames, sr.ID)
	}

	// 3. Resolve dependencies and collect tools.
	toolSet := make(map[string]ToolDefinition)
	toolScores := make(map[string]float64)

	for _, sr := range skillResults {
		skill, err := cb.store.GetSkill(sr.ID)
		if err != nil || skill == nil {
			continue
		}

		// Add this skill's tools.
		for _, def := range skill.Tools {
			toolSet[def.Name] = def
			if sr.Score > toolScores[def.Name] {
				toolScores[def.Name] = sr.Score
			}
		}

		// Resolve dependencies.
		for _, dep := range skill.Dependencies {
			depSkill, err := cb.store.GetSkill(dep)
			if err != nil || depSkill == nil {
				continue
			}
			for _, def := range depSkill.Tools {
				toolSet[def.Name] = def
				// Dependency tools get a fraction of the parent's score.
				score := sr.Score * 0.5
				if score > toolScores[def.Name] {
					toolScores[def.Name] = score
				}
			}
		}
	}

	// 4. If too many tools, rank by score and take top maxTools.
	defs := make([]ToolDefinition, 0, len(toolSet))
	for _, def := range toolSet {
		defs = append(defs, def)
	}

	if len(defs) > cb.maxTools {
		// Sort by score descending (insertion sort — small N).
		for i := 1; i < len(defs); i++ {
			for j := i; j > 0 && toolScores[defs[j].Name] > toolScores[defs[j-1].Name]; j-- {
				defs[j], defs[j-1] = defs[j-1], defs[j]
			}
		}
		defs = defs[:cb.maxTools]
	}

	return defs
}

// SelectSkills returns the most relevant skills for the query.
func (cb *ContextBuilder) SelectSkills(_ context.Context, query string, k int) []*Skill {
	if cb.index == nil || query == "" {
		skills, err := cb.store.ListSkills()
		if err != nil {
			return nil
		}
		result := make([]*Skill, 0, len(skills))
		for _, s := range skills {
			result = append(result, storedToSkill(s))
		}
		return result
	}

	results := cb.index.Search(query, k)
	skills := make([]*Skill, 0, len(results))
	for _, sr := range results {
		stored, err := cb.store.GetSkill(sr.ID)
		if err != nil || stored == nil {
			continue
		}
		skills = append(skills, storedToSkill(stored))
	}
	return skills
}

// allToolDefs returns all tool definitions from the store.
func (cb *ContextBuilder) allToolDefs() []ToolDefinition {
	tools, err := cb.store.ListTools()
	if err != nil {
		return nil
	}
	defs := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		defs = append(defs, t.ToolDefinition)
	}
	return defs
}

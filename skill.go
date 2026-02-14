package claudeagent

import "fmt"

// Skill represents a composable capability bundle with rich metadata.
type Skill struct {
	Name         string
	Description  string
	Tags         []string
	Category     string
	Dependencies []string
	Examples     []SkillExample
	Priority     int
	Metadata     map[string]string
}

// SkillRegistry manages skill registration, resolution, and querying.
type SkillRegistry struct {
	store *Store
}

// NewSkillRegistry creates a new SkillRegistry backed by the given store.
func NewSkillRegistry(store *Store) *SkillRegistry {
	return &SkillRegistry{store: store}
}

// Register adds a skill and its tools to the store.
// Each tool from the ToolRegistry is stored with Source="skill:<name>" and inherits the skill's tags.
func (sr *SkillRegistry) Register(skill Skill, tools *ToolRegistry) error {
	// Collect tool definitions and handlers.
	var toolDefs []ToolDefinition
	handlers := make(map[string]ToolHandler)

	if tools != nil {
		storedTools, err := tools.store.ListTools()
		if err != nil {
			return fmt.Errorf("list skill tools: %w", err)
		}
		for _, st := range storedTools {
			toolDefs = append(toolDefs, st.ToolDefinition)
			if st.Handler != nil {
				handlers[st.Name] = st.Handler
			}
		}
	}

	// Store the skill.
	stored := &StoredSkill{
		Name:         skill.Name,
		Description:  skill.Description,
		Tags:         skill.Tags,
		Category:     skill.Category,
		Tools:        toolDefs,
		Handlers:     handlers,
		Dependencies: skill.Dependencies,
		Examples:     skill.Examples,
		Priority:     skill.Priority,
		Metadata:     skill.Metadata,
	}
	if err := sr.store.InsertSkill(stored); err != nil {
		return fmt.Errorf("insert skill: %w", err)
	}

	// Store each tool with the skill source and tags.
	source := "skill:" + skill.Name
	for _, def := range toolDefs {
		handler := handlers[def.Name]
		if err := sr.store.InsertTool(&StoredTool{
			ToolDefinition: def,
			Source:         source,
			Tags:           skill.Tags,
			Handler:        handler,
		}); err != nil {
			return fmt.Errorf("insert skill tool %s: %w", def.Name, err)
		}
	}

	return nil
}

// Remove removes a skill and its associated tools from the store.
func (sr *SkillRegistry) Remove(name string) error {
	// First remove associated tools.
	source := "skill:" + name
	tools, err := sr.store.ListToolsBySource(source)
	if err != nil {
		return fmt.Errorf("list skill tools for removal: %w", err)
	}
	for _, t := range tools {
		if err := sr.store.DeleteTool(t.Name); err != nil {
			return fmt.Errorf("delete skill tool %s: %w", t.Name, err)
		}
	}

	return sr.store.DeleteSkill(name)
}

// Get returns a skill by name, or nil if not found.
func (sr *SkillRegistry) Get(name string) (*Skill, error) {
	stored, err := sr.store.GetSkill(name)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, nil
	}
	return storedToSkill(stored), nil
}

// ByTag returns skills matching the given tag.
func (sr *SkillRegistry) ByTag(tag string) ([]*Skill, error) {
	stored, err := sr.store.ListSkillsByTag(tag)
	if err != nil {
		return nil, err
	}
	return storedSliceToSkills(stored), nil
}

// ByCategory returns skills in the given category.
func (sr *SkillRegistry) ByCategory(category string) ([]*Skill, error) {
	stored, err := sr.store.ListSkillsByCategory(category)
	if err != nil {
		return nil, err
	}
	return storedSliceToSkills(stored), nil
}

// All returns all registered skills.
func (sr *SkillRegistry) All() ([]*Skill, error) {
	stored, err := sr.store.ListSkills()
	if err != nil {
		return nil, err
	}
	return storedSliceToSkills(stored), nil
}

// Resolve returns a ToolRegistry containing tools from the named skills and their transitive dependencies.
func (sr *SkillRegistry) Resolve(skillNames ...string) (*ToolRegistry, error) {
	resolved := make(map[string]bool)
	registry := NewToolRegistryWithStore(NewStore())

	var resolve func(name string) error
	resolve = func(name string) error {
		if resolved[name] {
			return nil
		}
		resolved[name] = true

		stored, err := sr.store.GetSkill(name)
		if err != nil {
			return fmt.Errorf("resolve skill %s: %w", name, err)
		}
		if stored == nil {
			return fmt.Errorf("skill not found: %s", name)
		}

		// Resolve dependencies first.
		for _, dep := range stored.Dependencies {
			if err := resolve(dep); err != nil {
				return err
			}
		}

		// Add this skill's tools to the registry.
		for _, def := range stored.Tools {
			handler := stored.Handlers[def.Name]
			registry.RegisterWithSource(def, handler, "skill:"+name, stored.Tags)
		}
		return nil
	}

	for _, name := range skillNames {
		if err := resolve(name); err != nil {
			return nil, err
		}
	}

	return registry, nil
}

func storedToSkill(s *StoredSkill) *Skill {
	return &Skill{
		Name:         s.Name,
		Description:  s.Description,
		Tags:         s.Tags,
		Category:     s.Category,
		Dependencies: s.Dependencies,
		Examples:     s.Examples,
		Priority:     s.Priority,
		Metadata:     s.Metadata,
	}
}

func storedSliceToSkills(stored []*StoredSkill) []*Skill {
	skills := make([]*Skill, 0, len(stored))
	for _, s := range stored {
		skills = append(skills, storedToSkill(s))
	}
	return skills
}

package claudeagent

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-memdb"
)

// hookIDCounter generates unique IDs for stored hooks.
var hookIDCounter atomic.Uint64

func nextHookID() string {
	return fmt.Sprintf("hook-%d", hookIDCounter.Add(1))
}

// StoredTool wraps a ToolDefinition with storage metadata.
type StoredTool struct {
	ToolDefinition        // embedded
	Source         string // "native", "mcp:<server>", "skill:<name>"
	Tags           []string
	Handler        ToolHandler // not indexed
}

// StoredHook wraps hook data with an ID for indexing.
type StoredHook struct {
	ID        string
	Pattern   string
	IsRegex   bool
	Timeout   time.Duration
	PreHooks  []PreToolUseHook
	PostHooks []PostToolUseHook
}

// StoredSkill is the skill record in memdb.
type StoredSkill struct {
	Name         string
	Description  string
	Tags         []string
	Category     string
	Tools        []ToolDefinition
	Handlers     map[string]ToolHandler
	Dependencies []string
	Examples     []SkillExample
	Priority     int
	Metadata     map[string]string
}

// SkillExample describes an example query that a skill can handle.
type SkillExample struct {
	Query       string
	ToolsUsed   []string
	Description string
}

// storeSchema builds the go-memdb schema with tools, skills, and hooks tables.
func storeSchema() *memdb.DBSchema {
	return &memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			"tools": {
				Name: "tools",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.StringFieldIndex{Field: "Name"},
					},
					"source": {
						Name:    "source",
						Unique:  false,
						Indexer: &memdb.StringFieldIndex{Field: "Source"},
					},
					"tags": {
						Name:         "tags",
						Unique:       false,
						AllowMissing: true,
						Indexer:      &memdb.StringSliceFieldIndex{Field: "Tags"},
					},
				},
			},
			"skills": {
				Name: "skills",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.StringFieldIndex{Field: "Name"},
					},
					"category": {
						Name:         "category",
						Unique:       false,
						AllowMissing: true,
						Indexer:      &memdb.StringFieldIndex{Field: "Category"},
					},
					"tags": {
						Name:         "tags",
						Unique:       false,
						AllowMissing: true,
						Indexer:      &memdb.StringSliceFieldIndex{Field: "Tags"},
					},
				},
			},
			"hooks": {
				Name: "hooks",
				Indexes: map[string]*memdb.IndexSchema{
					"id": {
						Name:    "id",
						Unique:  true,
						Indexer: &memdb.StringFieldIndex{Field: "ID"},
					},
					"pattern": {
						Name:    "pattern",
						Unique:  false,
						Indexer: &memdb.StringFieldIndex{Field: "Pattern"},
					},
				},
			},
		},
	}
}

// Store provides a unified memdb-backed storage for tools, skills, and hooks.
type Store struct {
	db *memdb.MemDB
}

// NewStore creates a new Store backed by go-memdb.
func NewStore() *Store {
	db, err := memdb.NewMemDB(storeSchema())
	if err != nil {
		// Schema is static and valid; panic indicates a programming error.
		panic(fmt.Sprintf("failed to create memdb: %v", err))
	}
	return &Store{db: db}
}

// --- Tool operations ---

// InsertTool adds or replaces a tool in the store.
func (s *Store) InsertTool(tool *StoredTool) error {
	txn := s.db.Txn(true)
	defer txn.Abort()
	if err := txn.Insert("tools", tool); err != nil {
		return fmt.Errorf("insert tool: %w", err)
	}
	txn.Commit()
	return nil
}

// DeleteTool removes a tool by name.
func (s *Store) DeleteTool(name string) error {
	txn := s.db.Txn(true)
	defer txn.Abort()
	raw, err := txn.First("tools", "id", name)
	if err != nil {
		return fmt.Errorf("delete tool lookup: %w", err)
	}
	if raw == nil {
		return nil // not found is not an error
	}
	if err := txn.Delete("tools", raw); err != nil {
		return fmt.Errorf("delete tool: %w", err)
	}
	txn.Commit()
	return nil
}

// GetTool retrieves a tool by name.
func (s *Store) GetTool(name string) (*StoredTool, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	raw, err := txn.First("tools", "id", name)
	if err != nil {
		return nil, fmt.Errorf("get tool: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	return raw.(*StoredTool), nil
}

// ListTools returns all stored tools.
func (s *Store) ListTools() ([]*StoredTool, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	return collectTools(txn, "tools", "id")
}

// ListToolsBySource returns tools matching the given source.
func (s *Store) ListToolsBySource(source string) ([]*StoredTool, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	return collectToolsFiltered(txn, "tools", "source", source)
}

// ListToolsByTag returns tools matching the given tag.
func (s *Store) ListToolsByTag(tag string) ([]*StoredTool, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	return collectToolsFiltered(txn, "tools", "tags", tag)
}

// --- Skill operations ---

// InsertSkill adds or replaces a skill in the store.
func (s *Store) InsertSkill(skill *StoredSkill) error {
	txn := s.db.Txn(true)
	defer txn.Abort()
	if err := txn.Insert("skills", skill); err != nil {
		return fmt.Errorf("insert skill: %w", err)
	}
	txn.Commit()
	return nil
}

// DeleteSkill removes a skill by name.
func (s *Store) DeleteSkill(name string) error {
	txn := s.db.Txn(true)
	defer txn.Abort()
	raw, err := txn.First("skills", "id", name)
	if err != nil {
		return fmt.Errorf("delete skill lookup: %w", err)
	}
	if raw == nil {
		return nil
	}
	if err := txn.Delete("skills", raw); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	txn.Commit()
	return nil
}

// GetSkill retrieves a skill by name.
func (s *Store) GetSkill(name string) (*StoredSkill, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	raw, err := txn.First("skills", "id", name)
	if err != nil {
		return nil, fmt.Errorf("get skill: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	return raw.(*StoredSkill), nil
}

// ListSkills returns all stored skills.
func (s *Store) ListSkills() ([]*StoredSkill, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	return collectSkills(txn, "skills", "id")
}

// ListSkillsByCategory returns skills in the given category.
func (s *Store) ListSkillsByCategory(category string) ([]*StoredSkill, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	return collectSkillsFiltered(txn, "skills", "category", category)
}

// ListSkillsByTag returns skills matching the given tag.
func (s *Store) ListSkillsByTag(tag string) ([]*StoredSkill, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	return collectSkillsFiltered(txn, "skills", "tags", tag)
}

// --- Hook operations ---

// InsertHook adds a hook to the store. If ID is empty, one is auto-generated.
func (s *Store) InsertHook(hook *StoredHook) error {
	if hook.ID == "" {
		hook.ID = nextHookID()
	}
	txn := s.db.Txn(true)
	defer txn.Abort()
	if err := txn.Insert("hooks", hook); err != nil {
		return fmt.Errorf("insert hook: %w", err)
	}
	txn.Commit()
	return nil
}

// DeleteHook removes a hook by ID.
func (s *Store) DeleteHook(id string) error {
	txn := s.db.Txn(true)
	defer txn.Abort()
	raw, err := txn.First("hooks", "id", id)
	if err != nil {
		return fmt.Errorf("delete hook lookup: %w", err)
	}
	if raw == nil {
		return nil
	}
	if err := txn.Delete("hooks", raw); err != nil {
		return fmt.Errorf("delete hook: %w", err)
	}
	txn.Commit()
	return nil
}

// GetHook retrieves a hook by ID.
func (s *Store) GetHook(id string) (*StoredHook, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	raw, err := txn.First("hooks", "id", id)
	if err != nil {
		return nil, fmt.Errorf("get hook: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	return raw.(*StoredHook), nil
}

// ListHooks returns all stored hooks.
func (s *Store) ListHooks() ([]*StoredHook, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	it, err := txn.Get("hooks", "id")
	if err != nil {
		return nil, fmt.Errorf("list hooks: %w", err)
	}
	var hooks []*StoredHook
	for obj := it.Next(); obj != nil; obj = it.Next() {
		hooks = append(hooks, obj.(*StoredHook))
	}
	return hooks, nil
}

// ListHooksByPattern returns hooks matching a specific pattern.
func (s *Store) ListHooksByPattern(pattern string) ([]*StoredHook, error) {
	txn := s.db.Txn(false)
	defer txn.Abort()
	it, err := txn.Get("hooks", "pattern", pattern)
	if err != nil {
		return nil, fmt.Errorf("list hooks by pattern: %w", err)
	}
	var hooks []*StoredHook
	for obj := it.Next(); obj != nil; obj = it.Next() {
		hooks = append(hooks, obj.(*StoredHook))
	}
	return hooks, nil
}

// --- Snapshot (read-only query interface) ---

// Snapshot returns a read-only point-in-time view of the store.
func (s *Store) Snapshot() *StoreSnapshot {
	return &StoreSnapshot{txn: s.db.Txn(false)}
}

// StoreSnapshot provides read-only access to a consistent store state.
type StoreSnapshot struct {
	txn *memdb.Txn
}

// Tools returns all tools in the snapshot.
func (ss *StoreSnapshot) Tools() ([]*StoredTool, error) {
	return collectTools(ss.txn, "tools", "id")
}

// ToolsBySource returns tools matching the given source.
func (ss *StoreSnapshot) ToolsBySource(source string) ([]*StoredTool, error) {
	return collectToolsFiltered(ss.txn, "tools", "source", source)
}

// ToolsByTag returns tools matching the given tag.
func (ss *StoreSnapshot) ToolsByTag(tag string) ([]*StoredTool, error) {
	return collectToolsFiltered(ss.txn, "tools", "tags", tag)
}

// Skills returns all skills in the snapshot.
func (ss *StoreSnapshot) Skills() ([]*StoredSkill, error) {
	return collectSkills(ss.txn, "skills", "id")
}

// SkillsByCategory returns skills in the given category.
func (ss *StoreSnapshot) SkillsByCategory(category string) ([]*StoredSkill, error) {
	return collectSkillsFiltered(ss.txn, "skills", "category", category)
}

// --- Internal helpers ---

func collectTools(txn *memdb.Txn, table, index string) ([]*StoredTool, error) {
	it, err := txn.Get(table, index)
	if err != nil {
		return nil, err
	}
	var tools []*StoredTool
	for obj := it.Next(); obj != nil; obj = it.Next() {
		tools = append(tools, obj.(*StoredTool))
	}
	return tools, nil
}

func collectToolsFiltered(txn *memdb.Txn, table, index, value string) ([]*StoredTool, error) {
	it, err := txn.Get(table, index, value)
	if err != nil {
		return nil, err
	}
	var tools []*StoredTool
	for obj := it.Next(); obj != nil; obj = it.Next() {
		tools = append(tools, obj.(*StoredTool))
	}
	return tools, nil
}

func collectSkills(txn *memdb.Txn, table, index string) ([]*StoredSkill, error) {
	it, err := txn.Get(table, index)
	if err != nil {
		return nil, err
	}
	var skills []*StoredSkill
	for obj := it.Next(); obj != nil; obj = it.Next() {
		skills = append(skills, obj.(*StoredSkill))
	}
	return skills, nil
}

func collectSkillsFiltered(txn *memdb.Txn, table, index, value string) ([]*StoredSkill, error) {
	it, err := txn.Get(table, index, value)
	if err != nil {
		return nil, err
	}
	var skills []*StoredSkill
	for obj := it.Next(); obj != nil; obj = it.Next() {
		skills = append(skills, obj.(*StoredSkill))
	}
	return skills, nil
}

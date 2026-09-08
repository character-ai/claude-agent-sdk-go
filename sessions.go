package claudeagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxSessionProjectKeyLength = 200

var sessionIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// SessionInfo contains display metadata for a persisted Claude Code session.
type SessionInfo struct {
	SessionID    string `json:"session_id"`
	Summary      string `json:"summary"`
	LastModified int64  `json:"last_modified"`
	FileSize     int64  `json:"file_size,omitempty"`
	CustomTitle  string `json:"custom_title,omitempty"`
	FirstPrompt  string `json:"first_prompt,omitempty"`
	GitBranch    string `json:"git_branch,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Tag          string `json:"tag,omitempty"`
	CreatedAt    int64  `json:"created_at,omitempty"`
}

// SessionMessage is a visible user or assistant message reconstructed from a
// persisted JSONL transcript. Message contains the raw Anthropic API message.
type SessionMessage struct {
	Type            MessageRole     `json:"type"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
	Message         json.RawMessage `json:"message"`
	ParentToolUseID string          `json:"parent_tool_use_id,omitempty"`
	ParentAgentID   string          `json:"parent_agent_id,omitempty"`
}

// SessionListOptions configures session discovery and pagination.
type SessionListOptions struct {
	// Directory restricts discovery to the Claude Code project directory for
	// this path. Empty searches every project under the configured projects root.
	Directory string
	// ConfigDir overrides CLAUDE_CONFIG_DIR. Empty checks CLAUDE_CONFIG_DIR and
	// then defaults to ~/.claude.
	ConfigDir string
	Limit     int
	Offset    int
}

// SessionMessageOptions configures transcript lookup and pagination.
type SessionMessageOptions struct {
	Directory string
	ConfigDir string
	Limit     int
	Offset    int
}

// SessionKey identifies a main transcript or one of its subagent transcripts.
type SessionKey struct {
	ProjectKey string `json:"project_key"`
	SessionID  string `json:"session_id"`
	Subpath    string `json:"subpath,omitempty"`
}

// SessionStoreEntry is one opaque CLI transcript record. Stores must preserve
// each JSON object but need not preserve JSON key order or whitespace.
type SessionStoreEntry map[string]any

// SessionStoreListEntry identifies a main transcript and its last-modified time.
type SessionStoreListEntry struct {
	SessionID string `json:"session_id"`
	MTime     int64  `json:"mtime"`
}

// SessionStore is the persistence adapter used by the store-backed helpers.
// Implementations should treat transcript entries as opaque JSON objects.
type SessionStore interface {
	Append(context.Context, SessionKey, []SessionStoreEntry) error
	Load(context.Context, SessionKey) ([]SessionStoreEntry, error)
	ListSessions(context.Context, string) ([]SessionStoreListEntry, error)
	Delete(context.Context, SessionKey) error
}

// ProjectKeyForDirectory derives the SessionStore project key used by Claude
// Code for directory-backed transcripts.
func ProjectKeyForDirectory(directory string) (string, error) {
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		canonical, err = filepath.Abs(directory)
		if err != nil {
			return "", err
		}
	}
	return sanitizeSessionProjectKey(canonical), nil
}

// ListSessions returns persisted sessions sorted by last-modified time, newest first.
func ListSessions(opts SessionListOptions) ([]SessionInfo, error) {
	projectsDir, err := sessionProjectsDir(opts.ConfigDir)
	if err != nil {
		return nil, err
	}

	var projectDirs []string
	if opts.Directory != "" {
		key, err := ProjectKeyForDirectory(opts.Directory)
		if err != nil {
			return nil, err
		}
		projectDirs = []string{filepath.Join(projectsDir, key)}
	} else {
		entries, err := os.ReadDir(projectsDir)
		if errors.Is(err, os.ErrNotExist) {
			return []SessionInfo{}, nil
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				projectDirs = append(projectDirs, filepath.Join(projectsDir, entry.Name()))
			}
		}
	}

	byID := make(map[string]SessionInfo)
	for _, projectDir := range projectDirs {
		infos, err := readSessionsFromDir(projectDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, info := range infos {
			if existing, ok := byID[info.SessionID]; !ok || info.LastModified > existing.LastModified {
				byID[info.SessionID] = info
			}
		}
	}

	result := make([]SessionInfo, 0, len(byID))
	for _, info := range byID {
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastModified > result[j].LastModified })
	return pageSessions(result, opts.Offset, opts.Limit), nil
}

// GetSessionMessages reconstructs the active user/assistant chain from a transcript.
func GetSessionMessages(sessionID string, opts SessionMessageOptions) ([]SessionMessage, error) {
	if !sessionIDPattern.MatchString(sessionID) {
		return []SessionMessage{}, nil
	}
	path, err := findSessionFile(sessionID, opts.Directory, opts.ConfigDir)
	if errors.Is(err, os.ErrNotExist) {
		return []SessionMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := readTranscriptEntries(path)
	if err != nil {
		return nil, err
	}
	return transcriptMessages(entries, opts.Offset, opts.Limit), nil
}

// DeleteSession permanently removes a session transcript and its sibling
// subdirectory, including all nested subagent transcripts.
func DeleteSession(sessionID, directory, configDir string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	path, err := findSessionFile(sessionID, directory, configDir)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(filepath.Dir(path), sessionID)); err != nil {
		return err
	}
	return nil
}

// GetSessionMessagesFromStore reconstructs a visible conversation from a SessionStore.
func GetSessionMessagesFromStore(ctx context.Context, store SessionStore, key SessionKey, opts SessionMessageOptions) ([]SessionMessage, error) {
	if !sessionIDPattern.MatchString(key.SessionID) {
		return []SessionMessage{}, nil
	}
	entries, err := store.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	return transcriptMessages(storeTranscriptEntries(entries), opts.Offset, opts.Limit), nil
}

// ListSessionsFromStore returns store-backed sessions sorted newest first.
func ListSessionsFromStore(ctx context.Context, store SessionStore, projectKey string, opts SessionListOptions) ([]SessionInfo, error) {
	listed, err := store.ListSessions(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	result := make([]SessionInfo, 0, len(listed))
	for _, item := range listed {
		if !sessionIDPattern.MatchString(item.SessionID) {
			continue
		}
		entries, err := store.Load(ctx, SessionKey{ProjectKey: projectKey, SessionID: item.SessionID})
		if err != nil {
			return nil, err
		}
		info := sessionInfoFromStore(item, entries)
		if info.Summary != "" {
			result = append(result, info)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LastModified > result[j].LastModified })
	return pageSessions(result, opts.Offset, opts.Limit), nil
}

// DeleteSessionFromStore deletes a main transcript. SessionStore implementations
// must cascade a main-key delete to that session's subkeys.
func DeleteSessionFromStore(ctx context.Context, store SessionStore, projectKey, sessionID string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session ID %q", sessionID)
	}
	return store.Delete(ctx, SessionKey{ProjectKey: projectKey, SessionID: sessionID})
}

type transcriptEntry struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	SessionID   string          `json:"sessionId"`
	Message     json.RawMessage `json:"message"`
	Timestamp   string          `json:"timestamp"`
	Cwd         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	CustomTitle string          `json:"customTitle"`
	AITitle     string          `json:"aiTitle"`
	Summary     string          `json:"summary"`
	LastPrompt  string          `json:"lastPrompt"`
	Tag         string          `json:"tag"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	TeamName    string          `json:"teamName"`
}

func sessionProjectsDir(configDir string) (string, error) {
	if configDir == "" {
		configDir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, ".claude")
	}
	return filepath.Join(configDir, "projects"), nil
}

func sanitizeSessionProjectKey(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	sanitized := b.String()
	if len(sanitized) <= maxSessionProjectKeyLength {
		return sanitized
	}
	return sanitized[:maxSessionProjectKeyLength] + "-" + sessionProjectHash(path)
}

func sessionProjectHash(s string) string {
	var hash int32
	for _, r := range s {
		hash = hash*31 + r
	}
	value := int64(hash)
	if value < 0 {
		value = -value
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if value == 0 {
		return "0"
	}
	var reversed []byte
	for value > 0 {
		reversed = append(reversed, digits[value%36])
		value /= 36
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return string(reversed)
}

func findSessionFile(sessionID, directory, configDir string) (string, error) {
	projectsDir, err := sessionProjectsDir(configDir)
	if err != nil {
		return "", err
	}
	fileName := sessionID + ".jsonl"
	if directory != "" {
		key, err := ProjectKeyForDirectory(directory)
		if err != nil {
			return "", err
		}
		path := filepath.Join(projectsDir, key, fileName)
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return path, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", os.ErrNotExist
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(projectsDir, entry.Name(), fileName)
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func readSessionsFromDir(projectDir string) ([]SessionInfo, error) {
	files, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}
	var result []SessionInfo
	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".jsonl" {
			continue
		}
		sessionID := strings.TrimSuffix(file.Name(), ".jsonl")
		if !sessionIDPattern.MatchString(sessionID) {
			continue
		}
		path := filepath.Join(projectDir, file.Name())
		entries, err := readTranscriptEntries(path)
		if err != nil {
			continue
		}
		stat, err := file.Info()
		if err != nil {
			continue
		}
		info := sessionInfoFromEntries(sessionID, stat.ModTime().UnixMilli(), stat.Size(), entries)
		if info.Summary != "" {
			result = append(result, info)
		}
	}
	return result, nil
}

func readTranscriptEntries(path string) ([]transcriptEntry, error) {
	file, err := os.Open(path) // #nosec G304 -- path is built from the projects dir and a validated session ID
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck
	return decodeTranscript(file)
}

func decodeTranscript(r io.Reader) ([]transcriptEntry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var entries []transcriptEntry
	for scanner.Scan() {
		var entry transcriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "user" || entry.Type == "assistant" || entry.Type == "progress" || entry.Type == "system" || entry.Type == "attachment" || entry.Type == "custom-title" || entry.Type == "tag" || entry.Type == "last-prompt" {
			entries = append(entries, entry)
		}
	}
	return entries, scanner.Err()
}

func transcriptMessages(entries []transcriptEntry, offset, limit int) []SessionMessage {
	chain := conversationChain(entries)
	messages := make([]SessionMessage, 0, len(chain))
	for _, entry := range chain {
		if (entry.Type != "user" && entry.Type != "assistant") || entry.IsMeta || entry.IsSidechain || entry.TeamName != "" {
			continue
		}
		messages = append(messages, SessionMessage{
			Type:      MessageRole(entry.Type),
			UUID:      entry.UUID,
			SessionID: entry.SessionID,
			Message:   append(json.RawMessage(nil), entry.Message...),
		})
	}
	return pageMessages(messages, offset, limit)
}

func conversationChain(entries []transcriptEntry) []transcriptEntry {
	byUUID := make(map[string]transcriptEntry)
	index := make(map[string]int)
	parents := make(map[string]bool)
	for i, entry := range entries {
		if entry.UUID == "" {
			continue
		}
		byUUID[entry.UUID] = entry
		index[entry.UUID] = i
		if entry.ParentUUID != "" {
			parents[entry.ParentUUID] = true
		}
	}
	var leaf *transcriptEntry
	bestIndex := -1
	for _, terminal := range entries {
		if terminal.UUID == "" || parents[terminal.UUID] {
			continue
		}
		current := terminal
		seen := make(map[string]bool)
		for current.UUID != "" && !seen[current.UUID] {
			seen[current.UUID] = true
			if current.Type == "user" || current.Type == "assistant" {
				if !current.IsSidechain && !current.IsMeta && current.TeamName == "" && index[current.UUID] > bestIndex {
					copy := current
					leaf = &copy
					bestIndex = index[current.UUID]
				}
				break
			}
			parent, ok := byUUID[current.ParentUUID]
			if !ok {
				break
			}
			current = parent
		}
	}
	if leaf == nil {
		return nil
	}
	var chain []transcriptEntry
	seen := make(map[string]bool)
	current := *leaf
	for current.UUID != "" && !seen[current.UUID] {
		seen[current.UUID] = true
		chain = append(chain, current)
		parent, ok := byUUID[current.ParentUUID]
		if !ok {
			break
		}
		current = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

func sessionInfoFromEntries(sessionID string, mtime, size int64, entries []transcriptEntry) SessionInfo {
	info := SessionInfo{SessionID: sessionID, LastModified: mtime, FileSize: size}
	for _, entry := range entries {
		if info.Cwd == "" && entry.Cwd != "" {
			info.Cwd = entry.Cwd
		}
		if entry.GitBranch != "" {
			info.GitBranch = entry.GitBranch
		}
		if info.CreatedAt == 0 && entry.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
				info.CreatedAt = parsed.UnixMilli()
			}
		}
		if entry.CustomTitle != "" {
			info.CustomTitle = entry.CustomTitle
		} else if info.CustomTitle == "" && entry.AITitle != "" {
			info.CustomTitle = entry.AITitle
		}
		if entry.Tag != "" && entry.Type == "tag" {
			info.Tag = entry.Tag
		}
		if entry.LastPrompt != "" {
			info.Summary = entry.LastPrompt
		} else if info.Summary == "" && entry.Summary != "" {
			info.Summary = entry.Summary
		}
		if info.FirstPrompt == "" && entry.Type == "user" && !entry.IsMeta {
			info.FirstPrompt = extractMessageText(entry.Message)
		}
	}
	if info.CustomTitle != "" {
		info.Summary = info.CustomTitle
	} else if info.Summary == "" {
		info.Summary = info.FirstPrompt
	}
	return info
}

func extractMessageText(raw json.RawMessage) string {
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil {
		return ""
	}
	var text string
	if json.Unmarshal(message.Content, &text) == nil {
		return truncateSessionSummary(text)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(message.Content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return truncateSessionSummary(strings.Join(parts, " "))
}

func truncateSessionSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= 200 {
		return value
	}
	return strings.TrimSpace(value[:200]) + "…"
}

func storeTranscriptEntries(entries []SessionStoreEntry) []transcriptEntry {
	var result []transcriptEntry
	for _, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		decoded, err := decodeTranscript(bytes.NewReader(append(raw, '\n')))
		if err == nil {
			result = append(result, decoded...)
		}
	}
	return result
}

func sessionInfoFromStore(item SessionStoreListEntry, entries []SessionStoreEntry) SessionInfo {
	rawEntries := storeTranscriptEntries(entries)
	return sessionInfoFromEntries(item.SessionID, item.MTime, 0, rawEntries)
}

func pageSessions(values []SessionInfo, offset, limit int) []SessionInfo {
	if offset < 0 {
		offset = 0
	}
	if len(values) == 0 || offset >= len(values) {
		return []SessionInfo{}
	}
	values = values[offset:]
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}

func pageMessages(values []SessionMessage, offset, limit int) []SessionMessage {
	if offset < 0 {
		offset = 0
	}
	if len(values) == 0 || offset >= len(values) {
		return []SessionMessage{}
	}
	values = values[offset:]
	if limit > 0 && limit < len(values) {
		values = values[:limit]
	}
	return values
}

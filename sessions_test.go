package claudeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	testSessionID      = "550e8400-e29b-41d4-a716-446655440000"
	testOtherSessionID = "660e8400-e29b-41d4-a716-446655440000"
)

func TestProjectKeyForDirectory(t *testing.T) {
	dir := t.TempDir()
	key, err := ProjectKeyForDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if key == "" || key[0] != '-' {
		t.Fatalf("unexpected project key %q", key)
	}
}

func TestGetSessionMessagesBuildsActiveChain(t *testing.T) {
	configDir, projectDir := makeSessionProject(t)
	writeTranscript(t, projectDir, testSessionID, []string{
		`{"type":"queue-operation","operation":"enqueue"}`,
		transcriptLine("user", "u1", "", testSessionID, `{"role":"user","content":"root"}`),
		transcriptLine("assistant", "a1", "u1", testSessionID, `{"role":"assistant","content":[{"type":"text","text":"first"}]}`),
		transcriptLine("user", "stale", "a1", testSessionID, `{"role":"user","content":"stale branch"}`),
		transcriptLine("user", "u2", "a1", testSessionID, `{"role":"user","content":"active branch"}`),
		transcriptLine("assistant", "a2", "u2", testSessionID, `{"role":"assistant","content":[{"type":"text","text":"done"}]}`),
	})

	messages, err := GetSessionMessages(testSessionID, SessionMessageOptions{ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 active-chain messages, got %d: %+v", len(messages), messages)
	}
	if messages[0].UUID != "u1" || messages[1].UUID != "a1" || messages[2].UUID != "u2" || messages[3].UUID != "a2" {
		t.Fatalf("unexpected chain: %+v", messages)
	}
}

func TestGetSessionMessagesPaginationAndInvalidID(t *testing.T) {
	configDir, projectDir := makeSessionProject(t)
	writeTranscript(t, projectDir, testSessionID, []string{
		transcriptLine("user", "u1", "", testSessionID, `{"role":"user","content":"one"}`),
		transcriptLine("assistant", "a1", "u1", testSessionID, `{"role":"assistant","content":"two"}`),
		transcriptLine("user", "u2", "a1", testSessionID, `{"role":"user","content":"three"}`),
	})

	messages, err := GetSessionMessages(testSessionID, SessionMessageOptions{ConfigDir: configDir, Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].UUID != "a1" {
		t.Fatalf("unexpected page: %+v", messages)
	}

	messages, err = GetSessionMessages("../not-a-session", SessionMessageOptions{ConfigDir: configDir})
	if err != nil || len(messages) != 0 {
		t.Fatalf("invalid IDs should safely return no messages, got %+v, %v", messages, err)
	}
}

func TestListSessionsMetadataAndSort(t *testing.T) {
	configDir, projectDir := makeSessionProject(t)
	writeTranscript(t, projectDir, testSessionID, []string{
		`{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"` + testSessionID + `","timestamp":"2026-01-02T03:04:05Z","cwd":"/work/project","gitBranch":"main","message":{"role":"user","content":"first prompt"}}`,
		`{"type":"assistant","uuid":"a1","parentUuid":"u1","sessionId":"` + testSessionID + `","gitBranch":"feature","message":{"role":"assistant","content":[]}}`,
		`{"type":"custom-title","customTitle":"My session"}`,
		`{"type":"tag","tag":"important"}`,
	})
	writeTranscript(t, projectDir, testOtherSessionID, []string{
		transcriptLine("user", "u2", "", testOtherSessionID, `{"role":"user","content":"newer prompt"}`),
	})
	old := time.Unix(100, 0)
	newer := time.Unix(200, 0)
	if err := os.Chtimes(filepath.Join(projectDir, testSessionID+".jsonl"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(projectDir, testOtherSessionID+".jsonl"), newer, newer); err != nil {
		t.Fatal(err)
	}

	infos, err := ListSessions(SessionListOptions{ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 || infos[0].SessionID != testOtherSessionID {
		t.Fatalf("expected newest first, got %+v", infos)
	}
	var titled SessionInfo
	for _, info := range infos {
		if info.SessionID == testSessionID {
			titled = info
		}
	}
	if titled.Summary != "My session" || titled.FirstPrompt != "first prompt" || titled.GitBranch != "feature" || titled.Tag != "important" {
		t.Fatalf("unexpected metadata: %+v", titled)
	}
	if titled.CreatedAt != time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli() {
		t.Fatalf("unexpected created timestamp: %d", titled.CreatedAt)
	}
}

func TestDeleteSessionCascadesToSubagents(t *testing.T) {
	configDir, projectDir := makeSessionProject(t)
	writeTranscript(t, projectDir, testSessionID, []string{
		transcriptLine("user", "u1", "", testSessionID, `{"role":"user","content":"hello"}`),
	})
	subagent := filepath.Join(projectDir, testSessionID, "subagents", "workflows", "run-1")
	if err := os.MkdirAll(subagent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagent, "agent-child.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := DeleteSession(testSessionID, "", configDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, testSessionID+".jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected transcript deletion, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, testSessionID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected subagent directory deletion, got %v", err)
	}
}

func TestSessionStoreHelpers(t *testing.T) {
	store := newTestSessionStore()
	key := SessionKey{ProjectKey: "project", SessionID: testSessionID}
	entries := []SessionStoreEntry{
		storeEntry(t, transcriptLine("user", "u1", "", testSessionID, `{"role":"user","content":"hello store"}`)),
		storeEntry(t, transcriptLine("assistant", "a1", "u1", testSessionID, `{"role":"assistant","content":"hi"}`)),
	}
	if err := store.Append(context.Background(), key, entries); err != nil {
		t.Fatal(err)
	}

	messages, err := GetSessionMessagesFromStore(context.Background(), store, key, SessionMessageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].UUID != "a1" {
		t.Fatalf("unexpected store messages: %+v", messages)
	}
	infos, err := ListSessionsFromStore(context.Background(), store, "project", SessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Summary != "hello store" {
		t.Fatalf("unexpected store sessions: %+v", infos)
	}

	subkey := SessionKey{ProjectKey: "project", SessionID: testSessionID, Subpath: "subagents/agent-child"}
	if err := store.Append(context.Background(), subkey, entries[:1]); err != nil {
		t.Fatal(err)
	}
	if err := DeleteSessionFromStore(context.Background(), store, "project", testSessionID); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := store.Load(context.Background(), key); loaded != nil {
		t.Fatal("main transcript was not deleted")
	}
	if loaded, _ := store.Load(context.Background(), subkey); loaded != nil {
		t.Fatal("subagent transcript was not cascade-deleted")
	}
}

func TestGetSessionMessagesWalksPastMetaLeaf(t *testing.T) {
	configDir, projectDir := makeSessionProject(t)
	writeTranscript(t, projectDir, testSessionID, []string{
		transcriptLine("user", "u1", "", testSessionID, `{"role":"user","content":"hello"}`),
		transcriptLine("assistant", "a1", "u1", testSessionID, `{"role":"assistant","content":"hi"}`),
		`{"type":"user","uuid":"meta","parentUuid":"a1","sessionId":"` + testSessionID + `","isMeta":true,"message":{"role":"user","content":"<tick>"}}`,
	})

	messages, err := GetSessionMessages(testSessionID, SessionMessageOptions{ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].UUID != "u1" || messages[1].UUID != "a1" {
		t.Fatalf("expected visible ancestors of the meta leaf, got %+v", messages)
	}
}

func TestProjectKeyForDirectoryResolvesRelativePath(t *testing.T) {
	abs, err := ProjectKeyForDirectory("")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := ProjectKeyForDirectory(".")
	if err != nil {
		t.Fatal(err)
	}
	if rel == "-" || rel != abs {
		t.Fatalf("relative directory should canonicalize to the absolute project key, got %q want %q", rel, abs)
	}
}

func TestListSessionsKeepsSummaryAndAITitleRecords(t *testing.T) {
	configDir, projectDir := makeSessionProject(t)
	writeTranscript(t, projectDir, testSessionID, []string{
		`{"type":"ai-title","aiTitle":"Generated title"}`,
		`{"type":"summary","summary":"Compacted summary"}`,
	})

	infos, err := ListSessions(SessionListOptions{ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected one session, got %+v", infos)
	}
	if infos[0].CustomTitle != "Generated title" {
		t.Fatalf("expected AI title, got %+v", infos[0])
	}
	if infos[0].Summary != "Generated title" {
		t.Fatalf("custom/AI title should win the summary, got %+v", infos[0])
	}
}

func TestTruncateSessionSummaryPreservesUTF8(t *testing.T) {
	value := strings.Repeat("你", 201)
	got := truncateSessionSummary(value)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated summary is not valid UTF-8: %q", got)
	}
	if got != strings.Repeat("你", 200)+"…" {
		t.Fatalf("unexpected truncation: %q", got)
	}
}

func TestListSessionsFromStoreSkipsFailedLoads(t *testing.T) {
	store := newTestSessionStore()
	okKey := SessionKey{ProjectKey: "project", SessionID: testSessionID}
	if err := store.Append(context.Background(), okKey, []SessionStoreEntry{
		storeEntry(t, transcriptLine("user", "u1", "", testSessionID, `{"role":"user","content":"keep me"}`)),
	}); err != nil {
		t.Fatal(err)
	}
	badKey := SessionKey{ProjectKey: "project", SessionID: testOtherSessionID}
	if err := store.Append(context.Background(), badKey, []SessionStoreEntry{
		storeEntry(t, transcriptLine("user", "u2", "", testOtherSessionID, `{"role":"user","content":"broken"}`)),
	}); err != nil {
		t.Fatal(err)
	}
	store.failIDs = map[string]error{testOtherSessionID: errors.New("unavailable")}

	infos, err := ListSessionsFromStore(context.Background(), store, "project", SessionListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].SessionID != testSessionID {
		t.Fatalf("failed loads should be skipped, got %+v", infos)
	}
}

func makeSessionProject(t *testing.T) (string, string) {
	t.Helper()
	configDir := t.TempDir()
	projectDir := filepath.Join(configDir, "projects", "-work-project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return configDir, projectDir
}

func writeTranscript(t *testing.T, projectDir, sessionID string, lines []string) {
	t.Helper()
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func transcriptLine(typ, uuid, parent, sessionID, message string) string {
	parentJSON := "null"
	if parent != "" {
		parentJSON = `"` + parent + `"`
	}
	return `{"type":"` + typ + `","uuid":"` + uuid + `","parentUuid":` + parentJSON + `,"sessionId":"` + sessionID + `","message":` + message + `}`
}

func storeEntry(t *testing.T, line string) SessionStoreEntry {
	t.Helper()
	var entry SessionStoreEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

type testSessionStore struct {
	entries map[string][]SessionStoreEntry
	mtimes  map[string]int64
	failIDs map[string]error
}

func newTestSessionStore() *testSessionStore {
	return &testSessionStore{entries: make(map[string][]SessionStoreEntry), mtimes: make(map[string]int64)}
}

func storeKey(key SessionKey) string {
	value := key.ProjectKey + "/" + key.SessionID
	if key.Subpath != "" {
		value += "/" + key.Subpath
	}
	return value
}

func (s *testSessionStore) Append(_ context.Context, key SessionKey, entries []SessionStoreEntry) error {
	k := storeKey(key)
	s.entries[k] = append(s.entries[k], entries...)
	s.mtimes[k] = time.Now().UnixMilli()
	return nil
}

func (s *testSessionStore) Load(_ context.Context, key SessionKey) ([]SessionStoreEntry, error) {
	if err := s.failIDs[key.SessionID]; err != nil {
		return nil, err
	}
	entries, ok := s.entries[storeKey(key)]
	if !ok {
		return nil, nil
	}
	return append([]SessionStoreEntry(nil), entries...), nil
}

func (s *testSessionStore) ListSessions(_ context.Context, projectKey string) ([]SessionStoreListEntry, error) {
	var result []SessionStoreListEntry
	prefix := projectKey + "/"
	for key, mtime := range s.mtimes {
		rest := key[len(prefix):]
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix && !containsRune(rest, '/') {
			result = append(result, SessionStoreListEntry{SessionID: rest, MTime: mtime})
		}
	}
	return result, nil
}

func (s *testSessionStore) Delete(_ context.Context, key SessionKey) error {
	base := storeKey(key)
	delete(s.entries, base)
	delete(s.mtimes, base)
	if key.Subpath == "" {
		for stored := range s.entries {
			if len(stored) > len(base) && stored[:len(base)+1] == base+"/" {
				delete(s.entries, stored)
				delete(s.mtimes, stored)
			}
		}
	}
	return nil
}

func containsRune(value string, target rune) bool {
	for _, r := range value {
		if r == target {
			return true
		}
	}
	return false
}

var _ SessionStore = (*testSessionStore)(nil)

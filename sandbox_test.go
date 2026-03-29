package claudeagent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSession implements SandboxSession for testing.
type mockSession struct {
	id       string
	language Language
	workDir  string
	files    map[string][]byte
	mu       sync.Mutex
	execFn   func(ctx context.Context, code string) (*ExecResult, error)
}

func (s *mockSession) ID() string { return s.id }

func (s *mockSession) Execute(ctx context.Context, code string) (*ExecResult, error) {
	if s.execFn != nil {
		return s.execFn(ctx, code)
	}
	return &ExecResult{
		ExitCode: 0,
		Stdout:   fmt.Sprintf("mock output for: %s", code),
		Duration: 100 * time.Millisecond,
	}, nil
}

func (s *mockSession) RunCommand(ctx context.Context, command string) (*ExecResult, error) {
	return &ExecResult{
		ExitCode: 0,
		Stdout:   fmt.Sprintf("mock command output: %s", command),
		Duration: 50 * time.Millisecond,
	}, nil
}

func (s *mockSession) WriteFile(_ context.Context, path string, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files == nil {
		s.files = make(map[string][]byte)
	}
	s.files[path] = content
	return nil
}

func (s *mockSession) ReadFile(_ context.Context, path string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return data, nil
}

func (s *mockSession) ListFiles(_ context.Context, dir string) ([]SandboxFileInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var files []SandboxFileInfo
	for path, content := range s.files {
		if dir == "." || strings.HasPrefix(path, dir) {
			files = append(files, SandboxFileInfo{
				Path: path,
				Size: int64(len(content)),
			})
		}
	}
	return files, nil
}

func (s *mockSession) Destroy(_ context.Context) error {
	return nil
}

// mockBackend implements SandboxBackend for testing.
type mockBackend struct {
	mu       sync.Mutex
	sessions []*mockSession
	nextID   int
	execFn   func(ctx context.Context, code string) (*ExecResult, error)
}

func (b *mockBackend) CreateSession(_ context.Context, opts SessionOptions) (SandboxSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	sess := &mockSession{
		id:       fmt.Sprintf("mock_%d", b.nextID),
		language: opts.Language,
		workDir:  opts.WorkDir,
		files:    make(map[string][]byte),
		execFn:   b.execFn,
	}
	b.sessions = append(b.sessions, sess)
	return sess, nil
}

func (b *mockBackend) Close() error { return nil }

func TestSandboxRegistry_ExecuteCode(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
	})
	defer reg.Close()

	tools := reg.Tools()

	if !tools.Has("execute_code") {
		t.Fatal("expected execute_code tool to be registered")
	}

	// Execute Python code.
	result, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"print('hello')"}`))
	if err != nil {
		t.Fatalf("execute_code failed: %v", err)
	}
	if !strings.Contains(result, "Exit code: 0") {
		t.Errorf("expected exit code 0, got: %s", result)
	}
	if !strings.Contains(result, "mock output for:") {
		t.Errorf("expected mock output, got: %s", result)
	}

	// Verify execution was recorded.
	if reg.ExecutionCount() != 1 {
		t.Errorf("expected 1 execution, got %d", reg.ExecutionCount())
	}
	execs := reg.Executions()
	if execs[0].Language != LangPython {
		t.Errorf("expected python, got %s", execs[0].Language)
	}
}

func TestSandboxRegistry_DisallowedLanguage(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:          backend,
		AllowedLanguages: []Language{LangPython},
	})
	defer reg.Close()

	tools := reg.Tools()

	_, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"javascript","code":"console.log('hi')"}`))
	if err == nil {
		t.Fatal("expected error for disallowed language")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected 'not allowed' error, got: %v", err)
	}
}

func TestSandboxRegistry_Sessions(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:        backend,
		EnableSessions: true,
	})
	defer reg.Close()

	tools := reg.Tools()

	// First call creates a session.
	result, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"x = 1"}`))
	if err != nil {
		t.Fatalf("first execute failed: %v", err)
	}
	if !strings.Contains(result, "Session: mock_1") {
		t.Errorf("expected session ID in result, got: %s", result)
	}

	// Session should be tracked.
	ids := reg.SessionIDs()
	if len(ids) != 1 {
		t.Fatalf("expected 1 session, got %d", len(ids))
	}

	// Reuse session.
	result, err = tools.Execute(context.Background(), "execute_code",
		[]byte(fmt.Sprintf(`{"language":"python","code":"print(x)","session_id":"%s"}`, ids[0])))
	if err != nil {
		t.Fatalf("second execute failed: %v", err)
	}
	if !strings.Contains(result, "Session: "+ids[0]) {
		t.Errorf("expected same session, got: %s", result)
	}

	// Should still be 1 session.
	if len(reg.SessionIDs()) != 1 {
		t.Errorf("expected still 1 session, got %d", len(reg.SessionIDs()))
	}
}

func TestSandboxRegistry_RunCommand(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:        backend,
		EnableCommands: true,
	})
	defer reg.Close()

	tools := reg.Tools()

	if !tools.Has("run_command") {
		t.Fatal("expected run_command tool to be registered")
	}

	result, err := tools.Execute(context.Background(), "run_command",
		[]byte(`{"command":"ls -la"}`))
	if err != nil {
		t.Fatalf("run_command failed: %v", err)
	}
	if !strings.Contains(result, "mock command output") {
		t.Errorf("expected mock output, got: %s", result)
	}
}

func TestSandboxRegistry_NoRunCommandByDefault(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
	})
	defer reg.Close()

	tools := reg.Tools()

	if tools.Has("run_command") {
		t.Error("run_command should not be registered when EnableCommands is false")
	}
}

func TestSandboxRegistry_FileIO(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:        backend,
		EnableSessions: true,
		EnableFileIO:   true,
	})
	defer reg.Close()

	tools := reg.Tools()

	for _, name := range []string{"sandbox_write_file", "sandbox_read_file", "sandbox_list_files"} {
		if !tools.Has(name) {
			t.Errorf("expected %s tool to be registered", name)
		}
	}

	// Create a session first.
	_, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"pass"}`))
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	sessID := reg.SessionIDs()[0]

	// Write a file.
	result, err := tools.Execute(context.Background(), "sandbox_write_file",
		[]byte(fmt.Sprintf(`{"session_id":"%s","path":"test.py","content":"print('hi')"}`, sessID)))
	if err != nil {
		t.Fatalf("write_file failed: %v", err)
	}
	if !strings.Contains(result, "Wrote") {
		t.Errorf("expected write confirmation, got: %s", result)
	}

	// Read it back.
	result, err = tools.Execute(context.Background(), "sandbox_read_file",
		[]byte(fmt.Sprintf(`{"session_id":"%s","path":"test.py"}`, sessID)))
	if err != nil {
		t.Fatalf("read_file failed: %v", err)
	}
	if result != "print('hi')" {
		t.Errorf("expected file content, got: %s", result)
	}

	// List files.
	result, err = tools.Execute(context.Background(), "sandbox_list_files",
		[]byte(fmt.Sprintf(`{"session_id":"%s"}`, sessID)))
	if err != nil {
		t.Fatalf("list_files failed: %v", err)
	}
	if !strings.Contains(result, "test.py") {
		t.Errorf("expected test.py in listing, got: %s", result)
	}
}

func TestSandboxRegistry_NoFileIOByDefault(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
	})
	defer reg.Close()

	tools := reg.Tools()

	for _, name := range []string{"sandbox_write_file", "sandbox_read_file", "sandbox_list_files"} {
		if tools.Has(name) {
			t.Errorf("%s should not be registered when EnableFileIO is false", name)
		}
	}
}

func TestSandboxRegistry_SystemPrompt(t *testing.T) {
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:          &mockBackend{},
		AllowedLanguages: []Language{LangPython, LangJavaScript},
		EnableSessions:   true,
		EnableCommands:   true,
		EnableFileIO:     true,
	})

	prompt := reg.SystemPrompt()

	if !strings.Contains(prompt, "python") {
		t.Error("expected python in system prompt")
	}
	if !strings.Contains(prompt, "javascript") {
		t.Error("expected javascript in system prompt")
	}
	if !strings.Contains(prompt, "session") {
		t.Error("expected session guidance in system prompt")
	}
	if !strings.Contains(prompt, "run_command") {
		t.Error("expected run_command guidance in system prompt")
	}
}

func TestSandboxRegistry_GetExecution(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
	})
	defer reg.Close()

	tools := reg.Tools()
	_, _ = tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"print(1)"}`))

	exec, ok := reg.GetExecution("exec_1")
	if !ok {
		t.Fatal("expected to find execution exec_1")
	}
	if exec.Language != LangPython {
		t.Errorf("expected python, got %s", exec.Language)
	}

	_, ok = reg.GetExecution("exec_999")
	if ok {
		t.Error("expected not to find exec_999")
	}
}

func TestSandboxRegistry_Close(t *testing.T) {
	backend := &mockBackend{}
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:        backend,
		EnableSessions: true,
	})

	tools := reg.Tools()
	_, _ = tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"pass"}`))

	if len(reg.SessionIDs()) != 1 {
		t.Fatal("expected 1 session before close")
	}

	if err := reg.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if len(reg.SessionIDs()) != 0 {
		t.Error("expected 0 sessions after close")
	}
}

func TestSandboxRegistry_EmptyCode(t *testing.T) {
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: &mockBackend{},
	})
	defer reg.Close()

	tools := reg.Tools()
	_, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":""}`))
	if err == nil {
		t.Error("expected error for empty code")
	}
}

func TestSandboxRegistry_SessionNotFound(t *testing.T) {
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:        &mockBackend{},
		EnableSessions: true,
	})
	defer reg.Close()

	tools := reg.Tools()
	_, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"pass","session_id":"nonexistent"}`))
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestProcessBackend_PathTraversal(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	backend := NewProcessBackend(ProcessBackendConfig{})
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:        backend,
		EnableSessions: true,
		EnableFileIO:   true,
	})
	defer reg.Close()

	tools := reg.Tools()

	// Create a session.
	_, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"pass"}`))
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	sessID := reg.SessionIDs()[0]

	// Attempt path traversal on write.
	_, err = tools.Execute(context.Background(), "sandbox_write_file",
		[]byte(fmt.Sprintf(`{"session_id":"%s","path":"../../etc/evil","content":"bad"}`, sessID)))
	if err == nil {
		t.Fatal("expected error for path traversal on write")
	}
	if !strings.Contains(err.Error(), "escapes sandbox") {
		t.Errorf("expected 'escapes sandbox' error, got: %v", err)
	}

	// Attempt path traversal on read.
	_, err = tools.Execute(context.Background(), "sandbox_read_file",
		[]byte(fmt.Sprintf(`{"session_id":"%s","path":"../../../etc/passwd"}`, sessID)))
	if err == nil {
		t.Fatal("expected error for path traversal on read")
	}
	if !strings.Contains(err.Error(), "escapes sandbox") {
		t.Errorf("expected 'escapes sandbox' error, got: %v", err)
	}

	// Attempt path traversal on list.
	_, err = tools.Execute(context.Background(), "sandbox_list_files",
		[]byte(fmt.Sprintf(`{"session_id":"%s","dir":"../../"}`, sessID)))
	if err == nil {
		t.Fatal("expected error for path traversal on list")
	}
	if !strings.Contains(err.Error(), "escapes sandbox") {
		t.Errorf("expected 'escapes sandbox' error, got: %v", err)
	}
}

func TestFormatExecResult(t *testing.T) {
	result := &ExecResult{
		ExitCode: 1,
		Stdout:   "hello\n",
		Stderr:   "error\n",
		Duration: 1500 * time.Millisecond,
		TimedOut: true,
	}

	formatted := formatExecResult("sandbox_1", true, result)

	if !strings.Contains(formatted, "Session: sandbox_1") {
		t.Error("expected session ID")
	}
	if !strings.Contains(formatted, "Exit code: 1") {
		t.Error("expected exit code")
	}
	if !strings.Contains(formatted, "TIMED OUT") {
		t.Error("expected timeout notice")
	}
	if !strings.Contains(formatted, "hello") {
		t.Error("expected stdout")
	}
	if !strings.Contains(formatted, "error") {
		t.Error("expected stderr")
	}

	// Without session.
	formatted = formatExecResult("sandbox_1", false, result)
	if strings.Contains(formatted, "Session:") {
		t.Error("should not contain session when showSession is false")
	}
}

func TestTruncateOutput(t *testing.T) {
	short := "hello"
	if truncateOutput(short, 100) != short {
		t.Error("short string should not be truncated")
	}

	long := strings.Repeat("x", 200)
	truncated := truncateOutput(long, 100)
	if len(truncated) >= len(long) {
		t.Error("long string should be truncated")
	}
	if !strings.Contains(truncated, "truncated") {
		t.Error("expected truncation notice")
	}
}

// Integration test using ProcessBackend (requires python3 to be available).
func TestProcessBackend_Integration(t *testing.T) {
	// Skip if python3 is not available.
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	backend := NewProcessBackend(ProcessBackendConfig{})
	reg := NewSandboxRegistry(SandboxConfig{
		Backend:          backend,
		AllowedLanguages: []Language{LangPython, LangBash},
		EnableSessions:   true,
		EnableCommands:   true,
		EnableFileIO:     true,
	})
	defer reg.Close()

	tools := reg.Tools()

	// Execute Python.
	result, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"print(2 + 2)"}`))
	if err != nil {
		t.Fatalf("execute python: %v", err)
	}
	if !strings.Contains(result, "4") {
		t.Errorf("expected 4, got: %s", result)
	}

	// Execute Bash.
	result, err = tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"bash","code":"echo hello"}`))
	if err != nil {
		t.Fatalf("execute bash: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected hello, got: %s", result)
	}

	// Run command.
	result, err = tools.Execute(context.Background(), "run_command",
		[]byte(`{"command":"echo world"}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	if !strings.Contains(result, "world") {
		t.Errorf("expected world, got: %s", result)
	}

	// Test file I/O with session.
	sessIDs := reg.SessionIDs()
	if len(sessIDs) == 0 {
		t.Fatal("expected at least one session")
	}
	sessID := sessIDs[0]

	// Write file.
	_, err = tools.Execute(context.Background(), "sandbox_write_file",
		[]byte(fmt.Sprintf(`{"session_id":"%s","path":"data.txt","content":"test data"}`, sessID)))
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Read file.
	result, err = tools.Execute(context.Background(), "sandbox_read_file",
		[]byte(fmt.Sprintf(`{"session_id":"%s","path":"data.txt"}`, sessID)))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if result != "test data" {
		t.Errorf("expected 'test data', got: %s", result)
	}

	// List files.
	result, err = tools.Execute(context.Background(), "sandbox_list_files",
		[]byte(fmt.Sprintf(`{"session_id":"%s"}`, sessID)))
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if !strings.Contains(result, "data.txt") {
		t.Errorf("expected data.txt in listing, got: %s", result)
	}
}

func TestProcessBackend_NonZeroExit(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	backend := NewProcessBackend(ProcessBackendConfig{})
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
	})
	defer reg.Close()

	tools := reg.Tools()
	result, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"import sys; sys.exit(42)"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Exit code: 42") {
		t.Errorf("expected exit code 42, got: %s", result)
	}
}

func TestProcessBackend_Timeout(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	backend := NewProcessBackend(ProcessBackendConfig{})
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
		Limits:  ResourceLimits{WallClockSec: 1},
	})
	defer reg.Close()

	tools := reg.Tools()
	result, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"import time; time.sleep(10)"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "TIMED OUT") {
		t.Errorf("expected timeout, got: %s", result)
	}
}

func TestProcessBackend_StderrCapture(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	backend := NewProcessBackend(ProcessBackendConfig{})
	reg := NewSandboxRegistry(SandboxConfig{
		Backend: backend,
	})
	defer reg.Close()

	tools := reg.Tools()
	result, err := tools.Execute(context.Background(), "execute_code",
		[]byte(`{"language":"python","code":"import sys; print('err', file=sys.stderr)"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "err") {
		t.Errorf("expected stderr, got: %s", result)
	}
}

func TestResourceLimits_Defaults(t *testing.T) {
	var zero ResourceLimits
	d := zero.withDefaults()

	if d.MemoryMB != 256 {
		t.Errorf("expected 256, got %d", d.MemoryMB)
	}
	if d.WallClockSec != 60 {
		t.Errorf("expected 60, got %d", d.WallClockSec)
	}
	if d.MaxOutputBytes != 1<<20 {
		t.Errorf("expected 1MB, got %d", d.MaxOutputBytes)
	}

	// Non-zero values should be preserved.
	custom := ResourceLimits{MemoryMB: 512}
	d = custom.withDefaults()
	if d.MemoryMB != 512 {
		t.Errorf("expected 512, got %d", d.MemoryMB)
	}
}

func TestAllLanguages(t *testing.T) {
	langs := AllLanguages()
	if len(langs) != 4 {
		t.Errorf("expected 4 languages, got %d", len(langs))
	}
}

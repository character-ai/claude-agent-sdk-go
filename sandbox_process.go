package claudeagent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ProcessBackendConfig configures the local process sandbox backend.
type ProcessBackendConfig struct {
	// TempDir is the base directory for sandbox working directories.
	// Defaults to os.TempDir().
	TempDir string
}

// ProcessBackend runs code as local subprocesses. It does NOT provide true
// isolation — suitable for development and testing only.
type ProcessBackend struct {
	tempDir  string
	mu       sync.Mutex
	sessions map[string]*processSession
	nextID   int
}

// NewProcessBackend creates a new process-based sandbox backend.
func NewProcessBackend(cfg ProcessBackendConfig) *ProcessBackend {
	dir := cfg.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	return &ProcessBackend{
		tempDir:  dir,
		sessions: make(map[string]*processSession),
	}
}

// CreateSession creates a new process-based sandbox session.
func (b *ProcessBackend) CreateSession(ctx context.Context, opts SessionOptions) (SandboxSession, error) {
	dir, err := os.MkdirTemp(b.tempDir, "sandbox-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	b.mu.Lock()
	b.nextID++
	id := fmt.Sprintf("sandbox_%d", b.nextID)
	sess := &processSession{
		id:       id,
		language: opts.Language,
		limits:   opts.Limits,
		workDir:  dir,
	}
	b.sessions[id] = sess
	b.mu.Unlock()

	return sess, nil
}

// Close cleans up all sessions.
func (b *ProcessBackend) Close() error {
	b.mu.Lock()
	sessions := make([]*processSession, 0, len(b.sessions))
	for _, s := range b.sessions {
		sessions = append(sessions, s)
	}
	b.sessions = make(map[string]*processSession)
	b.mu.Unlock()

	for _, s := range sessions {
		_ = s.Destroy(context.Background())
	}
	return nil
}

type processSession struct {
	id       string
	language Language
	limits   ResourceLimits
	workDir  string
}

func (s *processSession) ID() string { return s.id }

func (s *processSession) Execute(ctx context.Context, code string) (*ExecResult, error) {
	// Go needs a temp file; other languages use -c/-e flags.
	if s.language == LangGo {
		path := filepath.Join(s.workDir, "main.go")
		if err := os.WriteFile(path, []byte(code), 0600); err != nil {
			return nil, fmt.Errorf("write code file: %w", err)
		}
		defer os.Remove(path) //nolint:errcheck // best-effort cleanup
		return s.runCmd(ctx, "go", "run", path)
	}

	interpreter, flag := s.interpreterFlag()
	return s.runCmd(ctx, interpreter, flag, code)
}

func (s *processSession) RunCommand(ctx context.Context, command string) (*ExecResult, error) {
	return s.runCmd(ctx, "sh", "-c", command)
}

// runCmd executes a command with timeout and captures stdout/stderr.
func (s *processSession) runCmd(ctx context.Context, name string, args ...string) (*ExecResult, error) {
	timeout := time.Duration(s.limits.WallClockSec) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- sandbox intentionally executes user-provided code
	cmd.Dir = s.workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	result := &ExecResult{
		Stdout:   truncateOutput(stdout.String(), s.limits.MaxOutputBytes),
		Stderr:   truncateOutput(stderr.String(), s.limits.MaxOutputBytes),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = 124 // standard timeout exit code
	} else if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("execution error: %w", runErr)
		}
	}

	return result, nil
}

func (s *processSession) safePath(path string) (string, error) {
	return sandboxSafePath(s.workDir, path)
}

func (s *processSession) WriteFile(_ context.Context, path string, content []byte) error {
	full, err := s.safePath(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(full, content, 0600)
}

func (s *processSession) ReadFile(_ context.Context, path string) ([]byte, error) {
	full, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full) // #nosec G304 -- path is validated by safePath to stay within sandbox
}

func (s *processSession) ListFiles(_ context.Context, dir string) ([]SandboxFileInfo, error) {
	full, err := s.safePath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}

	files := make([]SandboxFileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, SandboxFileInfo{
			Path:  e.Name(),
			Size:  info.Size(),
			IsDir: e.IsDir(),
		})
	}
	return files, nil
}

func (s *processSession) Destroy(_ context.Context) error {
	return os.RemoveAll(s.workDir)
}

// interpreterFlag returns the interpreter binary and its code-passing flag.
func (s *processSession) interpreterFlag() (string, string) {
	switch s.language {
	case LangPython:
		return "python3", "-c"
	case LangJavaScript:
		return "node", "-e"
	default: // LangBash and others
		return "bash", "-c"
	}
}

// truncateOutput truncates output to maxBytes, appending a truncation notice.
func truncateOutput(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	truncated := s[:maxBytes]
	// Try to truncate at a line boundary.
	if idx := strings.LastIndex(truncated, "\n"); idx > maxBytes/2 {
		truncated = truncated[:idx+1]
	}
	return truncated + fmt.Sprintf("\n... (truncated, %d bytes total)", len(s))
}

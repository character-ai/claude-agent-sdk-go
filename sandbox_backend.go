package claudeagent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Language represents a supported programming language for sandbox execution.
type Language string

const (
	LangPython     Language = "python"
	LangJavaScript Language = "javascript"
	LangGo         Language = "go"
	LangBash       Language = "bash"
)

// AllLanguages returns all built-in supported languages.
func AllLanguages() []Language {
	return []Language{LangPython, LangJavaScript, LangGo, LangBash}
}

// ResourceLimits defines execution constraints for sandbox sessions.
type ResourceLimits struct {
	// MemoryMB is the maximum memory in megabytes. 0 uses the default (256MB).
	// Enforced by Docker backend via --memory flag.
	MemoryMB int `json:"memory_mb,omitempty"`
	// WallClockSec is the maximum wall-clock time in seconds. 0 uses the default (60s).
	// Enforced by both backends via context timeout.
	WallClockSec int `json:"wall_clock_sec,omitempty"`
	// MaxOutputBytes truncates stdout/stderr to this many bytes. 0 uses the default (1MB).
	MaxOutputBytes int `json:"max_output_bytes,omitempty"`
}

// DefaultResourceLimits returns safe default resource limits.
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MemoryMB:       256,
		WallClockSec:   60,
		MaxOutputBytes: 1 << 20, // 1MB
	}
}

// withDefaults returns a copy with zero values replaced by defaults.
func (r ResourceLimits) withDefaults() ResourceLimits {
	d := DefaultResourceLimits()
	if r.MemoryMB == 0 {
		r.MemoryMB = d.MemoryMB
	}
	if r.WallClockSec == 0 {
		r.WallClockSec = d.WallClockSec
	}
	if r.MaxOutputBytes == 0 {
		r.MaxOutputBytes = d.MaxOutputBytes
	}
	return r
}

// SessionOptions configures a new sandbox session.
type SessionOptions struct {
	Language Language
	Limits   ResourceLimits
	WorkDir  string
}

// ExecResult holds the output of a code execution or command.
type ExecResult struct {
	ExitCode  int           `json:"exit_code"`
	Stdout    string        `json:"stdout"`
	Stderr    string        `json:"stderr"`
	Duration  time.Duration `json:"duration"`
	OOMKilled bool          `json:"oom_killed,omitempty"`
	TimedOut  bool          `json:"timed_out,omitempty"`
}

// SandboxFileInfo describes a file inside a sandbox session.
type SandboxFileInfo struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"is_dir"`
}

// sandboxSafePath resolves a path within baseDir and ensures it does not
// escape via traversal (e.g., "../../etc/passwd").
func sandboxSafePath(baseDir, path string) (string, error) {
	full := filepath.Join(baseDir, path)
	cleanBase := filepath.Clean(baseDir) + string(filepath.Separator)
	cleanFull := filepath.Clean(full)
	if cleanFull != filepath.Clean(baseDir) && !strings.HasPrefix(cleanFull+string(filepath.Separator), cleanBase) {
		return "", fmt.Errorf("path escapes sandbox: %s", path)
	}
	return full, nil
}

// SandboxBackend creates and manages isolated execution environments.
type SandboxBackend interface {
	// CreateSession creates a new sandbox session.
	CreateSession(ctx context.Context, opts SessionOptions) (SandboxSession, error)

	// Close cleans up all backend resources.
	Close() error
}

// SandboxSession represents an active sandbox environment.
type SandboxSession interface {
	// ID returns the unique session identifier.
	ID() string

	// Execute runs code and returns the result.
	Execute(ctx context.Context, code string) (*ExecResult, error)

	// RunCommand runs a shell command and returns the result.
	RunCommand(ctx context.Context, command string) (*ExecResult, error)

	// WriteFile writes content to a file inside the sandbox.
	WriteFile(ctx context.Context, path string, content []byte) error

	// ReadFile reads a file from the sandbox.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// ListFiles lists files in a directory inside the sandbox.
	ListFiles(ctx context.Context, dir string) ([]SandboxFileInfo, error)

	// Destroy tears down the session and releases resources.
	Destroy(ctx context.Context) error
}

package claudeagent

import (
	"errors"
	"fmt"
)

var (
	// ErrCLINotFound indicates the claude CLI is not installed or not in PATH.
	ErrCLINotFound = errors.New("claude CLI not found in PATH")

	// ErrAlreadyRunning indicates the client is already processing a query.
	ErrAlreadyRunning = errors.New("client already running")

	// ErrNotRunning indicates no query is in progress.
	ErrNotRunning = errors.New("no query in progress")
)

// ProcessError represents an error from the CLI process.
type ProcessError struct {
	ExitCode int
	Stderr   string
}

func (e *ProcessError) Error() string {
	return fmt.Sprintf("claude process exited with code %d: %s", e.ExitCode, e.Stderr)
}

// JSONDecodeError represents a JSON parsing error.
type JSONDecodeError struct {
	Line string
	Err  error
}

func (e *JSONDecodeError) Error() string {
	return fmt.Sprintf("failed to decode JSON: %v (line: %s)", e.Err, truncate(e.Line, 100))
}

func (e *JSONDecodeError) Unwrap() error {
	return e.Err
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

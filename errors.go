package claudeagent

import (
	"errors"
	"fmt"
	"strings"
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

// ResultError is returned when the CLI reports a terminal error result — a
// "result" message with IsError true (see ResultMessage). It carries the
// result's payload so callers can branch on why the run failed without
// string matching, e.g.:
//
//	if re, ok := err.(*ResultError); ok {
//	    if re.TerminalReason == "api_error" {
//	        retry()
//	    }
//	}
type ResultError struct {
	// Subtype is the result subtype (e.g. "error_max_turns",
	// "error_during_execution", or "success" when the agent loop itself
	// completed but the last turn was an API error).
	Subtype string
	// Errors lists error strings reported by the CLI (may be empty).
	Errors []string
	// Result is the result text, if any. For API failures this holds the
	// "API Error: ..." prose.
	Result string
	// APIErrorStatus is the HTTP status of the failing API call, if any.
	APIErrorStatus *int
	// TerminalReason explains why the run ended (e.g. "api_error", "max_turns"),
	// if reported by the CLI.
	TerminalReason string
	// SessionID is the session the result belongs to, if reported.
	SessionID string
}

func (e *ResultError) Error() string {
	msg := "claude reported an error result"
	if e.Result != "" {
		msg = e.Result
	} else if len(e.Errors) > 0 {
		msg = strings.Join(e.Errors, "; ")
	}
	if e.Subtype != "" {
		msg = fmt.Sprintf("%s (subtype: %s)", msg, e.Subtype)
	}
	return msg
}

// NewResultError builds a ResultError from a terminal-error ResultMessage.
func NewResultError(r *ResultMessage) *ResultError {
	if r == nil {
		return &ResultError{}
	}
	return &ResultError{
		Subtype:        r.Subtype,
		Errors:         r.Errors,
		Result:         r.Result,
		APIErrorStatus: r.APIErrorStatus,
		TerminalReason: r.TerminalReason,
		SessionID:      r.SessionID,
	}
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

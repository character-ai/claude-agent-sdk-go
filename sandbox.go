package claudeagent

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// SandboxConfig configures the sandbox registry.
type SandboxConfig struct {
	// Backend is the execution backend (required).
	Backend SandboxBackend

	// AllowedLanguages restricts which languages the agent can use.
	// Empty means all built-in languages are allowed.
	AllowedLanguages []Language

	// Limits sets resource constraints for all executions.
	// Zero values use defaults from DefaultResourceLimits().
	Limits ResourceLimits

	// EnableSessions allows the agent to create persistent sessions
	// that survive across multiple execute_code calls.
	EnableSessions bool

	// EnableFileIO exposes write_file, read_file, list_files tools.
	EnableFileIO bool

	// EnableCommands exposes the run_command tool.
	EnableCommands bool

	// WorkDir is the working directory inside the sandbox.
	// Defaults to "/workspace".
	WorkDir string
}

// Execution records a single code execution for host app inspection.
type Execution struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id"`
	Language  Language    `json:"language"`
	Code      string      `json:"code"`
	Command   string      `json:"command,omitempty"`
	Result    *ExecResult `json:"result"`
	CreatedAt time.Time   `json:"created_at"`
}

// SandboxRegistry manages sandbox sessions and provides tools for agent use.
type SandboxRegistry struct {
	mu         sync.RWMutex
	config     SandboxConfig
	sessions   map[string]SandboxSession
	executions []*Execution
	nextExecID int
	tools      *ToolRegistry // cached
}

// NewSandboxRegistry creates a new sandbox registry.
func NewSandboxRegistry(cfg SandboxConfig) *SandboxRegistry {
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/workspace"
	}
	cfg.Limits = cfg.Limits.withDefaults()
	return &SandboxRegistry{
		config:   cfg,
		sessions: make(map[string]SandboxSession),
	}
}

// Tools returns a ToolRegistry with sandbox tools.
// The registry is created once and cached.
func (r *SandboxRegistry) Tools() *ToolRegistry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools != nil {
		return r.tools
	}

	tools := NewToolRegistry()

	langStrings := r.allowedLanguageStrings()

	sessionDesc := ""
	if r.config.EnableSessions {
		sessionDesc = " Use session_id to reuse an existing session (e.g., after installing packages)."
	}

	execSchema := map[string]any{
		"language": EnumParam("Programming language to execute", langStrings...),
		"code":     StringParam("The code to execute"),
	}
	required := []string{"language", "code"}
	if r.config.EnableSessions {
		execSchema["session_id"] = StringParam("Optional: reuse an existing sandbox session by ID")
	}

	RegisterFunc(tools, ToolDefinition{
		Name:        "execute_code",
		Description: "Execute code in an isolated sandbox. Returns stdout, stderr, and exit code." + sessionDesc,
		InputSchema: ObjectSchema(execSchema, required...),
	}, r.handleExecuteCode)

	if r.config.EnableCommands {
		cmdSchema := map[string]any{
			"command": StringParam("The shell command to run"),
		}
		cmdRequired := []string{"command"}
		if r.config.EnableSessions {
			cmdSchema["session_id"] = StringParam("Optional: reuse an existing sandbox session by ID")
		}

		RegisterFunc(tools, ToolDefinition{
			Name:        "run_command",
			Description: "Run a shell command in the sandbox. Returns stdout, stderr, and exit code." + sessionDesc,
			InputSchema: ObjectSchema(cmdSchema, cmdRequired...),
		}, r.handleRunCommand)
	}

	if r.config.EnableFileIO {
		RegisterFunc(tools, ToolDefinition{
			Name:        "sandbox_write_file",
			Description: "Write content to a file inside a sandbox session.",
			InputSchema: ObjectSchema(map[string]any{
				"session_id": StringParam("The sandbox session ID"),
				"path":       StringParam("File path relative to the working directory"),
				"content":    StringParam("The file content to write"),
			}, "session_id", "path", "content"),
		}, r.handleWriteFile)

		RegisterFunc(tools, ToolDefinition{
			Name:        "sandbox_read_file",
			Description: "Read a file from inside a sandbox session.",
			InputSchema: ObjectSchema(map[string]any{
				"session_id": StringParam("The sandbox session ID"),
				"path":       StringParam("File path relative to the working directory"),
			}, "session_id", "path"),
		}, r.handleReadFile)

		RegisterFunc(tools, ToolDefinition{
			Name:        "sandbox_list_files",
			Description: "List files in a directory inside a sandbox session.",
			InputSchema: ObjectSchema(map[string]any{
				"session_id": StringParam("The sandbox session ID"),
				"dir":        StringParam("Directory path relative to the working directory. Defaults to the working directory root."),
			}, "session_id"),
		}, r.handleListFiles)
	}

	r.tools = tools
	return tools
}

func (r *SandboxRegistry) allowedLanguageStrings() []string {
	langs := r.config.AllowedLanguages
	if len(langs) == 0 {
		langs = AllLanguages()
	}
	s := make([]string, len(langs))
	for i, l := range langs {
		s[i] = string(l)
	}
	return s
}

func (r *SandboxRegistry) isLanguageAllowed(lang Language) bool {
	if len(r.config.AllowedLanguages) == 0 {
		return true
	}
	return slices.Contains(r.config.AllowedLanguages, lang)
}

// getOrCreateSession retrieves an existing session or creates a new one.
// Caller must NOT hold r.mu.
func (r *SandboxRegistry) getOrCreateSession(ctx context.Context, sessionID string, lang Language) (SandboxSession, error) {
	if sessionID != "" && r.config.EnableSessions {
		return r.getSession(sessionID)
	}

	sess, err := r.config.Backend.CreateSession(ctx, SessionOptions{
		Language: lang,
		Limits:   r.config.Limits,
		WorkDir:  r.config.WorkDir,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox session: %w", err)
	}

	if r.config.EnableSessions {
		r.mu.Lock()
		r.sessions[sess.ID()] = sess
		r.mu.Unlock()
	}

	return sess, nil
}

func (r *SandboxRegistry) recordExecution(sessionID string, lang Language, code, command string, result *ExecResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextExecID++
	id := fmt.Sprintf("exec_%d", r.nextExecID)
	r.executions = append(r.executions, &Execution{
		ID:        id,
		SessionID: sessionID,
		Language:  lang,
		Code:      code,
		Command:   command,
		Result:    result,
		CreatedAt: time.Now(),
	})
}

// getSession looks up an active session by ID. Returns an error if not found.
func (r *SandboxRegistry) getSession(sessionID string) (SandboxSession, error) {
	r.mu.RLock()
	sess, ok := r.sessions[sessionID]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return sess, nil
}

// Tool handlers

type executeCodeInput struct {
	Language  Language `json:"language"`
	Code      string   `json:"code"`
	SessionID string   `json:"session_id,omitempty"`
}

func (r *SandboxRegistry) handleExecuteCode(ctx context.Context, input executeCodeInput) (string, error) {
	if !r.isLanguageAllowed(input.Language) {
		return "", fmt.Errorf("language %q is not allowed", input.Language)
	}
	if input.Code == "" {
		return "", fmt.Errorf("code is required")
	}

	sess, err := r.getOrCreateSession(ctx, input.SessionID, input.Language)
	if err != nil {
		return "", err
	}

	// Destroy ephemeral sessions after execution.
	if !r.config.EnableSessions {
		defer sess.Destroy(ctx) //nolint:errcheck // best-effort cleanup
	}

	result, err := sess.Execute(ctx, input.Code)
	if err != nil {
		return "", fmt.Errorf("execution failed: %w", err)
	}

	r.recordExecution(sess.ID(), input.Language, input.Code, "", result)
	return formatExecResult(sess.ID(), r.config.EnableSessions, result), nil
}

type runCommandInput struct {
	Command   string `json:"command"`
	SessionID string `json:"session_id,omitempty"`
}

func (r *SandboxRegistry) handleRunCommand(ctx context.Context, input runCommandInput) (string, error) {
	if input.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	sess, err := r.getOrCreateSession(ctx, input.SessionID, LangBash)
	if err != nil {
		return "", err
	}

	if !r.config.EnableSessions {
		defer sess.Destroy(ctx) //nolint:errcheck // best-effort cleanup
	}

	result, err := sess.RunCommand(ctx, input.Command)
	if err != nil {
		return "", fmt.Errorf("command execution failed: %w", err)
	}

	r.recordExecution(sess.ID(), LangBash, "", input.Command, result)
	return formatExecResult(sess.ID(), r.config.EnableSessions, result), nil
}

type writeFileInput struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	Content   string `json:"content"`
}

func (r *SandboxRegistry) handleWriteFile(ctx context.Context, input writeFileInput) (string, error) {
	sess, err := r.getSession(input.SessionID)
	if err != nil {
		return "", err
	}

	if err := sess.WriteFile(ctx, input.Path, []byte(input.Content)); err != nil {
		return "", fmt.Errorf("write failed: %w", err)
	}
	return fmt.Sprintf("Wrote %d bytes to %s", len(input.Content), input.Path), nil
}

type readFileInput struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
}

func (r *SandboxRegistry) handleReadFile(ctx context.Context, input readFileInput) (string, error) {
	sess, err := r.getSession(input.SessionID)
	if err != nil {
		return "", err
	}

	data, err := sess.ReadFile(ctx, input.Path)
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	return string(data), nil
}

type listFilesInput struct {
	SessionID string `json:"session_id"`
	Dir       string `json:"dir,omitempty"`
}

func (r *SandboxRegistry) handleListFiles(ctx context.Context, input listFilesInput) (string, error) {
	sess, err := r.getSession(input.SessionID)
	if err != nil {
		return "", err
	}

	dir := input.Dir
	if dir == "" {
		dir = "."
	}

	files, err := sess.ListFiles(ctx, dir)
	if err != nil {
		return "", fmt.Errorf("list files failed: %w", err)
	}

	if len(files) == 0 {
		return "(empty directory)", nil
	}

	var b strings.Builder
	for _, f := range files {
		if f.IsDir {
			fmt.Fprintf(&b, "%s/\n", f.Path)
		} else {
			fmt.Fprintf(&b, "%s (%d bytes)\n", f.Path, f.Size)
		}
	}
	return b.String(), nil
}

// Public query methods

// Executions returns all executions in chronological order.
func (r *SandboxRegistry) Executions() []Execution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Execution, len(r.executions))
	for i, e := range r.executions {
		result[i] = *e
	}
	return result
}

// GetExecution returns a specific execution by ID.
func (r *SandboxRegistry) GetExecution(id string) (*Execution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.executions {
		if e.ID == id {
			copy := *e
			return &copy, true
		}
	}
	return nil, false
}

// ExecutionCount returns the total number of executions.
func (r *SandboxRegistry) ExecutionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.executions)
}

// SessionIDs returns all active session IDs.
func (r *SandboxRegistry) SessionIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Close destroys all sessions and releases backend resources.
func (r *SandboxRegistry) Close() error {
	r.mu.Lock()
	sessions := make([]SandboxSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.sessions = make(map[string]SandboxSession)
	r.mu.Unlock()

	ctx := context.Background()
	for _, s := range sessions {
		_ = s.Destroy(ctx)
	}
	return r.config.Backend.Close()
}

// SystemPrompt returns a system prompt snippet for sandbox usage.
func (r *SandboxRegistry) SystemPrompt() string {
	var b strings.Builder
	b.WriteString("You have access to a code execution sandbox for running code in isolated environments.\n\n")

	b.WriteString("Supported languages: ")
	langs := r.allowedLanguageStrings()
	b.WriteString(strings.Join(langs, ", "))
	b.WriteString("\n\n")

	b.WriteString("Guidelines:\n")
	b.WriteString("- Use execute_code to run code snippets. Output (stdout/stderr) and exit code are returned.\n")
	b.WriteString("- Write small, focused code blocks. Print results explicitly — the sandbox captures stdout.\n")
	b.WriteString("- For Python: use print() for output. For JavaScript: use console.log().\n")

	if r.config.EnableSessions {
		b.WriteString("- Sessions persist across calls. The first execute_code call returns a session_id — pass it to subsequent calls to reuse the environment (e.g., install packages then use them).\n")
	}
	if r.config.EnableCommands {
		b.WriteString("- Use run_command for shell operations (installing packages, file manipulation, etc.).\n")
	}
	if r.config.EnableFileIO {
		b.WriteString("- Use sandbox_write_file/sandbox_read_file/sandbox_list_files to manage files within a session.\n")
	}

	fmt.Fprintf(&b, "- Resource limits: %dMB memory, %ds wall-clock timeout.\n",
		r.config.Limits.MemoryMB, r.config.Limits.WallClockSec)
	b.WriteString("- If execution times out or runs out of memory, you'll see that in the result. Optimize and retry.\n")

	return b.String()
}

// formatExecResult formats an ExecResult for return to the agent.
func formatExecResult(sessionID string, showSession bool, r *ExecResult) string {
	var b strings.Builder

	if showSession {
		fmt.Fprintf(&b, "Session: %s\n", sessionID)
	}
	fmt.Fprintf(&b, "Exit code: %d\n", r.ExitCode)
	fmt.Fprintf(&b, "Duration: %.2fs\n", r.Duration.Seconds())

	if r.TimedOut {
		b.WriteString("** TIMED OUT **\n")
	}
	if r.OOMKilled {
		b.WriteString("** OUT OF MEMORY **\n")
	}

	b.WriteString("\n--- stdout ---\n")
	if r.Stdout == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(r.Stdout)
		if !strings.HasSuffix(r.Stdout, "\n") {
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n--- stderr ---\n")
	if r.Stderr == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(r.Stderr)
		if !strings.HasSuffix(r.Stderr, "\n") {
			b.WriteByte('\n')
		}
	}

	return b.String()
}

package claudeagent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DockerBackendConfig configures the Docker sandbox backend.
type DockerBackendConfig struct {
	// Images maps languages to Docker image names. Defaults are provided for all
	// built-in languages if not specified.
	Images map[Language]string

	// NetworkMode is the Docker network mode. Defaults to "none" (no network access).
	NetworkMode string
}

// DefaultDockerImages returns the default Docker images for each language.
func DefaultDockerImages() map[Language]string {
	return map[Language]string{
		LangPython:     "python:3.12-slim",
		LangJavaScript: "node:20-slim",
		LangGo:         "golang:1.23-alpine",
		LangBash:       "alpine:3.19",
	}
}

// DockerBackend runs code inside Docker containers for production-grade isolation.
type DockerBackend struct {
	images      map[Language]string
	networkMode string
	mu          sync.Mutex
	sessions    map[string]*dockerSession
	nextID      int
}

// NewDockerBackend creates a new Docker-based sandbox backend.
func NewDockerBackend(cfg DockerBackendConfig) *DockerBackend {
	images := DefaultDockerImages()
	for lang, img := range cfg.Images {
		images[lang] = img
	}

	networkMode := cfg.NetworkMode
	if networkMode == "" {
		networkMode = "none"
	}

	return &DockerBackend{
		images:      images,
		networkMode: networkMode,
		sessions:    make(map[string]*dockerSession),
	}
}

// CreateSession creates a new Docker container as a sandbox session.
func (b *DockerBackend) CreateSession(ctx context.Context, opts SessionOptions) (SandboxSession, error) {
	image, ok := b.images[opts.Language]
	if !ok {
		return nil, fmt.Errorf("no Docker image configured for language %q", opts.Language)
	}

	b.mu.Lock()
	b.nextID++
	id := fmt.Sprintf("sandbox_%d", b.nextID)
	b.mu.Unlock()

	containerName := fmt.Sprintf("claude-sandbox-%s", id)
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "/workspace"
	}

	// Create and start a long-running container.
	args := []string{
		"create",
		"--name", containerName,
		"--network", b.networkMode,
		"--workdir", workDir,
		fmt.Sprintf("--memory=%dm", opts.Limits.MemoryMB),
		"--oom-kill-disable=false",
		"--cpus=1",
		image, "sleep", "3600", // keep container alive
	}

	if err := dockerExec(ctx, args...); err != nil {
		return nil, fmt.Errorf("docker create: %w", err)
	}

	if err := dockerExec(ctx, "start", containerName); err != nil {
		// Clean up on failure.
		_ = dockerExec(context.Background(), "rm", "-f", containerName)
		return nil, fmt.Errorf("docker start: %w", err)
	}

	sess := &dockerSession{
		id:            id,
		containerName: containerName,
		language:      opts.Language,
		limits:        opts.Limits,
		workDir:       workDir,
	}

	b.mu.Lock()
	b.sessions[id] = sess
	b.mu.Unlock()

	return sess, nil
}

// Close destroys all containers and cleans up.
func (b *DockerBackend) Close() error {
	b.mu.Lock()
	sessions := make([]*dockerSession, 0, len(b.sessions))
	for _, s := range b.sessions {
		sessions = append(sessions, s)
	}
	b.sessions = make(map[string]*dockerSession)
	b.mu.Unlock()

	ctx := context.Background()
	for _, s := range sessions {
		_ = s.Destroy(ctx)
	}
	return nil
}

type dockerSession struct {
	id            string
	containerName string
	language      Language
	limits        ResourceLimits
	workDir       string
}

func (s *dockerSession) ID() string { return s.id }

func (s *dockerSession) Execute(ctx context.Context, code string) (*ExecResult, error) {
	var cmd string
	switch s.language {
	case LangPython:
		cmd = fmt.Sprintf("python3 -c %s", shellQuote(code))
	case LangJavaScript:
		cmd = fmt.Sprintf("node -e %s", shellQuote(code))
	case LangBash:
		cmd = fmt.Sprintf("bash -c %s", shellQuote(code))
	case LangGo:
		return s.executeGoCode(ctx, code)
	default:
		return nil, fmt.Errorf("unsupported language: %s", s.language)
	}

	return s.dockerExecCmd(ctx, cmd)
}

func (s *dockerSession) executeGoCode(ctx context.Context, code string) (*ExecResult, error) {
	// Write code via stdin using docker exec, then run it.
	// Use the parent context directly — dockerExecCmd applies the timeout.
	writeCmd := exec.CommandContext(ctx, "docker", "exec", "-i", s.containerName, // #nosec G204 -- sandbox intentionally executes in containers
		"sh", "-c", "cat > /tmp/main.go")
	writeCmd.Stdin = strings.NewReader(code)
	if err := writeCmd.Run(); err != nil {
		return nil, fmt.Errorf("write go file: %w", err)
	}

	return s.dockerExecCmd(ctx, "cd /tmp && go run main.go")
}

func (s *dockerSession) dockerExecCmd(ctx context.Context, cmd string) (*ExecResult, error) {
	timeout := time.Duration(s.limits.WallClockSec) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execCmd := exec.CommandContext(ctx, "docker", "exec", s.containerName, "sh", "-c", cmd) // #nosec G204 -- sandbox intentionally executes in containers
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	start := time.Now()
	runErr := execCmd.Run()
	duration := time.Since(start)

	result := &ExecResult{
		Stdout:   truncateOutput(stdout.String(), s.limits.MaxOutputBytes),
		Stderr:   truncateOutput(stderr.String(), s.limits.MaxOutputBytes),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = 124
	} else if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			// Check for OOM kill via docker inspect.
			if oomKilled := s.checkOOMKilled(context.Background()); oomKilled {
				result.OOMKilled = true
			}
		} else {
			return nil, fmt.Errorf("docker exec: %w", runErr)
		}
	}

	return result, nil
}

func (s *dockerSession) RunCommand(ctx context.Context, command string) (*ExecResult, error) {
	return s.dockerExecCmd(ctx, command)
}

func (s *dockerSession) safePath(path string) (string, error) {
	return sandboxSafePath(s.workDir, path)
}

func (s *dockerSession) WriteFile(ctx context.Context, path string, content []byte) error {
	fullPath, err := s.safePath(path)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "docker", "exec", "-i", s.containerName, // #nosec G204 -- sandbox intentionally executes in containers
		"sh", "-c", fmt.Sprintf("mkdir -p %s && cat > %s",
			shellQuote(filepath.Dir(fullPath)), shellQuote(fullPath)))
	cmd.Stdin = bytes.NewReader(content)
	return cmd.Run()
}

func (s *dockerSession) ReadFile(ctx context.Context, path string) ([]byte, error) {
	fullPath, err := s.safePath(path)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "docker", "exec", s.containerName, "cat", fullPath) // #nosec G204 -- sandbox intentionally executes in containers
	return cmd.Output()
}

func (s *dockerSession) ListFiles(ctx context.Context, dir string) ([]SandboxFileInfo, error) {
	fullPath, err := s.safePath(dir)
	if err != nil {
		return nil, err
	}
	// Use ls -la which works on both Alpine (BusyBox) and Debian images.
	cmd := exec.CommandContext(ctx, "docker", "exec", s.containerName, // #nosec G204 -- sandbox intentionally executes in containers
		"sh", "-c", fmt.Sprintf("ls -1ap %s", shellQuote(fullPath)))
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []SandboxFileInfo
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name == "" || name == "./" || name == "../" {
			continue
		}
		isDir := strings.HasSuffix(name, "/")
		if isDir {
			name = strings.TrimSuffix(name, "/")
		}
		files = append(files, SandboxFileInfo{
			Path:  name,
			IsDir: isDir,
		})
	}
	return files, nil
}

func (s *dockerSession) Destroy(_ context.Context) error {
	return dockerExec(context.Background(), "rm", "-f", s.containerName)
}

func (s *dockerSession) checkOOMKilled(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "docker", "inspect", // #nosec G204 -- container name is internally generated
		"--format", "{{.State.OOMKilled}}", s.containerName)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// dockerExec runs a docker command and returns any error.
func dockerExec(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...) // #nosec G204 -- args are internally constructed
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// shellQuote quotes a string for safe use in shell commands.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

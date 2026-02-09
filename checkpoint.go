package claudeagent

import (
	"context"
	"fmt"
	"os/exec"
)

// CheckpointManager handles file checkpointing and rewind operations.
// Requires the CLI to have been run with --enable-file-checkpointing.
type CheckpointManager struct {
	sessionID string
	cliPath   string
	cwd       string
}

// NewCheckpointManager creates a new checkpoint manager.
func NewCheckpointManager(sessionID, cliPath, cwd string) *CheckpointManager {
	if cliPath == "" {
		cliPath = "claude"
	}
	return &CheckpointManager{
		sessionID: sessionID,
		cliPath:   cliPath,
		cwd:       cwd,
	}
}

// RewindFiles reverts file changes to the state at the given user message ID.
// This uses the Claude CLI's checkpoint rewind functionality.
func (cm *CheckpointManager) RewindFiles(ctx context.Context, userMessageID string) error {
	if cm.sessionID == "" {
		return fmt.Errorf("session ID required for file rewind")
	}

	cliPath := cm.cliPath
	if cliPath == "" {
		cliPath = "claude"
	}

	args := []string{
		"checkpoint", "rewind",
		"--session-id", cm.sessionID,
		"--message-id", userMessageID,
	}

	if cm.cwd != "" {
		args = append(args, "--cwd", cm.cwd)
	}

	cmd := exec.CommandContext(ctx, cliPath, args...) // #nosec G204 -- cliPath is intentionally configurable
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkpoint rewind failed: %w (output: %s)", err, string(output))
	}

	return nil
}

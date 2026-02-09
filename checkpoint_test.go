package claudeagent

import "testing"

func TestNewCheckpointManager(t *testing.T) {
	cm := NewCheckpointManager("sess-123", "/usr/bin/claude", "/tmp")

	if cm.sessionID != "sess-123" {
		t.Fatalf("unexpected session ID: %s", cm.sessionID)
	}
	if cm.cliPath != "/usr/bin/claude" {
		t.Fatalf("unexpected CLI path: %s", cm.cliPath)
	}
	if cm.cwd != "/tmp" {
		t.Fatalf("unexpected cwd: %s", cm.cwd)
	}
}

func TestNewCheckpointManagerDefaultCLIPath(t *testing.T) {
	cm := NewCheckpointManager("sess-123", "", "/tmp")

	if cm.cliPath != "claude" {
		t.Fatalf("expected default CLI path 'claude', got: %s", cm.cliPath)
	}
}

func TestCheckpointManagerRewindFilesRequiresSessionID(t *testing.T) {
	cm := NewCheckpointManager("", "", "")

	err := cm.RewindFiles(nil, "msg-123")
	if err == nil {
		t.Fatal("expected error when session ID is empty")
	}
	if err.Error() != "session ID required for file rewind" {
		t.Fatalf("unexpected error: %v", err)
	}
}

package server

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunPostUpdateHook_Success(t *testing.T) {
	tmpDir := t.TempDir()
	markerFilepath := filepath.Join(tmpDir, "hook-ran")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	hookCmd := "touch " + markerFilepath
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	// Verify the hook ran
	if _, err := os.Stat(markerFilepath); os.IsNotExist(err) {
		t.Error("expected hook to create marker file")
	}

	// Verify success log
	logOutput := buf.String()
	if !strings.Contains(logOutput, "succeeded") {
		t.Errorf("expected success log, got: %s", logOutput)
	}
}

func TestRunPostUpdateHook_Failure(t *testing.T) {
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	hookCmd := "exit 1"
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	// Verify failure log (should not panic or return error)
	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed") {
		t.Errorf("expected failure log, got: %s", logOutput)
	}
}

func TestRunPostUpdateHook_WorkingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	markerFilepath := filepath.Join(tmpDir, "pwd-output")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	// Write pwd to a file — should be tmpDir
	hookCmd := "pwd > " + markerFilepath
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	data, err := os.ReadFile(markerFilepath)
	if err != nil {
		t.Fatalf("failed to read pwd output: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != tmpDir {
		t.Errorf("expected working dir %s, got %s", tmpDir, got)
	}
}

func TestRunPostUpdateHook_Timeout(t *testing.T) {
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	// Use a short-lived context to simulate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hookCmd := "sleep 10"
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed") {
		t.Errorf("expected failure log for timeout, got: %s", logOutput)
	}
}

func TestAbbreviateSHA(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"abcdef1234567890abcdef1234567890abcdef12", "abcdef12"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"123456789", "12345678"},
		{"", ""},
	}

	for _, tt := range tests {
		got := abbreviateSHA(tt.input)
		if got != tt.expected {
			t.Errorf("abbreviateSHA(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

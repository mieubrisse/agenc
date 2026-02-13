package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsCommandOnlyEntry(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Command-only entries (should be filtered out)
		{input: "/login", want: true},
		{input: "/help", want: true},
		{input: "/commit", want: true},
		{input: "/software-engineer", want: true},

		// Real user prompts (should NOT be filtered out)
		{input: "/login to configure authentication", want: false},
		{input: "/help me debug this issue", want: false},
		{input: "please help with this bug", want: false},
		{input: "how do I fix the SESSION column bug", want: false},
		{input: "", want: false},
		{input: "no slash command here", want: false},

		// Edge cases
		{input: "/", want: false}, // Just a slash, no command name
		{input: "/this-is-a-very-long-command-name-that-exceeds-thirty-chars", want: false}, // Too long
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isCommandOnlyEntry(tt.input)
			if got != tt.want {
				t.Errorf("isCommandOnlyEntry(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindFirstPrompt(t *testing.T) {
	// Create a temporary history.jsonl file for testing
	tmpDir := t.TempDir()
	historyFilepath := filepath.Join(tmpDir, "history.jsonl")

	missionID := "test-mission-abc123"
	projectPath := "/tmp/test/" + missionID

	// Write test data that mimics real Claude Code history.jsonl format
	content := `{"display":"Initial user message about fixing a bug","project":"` + projectPath + `","sessionId":"session-1","timestamp":1000000}
{"display":"/login ","project":"` + projectPath + `","sessionId":"session-1","timestamp":1000100}
{"display":"Follow-up message about the bug fix","project":"` + projectPath + `","sessionId":"session-1","timestamp":1000200}
{"display":"Message for a different mission","project":"/tmp/other-mission","sessionId":"session-2","timestamp":1000300}
`

	if err := os.WriteFile(historyFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test history file: %v", err)
	}

	// Test that FindFirstPrompt returns the first real user message, not the command
	got := FindFirstPrompt(historyFilepath, missionID)
	want := "Initial user message about fixing a bug"

	if got != want {
		t.Errorf("FindFirstPrompt() = %q, want %q", got, want)
	}
}

func TestFindFirstPromptSkipsCommandOnly(t *testing.T) {
	// Test scenario where the first entry for a mission is a command-only entry
	tmpDir := t.TempDir()
	historyFilepath := filepath.Join(tmpDir, "history.jsonl")

	missionID := "test-mission-xyz789"
	projectPath := "/tmp/test/" + missionID

	// Write test data where command appears FIRST (the bug scenario)
	content := `{"display":"/login","project":"` + projectPath + `","sessionId":"session-1","timestamp":1000000}
{"display":"there's a bug in how we pick up the SESSION","project":"` + projectPath + `","sessionId":"session-1","timestamp":1000100}
`

	if err := os.WriteFile(historyFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test history file: %v", err)
	}

	// Test that FindFirstPrompt skips the command-only entry and returns the real message
	got := FindFirstPrompt(historyFilepath, missionID)
	want := "there's a bug in how we pick up the SESSION"

	if got != want {
		t.Errorf("FindFirstPrompt() = %q, want %q (should skip command-only entry)", got, want)
	}
}

func TestFindFirstPromptNoMatch(t *testing.T) {
	tmpDir := t.TempDir()
	historyFilepath := filepath.Join(tmpDir, "history.jsonl")

	content := `{"display":"Some message","project":"/tmp/other-mission","sessionId":"session-1","timestamp":1000000}
`

	if err := os.WriteFile(historyFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test history file: %v", err)
	}

	// Test that FindFirstPrompt returns empty string when no match is found
	got := FindFirstPrompt(historyFilepath, "nonexistent-mission-id")
	if got != "" {
		t.Errorf("FindFirstPrompt() = %q, want empty string for non-matching mission ID", got)
	}
}

func TestFindFirstPromptFileNotFound(t *testing.T) {
	// Test that FindFirstPrompt returns empty string when file doesn't exist
	got := FindFirstPrompt("/nonexistent/path/history.jsonl", "test-mission")
	if got != "" {
		t.Errorf("FindFirstPrompt() = %q, want empty string for non-existent file", got)
	}
}

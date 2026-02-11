package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeProjectDirname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "typical mission agent path",
			input:    "/Users/odyssey/.agenc/missions/888964eb-59c0-4f2d-9916-947543b04f36/agent",
			expected: "-Users-odyssey--agenc-missions-888964eb-59c0-4f2d-9916-947543b04f36-agent",
		},
		{
			name:     "path with no dots",
			input:    "/tmp/test/agent",
			expected: "-tmp-test-agent",
		},
		{
			name:     "path with multiple dots",
			input:    "/Users/foo/.config/.local/bar",
			expected: "-Users-foo--config--local-bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeProjectDirname(tt.input)
			if result != tt.expected {
				t.Errorf("EncodeProjectDirname(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFindLatestSessionID(t *testing.T) {
	dirpath := t.TempDir()

	// Write two JSONL files and set explicit modtimes to control which is "latest"
	writeTestFile(t, dirpath, "aaaa-1111.jsonl", `{"type":"user","sessionId":"aaaa-1111"}`)
	writeTestFile(t, dirpath, "bbbb-2222.jsonl", `{"type":"user","sessionId":"bbbb-2222"}`)

	// Set bbbb-2222 to be newer than aaaa-1111
	olderTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newerTime := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(dirpath, "aaaa-1111.jsonl"), olderTime, olderTime)
	os.Chtimes(filepath.Join(dirpath, "bbbb-2222.jsonl"), newerTime, newerTime)

	// Also write a non-JSONL file that should be ignored
	writeTestFile(t, dirpath, "not-a-session.json", `{}`)

	sessionID, err := FindLatestSessionID(dirpath)
	if err != nil {
		t.Fatalf("FindLatestSessionID returned error: %v", err)
	}
	if sessionID != "bbbb-2222" {
		t.Errorf("FindLatestSessionID = %q, want %q", sessionID, "bbbb-2222")
	}
}

func TestFindLatestSessionIDEmptyDir(t *testing.T) {
	dirpath := t.TempDir()

	_, err := FindLatestSessionID(dirpath)
	if err == nil {
		t.Fatal("expected error for empty directory, got nil")
	}
}

func writeTestFile(t *testing.T, dirpath string, filename string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dirpath, filename), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file %s: %v", filename, err)
	}
}

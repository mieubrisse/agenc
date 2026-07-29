package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFindActiveJSONLPath verifies that FindActiveJSONLPath returns the most
// recently modified .jsonl file in the given project directory.
func TestFindActiveJSONLPath(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	t.Run("empty directory returns empty string", func(t *testing.T) {
		result := FindActiveJSONLPath(tmpDir)
		if result != "" {
			t.Errorf("FindActiveJSONLPath() = %q, want empty string", result)
		}
	})

	t.Run("nonexistent directory returns empty string", func(t *testing.T) {
		result := FindActiveJSONLPath(filepath.Join(tmpDir, "nonexistent"))
		if result != "" {
			t.Errorf("FindActiveJSONLPath() = %q, want empty string", result)
		}
	})

	t.Run("returns most recently modified jsonl", func(t *testing.T) {
		projectDir, err := os.MkdirTemp(claudeTmpDir, "project-")
		if err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}

		// Create two JSONL files with different modification times
		olderFile := filepath.Join(projectDir, "older.jsonl")
		newerFile := filepath.Join(projectDir, "newer.jsonl")

		if err := os.WriteFile(olderFile, []byte(`{"type":"user"}`), 0644); err != nil {
			t.Fatalf("failed to write older file: %v", err)
		}
		// Ensure different modification times
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(newerFile, []byte(`{"type":"user"}`), 0644); err != nil {
			t.Fatalf("failed to write newer file: %v", err)
		}

		result := FindActiveJSONLPath(projectDir)
		if result != newerFile {
			t.Errorf("FindActiveJSONLPath() = %q, want %q", result, newerFile)
		}
	})

	t.Run("ignores non-jsonl files", func(t *testing.T) {
		projectDir, err := os.MkdirTemp(claudeTmpDir, "project-")
		if err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}

		// Create a non-JSONL file
		if err := os.WriteFile(filepath.Join(projectDir, "sessions-index.json"), []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to write non-jsonl file: %v", err)
		}

		result := FindActiveJSONLPath(projectDir)
		if result != "" {
			t.Errorf("FindActiveJSONLPath() = %q, want empty string for non-jsonl files", result)
		}
	})
}

// TestListSessionIDs verifies that ListSessionIDs returns session IDs from
// .jsonl files containing conversation data, sorted by modification time.
func TestListSessionIDs(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	t.Run("empty directory returns empty", func(t *testing.T) {
		result := ListSessionIDs(tmpDir)
		if len(result) != 0 {
			t.Errorf("ListSessionIDs() = %v, want empty", result)
		}
	})

	t.Run("nonexistent directory returns nil", func(t *testing.T) {
		result := ListSessionIDs(filepath.Join(tmpDir, "nonexistent"))
		if result != nil {
			t.Errorf("ListSessionIDs() = %v, want nil", result)
		}
	})

	t.Run("returns session IDs sorted by modification time (newest first)", func(t *testing.T) {
		projectDir, err := os.MkdirTemp(claudeTmpDir, "project-")
		if err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}

		// Conversation content so hasConversationData returns true
		conversationContent := `{"type":"user","message":"hello"}
{"type":"assistant","message":"hi"}
`

		olderID := "session-older-id"
		newerID := "session-newer-id"

		if err := os.WriteFile(filepath.Join(projectDir, olderID+".jsonl"), []byte(conversationContent), 0644); err != nil {
			t.Fatalf("failed to write older session: %v", err)
		}
		// Ensure different modification times
		time.Sleep(10 * time.Millisecond)
		if err := os.WriteFile(filepath.Join(projectDir, newerID+".jsonl"), []byte(conversationContent), 0644); err != nil {
			t.Fatalf("failed to write newer session: %v", err)
		}

		result := ListSessionIDs(projectDir)
		if len(result) != 2 {
			t.Fatalf("ListSessionIDs() returned %d sessions, want 2", len(result))
		}
		if result[0] != newerID {
			t.Errorf("ListSessionIDs()[0] = %q, want %q (newest first)", result[0], newerID)
		}
		if result[1] != olderID {
			t.Errorf("ListSessionIDs()[1] = %q, want %q", result[1], olderID)
		}
	})

	t.Run("skips sessions without conversation data", func(t *testing.T) {
		projectDir, err := os.MkdirTemp(claudeTmpDir, "project-")
		if err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}

		// Only metadata — no user/assistant records
		metadataOnly := `{"type":"summary","summary":"test"}
`
		conversationContent := `{"type":"user","message":"hello"}
`

		if err := os.WriteFile(filepath.Join(projectDir, "metadata-only.jsonl"), []byte(metadataOnly), 0644); err != nil {
			t.Fatalf("failed to write metadata-only session: %v", err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, "with-conversation.jsonl"), []byte(conversationContent), 0644); err != nil {
			t.Fatalf("failed to write conversation session: %v", err)
		}

		result := ListSessionIDs(projectDir)
		if len(result) != 1 {
			t.Fatalf("ListSessionIDs() returned %d sessions, want 1", len(result))
		}
		if result[0] != "with-conversation" {
			t.Errorf("ListSessionIDs()[0] = %q, want %q", result[0], "with-conversation")
		}
	})

	t.Run("ignores non-jsonl files", func(t *testing.T) {
		projectDir, err := os.MkdirTemp(claudeTmpDir, "project-")
		if err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}

		if err := os.WriteFile(filepath.Join(projectDir, "sessions-index.json"), []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to write non-jsonl file: %v", err)
		}

		result := ListSessionIDs(projectDir)
		if len(result) != 0 {
			t.Errorf("ListSessionIDs() = %v, want empty for non-jsonl files", result)
		}
	})
}

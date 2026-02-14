package session

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestExtractUserMessages verifies that user messages are correctly extracted
// from JSONL session files.
func TestExtractUserMessages(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}

	tests := []struct {
		name         string
		jsonlContent string
		maxMessages  int
		want         []string
	}{
		{
			name: "single user message",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Hello, Claude!"}}
`,
			maxMessages: 10,
			want:        []string{"Hello, Claude!"},
		},
		{
			name: "multiple user messages with assistant messages",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"First question"}}
{"type":"assistant","message":"Assistant response"}
{"type":"user","message":{"role":"user","content":"Second question"}}
{"type":"assistant","message":"Another response"}
{"type":"user","message":{"role":"user","content":"Third question"}}
`,
			maxMessages: 10,
			want:        []string{"First question", "Second question", "Third question"},
		},
		{
			name: "only non-user entries",
			jsonlContent: `{"type":"assistant","message":"Assistant response"}
{"type":"summary","summary":"Session summary"}
{"type":"custom-title","customTitle":"My Title"}
`,
			maxMessages: 10,
			want:        nil,
		},
		{
			name:         "empty file",
			jsonlContent: "",
			maxMessages:  10,
			want:         nil,
		},
		{
			name: "malformed JSON lines are skipped",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Valid message 1"}}
{malformed json line
{"type":"user","message":{"role":"user","content":"Valid message 2"}}
invalid line without braces
{"type":"user","message":{"role":"user","content":"Valid message 3"}}
`,
			maxMessages: 10,
			want:        []string{"Valid message 1", "Valid message 2", "Valid message 3"},
		},
		{
			name: "user entry with empty content",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Message with content"}}
{"type":"user","message":{"role":"user","content":""}}
{"type":"user","message":{"role":"user","content":"Another message"}}
`,
			maxMessages: 10,
			want:        []string{"Message with content", "Another message"},
		},
		{
			name: "user entry with malformed message field",
			jsonlContent: `{"type":"user","message":{"role":"user","content":"Valid message"}}
{"type":"user","message":"not a json object"}
{"type":"user","message":{"role":"user","content":"Another valid message"}}
`,
			maxMessages: 10,
			want:        []string{"Valid message", "Another valid message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp(claudeTmpDir, "conversation-test-")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			jsonlFilepath := filepath.Join(tmpDir, "session.jsonl")
			if tt.jsonlContent != "" {
				if err := os.WriteFile(jsonlFilepath, []byte(tt.jsonlContent), 0644); err != nil {
					t.Fatalf("failed to write JSONL file: %v", err)
				}
			}

			got := extractUserMessagesFromJSONL(jsonlFilepath, tt.maxMessages)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractUserMessagesFromJSONL() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMessageTruncation verifies that only the last maxMessages are returned
// when there are more messages than the limit.
func TestMessageTruncation(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "conversation-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name         string
		messageCount int
		maxMessages  int
		wantCount    int
		wantFirst    string
		wantLast     string
	}{
		{
			name:         "exactly at limit",
			messageCount: 15,
			maxMessages:  15,
			wantCount:    15,
			wantFirst:    "Message 1",
			wantLast:     "Message 15",
		},
		{
			name:         "fewer than limit",
			messageCount: 10,
			maxMessages:  15,
			wantCount:    10,
			wantFirst:    "Message 1",
			wantLast:     "Message 10",
		},
		{
			name:         "more than limit - truncate to last N",
			messageCount: 20,
			maxMessages:  15,
			wantCount:    15,
			wantFirst:    "Message 6",
			wantLast:     "Message 20",
		},
		{
			name:         "many messages with small limit",
			messageCount: 100,
			maxMessages:  5,
			wantCount:    5,
			wantFirst:    "Message 96",
			wantLast:     "Message 100",
		},
		{
			name:         "max messages is 1",
			messageCount: 10,
			maxMessages:  1,
			wantCount:    1,
			wantFirst:    "Message 10",
			wantLast:     "Message 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

			// Build JSONL content with the specified number of messages
			var jsonlContent string
			for i := 1; i <= tt.messageCount; i++ {
				jsonlContent += fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"Message %d"}}
`, i)
			}

			if err := os.WriteFile(jsonlFilepath, []byte(jsonlContent), 0644); err != nil {
				t.Fatalf("failed to write JSONL file: %v", err)
			}

			got := extractUserMessagesFromJSONL(jsonlFilepath, tt.maxMessages)
			if len(got) != tt.wantCount {
				t.Errorf("extractUserMessagesFromJSONL() returned %d messages, want %d", len(got), tt.wantCount)
			}
			if len(got) > 0 {
				if got[0] != tt.wantFirst {
					t.Errorf("first message = %q, want %q", got[0], tt.wantFirst)
				}
				if got[len(got)-1] != tt.wantLast {
					t.Errorf("last message = %q, want %q", got[len(got)-1], tt.wantLast)
				}
			}
		})
	}
}

// TestEdgeCases tests edge cases including empty JSONL, corrupted entries,
// and missing files.
func TestEdgeCases(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "conversation-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name        string
		setupFunc   func(string) (string, error)
		maxMessages int
		want        []string
	}{
		{
			name: "nonexistent file",
			setupFunc: func(tmpDir string) (string, error) {
				return filepath.Join(tmpDir, "nonexistent.jsonl"), nil
			},
			maxMessages: 10,
			want:        nil,
		},
		{
			name: "empty file",
			setupFunc: func(tmpDir string) (string, error) {
				jsonlFilepath := filepath.Join(tmpDir, "empty.jsonl")
				return jsonlFilepath, os.WriteFile(jsonlFilepath, []byte(""), 0644)
			},
			maxMessages: 10,
			want:        nil,
		},
		{
			name: "file with only whitespace",
			setupFunc: func(tmpDir string) (string, error) {
				jsonlFilepath := filepath.Join(tmpDir, "whitespace.jsonl")
				return jsonlFilepath, os.WriteFile(jsonlFilepath, []byte("   \n  \n  "), 0644)
			},
			maxMessages: 10,
			want:        nil,
		},
		{
			name: "file with only corrupted entries",
			setupFunc: func(tmpDir string) (string, error) {
				jsonlFilepath := filepath.Join(tmpDir, "corrupted.jsonl")
				content := `{not valid json
[array instead of object]
totally broken
`
				return jsonlFilepath, os.WriteFile(jsonlFilepath, []byte(content), 0644)
			},
			maxMessages: 10,
			want:        nil,
		},
		{
			name: "mixed valid and invalid entries",
			setupFunc: func(tmpDir string) (string, error) {
				jsonlFilepath := filepath.Join(tmpDir, "mixed.jsonl")
				content := `{broken json
{"type":"user","message":{"role":"user","content":"Valid message 1"}}
[invalid array]
{"type":"user","message":{"role":"user","content":"Valid message 2"}}
totally broken line
{"type":"user","message":{"role":"user","content":"Valid message 3"}}
`
				return jsonlFilepath, os.WriteFile(jsonlFilepath, []byte(content), 0644)
			},
			maxMessages: 10,
			want:        []string{"Valid message 1", "Valid message 2", "Valid message 3"},
		},
		{
			name: "entry with type field but no user type",
			setupFunc: func(tmpDir string) (string, error) {
				jsonlFilepath := filepath.Join(tmpDir, "no-user-type.jsonl")
				content := `{"type":"assistant","message":"Assistant message"}
{"type":"summary","summary":"Summary text"}
{"type":"metadata","data":"some data"}
`
				return jsonlFilepath, os.WriteFile(jsonlFilepath, []byte(content), 0644)
			},
			maxMessages: 10,
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonlFilepath, err := tt.setupFunc(tmpDir)
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got := extractUserMessagesFromJSONL(jsonlFilepath, tt.maxMessages)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractUserMessagesFromJSONL() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestExtractRecentUserMessages tests the full integration with project
// directory resolution and JSONL file discovery.
func TestExtractRecentUserMessages(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "conversation-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	missionID := "test-mission-integration"
	projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
	if err := os.MkdirAll(projectDirpath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	tests := []struct {
		name        string
		setupFunc   func(string) error
		maxMessages int
		want        []string
	}{
		{
			name: "single JSONL file with messages",
			setupFunc: func(projectDirpath string) error {
				jsonlFilepath := filepath.Join(projectDirpath, "session.jsonl")
				content := `{"type":"user","message":{"role":"user","content":"First message"}}
{"type":"user","message":{"role":"user","content":"Second message"}}
`
				return os.WriteFile(jsonlFilepath, []byte(content), 0644)
			},
			maxMessages: 10,
			want:        []string{"First message", "Second message"},
		},
		{
			name: "no project directory",
			setupFunc: func(projectDirpath string) error {
				// Remove the project directory
				return os.RemoveAll(projectDirpath)
			},
			maxMessages: 10,
			want:        nil,
		},
		{
			name: "project directory with no JSONL files",
			setupFunc: func(projectDirpath string) error {
				// Directory exists but has no JSONL files
				return nil
			},
			maxMessages: 10,
			want:        nil,
		},
		{
			name: "multiple JSONL files - most recent is used",
			setupFunc: func(projectDirpath string) error {
				// Create older file
				oldFilepath := filepath.Join(projectDirpath, "old-session.jsonl")
				oldContent := `{"type":"user","message":{"role":"user","content":"Old message"}}`
				if err := os.WriteFile(oldFilepath, []byte(oldContent), 0644); err != nil {
					return err
				}

				// Wait a bit to ensure different modification times
				// (Not perfect, but should work in most cases)
				// Create newer file
				newFilepath := filepath.Join(projectDirpath, "new-session.jsonl")
				newContent := `{"type":"user","message":{"role":"user","content":"New message"}}`
				return os.WriteFile(newFilepath, []byte(newContent), 0644)
			},
			maxMessages: 10,
			want:        []string{"New message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean and recreate project directory for each test
			os.RemoveAll(projectDirpath)
			if err := os.MkdirAll(projectDirpath, 0755); err != nil {
				t.Fatalf("failed to create project dir: %v", err)
			}

			if err := tt.setupFunc(projectDirpath); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			got := ExtractRecentUserMessages(tmpDir, missionID, tt.maxMessages)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractRecentUserMessages() = %v, want %v", got, tt.want)
			}
		})
	}
}

package session

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCustomTitlePriority verifies that custom title set via /rename
// overrides auto-generated summaries from both sessions-index.json
// and JSONL summary entries.
func TestCustomTitlePriority(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	missionID := "test-mission-123"
	projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
	if err := os.MkdirAll(projectDirpath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	// Create a JSONL file with both summary and custom-title entries
	jsonlFilepath := filepath.Join(projectDirpath, "session.jsonl")
	jsonlContent := `{"type":"summary","summary":"Auto-generated summary from JSONL"}
{"type":"custom-title","customTitle":"My Custom Title"}
`
	if err := os.WriteFile(jsonlFilepath, []byte(jsonlContent), 0644); err != nil {
		t.Fatalf("failed to write JSONL file: %v", err)
	}

	// Create sessions-index.json with a summary
	indexFilepath := filepath.Join(projectDirpath, "sessions-index.json")
	indexContent := `{"entries":[{"sessionId":"session-1","summary":"Summary from index","modified":"2026-02-14T10:00:00Z"}]}`
	if err := os.WriteFile(indexFilepath, []byte(indexContent), 0644); err != nil {
		t.Fatalf("failed to write sessions-index.json: %v", err)
	}

	// FindSessionName should return the custom title, not the summaries
	result := FindSessionName(tmpDir, missionID)
	expected := "My Custom Title"
	if result != expected {
		t.Errorf("FindSessionName() = %q, want %q", result, expected)
	}
}

// TestSummaryFallback verifies the fallback priority when no custom title
// is present: sessions-index.json summary is preferred over JSONL summary.
func TestSummaryFallback(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	missionID := "test-mission-456"
	projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
	if err := os.MkdirAll(projectDirpath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	tests := []struct {
		name           string
		hasIndexFile   bool
		hasJSONLSummary bool
		indexSummary   string
		jsonlSummary   string
		want           string
	}{
		{
			name:           "index summary preferred over JSONL summary",
			hasIndexFile:   true,
			hasJSONLSummary: true,
			indexSummary:   "Summary from index",
			jsonlSummary:   "Summary from JSONL",
			want:           "Summary from index",
		},
		{
			name:           "JSONL summary used when index missing",
			hasIndexFile:   false,
			hasJSONLSummary: true,
			jsonlSummary:   "Summary from JSONL",
			want:           "Summary from JSONL",
		},
		{
			name:           "empty when no summaries exist",
			hasIndexFile:   false,
			hasJSONLSummary: false,
			want:           "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up project directory before each subtest
			os.RemoveAll(projectDirpath)
			if err := os.MkdirAll(projectDirpath, 0755); err != nil {
				t.Fatalf("failed to create project dir: %v", err)
			}

			if tt.hasJSONLSummary {
				jsonlFilepath := filepath.Join(projectDirpath, "session.jsonl")
				jsonlContent := `{"type":"summary","summary":"` + tt.jsonlSummary + `"}
`
				if err := os.WriteFile(jsonlFilepath, []byte(jsonlContent), 0644); err != nil {
					t.Fatalf("failed to write JSONL file: %v", err)
				}
			}

			if tt.hasIndexFile {
				indexFilepath := filepath.Join(projectDirpath, "sessions-index.json")
				indexContent := `{"entries":[{"sessionId":"session-1","summary":"` + tt.indexSummary + `","modified":"2026-02-14T10:00:00Z"}]}`
				if err := os.WriteFile(indexFilepath, []byte(indexContent), 0644); err != nil {
					t.Fatalf("failed to write sessions-index.json: %v", err)
				}
			}

			result := FindSessionName(tmpDir, missionID)
			if result != tt.want {
				t.Errorf("FindSessionName() = %q, want %q", result, tt.want)
			}
		})
	}
}

// TestJSONLParsing verifies handling of malformed JSONL files, missing files,
// and edge cases in parsing.
func TestJSONLParsing(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	missionID := "test-mission-789"
	projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
	if err := os.MkdirAll(projectDirpath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	tests := []struct {
		name        string
		jsonlContent string
		wantTitle   string
		wantSummary string
	}{
		{
			name:        "malformed JSON lines are skipped",
			jsonlContent: `{"type":"summary","summary":"Valid summary"}
{malformed json line
{"type":"custom-title","customTitle":"Valid title"}
`,
			wantTitle:   "Valid title",
			wantSummary: "Valid summary",
		},
		{
			name:        "empty file returns empty strings",
			jsonlContent: "",
			wantTitle:   "",
			wantSummary: "",
		},
		{
			name:        "lines without type field are skipped",
			jsonlContent: `{"something":"else"}
{"type":"summary","summary":"Good summary"}
`,
			wantTitle:   "",
			wantSummary: "Good summary",
		},
		{
			name:        "multiple summary entries - last one wins",
			jsonlContent: `{"type":"summary","summary":"First summary"}
{"type":"summary","summary":"Second summary"}
{"type":"summary","summary":"Last summary"}
`,
			wantTitle:   "",
			wantSummary: "Last summary",
		},
		{
			name:        "multiple custom-title entries - last one wins",
			jsonlContent: `{"type":"custom-title","customTitle":"First title"}
{"type":"custom-title","customTitle":"Second title"}
{"type":"custom-title","customTitle":"Last title"}
`,
			wantTitle:   "Last title",
			wantSummary: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonlFilepath := filepath.Join(projectDirpath, "test-session.jsonl")

			// Remove any existing file
			os.Remove(jsonlFilepath)

			if tt.jsonlContent != "" {
				if err := os.WriteFile(jsonlFilepath, []byte(tt.jsonlContent), 0644); err != nil {
					t.Fatalf("failed to write JSONL file: %v", err)
				}
			}

			gotTitle, gotSummary := findNamesInJSONL(jsonlFilepath)
			if gotTitle != tt.wantTitle {
				t.Errorf("findNamesInJSONL() title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotSummary != tt.wantSummary {
				t.Errorf("findNamesInJSONL() summary = %q, want %q", gotSummary, tt.wantSummary)
			}
		})
	}
}

// TestEmptySession verifies handling of sessions with no messages or
// metadata entries.
func TestEmptySession(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name            string
		setupFunc       func(string, string) error
		missionID       string
		wantSessionName string
	}{
		{
			name:      "no project directory",
			missionID: "nonexistent-mission",
			setupFunc: func(tmpDir, missionID string) error {
				// Don't create any project directory
				return nil
			},
			wantSessionName: "",
		},
		{
			name:      "empty project directory",
			missionID: "empty-mission",
			setupFunc: func(tmpDir, missionID string) error {
				projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
				return os.MkdirAll(projectDirpath, 0755)
			},
			wantSessionName: "",
		},
		{
			name:      "JSONL exists but has no metadata entries",
			missionID: "no-metadata-mission",
			setupFunc: func(tmpDir, missionID string) error {
				projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
				if err := os.MkdirAll(projectDirpath, 0755); err != nil {
					return err
				}
				jsonlFilepath := filepath.Join(projectDirpath, "session.jsonl")
				// JSONL with only non-metadata entries
				content := `{"type":"user","message":"some user message"}
{"type":"assistant","message":"some assistant message"}
`
				return os.WriteFile(jsonlFilepath, []byte(content), 0644)
			},
			wantSessionName: "",
		},
		{
			name:      "sessions-index.json exists but is empty",
			missionID: "empty-index-mission",
			setupFunc: func(tmpDir, missionID string) error {
				projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
				if err := os.MkdirAll(projectDirpath, 0755); err != nil {
					return err
				}
				indexFilepath := filepath.Join(projectDirpath, "sessions-index.json")
				content := `{"entries":[]}`
				return os.WriteFile(indexFilepath, []byte(content), 0644)
			},
			wantSessionName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.setupFunc(tmpDir, tt.missionID); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			result := FindSessionName(tmpDir, tt.missionID)
			if result != tt.wantSessionName {
				t.Errorf("FindSessionName() = %q, want %q", result, tt.wantSessionName)
			}
		})
	}
}

// TestFindCustomTitle verifies that FindCustomTitle returns only the custom
// title and ignores summaries.
func TestFindCustomTitle(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	missionID := "custom-title-mission"
	projectDirpath := filepath.Join(tmpDir, "projects", "project-"+missionID)
	if err := os.MkdirAll(projectDirpath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	tests := []struct {
		name         string
		jsonlContent string
		want         string
	}{
		{
			name: "custom title present",
			jsonlContent: `{"type":"summary","summary":"Some summary"}
{"type":"custom-title","customTitle":"Custom Title"}
`,
			want: "Custom Title",
		},
		{
			name: "no custom title",
			jsonlContent: `{"type":"summary","summary":"Some summary"}
`,
			want: "",
		},
		{
			name:         "no JSONL file",
			jsonlContent: "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonlFilepath := filepath.Join(projectDirpath, "session.jsonl")
			os.Remove(jsonlFilepath)

			if tt.jsonlContent != "" {
				if err := os.WriteFile(jsonlFilepath, []byte(tt.jsonlContent), 0644); err != nil {
					t.Fatalf("failed to write JSONL file: %v", err)
				}
			}

			result := FindCustomTitle(tmpDir, missionID)
			if result != tt.want {
				t.Errorf("FindCustomTitle() = %q, want %q", result, tt.want)
			}
		})
	}
}

// TestSessionsIndexLatestEntry verifies that sessions-index.json correctly
// identifies the entry with the latest modified timestamp.
func TestSessionsIndexLatestEntry(t *testing.T) {
	claudeTmpDir := "/tmp/claude"
	if err := os.MkdirAll(claudeTmpDir, 0755); err != nil {
		t.Fatalf("failed to create /tmp/claude: %v", err)
	}
	tmpDir, err := os.MkdirTemp(claudeTmpDir, "session-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	projectDirpath := filepath.Join(tmpDir, "test-project")
	if err := os.MkdirAll(projectDirpath, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	tests := []struct {
		name         string
		indexContent string
		want         string
	}{
		{
			name: "single entry",
			indexContent: `{"entries":[
				{"sessionId":"s1","summary":"First summary","modified":"2026-02-14T10:00:00Z"}
			]}`,
			want: "First summary",
		},
		{
			name: "multiple entries - latest wins",
			indexContent: `{"entries":[
				{"sessionId":"s1","summary":"Old summary","modified":"2026-02-14T10:00:00Z"},
				{"sessionId":"s2","summary":"Middle summary","modified":"2026-02-14T11:00:00Z"},
				{"sessionId":"s3","summary":"Latest summary","modified":"2026-02-14T12:00:00Z"}
			]}`,
			want: "Latest summary",
		},
		{
			name: "entries not in chronological order",
			indexContent: `{"entries":[
				{"sessionId":"s1","summary":"Middle summary","modified":"2026-02-14T11:00:00Z"},
				{"sessionId":"s2","summary":"Latest summary","modified":"2026-02-14T12:00:00Z"},
				{"sessionId":"s3","summary":"Old summary","modified":"2026-02-14T10:00:00Z"}
			]}`,
			want: "Latest summary",
		},
		{
			name:         "malformed JSON",
			indexContent: `{malformed json`,
			want:         "",
		},
		{
			name:         "empty entries array",
			indexContent: `{"entries":[]}`,
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexFilepath := filepath.Join(projectDirpath, "sessions-index.json")
			if err := os.WriteFile(indexFilepath, []byte(tt.indexContent), 0644); err != nil {
				t.Fatalf("failed to write sessions-index.json: %v", err)
			}

			result := findSummaryFromIndex(projectDirpath)
			if result != tt.want {
				t.Errorf("findSummaryFromIndex() = %q, want %q", result, tt.want)
			}
		})
	}
}

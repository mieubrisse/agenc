package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/database"
)

func TestExtractSessionAndMissionID(t *testing.T) {
	agencDirpath := "/home/user/.agenc"

	tests := []struct {
		name          string
		jsonlFilepath string
		wantSession   string
		wantMission   string
		wantOK        bool
	}{
		{
			name:          "standard path",
			jsonlFilepath: "/home/user/.agenc/missions/abc-123/claude-config/projects/encoded-path/session-uuid.jsonl",
			wantSession:   "session-uuid",
			wantMission:   "abc-123",
			wantOK:        true,
		},
		{
			name:          "too few path components",
			jsonlFilepath: "/home/user/.agenc/missions/abc-123/some.jsonl",
			wantSession:   "",
			wantMission:   "",
			wantOK:        false,
		},
		{
			name:          "path adjacent to missions dir has too few components",
			jsonlFilepath: "/home/user/.agenc/other/session.jsonl",
			wantSession:   "",
			wantMission:   "",
			wantOK:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionID, missionID, ok := extractSessionAndMissionID(agencDirpath, tt.jsonlFilepath)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if sessionID != tt.wantSession {
				t.Errorf("sessionID = %q, want %q", sessionID, tt.wantSession)
			}
			if missionID != tt.wantMission {
				t.Errorf("missionID = %q, want %q", missionID, tt.wantMission)
			}
		})
	}
}

func TestScanJSONLFromOffset(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n" +
		"{\"type\":\"summary\",\"summary\":\"Working on auth system\"}\n" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"I will help with that\"}\n" +
		"{\"type\":\"custom-title\",\"customTitle\":\"Auth Feature\"}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, 0)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "Auth Feature" {
		t.Errorf("customTitle = %q, want %q", customTitle, "Auth Feature")
	}
	if autoSummary != "Working on auth system" {
		t.Errorf("autoSummary = %q, want %q", autoSummary, "Working on auth system")
	}
}

func TestScanJSONLFromOffset_IncrementalScan(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	initialContent := "{\"type\":\"summary\",\"summary\":\"Initial summary\"}\n" +
		"{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	initialSize := int64(len(initialContent))

	appendedContent := "{\"type\":\"custom-title\",\"customTitle\":\"New Title\"}\n"
	f, err := os.OpenFile(jsonlFilepath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	if _, err := f.WriteString(appendedContent); err != nil {
		f.Close()
		t.Fatalf("failed to append: %v", err)
	}
	f.Close()

	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, initialSize)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "New Title" {
		t.Errorf("customTitle = %q, want %q", customTitle, "New Title")
	}
	if autoSummary != "" {
		t.Errorf("autoSummary = %q, want empty (initial summary is before offset)", autoSummary)
	}
}

func TestScanJSONLFromOffset_NoMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"hi there\"}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, 0)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "" {
		t.Errorf("customTitle = %q, want empty", customTitle)
	}
	if autoSummary != "" {
		t.Errorf("autoSummary = %q, want empty", autoSummary)
	}
}

func TestScanJSONLFromOffset_LastValueWins(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"custom-title\",\"customTitle\":\"First Title\"}\n" +
		"{\"type\":\"summary\",\"summary\":\"First summary\"}\n" +
		"{\"type\":\"custom-title\",\"customTitle\":\"Second Title\"}\n" +
		"{\"type\":\"summary\",\"summary\":\"Second summary\"}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, 0)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "Second Title" {
		t.Errorf("customTitle = %q, want %q", customTitle, "Second Title")
	}
	if autoSummary != "Second summary" {
		t.Errorf("autoSummary = %q, want %q", autoSummary, "Second summary")
	}
}

func TestScanJSONLFromOffset_OversizedLines(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	// Build a file with: small metadata, then a huge line (>10KB), then more metadata.
	// The scanner must read past the huge line without aborting.
	hugeLine := `{"type":"message","role":"assistant","content":"` + strings.Repeat("x", 20*1024) + "\"}\n"

	content := `{"type":"summary","summary":"Before huge line"}` + "\n" +
		hugeLine +
		`{"type":"custom-title","customTitle":"After huge line"}` + "\n"

	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, 0)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "After huge line" {
		t.Errorf("customTitle = %q, want %q (metadata after oversized line must be found)", customTitle, "After huge line")
	}
	if autoSummary != "Before huge line" {
		t.Errorf("autoSummary = %q, want %q", autoSummary, "Before huge line")
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		title  string
		maxLen int
		want   string
	}{
		{"short", 30, "short"},
		{"this is a very long title that exceeds the maximum length", 30, "this is a very long title tha\u2026"},
		{"  lots   of    whitespace  ", 30, "lots of whitespace"},
	}

	for _, tt := range tests {
		got := truncateTitle(tt.title, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.title, tt.maxLen, got, tt.want)
		}
	}
}

func TestExtractRepoShortName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"owner/repo", "repo"},
		{"github.com/owner/repo", "repo"},
		{"repo", "repo"},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractRepoShortName(tt.input)
		if got != tt.want {
			t.Errorf("extractRepoShortName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetermineBestTitle(t *testing.T) {
	mission := &database.Mission{
		ShortID: "abc12345",
		GitRepo: "github.com/owner/my-project",
	}

	// With custom title -- highest priority
	sessionWithCustom := &database.Session{
		CustomTitle: "My Custom Name",
		AutoSummary: "Auto generated summary",
	}
	if got := determineBestTitle(sessionWithCustom, mission); got != "My Custom Name" {
		t.Errorf("with custom title: got %q, want %q", got, "My Custom Name")
	}

	// Without custom title, with auto summary
	sessionWithSummary := &database.Session{
		AutoSummary: "Auto generated summary",
	}
	if got := determineBestTitle(sessionWithSummary, mission); got != "Auto generated summary" {
		t.Errorf("with auto summary: got %q, want %q", got, "Auto generated summary")
	}

	// No session -- falls back to repo name
	if got := determineBestTitle(nil, mission); got != "my-project" {
		t.Errorf("no session: got %q, want %q", got, "my-project")
	}

	// No session, no repo -- falls back to short ID
	missionNoRepo := &database.Mission{ShortID: "abc12345"}
	if got := determineBestTitle(nil, missionNoRepo); got != "abc12345" {
		t.Errorf("no session, no repo: got %q, want %q", got, "abc12345")
	}
}

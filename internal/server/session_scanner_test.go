package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/database"
)

func TestScanJSONLFromOffset(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"hello world\"}}\n" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"I will help with that\"}\n" +
		"{\"type\":\"custom-title\",\"customTitle\":\"Auth Feature\"}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := scanJSONLFromOffset(jsonlFilepath, 0, true)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if result.customTitle != "Auth Feature" {
		t.Errorf("customTitle = %q, want %q", result.customTitle, "Auth Feature")
	}
	if result.firstUserMessage != "hello world" {
		t.Errorf("firstUserMessage = %q, want %q", result.firstUserMessage, "hello world")
	}
}

func TestScanJSONLFromOffset_IncrementalScan(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	initialContent := "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"first message\"}}\n" +
		"{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"hello\"}\n"
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

	result, err := scanJSONLFromOffset(jsonlFilepath, initialSize, true)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if result.customTitle != "New Title" {
		t.Errorf("customTitle = %q, want %q", result.customTitle, "New Title")
	}
	// User message is before offset, so should not be found
	if result.firstUserMessage != "" {
		t.Errorf("firstUserMessage = %q, want empty (user message is before offset)", result.firstUserMessage)
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

	result, err := scanJSONLFromOffset(jsonlFilepath, 0, false)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if result.customTitle != "" {
		t.Errorf("customTitle = %q, want empty", result.customTitle)
	}
	if result.firstUserMessage != "" {
		t.Errorf("firstUserMessage = %q, want empty (extractUserMessage=false)", result.firstUserMessage)
	}
}

func TestScanJSONLFromOffset_LastCustomTitleWins(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"custom-title\",\"customTitle\":\"First Title\"}\n" +
		"{\"type\":\"custom-title\",\"customTitle\":\"Second Title\"}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := scanJSONLFromOffset(jsonlFilepath, 0, false)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if result.customTitle != "Second Title" {
		t.Errorf("customTitle = %q, want %q", result.customTitle, "Second Title")
	}
}

func TestScanJSONLFromOffset_OversizedLines(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	// Build a file with: small metadata, then a huge line (>10KB), then more metadata.
	// The scanner must read past the huge line without aborting.
	hugeLine := `{"type":"message","role":"assistant","content":"` + strings.Repeat("x", 20*1024) + "\"}\n"

	content := `{"type":"custom-title","customTitle":"Before huge line"}` + "\n" +
		hugeLine +
		`{"type":"custom-title","customTitle":"After huge line"}` + "\n"

	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := scanJSONLFromOffset(jsonlFilepath, 0, false)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if result.customTitle != "After huge line" {
		t.Errorf("customTitle = %q, want %q (metadata after oversized line must be found)", result.customTitle, "After huge line")
	}
}

func TestScanJSONLFromOffset_ExtractsFirstUserMessage(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"first prompt\"}}\n" +
		"{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"second prompt\"}}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := scanJSONLFromOffset(jsonlFilepath, 0, true)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	// Should return the first user message, not the second
	if result.firstUserMessage != "first prompt" {
		t.Errorf("firstUserMessage = %q, want %q", result.firstUserMessage, "first prompt")
	}
}

func TestScanJSONLFromOffset_SkipsUserMessageWhenNotRequested(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"hello\"}}\n"
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	result, err := scanJSONLFromOffset(jsonlFilepath, 0, false)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if result.firstUserMessage != "" {
		t.Errorf("firstUserMessage = %q, want empty (extractUserMessage=false)", result.firstUserMessage)
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		title  string
		maxLen int
		want   string
	}{
		{"short", 30, "short"},
		{"this is a very long title that exceeds the maximum length", 30, "this is a very long title tha…"},
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

func TestPrependEmoji(t *testing.T) {
	tests := []struct {
		emoji string
		title string
		want  string
	}{
		{"", "my-title", "my-title"},
		{"🔥", "my-title", "🔥  my-title"},
		{"🤖", "my-title", "🤖  my-title"},
		{"A", "my-title", "A  my-title"},
	}

	for _, tt := range tests {
		got := prependEmoji(tt.emoji, tt.title)
		if got != tt.want {
			t.Errorf("prependEmoji(%q, %q) = %q, want %q", tt.emoji, tt.title, got, tt.want)
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
	if got := determineBestTitle(sessionWithCustom, mission, ""); got != "My Custom Name" {
		t.Errorf("with custom title: got %q, want %q", got, "My Custom Name")
	}

	// Without custom title, with auto summary
	sessionWithSummary := &database.Session{
		AutoSummary: "Auto generated summary",
	}
	if got := determineBestTitle(sessionWithSummary, mission, ""); got != "Auto generated summary" {
		t.Errorf("with auto summary: got %q, want %q", got, "Auto generated summary")
	}

	// No session -- falls back to repo name
	if got := determineBestTitle(nil, mission, ""); got != "my-project" {
		t.Errorf("no session: got %q, want %q", got, "my-project")
	}

	// No session, no repo -- falls back to short ID
	missionNoRepo := &database.Mission{ShortID: "abc12345"}
	if got := determineBestTitle(nil, missionNoRepo, ""); got != "abc12345" {
		t.Errorf("no session, no repo: got %q, want %q", got, "abc12345")
	}
}

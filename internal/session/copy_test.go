package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyAndForkSession(t *testing.T) {
	srcDirpath := t.TempDir()
	dstDirpath := t.TempDir()

	srcSessionID := "aaaa-1111-2222-3333-444444444444"
	newSessionID := "bbbb-5555-6666-7777-888888888888"

	// Create source JSONL with sessionId references
	jsonlContent := strings.Join([]string{
		`{"type":"user","sessionId":"aaaa-1111-2222-3333-444444444444","message":"hello"}`,
		`{"type":"assistant","sessionId":"aaaa-1111-2222-3333-444444444444","message":"hi"}`,
		`{"type":"summary","sessionId":"aaaa-1111-2222-3333-444444444444","summary":"test"}`,
	}, "\n") + "\n"
	writeTestFile(t, srcDirpath, srcSessionID+".jsonl", jsonlContent)

	// Create session subdirectory with a file
	sessionSubdirpath := filepath.Join(srcDirpath, srcSessionID, "subagents")
	if err := os.MkdirAll(sessionSubdirpath, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(srcDirpath, srcSessionID, "subagents"), "log.jsonl", `{"data":"test"}`)

	// Create sessions-index.json
	indexContent := `{"entries":[{"sessionId":"aaaa-1111-2222-3333-444444444444","summary":"test session"}]}`
	writeTestFile(t, srcDirpath, "sessions-index.json", indexContent)

	// Create memory directory
	memoryDirpath := filepath.Join(srcDirpath, "memory")
	if err := os.MkdirAll(memoryDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, memoryDirpath, "MEMORY.md", "# Memory\nSome notes")

	// Run the copy
	if err := CopyAndForkSession(srcDirpath, dstDirpath, srcSessionID, newSessionID); err != nil {
		t.Fatalf("CopyAndForkSession failed: %v", err)
	}

	// Verify JSONL was copied with session ID replaced
	dstJSONLData, err := os.ReadFile(filepath.Join(dstDirpath, newSessionID+".jsonl"))
	if err != nil {
		t.Fatalf("failed to read destination JSONL: %v", err)
	}
	dstJSONL := string(dstJSONLData)

	if strings.Contains(dstJSONL, srcSessionID) {
		t.Error("destination JSONL still contains source session ID")
	}
	if !strings.Contains(dstJSONL, newSessionID) {
		t.Error("destination JSONL does not contain new session ID")
	}
	if count := strings.Count(dstJSONL, newSessionID); count != 3 {
		t.Errorf("expected 3 occurrences of new session ID, got %d", count)
	}

	// Verify session subdirectory was copied
	subagentLogFilepath := filepath.Join(dstDirpath, newSessionID, "subagents", "log.jsonl")
	if _, err := os.Stat(subagentLogFilepath); os.IsNotExist(err) {
		t.Error("session subdirectory was not copied")
	}

	// Verify sessions-index.json was copied with ID replaced
	dstIndexData, err := os.ReadFile(filepath.Join(dstDirpath, "sessions-index.json"))
	if err != nil {
		t.Fatalf("failed to read destination sessions-index.json: %v", err)
	}
	dstIndex := string(dstIndexData)
	if strings.Contains(dstIndex, srcSessionID) {
		t.Error("destination sessions-index.json still contains source session ID")
	}
	if !strings.Contains(dstIndex, newSessionID) {
		t.Error("destination sessions-index.json does not contain new session ID")
	}

	// Verify memory directory was copied
	memoryFilepath := filepath.Join(dstDirpath, "memory", "MEMORY.md")
	memoryData, err := os.ReadFile(memoryFilepath)
	if err != nil {
		t.Fatalf("failed to read destination memory file: %v", err)
	}
	if string(memoryData) != "# Memory\nSome notes" {
		t.Errorf("memory file content mismatch: got %q", string(memoryData))
	}
}

func TestCopyAndForkSessionNoOptionalFiles(t *testing.T) {
	srcDirpath := t.TempDir()
	dstDirpath := t.TempDir()

	srcSessionID := "aaaa-1111"
	newSessionID := "bbbb-2222"

	// Create only the required JSONL file (no subdirectory, no index, no memory)
	writeTestFile(t, srcDirpath, srcSessionID+".jsonl", `{"type":"user","sessionId":"aaaa-1111"}`)

	if err := CopyAndForkSession(srcDirpath, dstDirpath, srcSessionID, newSessionID); err != nil {
		t.Fatalf("CopyAndForkSession failed: %v", err)
	}

	// Verify JSONL was copied
	if _, err := os.Stat(filepath.Join(dstDirpath, newSessionID+".jsonl")); os.IsNotExist(err) {
		t.Error("destination JSONL was not created")
	}

	// Verify optional files were not created
	if _, err := os.Stat(filepath.Join(dstDirpath, newSessionID)); !os.IsNotExist(err) {
		t.Error("session subdirectory should not exist when source doesn't have one")
	}
	if _, err := os.Stat(filepath.Join(dstDirpath, "sessions-index.json")); !os.IsNotExist(err) {
		t.Error("sessions-index.json should not exist when source doesn't have one")
	}
}

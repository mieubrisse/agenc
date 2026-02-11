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

	// Create source JSONL with sessionId references.
	// Include a line where the UUID appears in user message content to verify
	// that only the "sessionId" JSON key is replaced.
	jsonlContent := strings.Join([]string{
		`{"type":"user","sessionId":"aaaa-1111-2222-3333-444444444444","message":"hello"}`,
		`{"type":"assistant","sessionId":"aaaa-1111-2222-3333-444444444444","message":"hi"}`,
		`{"type":"summary","sessionId":"aaaa-1111-2222-3333-444444444444","summary":"test"}`,
		`{"type":"user","sessionId":"aaaa-1111-2222-3333-444444444444","message":"my session is aaaa-1111-2222-3333-444444444444 ok"}`,
	}, "\n") + "\n"
	writeTestFile(t, srcDirpath, srcSessionID+".jsonl", jsonlContent)

	// Create session subdirectory with a subagent log that references the parent session
	sessionSubdirpath := filepath.Join(srcDirpath, srcSessionID, "subagents")
	if err := os.MkdirAll(sessionSubdirpath, 0755); err != nil {
		t.Fatal(err)
	}
	subagentContent := `{"isSidechain":true,"sessionId":"aaaa-1111-2222-3333-444444444444","agentId":"a0893d7","type":"user","message":"warmup"}`
	writeTestFile(t, sessionSubdirpath, "agent-a0893d7.jsonl", subagentContent)

	// Create sessions-index.json (should NOT be copied)
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

	// Verify JSONL was copied with sessionId keys replaced
	dstJSONLData, err := os.ReadFile(filepath.Join(dstDirpath, newSessionID+".jsonl"))
	if err != nil {
		t.Fatalf("failed to read destination JSONL: %v", err)
	}
	dstJSONL := string(dstJSONLData)

	// The "sessionId" keys should all be replaced (4 lines × 1 key each = 4 occurrences)
	if count := strings.Count(dstJSONL, `"sessionId":"`+newSessionID+`"`); count != 4 {
		t.Errorf("expected 4 sessionId key replacements, got %d", count)
	}
	// The UUID in the user message content should NOT be replaced
	if !strings.Contains(dstJSONL, `"message":"my session is `+srcSessionID+` ok"`) {
		t.Error("UUID inside message content was incorrectly replaced")
	}

	// Verify session subdirectory was copied
	subagentLogFilepath := filepath.Join(dstDirpath, newSessionID, "subagents", "agent-a0893d7.jsonl")
	if _, err := os.Stat(subagentLogFilepath); os.IsNotExist(err) {
		t.Error("session subdirectory was not copied")
	}

	// Verify subagent JSONL got sessionId replacement
	subagentData, err := os.ReadFile(subagentLogFilepath)
	if err != nil {
		t.Fatalf("failed to read subagent log: %v", err)
	}
	subagentLog := string(subagentData)
	if strings.Contains(subagentLog, `"sessionId":"`+srcSessionID+`"`) {
		t.Error("subagent log still contains source session ID in sessionId key")
	}
	if !strings.Contains(subagentLog, `"sessionId":"`+newSessionID+`"`) {
		t.Error("subagent log does not contain new session ID in sessionId key")
	}

	// Verify sessions-index.json was NOT copied (intentionally skipped)
	if _, err := os.Stat(filepath.Join(dstDirpath, "sessions-index.json")); !os.IsNotExist(err) {
		t.Error("sessions-index.json should not be copied (Claude Code regenerates it)")
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

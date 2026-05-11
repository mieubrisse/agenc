package server

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// newAutoSummaryTestServer constructs a *Server with a real DB and a tempdir
// agencDirpath, suitable for exercising the auto-summary loop body. It also
// redirects $HOME to the tempdir so that claudeconfig.ComputeProjectDirpath
// writes under the test's sandbox rather than the user's real ~/.claude.
func newAutoSummaryTestServer(t *testing.T) *Server {
	t.Helper()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dbFilepath := filepath.Join(tmpDir, "database.sqlite")
	db, err := database.Open(dbFilepath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv := &Server{
		agencDirpath: tmpDir,
		logger:       log.New(os.Stderr, "", 0),
		db:           db,
	}
	srv.cachedConfig.Store(&config.AgencConfig{})
	return srv
}

// writeSessionJSONL writes content to the JSONL filepath where the auto-summary
// loop will look for it: claudeconfig.ComputeProjectDirpath(agentDirpath) /
// <sessionID>.jsonl. Returns the absolute filepath.
func writeSessionJSONL(t *testing.T, agencDirpath, missionID, sessionID, content string) string {
	t.Helper()
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	projectDirpath, err := claudeconfig.ComputeProjectDirpath(agentDirpath)
	if err != nil {
		t.Fatalf("ComputeProjectDirpath failed: %v", err)
	}
	if err := os.MkdirAll(projectDirpath, 0o755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	jsonlFilepath := filepath.Join(projectDirpath, sessionID+".jsonl")
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}
	return jsonlFilepath
}

// TestRunAutoSummaryCycle_HaikuFailureDoesNotAdvanceOffset locks down the bug
// fix: when the summarizer (Haiku CLI) returns an error, the loop must NOT
// advance last_auto_summary_scan_offset, so the next cycle will retry. This
// would have detected the original bug — the old pipeline advanced the offset
// before/regardless of the summarizer succeeding.
func TestRunAutoSummaryCycle_HaikuFailureDoesNotAdvanceOffset(t *testing.T) {
	s := newAutoSummaryTestServer(t)

	mission, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	sess, err := s.db.CreateSession(mission.ID, "sess-haiku-fail")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	content := `{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"
	jsonlPath := writeSessionJSONL(t, s.agencDirpath, mission.ID, sess.ID, content)
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat JSONL failed: %v", err)
	}
	if err := s.db.UpdateKnownFileSize(sess.ID, info.Size()); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	summarizerCalled := false
	failingSummarizer := func(_ context.Context, _, gotMsg string, _ int) (string, error) {
		summarizerCalled = true
		if gotMsg != "hello" {
			t.Errorf("summarizer received message %q, want %q", gotMsg, "hello")
		}
		return "", errors.New("simulated haiku failure")
	}

	s.runAutoSummaryCycleWith(context.Background(), failingSummarizer)

	// Guardrail: the summarizer must actually have been invoked. Without this
	// check, a regression that silently skipped the session (e.g., the row not
	// being selected) would make the offset/summary assertions trivially pass.
	if !summarizerCalled {
		t.Fatal("summarizer was never called — session was not selected for processing")
	}

	got, err := s.db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AutoSummary != "" {
		t.Errorf("AutoSummary = %q, want empty (Haiku failed, must not write)", got.AutoSummary)
	}
	if got.LastAutoSummaryScanOffset != 0 {
		t.Errorf("LastAutoSummaryScanOffset = %d, want 0 (must not advance on failure — this is the bug)", got.LastAutoSummaryScanOffset)
	}
}

// TestRunAutoSummaryCycle_SuccessAdvancesOffsetAndSetsSummary verifies the
// happy path: when the summarizer succeeds, both AutoSummary and
// LastAutoSummaryScanOffset are written atomically.
func TestRunAutoSummaryCycle_SuccessAdvancesOffsetAndSetsSummary(t *testing.T) {
	s := newAutoSummaryTestServer(t)

	mission, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	sess, err := s.db.CreateSession(mission.ID, "sess-success")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	content := `{"type":"user","message":{"role":"user","content":"hello world"}}` + "\n"
	jsonlPath := writeSessionJSONL(t, s.agencDirpath, mission.ID, sess.ID, content)
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat JSONL failed: %v", err)
	}
	expectedOffset := info.Size()
	if err := s.db.UpdateKnownFileSize(sess.ID, expectedOffset); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	successSummarizer := func(_ context.Context, _, gotMsg string, _ int) (string, error) {
		if gotMsg != "hello world" {
			t.Errorf("summarizer received message %q, want %q", gotMsg, "hello world")
		}
		return "Greet World", nil
	}

	s.runAutoSummaryCycleWith(context.Background(), successSummarizer)

	got, err := s.db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AutoSummary != "Greet World" {
		t.Errorf("AutoSummary = %q, want %q", got.AutoSummary, "Greet World")
	}
	if got.LastAutoSummaryScanOffset != expectedOffset {
		t.Errorf("LastAutoSummaryScanOffset = %d, want %d", got.LastAutoSummaryScanOffset, expectedOffset)
	}
}

// TestRunAutoSummaryCycle_NoUserMessageAdvancesOffsetOnly verifies that when
// the JSONL contains no string-content user message yet, the loop advances the
// offset to known_file_size but leaves AutoSummary empty, so the session is
// re-selected only when the file grows.
func TestRunAutoSummaryCycle_NoUserMessageAdvancesOffsetOnly(t *testing.T) {
	s := newAutoSummaryTestServer(t)

	mission, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	sess, err := s.db.CreateSession(mission.ID, "sess-no-user-msg")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Only an assistant line — no user-role string-content message.
	content := `{"type":"assistant","message":{"role":"assistant","content":[]}}` + "\n"
	jsonlPath := writeSessionJSONL(t, s.agencDirpath, mission.ID, sess.ID, content)
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat JSONL failed: %v", err)
	}
	expectedOffset := info.Size()
	if err := s.db.UpdateKnownFileSize(sess.ID, expectedOffset); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	// Summarizer should NOT be called.
	calledSummarizer := false
	noCallSummarizer := func(_ context.Context, _, _ string, _ int) (string, error) {
		calledSummarizer = true
		return "should not be called", nil
	}

	s.runAutoSummaryCycleWith(context.Background(), noCallSummarizer)

	if calledSummarizer {
		t.Error("summarizer was called despite no user message in JSONL")
	}

	got, err := s.db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AutoSummary != "" {
		t.Errorf("AutoSummary = %q, want empty", got.AutoSummary)
	}
	if got.LastAutoSummaryScanOffset != expectedOffset {
		t.Errorf("LastAutoSummaryScanOffset = %d, want %d", got.LastAutoSummaryScanOffset, expectedOffset)
	}
}

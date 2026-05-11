package database

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCreateSessionRejectsBadMissionID(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateSession("nonexistent-mission-id", "session-123")
	if err == nil {
		t.Fatal("expected error when creating session with nonexistent mission ID, got nil")
	}
}

func TestCreateAndGetSession(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	session, err := db.CreateSession(mission.ID, "session-uuid-123")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if session.ID != "session-uuid-123" {
		t.Errorf("expected ID 'session-uuid-123', got %q", session.ID)
	}
	if session.MissionID != mission.ID {
		t.Errorf("expected MissionID %q, got %q", mission.ID, session.MissionID)
	}
	if session.CustomTitle != "" {
		t.Errorf("expected empty custom_title, got %q", session.CustomTitle)
	}

	got, err := db.GetSession("session-uuid-123")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.MissionID != mission.ID {
		t.Errorf("expected MissionID %q, got %q", mission.ID, got.MissionID)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetSession("nonexistent")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown session, got %v", got)
	}
}

func TestListSessionsByMission(t *testing.T) {
	db := openTestDB(t)

	m1, err := db.CreateMission("github.com/owner/repo1", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	m2, err := db.CreateMission("github.com/owner/repo2", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(m1.ID, "s1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(m1.ID, "s2"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(m2.ID, "s3"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	sessions, err := db.ListSessionsByMission(m1.ID)
	if err != nil {
		t.Fatalf("ListSessionsByMission failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions for m1, got %d", len(sessions))
	}
}

func TestGetActiveSession(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(mission.ID, "older-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(mission.ID, "newer-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Sleep so the second update gets a strictly later RFC3339 second.
	time.Sleep(1100 * time.Millisecond)

	// Update the older session to make it "most recently modified"
	if err := db.UpdateCustomTitleAndOffset("older-session", "Updated", 100); err != nil {
		t.Fatalf("UpdateCustomTitleAndOffset failed: %v", err)
	}

	active, err := db.GetActiveSession(mission.ID)
	if err != nil {
		t.Fatalf("GetActiveSession failed: %v", err)
	}
	if active == nil {
		t.Fatal("expected active session, got nil")
	}
	if active.ID != "older-session" {
		t.Errorf("expected active session 'older-session', got %q", active.ID)
	}
}

func TestGetActiveSession_NoSessions(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	active, err := db.GetActiveSession(mission.ID)
	if err != nil {
		t.Fatalf("GetActiveSession failed: %v", err)
	}
	if active != nil {
		t.Errorf("expected nil for mission with no sessions, got %v", active)
	}
}

func TestUpdateSessionAgencCustomTitle(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	_, err = db.CreateSession(mission.ID, "s-rename-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Set agenc_custom_title
	if err := db.UpdateSessionAgencCustomTitle("s-rename-1", "My Custom Name"); err != nil {
		t.Fatalf("UpdateSessionAgencCustomTitle failed: %v", err)
	}

	got, err := db.GetSession("s-rename-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AgencCustomTitle != "My Custom Name" {
		t.Errorf("expected agenc_custom_title %q, got %q", "My Custom Name", got.AgencCustomTitle)
	}

	// Clear agenc_custom_title with empty string
	if err := db.UpdateSessionAgencCustomTitle("s-rename-1", ""); err != nil {
		t.Fatalf("UpdateSessionAgencCustomTitle (clear) failed: %v", err)
	}

	got, err = db.GetSession("s-rename-1")
	if err != nil {
		t.Fatalf("GetSession (after clear) failed: %v", err)
	}
	if got.AgencCustomTitle != "" {
		t.Errorf("expected empty agenc_custom_title after clear, got %q", got.AgencCustomTitle)
	}
}

func TestSessionsCascadeDeleteWithMission(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(mission.ID, "s1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := db.DeleteMission(mission.ID); err != nil {
		t.Fatalf("DeleteMission failed: %v", err)
	}

	got, err := db.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected session to be cascade-deleted, but it still exists")
	}
}

func TestSessionsNeedingCustomTitleUpdate(t *testing.T) {
	db := openTestDB(t)
	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Session with known_size > custom_title_offset → selected
	sess1, err := db.CreateSession(mission.ID, "sess-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := db.UpdateKnownFileSize(sess1.ID, 100); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	// Session with known_size == custom_title_offset → not selected
	sess2, err := db.CreateSession(mission.ID, "sess-2")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := db.UpdateKnownFileSize(sess2.ID, 50); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}
	if err := db.UpdateCustomTitleScanOffset(sess2.ID, 50); err != nil {
		t.Fatalf("UpdateCustomTitleScanOffset failed: %v", err)
	}

	// Session with NULL known_size → not selected
	if _, err := db.CreateSession(mission.ID, "sess-3"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	rows, err := db.SessionsNeedingCustomTitleUpdate()
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "sess-1" {
		t.Errorf("expected exactly sess-1, got %v", rows)
	}
}

func TestSessionsNeedingAutoSummary(t *testing.T) {
	db := openTestDB(t)
	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Empty auto_summary, known_size > offset → selected
	sess1, err := db.CreateSession(mission.ID, "sess-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := db.UpdateKnownFileSize(sess1.ID, 100); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	// Non-empty auto_summary → NOT selected even though offset is 0
	sess2, err := db.CreateSession(mission.ID, "sess-2")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := db.UpdateKnownFileSize(sess2.ID, 100); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}
	if err := db.UpdateAutoSummaryAndOffset(sess2.ID, "an existing summary", 100); err != nil {
		t.Fatalf("UpdateAutoSummaryAndOffset failed: %v", err)
	}

	// Empty auto_summary but offset == known_size → not selected
	sess3, err := db.CreateSession(mission.ID, "sess-3")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := db.UpdateKnownFileSize(sess3.ID, 50); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}
	if err := db.UpdateAutoSummaryScanOffset(sess3.ID, 50); err != nil {
		t.Fatalf("UpdateAutoSummaryScanOffset failed: %v", err)
	}

	rows, err := db.SessionsNeedingAutoSummary()
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "sess-1" {
		t.Errorf("expected exactly sess-1, got %v", rows)
	}
}

func TestUpdateCustomTitleAndOffset(t *testing.T) {
	db := openTestDB(t)
	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "sess-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Pre-populate the auto_summary side so we can verify the update doesn't
	// clobber sibling fields.
	if err := db.UpdateAutoSummaryAndOffset(sess.ID, "sibling summary", 42); err != nil {
		t.Fatalf("UpdateAutoSummaryAndOffset (pre-populate) failed: %v", err)
	}

	// Capture pre-update state, then sleep > 1s so the post-update
	// RFC3339 timestamp (second precision) is strictly greater. This lets
	// us assert UpdatedAt moves strictly forward with .After() rather than
	// settling for .Before() == false. The 1s cost is acceptable for the
	// guarantee that the function actually bumps updated_at.
	before, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession (before) failed: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)

	if err := db.UpdateCustomTitleAndOffset(sess.ID, "My Title", 1234); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	got, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "My Title" {
		t.Errorf("custom_title = %q, want %q", got.CustomTitle, "My Title")
	}
	if got.LastCustomTitleScanOffset != 1234 {
		t.Errorf("offset = %d, want 1234", got.LastCustomTitleScanOffset)
	}
	// Sibling fields must be untouched.
	if got.AutoSummary != "sibling summary" {
		t.Errorf("auto_summary clobbered: got %q, want %q", got.AutoSummary, "sibling summary")
	}
	if got.LastAutoSummaryScanOffset != 42 {
		t.Errorf("last_auto_summary_scan_offset clobbered: got %d, want 42", got.LastAutoSummaryScanOffset)
	}
	// updated_at must strictly advance.
	if !got.UpdatedAt.After(before.UpdatedAt) {
		t.Errorf("UpdatedAt did not advance: before=%v after=%v", before.UpdatedAt, got.UpdatedAt)
	}
}

func TestUpdateCustomTitleScanOffset(t *testing.T) {
	db := openTestDB(t)
	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "sess-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Pre-populate the auto_summary side so we can verify the update doesn't
	// clobber sibling fields.
	if err := db.UpdateAutoSummaryAndOffset(sess.ID, "sibling summary", 42); err != nil {
		t.Fatalf("UpdateAutoSummaryAndOffset (pre-populate) failed: %v", err)
	}

	if err := db.UpdateCustomTitleScanOffset(sess.ID, 999); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "" {
		t.Errorf("custom_title should be empty, got %q", got.CustomTitle)
	}
	if got.LastCustomTitleScanOffset != 999 {
		t.Errorf("offset = %d, want 999", got.LastCustomTitleScanOffset)
	}
	// Sibling fields must be untouched.
	if got.AutoSummary != "sibling summary" {
		t.Errorf("auto_summary clobbered: got %q, want %q", got.AutoSummary, "sibling summary")
	}
	if got.LastAutoSummaryScanOffset != 42 {
		t.Errorf("last_auto_summary_scan_offset clobbered: got %d, want 42", got.LastAutoSummaryScanOffset)
	}
}

func TestUpdateAutoSummaryAndOffset(t *testing.T) {
	db := openTestDB(t)
	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "sess-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Pre-populate the custom_title side so we can verify the update doesn't
	// clobber sibling fields.
	if err := db.UpdateCustomTitleAndOffset(sess.ID, "sibling title", 99); err != nil {
		t.Fatalf("UpdateCustomTitleAndOffset (pre-populate) failed: %v", err)
	}

	if err := db.UpdateAutoSummaryAndOffset(sess.ID, "the summary", 5000); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AutoSummary != "the summary" {
		t.Errorf("auto_summary = %q", got.AutoSummary)
	}
	if got.LastAutoSummaryScanOffset != 5000 {
		t.Errorf("offset = %d", got.LastAutoSummaryScanOffset)
	}
	// Sibling fields must be untouched.
	if got.CustomTitle != "sibling title" {
		t.Errorf("custom_title clobbered: got %q, want %q", got.CustomTitle, "sibling title")
	}
	if got.LastCustomTitleScanOffset != 99 {
		t.Errorf("last_custom_title_scan_offset clobbered: got %d, want 99", got.LastCustomTitleScanOffset)
	}
}

func TestUpdateAutoSummaryScanOffset(t *testing.T) {
	db := openTestDB(t)
	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "sess-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Pre-populate the custom_title side so we can verify the update doesn't
	// clobber sibling fields.
	if err := db.UpdateCustomTitleAndOffset(sess.ID, "sibling title", 99); err != nil {
		t.Fatalf("UpdateCustomTitleAndOffset (pre-populate) failed: %v", err)
	}

	if err := db.UpdateAutoSummaryScanOffset(sess.ID, 777); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AutoSummary != "" {
		t.Errorf("auto_summary should be empty, got %q", got.AutoSummary)
	}
	if got.LastAutoSummaryScanOffset != 777 {
		t.Errorf("offset = %d, want 777", got.LastAutoSummaryScanOffset)
	}
	// Sibling fields must be untouched.
	if got.CustomTitle != "sibling title" {
		t.Errorf("custom_title clobbered: got %q, want %q", got.CustomTitle, "sibling title")
	}
	if got.LastCustomTitleScanOffset != 99 {
		t.Errorf("last_custom_title_scan_offset clobbered: got %d, want 99", got.LastCustomTitleScanOffset)
	}
}

func TestOrphanedSessionsCleanedOnOpen(t *testing.T) {
	dbFilepath := filepath.Join(t.TempDir(), "test.sqlite")

	// Open the database to create the schema
	db1, err := Open(dbFilepath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	// Create a mission and a session
	mission, err := db1.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	if _, err := db1.CreateSession(mission.ID, "legit-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Insert an orphaned session directly via SQL (bypass FK check by
	// temporarily disabling foreign keys at the connection level)
	if _, err := db1.conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("failed to disable foreign keys: %v", err)
	}
	if _, err := db1.conn.Exec(
		"INSERT INTO sessions (id, short_id, mission_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		"orphan-session", "orphan-s", "nonexistent-mission", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("failed to insert orphaned session: %v", err)
	}
	db1.Close()

	// Re-open the database — the migration should clean up the orphan
	db2, err := Open(dbFilepath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer db2.Close()

	// The orphaned session should be gone
	got, err := db2.GetSession("orphan-session")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected orphaned session to be cleaned up, but it still exists")
	}

	// The legitimate session should still exist
	legit, err := db2.GetSession("legit-session")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if legit == nil {
		t.Error("expected legitimate session to survive cleanup, but it was deleted")
	}
}

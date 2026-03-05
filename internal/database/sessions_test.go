package database

import (
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

func TestUpdateSessionScanResults(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	_, err = db.CreateSession(mission.ID, "session-uuid-456")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = db.UpdateSessionScanResults("session-uuid-456", "My Custom Title", 4096)
	if err != nil {
		t.Fatalf("UpdateSessionScanResults failed: %v", err)
	}

	got, err := db.GetSession("session-uuid-456")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "My Custom Title" {
		t.Errorf("expected custom_title %q, got %q", "My Custom Title", got.CustomTitle)
	}
	if got.LastScannedOffset != 4096 {
		t.Errorf("expected last_scanned_offset 4096, got %d", got.LastScannedOffset)
	}
}

func TestUpdateSessionScanResults_PreservesExisting(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	_, err = db.CreateSession(mission.ID, "s1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Set initial values
	err = db.UpdateSessionScanResults("s1", "Original Title", 100)
	if err != nil {
		t.Fatalf("first UpdateSessionScanResults failed: %v", err)
	}

	// Update with empty strings -- should preserve existing values
	err = db.UpdateSessionScanResults("s1", "", 200)
	if err != nil {
		t.Fatalf("second UpdateSessionScanResults failed: %v", err)
	}

	got, err := db.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "Original Title" {
		t.Errorf("expected custom_title preserved as %q, got %q", "Original Title", got.CustomTitle)
	}
	if got.LastScannedOffset != 200 {
		t.Errorf("expected last_scanned_offset updated to 200, got %d", got.LastScannedOffset)
	}
}

func TestUpdateSessionAutoSummary(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	_, err = db.CreateSession(mission.ID, "s-summary-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := db.UpdateSessionAutoSummary("s-summary-1", "Working on auth system"); err != nil {
		t.Fatalf("UpdateSessionAutoSummary failed: %v", err)
	}

	got, err := db.GetSession("s-summary-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AutoSummary != "Working on auth system" {
		t.Errorf("expected auto_summary %q, got %q", "Working on auth system", got.AutoSummary)
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

	// Update the older session to make it "most recently modified"
	if err := db.UpdateSessionScanResults("older-session", "Updated", 100); err != nil {
		t.Fatalf("UpdateSessionScanResults failed: %v", err)
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

func TestGetActiveSession_ScanOffsetDoesNotDisplaceRename(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Create two sessions. "older" is created first, "current" is created second.
	if _, err := db.CreateSession(mission.ID, "older-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(mission.ID, "current-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Sleep to ensure the rename gets a distinct RFC3339 timestamp
	// (second-level precision).
	time.Sleep(1100 * time.Millisecond)

	// Rename the current session — this bumps updated_at
	if err := db.UpdateSessionAgencCustomTitle("current-session", "My Rename"); err != nil {
		t.Fatalf("UpdateSessionAgencCustomTitle failed: %v", err)
	}

	// Simulate the scanner advancing the offset on the older session
	// (e.g., it discovered the JSONL and scanned it). This must NOT bump
	// updated_at and must NOT displace the renamed session.
	if err := db.UpdateSessionScanResults("older-session", "", 4096); err != nil {
		t.Fatalf("UpdateSessionScanResults failed: %v", err)
	}

	active, err := db.GetActiveSession(mission.ID)
	if err != nil {
		t.Fatalf("GetActiveSession failed: %v", err)
	}
	if active == nil {
		t.Fatal("expected active session, got nil")
	}
	if active.ID != "current-session" {
		t.Errorf("expected renamed session 'current-session' to remain active, got %q", active.ID)
	}
	if active.AgencCustomTitle != "My Rename" {
		t.Errorf("expected agenc_custom_title %q, got %q", "My Rename", active.AgencCustomTitle)
	}
}

func TestGetActiveSession_AutoSummaryDoesNotDisplaceRename(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(mission.ID, "older-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(mission.ID, "current-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Sleep to ensure the rename gets a distinct RFC3339 timestamp
	// (second-level precision).
	time.Sleep(1100 * time.Millisecond)

	// Rename the current session
	if err := db.UpdateSessionAgencCustomTitle("current-session", "My Rename"); err != nil {
		t.Fatalf("UpdateSessionAgencCustomTitle failed: %v", err)
	}

	// Simulate the summarizer finishing an auto_summary on the older session.
	// This must NOT displace the renamed session.
	if err := db.UpdateSessionAutoSummary("older-session", "Working on auth"); err != nil {
		t.Fatalf("UpdateSessionAutoSummary failed: %v", err)
	}

	active, err := db.GetActiveSession(mission.ID)
	if err != nil {
		t.Fatalf("GetActiveSession failed: %v", err)
	}
	if active == nil {
		t.Fatal("expected active session, got nil")
	}
	if active.ID != "current-session" {
		t.Errorf("expected renamed session 'current-session' to remain active, got %q", active.ID)
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

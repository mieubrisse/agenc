package database

import (
	"testing"
)

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

	err = db.UpdateSessionScanResults("session-uuid-456", "My Custom Title", "Auto summary text", 4096)
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
	if got.AutoSummary != "Auto summary text" {
		t.Errorf("expected auto_summary %q, got %q", "Auto summary text", got.AutoSummary)
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
	err = db.UpdateSessionScanResults("s1", "Original Title", "Original Summary", 100)
	if err != nil {
		t.Fatalf("first UpdateSessionScanResults failed: %v", err)
	}

	// Update with empty strings -- should preserve existing values
	err = db.UpdateSessionScanResults("s1", "", "", 200)
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
	if got.AutoSummary != "Original Summary" {
		t.Errorf("expected auto_summary preserved as %q, got %q", "Original Summary", got.AutoSummary)
	}
	if got.LastScannedOffset != 200 {
		t.Errorf("expected last_scanned_offset updated to 200, got %d", got.LastScannedOffset)
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
	if err := db.UpdateSessionScanResults("older-session", "Updated", "", 100); err != nil {
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

func TestSessionsCascadeDeleteWithMission(t *testing.T) {
	db := openTestDB(t)

	// Enable foreign keys (SQLite has them off by default)
	if _, err := db.conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

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

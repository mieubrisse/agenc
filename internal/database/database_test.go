package database

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dbFilepath := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := Open(dbFilepath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSetAndGetMissionByTmuxPane(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Set pane (stored without % prefix)
	if err := db.SetTmuxPane(mission.ID, "42"); err != nil {
		t.Fatalf("SetTmuxPane failed: %v", err)
	}

	// Retrieve by pane
	got, err := db.GetMissionByTmuxPane("42")
	if err != nil {
		t.Fatalf("GetMissionByTmuxPane failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected mission, got nil")
	}
	if got.ID != mission.ID {
		t.Errorf("expected mission ID '%s', got '%s'", mission.ID, got.ID)
	}
	if got.TmuxPane == nil || *got.TmuxPane != "42" {
		t.Errorf("expected TmuxPane '42', got %v", got.TmuxPane)
	}
}

func TestClearTmuxPane(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if err := db.SetTmuxPane(mission.ID, "42"); err != nil {
		t.Fatalf("SetTmuxPane failed: %v", err)
	}

	// Clear pane
	if err := db.ClearTmuxPane(mission.ID); err != nil {
		t.Fatalf("ClearTmuxPane failed: %v", err)
	}

	// Should no longer be found
	got, err := db.GetMissionByTmuxPane("42")
	if err != nil {
		t.Fatalf("GetMissionByTmuxPane failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after clear, got mission '%s'", got.ID)
	}

	// Mission should still exist with nil TmuxPane
	m, err := db.GetMission(mission.ID)
	if err != nil {
		t.Fatalf("GetMission failed: %v", err)
	}
	if m.TmuxPane != nil {
		t.Errorf("expected nil TmuxPane, got '%v'", *m.TmuxPane)
	}
}

func TestGetMission_NotFound(t *testing.T) {
	db := openTestDB(t)

	// Query for a mission that doesn't exist
	got, err := db.GetMission("00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetMission failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown mission, got mission '%s'", got.ID)
	}
}

func TestGetMissionByTmuxPane_UnknownPane(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetMissionByTmuxPane("999")
	if err != nil {
		t.Fatalf("GetMissionByTmuxPane failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown pane, got mission '%s'", got.ID)
	}
}

func TestGetMostRecentMissionForCron_NoCronMissions(t *testing.T) {
	db := openTestDB(t)

	// Create a regular mission without cron_id
	_, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Query for a cron ID that has no missions
	got, err := db.GetMostRecentMissionForCron("nonexistent-cron-id")
	if err != nil {
		t.Fatalf("GetMostRecentMissionForCron failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for cron with no missions, got mission '%s'", got.ID)
	}
}

func TestBackfillStripsTmuxPanePercent(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Simulate old behavior: store with % prefix directly via SQL
	if _, err := db.conn.Exec("UPDATE missions SET tmux_pane = ? WHERE id = ?", "%42", mission.ID); err != nil {
		t.Fatalf("failed to set old-style pane: %v", err)
	}

	// Run the backfill (same SQL as the migration)
	if _, err := db.conn.Exec(stripTmuxPanePercentSQL); err != nil {
		t.Fatalf("backfill failed: %v", err)
	}

	// Should now be findable without % prefix
	got, err := db.GetMissionByTmuxPane("42")
	if err != nil {
		t.Fatalf("GetMissionByTmuxPane failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected mission after backfill, got nil")
	}
	if got.TmuxPane == nil || *got.TmuxPane != "42" {
		t.Errorf("expected TmuxPane '42' after backfill, got %v", got.TmuxPane)
	}
}

func TestGetMissionByTmuxPane_OnlyActiveReturned(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if err := db.SetTmuxPane(mission.ID, "42"); err != nil {
		t.Fatalf("SetTmuxPane failed: %v", err)
	}

	// Archive the mission
	if err := db.ArchiveMission(mission.ID); err != nil {
		t.Fatalf("ArchiveMission failed: %v", err)
	}

	// Should not be found (archived missions are excluded)
	got, err := db.GetMissionByTmuxPane("42")
	if err != nil {
		t.Fatalf("GetMissionByTmuxPane failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for archived mission pane, got mission '%s'", got.ID)
	}
}

func TestListMissions_SortsByNewestActivity(t *testing.T) {
	db := openTestDB(t)

	// Create three missions with different activity patterns
	m1, err := db.CreateMission("github.com/owner/repo1", nil)
	if err != nil {
		t.Fatalf("failed to create mission 1: %v", err)
	}

	m2, err := db.CreateMission("github.com/owner/repo2", nil)
	if err != nil {
		t.Fatalf("failed to create mission 2: %v", err)
	}

	m3, err := db.CreateMission("github.com/owner/repo3", nil)
	if err != nil {
		t.Fatalf("failed to create mission 3: %v", err)
	}

	// m1: has only last_active (most recent)
	if err := db.UpdateLastActive(m1.ID); err != nil {
		t.Fatalf("failed to update last_active for m1: %v", err)
	}

	// m2: has only last_heartbeat (older than m1, newer than m3)
	if err := db.UpdateHeartbeat(m2.ID); err != nil {
		t.Fatalf("failed to update heartbeat for m2: %v", err)
	}

	// m3: has neither (only created_at, oldest)
	// No updates needed - it keeps NULL for both timestamps

	// List missions
	missions, err := db.ListMissions(ListMissionsParams{IncludeArchived: false})
	if err != nil {
		t.Fatalf("failed to list missions: %v", err)
	}

	if len(missions) != 3 {
		t.Fatalf("expected 3 missions, got %d", len(missions))
	}

	// Verify sort order: m1 (last_active), m2 (last_heartbeat), m3 (created_at only)
	if missions[0].ID != m1.ID {
		t.Errorf("expected first mission to be m1 (has last_active), got %s", missions[0].ID)
	}
	if missions[1].ID != m2.ID {
		t.Errorf("expected second mission to be m2 (has last_heartbeat), got %s", missions[1].ID)
	}
	if missions[2].ID != m3.ID {
		t.Errorf("expected third mission to be m3 (has only created_at), got %s", missions[2].ID)
	}
}

func TestListMissions_BrandNewMissionAppearsFirst(t *testing.T) {
	db := openTestDB(t)

	// Create an older mission and update its created_at to be in the past
	older, err := db.CreateMission("github.com/owner/old-repo", nil)
	if err != nil {
		t.Fatalf("failed to create older mission: %v", err)
	}

	// Manually set created_at to 1 hour ago to simulate an old mission
	oneHourAgo := "2026-01-01T12:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", oneHourAgo, older.ID); err != nil {
		t.Fatalf("failed to backdate older mission: %v", err)
	}

	// Give it a heartbeat that's also in the past
	oldHeartbeat := "2026-01-01T12:05:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET last_heartbeat = ? WHERE id = ?", oldHeartbeat, older.ID); err != nil {
		t.Fatalf("failed to set old heartbeat: %v", err)
	}

	// Create a brand new mission (no heartbeat, no last_active)
	newer, err := db.CreateMission("github.com/owner/new-repo", nil)
	if err != nil {
		t.Fatalf("failed to create newer mission: %v", err)
	}

	// List missions - the newer one should appear first because its created_at
	// is more recent than the older mission's last_heartbeat
	missions, err := db.ListMissions(ListMissionsParams{IncludeArchived: false})
	if err != nil {
		t.Fatalf("failed to list missions: %v", err)
	}

	if len(missions) != 2 {
		t.Fatalf("expected 2 missions, got %d", len(missions))
	}

	if missions[0].ID != newer.ID {
		t.Errorf("expected brand new mission to appear first, got %s", missions[0].ID)
	}
}


func TestSetAndGetMissionTmuxWindowTitle(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Initially empty
	title, err := db.GetMissionTmuxWindowTitle(mission.ID)
	if err != nil {
		t.Fatalf("GetMissionTmuxWindowTitle failed: %v", err)
	}
	if title != "" {
		t.Errorf("expected empty initial title, got %q", title)
	}

	// Set a title
	if err := db.SetMissionTmuxWindowTitle(mission.ID, "agenc"); err != nil {
		t.Fatalf("SetMissionTmuxWindowTitle failed: %v", err)
	}

	// Read it back
	got, err := db.GetMissionTmuxWindowTitle(mission.ID)
	if err != nil {
		t.Fatalf("GetMissionTmuxWindowTitle after set failed: %v", err)
	}
	if got != "agenc" {
		t.Errorf("expected %q, got %q", "agenc", got)
	}
}

func TestSetMissionTmuxWindowTitleOverwrite(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if err := db.SetMissionTmuxWindowTitle(mission.ID, "first"); err != nil {
		t.Fatalf("first SetMissionTmuxWindowTitle failed: %v", err)
	}
	if err := db.SetMissionTmuxWindowTitle(mission.ID, "second"); err != nil {
		t.Fatalf("second SetMissionTmuxWindowTitle failed: %v", err)
	}

	got, err := db.GetMissionTmuxWindowTitle(mission.ID)
	if err != nil {
		t.Fatalf("GetMissionTmuxWindowTitle failed: %v", err)
	}
	if got != "second" {
		t.Errorf("expected %q after overwrite, got %q", "second", got)
	}
}

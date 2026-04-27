package database

import (
	"path/filepath"
	"testing"
	"time"
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

	// m1: has last_heartbeat (most recent)
	if err := db.UpdateHeartbeat(m1.ID); err != nil {
		t.Fatalf("failed to update heartbeat for m1: %v", err)
	}

	// m2: has older heartbeat
	oldHeartbeat := "2026-01-01T12:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET last_heartbeat = ? WHERE id = ?", oldHeartbeat, m2.ID); err != nil {
		t.Fatalf("failed to set old heartbeat for m2: %v", err)
	}

	// m3: no heartbeat, backdate created_at to ensure it's oldest
	oldCreated := "2025-01-01T00:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", oldCreated, m3.ID); err != nil {
		t.Fatalf("failed to backdate m3: %v", err)
	}

	missions, err := db.ListMissions(ListMissionsParams{IncludeArchived: false})
	if err != nil {
		t.Fatalf("failed to list missions: %v", err)
	}

	if len(missions) != 3 {
		t.Fatalf("expected 3 missions, got %d", len(missions))
	}

	if missions[0].ID != m1.ID {
		t.Errorf("expected first mission to be m1 (recent heartbeat), got %s", missions[0].ID)
	}
	if missions[1].ID != m2.ID {
		t.Errorf("expected second mission to be m2 (old heartbeat), got %s", missions[1].ID)
	}
	if missions[2].ID != m3.ID {
		t.Errorf("expected third mission to be m3 (only created_at), got %s", missions[2].ID)
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

	// Create a brand new mission (no heartbeat)
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

func TestMigrateAddLastUserPromptAt(t *testing.T) {
	db := openTestDB(t)
	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if err := db.UpdateLastUserPromptAt(m.ID); err != nil {
		t.Fatalf("failed to update last_user_prompt_at: %v", err)
	}
	got, err := db.GetMission(m.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if got.LastUserPromptAt == nil {
		t.Fatal("expected last_user_prompt_at to be set, got nil")
	}
}

func TestSetLastUserPromptAt_SkipsEmpty(t *testing.T) {
	db := openTestDB(t)
	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if err := db.UpdateLastUserPromptAt(m.ID); err != nil {
		t.Fatalf("failed to set initial last_user_prompt_at: %v", err)
	}
	got, _ := db.GetMission(m.ID)
	original := got.LastUserPromptAt

	if err := db.SetLastUserPromptAt(m.ID, ""); err != nil {
		t.Fatalf("SetLastUserPromptAt with empty string failed: %v", err)
	}
	got, _ = db.GetMission(m.ID)
	if got.LastUserPromptAt == nil {
		t.Fatal("expected last_user_prompt_at to be preserved, got nil")
	}
	if !got.LastUserPromptAt.Equal(*original) {
		t.Errorf("expected last_user_prompt_at to be unchanged, got %v (was %v)", got.LastUserPromptAt, original)
	}
}

func TestClearAllTmuxPanes(t *testing.T) {
	db := openTestDB(t)

	// Create two missions with pane IDs
	m1, err := db.CreateMission("repo1", nil)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := db.CreateMission("repo2", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.SetTmuxPane(m1.ID, "42"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTmuxPane(m2.ID, "99"); err != nil {
		t.Fatal(err)
	}

	// Clear all
	if err := db.ClearAllTmuxPanes(); err != nil {
		t.Fatalf("ClearAllTmuxPanes failed: %v", err)
	}

	// Verify both are cleared
	got1, _ := db.GetMission(m1.ID)
	got2, _ := db.GetMission(m2.ID)
	if got1.TmuxPane != nil {
		t.Errorf("expected nil pane for m1, got %v", *got1.TmuxPane)
	}
	if got2.TmuxPane != nil {
		t.Errorf("expected nil pane for m2, got %v", *got2.TmuxPane)
	}
}

func TestListMissions_SinceFilter(t *testing.T) {
	db := openTestDB(t)

	old, err := db.CreateMission("github.com/owner/old-repo", nil)
	if err != nil {
		t.Fatalf("failed to create old mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-01-01T00:00:00Z", old.ID); err != nil {
		t.Fatalf("failed to backdate old mission: %v", err)
	}

	recent, err := db.CreateMission("github.com/owner/recent-repo", nil)
	if err != nil {
		t.Fatalf("failed to create recent mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-04-15T12:00:00Z", recent.ID); err != nil {
		t.Fatalf("failed to set recent mission time: %v", err)
	}

	since := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	missions, err := db.ListMissions(ListMissionsParams{Since: &since})
	if err != nil {
		t.Fatalf("ListMissions with Since failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission, got %d", len(missions))
	}
	if missions[0].ID != recent.ID {
		t.Errorf("expected recent mission, got %s", missions[0].ID)
	}
}

func TestListMissions_UntilFilter(t *testing.T) {
	db := openTestDB(t)

	old, err := db.CreateMission("github.com/owner/old-repo", nil)
	if err != nil {
		t.Fatalf("failed to create old mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-01-15T00:00:00Z", old.ID); err != nil {
		t.Fatalf("failed to backdate old mission: %v", err)
	}

	recent, err := db.CreateMission("github.com/owner/recent-repo", nil)
	if err != nil {
		t.Fatalf("failed to create recent mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-04-15T12:00:00Z", recent.ID); err != nil {
		t.Fatalf("failed to set recent mission time: %v", err)
	}

	until := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	missions, err := db.ListMissions(ListMissionsParams{Until: &until})
	if err != nil {
		t.Fatalf("ListMissions with Until failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission, got %d", len(missions))
	}
	if missions[0].ID != old.ID {
		t.Errorf("expected old mission, got %s", missions[0].ID)
	}
}

func TestListMissions_SinceAndUntilFilter(t *testing.T) {
	db := openTestDB(t)

	jan, err := db.CreateMission("github.com/owner/jan", nil)
	if err != nil {
		t.Fatalf("failed to create jan mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-01-15T00:00:00Z", jan.ID); err != nil {
		t.Fatalf("failed to set jan time: %v", err)
	}

	mar, err := db.CreateMission("github.com/owner/mar", nil)
	if err != nil {
		t.Fatalf("failed to create mar mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-03-15T00:00:00Z", mar.ID); err != nil {
		t.Fatalf("failed to set mar time: %v", err)
	}

	may, err := db.CreateMission("github.com/owner/may", nil)
	if err != nil {
		t.Fatalf("failed to create may mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-05-15T00:00:00Z", may.ID); err != nil {
		t.Fatalf("failed to set may time: %v", err)
	}

	since := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)
	missions, err := db.ListMissions(ListMissionsParams{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("ListMissions with Since+Until failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission, got %d", len(missions))
	}
	if missions[0].ID != mar.ID {
		t.Errorf("expected mar mission, got %s", missions[0].ID)
	}
}

func TestListMissions_TimeFilterWithArchived(t *testing.T) {
	db := openTestDB(t)

	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-03-15T00:00:00Z", m.ID); err != nil {
		t.Fatalf("failed to set time: %v", err)
	}
	if err := db.ArchiveMission(m.ID); err != nil {
		t.Fatalf("failed to archive: %v", err)
	}

	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Without IncludeArchived: should return 0
	missions, err := db.ListMissions(ListMissionsParams{Since: &since})
	if err != nil {
		t.Fatalf("ListMissions failed: %v", err)
	}
	if len(missions) != 0 {
		t.Fatalf("expected 0 missions without IncludeArchived, got %d", len(missions))
	}

	// With IncludeArchived: should return 1
	missions, err = db.ListMissions(ListMissionsParams{Since: &since, IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListMissions failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission with IncludeArchived, got %d", len(missions))
	}
}

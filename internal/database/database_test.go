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

func TestCreateMission_InitializesLastActive(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// last_active should be set to creation time
	if mission.LastActive == nil {
		t.Fatal("expected LastActive to be set at creation, got nil")
	}

	// Verify it's close to the creation time (within 1 second)
	if mission.LastActive.Sub(mission.CreatedAt).Abs().Seconds() > 1.0 {
		t.Errorf("LastActive (%v) differs from CreatedAt (%v) by more than 1 second",
			mission.LastActive, mission.CreatedAt)
	}

	// Retrieve from database and verify persistence
	retrieved, err := db.GetMission(mission.ID)
	if err != nil {
		t.Fatalf("failed to retrieve mission: %v", err)
	}

	if retrieved.LastActive == nil {
		t.Fatal("expected persisted LastActive, got nil")
	}

	// RFC3339 timestamps lose microsecond precision, so compare rounded to seconds
	if retrieved.LastActive.Truncate(1).Unix() != mission.LastActive.Truncate(1).Unix() {
		t.Errorf("persisted LastActive (%v) differs from created LastActive (%v)",
			retrieved.LastActive, mission.LastActive)
	}
}

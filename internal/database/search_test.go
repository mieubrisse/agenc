package database

import (
	"testing"
)

func TestInsertSearchContentAndUpdateOffset(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/test/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "session-search-1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	err = db.InsertSearchContentAndUpdateOffset(mission.ID, sess.ID, "discussing authentication refactoring", 1024)
	if err != nil {
		t.Fatalf("InsertSearchContentAndUpdateOffset failed: %v", err)
	}

	// Verify offset was updated
	got, err := db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.LastIndexedOffset != 1024 {
		t.Errorf("expected last_indexed_offset 1024, got %d", got.LastIndexedOffset)
	}
}

func TestSearchMissions(t *testing.T) {
	db := openTestDB(t)

	m1, err := db.CreateMission("github.com/test/repo1", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	m2, err := db.CreateMission("github.com/test/repo2", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	s1, err := db.CreateSession(m1.ID, "session-a1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	s2, err := db.CreateSession(m2.ID, "session-b1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := db.InsertSearchContentAndUpdateOffset(m1.ID, s1.ID, "building the authentication system with OAuth", 100); err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if err := db.InsertSearchContentAndUpdateOffset(m2.ID, s2.ID, "refactoring the database migration layer", 100); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	results, err := db.SearchMissions("authentication", 10)
	if err != nil {
		t.Fatalf("SearchMissions failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MissionID != m1.ID {
		t.Errorf("expected mission %s, got %s", m1.ID, results[0].MissionID)
	}
	if results[0].Snippet == "" {
		t.Error("expected non-empty snippet")
	}
}

func TestSearchMissions_DeduplicatesByMission(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/test/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	s1, err := db.CreateSession(mission.ID, "session-dedup-1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	s2, err := db.CreateSession(mission.ID, "session-dedup-2")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Index content in two different sessions for the same mission
	if err := db.InsertSearchContentAndUpdateOffset(mission.ID, s1.ID, "working on authentication flow", 100); err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if err := db.InsertSearchContentAndUpdateOffset(mission.ID, s2.ID, "continuing authentication work", 100); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	results, err := db.SearchMissions("authentication", 10)
	if err != nil {
		t.Fatalf("SearchMissions failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (deduplicated), got %d", len(results))
	}
}

func TestSearchMissions_EmptyQuery(t *testing.T) {
	db := openTestDB(t)
	results, err := db.SearchMissions("", 10)
	if err != nil {
		t.Fatalf("SearchMissions with empty query failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestSearchMissions_NoResults(t *testing.T) {
	db := openTestDB(t)
	results, err := db.SearchMissions("xyznonexistent", 10)
	if err != nil {
		t.Fatalf("SearchMissions failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchMissions_SpecialCharacters(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/test/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "session-special")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := db.InsertSearchContentAndUpdateOffset(mission.ID, sess.ID, "fixing the user's config.yml parsing", 100); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Query with special chars should not crash
	_, err = db.SearchMissions(`config "yml`, 10)
	if err != nil {
		t.Fatalf("SearchMissions with special chars failed: %v", err)
	}
}

func TestDeleteAllSearchContent(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/test/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	sess, err := db.CreateSession(mission.ID, "session-del")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	if err := db.InsertSearchContentAndUpdateOffset(mission.ID, sess.ID, "some content to delete", 100); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	results, _ := db.SearchMissions("delete", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result before delete, got %d", len(results))
	}

	err = db.DeleteAllSearchContent()
	if err != nil {
		t.Fatalf("DeleteAllSearchContent failed: %v", err)
	}

	results, _ = db.SearchMissions("delete", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}

func TestSessionsNeedingIndexing(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/test/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	sess, err := db.CreateSession(mission.ID, "session-idx-1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Set known_file_size > last_indexed_offset
	if err := db.UpdateKnownFileSize(sess.ID, 5000); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	sessions, err := db.SessionsNeedingIndexing()
	if err != nil {
		t.Fatalf("SessionsNeedingIndexing failed: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session needing indexing, got %d", len(sessions))
	}
	if sessions[0].ID != sess.ID {
		t.Errorf("expected session %s, got %s", sess.ID, sessions[0].ID)
	}

	// After indexing up to the known size, it should no longer appear
	if err := db.InsertSearchContentAndUpdateOffset(mission.ID, sess.ID, "test content", 5000); err != nil {
		t.Fatalf("InsertSearchContentAndUpdateOffset failed: %v", err)
	}

	sessions, err = db.SessionsNeedingIndexing()
	if err != nil {
		t.Fatalf("SessionsNeedingIndexing (after index) failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions needing indexing after catchup, got %d", len(sessions))
	}
}

package server

import (
	"os"
	"testing"
)

// TestRunCustomTitleCycle_FindsTitleAndAdvances verifies the happy path:
// when the JSONL contains a custom-title line that differs from the current
// CustomTitle, the loop must atomically write the title and advance the
// last_custom_title_scan_offset to known_file_size.
func TestRunCustomTitleCycle_FindsTitleAndAdvances(t *testing.T) {
	s := newAutoSummaryTestServer(t)

	mission, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	sess, err := s.db.CreateSession(mission.ID, "sess-title-found")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	content := `{"type":"custom-title","customTitle":"My Title"}` + "\n"
	jsonlPath := writeSessionJSONL(t, s.agencDirpath, mission.ID, sess.ID, content)
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat JSONL failed: %v", err)
	}
	expectedOffset := info.Size()
	if err := s.db.UpdateKnownFileSize(sess.ID, expectedOffset); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	s.runCustomTitleCycle()

	got, err := s.db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "My Title" {
		t.Errorf("CustomTitle = %q, want %q", got.CustomTitle, "My Title")
	}
	if got.LastCustomTitleScanOffset != expectedOffset {
		t.Errorf("LastCustomTitleScanOffset = %d, want %d", got.LastCustomTitleScanOffset, expectedOffset)
	}
}

// TestRunCustomTitleCycle_NoTitleAdvancesOffsetOnly verifies that when the
// JSONL contains no custom-title metadata, the loop leaves CustomTitle empty
// but still advances the offset to known_file_size, so the same bytes are not
// re-scanned next cycle.
func TestRunCustomTitleCycle_NoTitleAdvancesOffsetOnly(t *testing.T) {
	s := newAutoSummaryTestServer(t)

	mission, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	sess, err := s.db.CreateSession(mission.ID, "sess-no-title")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// JSONL with a user message but no custom-title metadata.
	content := `{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"
	jsonlPath := writeSessionJSONL(t, s.agencDirpath, mission.ID, sess.ID, content)
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat JSONL failed: %v", err)
	}
	expectedOffset := info.Size()
	if err := s.db.UpdateKnownFileSize(sess.ID, expectedOffset); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	s.runCustomTitleCycle()

	got, err := s.db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "" {
		t.Errorf("CustomTitle = %q, want empty", got.CustomTitle)
	}
	if got.LastCustomTitleScanOffset != expectedOffset {
		t.Errorf("LastCustomTitleScanOffset = %d, want %d", got.LastCustomTitleScanOffset, expectedOffset)
	}
}

// TestRunCustomTitleCycle_UnchangedTitleDoesNotWriteOrReconcile verifies that
// when the scanned title equals the existing CustomTitle, the loop still
// advances the offset but does not change the CustomTitle. Distinguishing the
// offset-only branch from the (title+offset) branch via DB observation alone
// is hard — both writes bump updated_at, and updated_at is stored at RFC3339
// second precision, which makes monotonicity assertions fragile. Without a
// tmux/DB test double we settle for the permissive contract per the plan:
// CustomTitle is unchanged and the offset advanced to known_file_size.
func TestRunCustomTitleCycle_UnchangedTitleDoesNotWriteOrReconcile(t *testing.T) {
	s := newAutoSummaryTestServer(t)

	mission, err := s.db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	sess, err := s.db.CreateSession(mission.ID, "sess-unchanged-title")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Pre-set the CustomTitle to "Same Title" with offset 0, so the cycle is
	// forced to select this session (known_file_size > 0 once we set it
	// below).
	if err := s.db.UpdateCustomTitleAndOffset(sess.ID, "Same Title", 0); err != nil {
		t.Fatalf("seed UpdateCustomTitleAndOffset failed: %v", err)
	}

	content := `{"type":"custom-title","customTitle":"Same Title"}` + "\n"
	jsonlPath := writeSessionJSONL(t, s.agencDirpath, mission.ID, sess.ID, content)
	info, err := os.Stat(jsonlPath)
	if err != nil {
		t.Fatalf("stat JSONL failed: %v", err)
	}
	expectedOffset := info.Size()
	if err := s.db.UpdateKnownFileSize(sess.ID, expectedOffset); err != nil {
		t.Fatalf("UpdateKnownFileSize failed: %v", err)
	}

	s.runCustomTitleCycle()

	got, err := s.db.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "Same Title" {
		t.Errorf("CustomTitle = %q, want %q (title must not change when scan equals existing)", got.CustomTitle, "Same Title")
	}
	if got.LastCustomTitleScanOffset != expectedOffset {
		t.Errorf("LastCustomTitleScanOffset = %d, want %d (offset must still advance)", got.LastCustomTitleScanOffset, expectedOffset)
	}
}

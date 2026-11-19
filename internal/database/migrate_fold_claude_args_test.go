package database

import (
	"reflect"
	"testing"
)

const hostileClaudeArgValueForFoldTest = "Tonight is for reflection.\nDon't open \"work\" topics."

// seedLegacyClaudeArgsColumn puts a test database back into the shape it had
// before claude_args was folded away, so the fold migration has something to
// do.
func seedLegacyClaudeArgsColumn(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec("ALTER TABLE missions ADD COLUMN claude_args TEXT"); err != nil {
		t.Fatalf("failed to re-add the legacy claude_args column: %v", err)
	}
}

// createLegacyMission creates a mission and stores a raw claude_args value on
// it, the way a pre-fold AgenC would have.
func createLegacyMission(t *testing.T, db *DB, storedKeyedArgs *string, passthroughArgs []string) string {
	t.Helper()
	created, err := db.CreateMission("github.com/owner/repo", &CreateMissionParams{ClaudePassthroughArgs: passthroughArgs})
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET claude_args = ? WHERE id = ?", storedKeyedArgs, created.ID); err != nil {
		t.Fatalf("failed to store legacy claude_args: %v", err)
	}
	return created.ID
}

func stringPointer(value string) *string {
	return &value
}

func TestFoldClaudeArgsMigrationPrependsKeyedArgsInOldEmissionOrder(t *testing.T) {
	db := openTestDB(t)
	seedLegacyClaudeArgsColumn(t, db)

	modelAndEffortID := createLegacyMission(t, db, stringPointer(`{"effort":"high","model":"opus"}`), nil)
	effortOnlyID := createLegacyMission(t, db, stringPointer(`{"effort":"high"}`), nil)
	bothChannelsID := createLegacyMission(t, db, stringPointer(`{"model":"fable"}`), []string{"--append-system-prompt", hostileClaudeArgValueForFoldTest})
	passthroughOnlyID := createLegacyMission(t, db, nil, []string{"--add-dir", "/one"})
	unknownAndEmptyID := createLegacyMission(t, db, stringPointer(`{"bogus":"x","model":""}`), nil)
	neitherID := createLegacyMission(t, db, nil, nil)

	if err := migrateFoldClaudeArgsIntoPassthroughArgs(db.conn); err != nil {
		t.Fatalf("fold migration failed: %v", err)
	}

	expectations := map[string][]string{
		modelAndEffortID:  {"--model", "opus", "--effort", "high"},
		effortOnlyID:      {"--effort", "high"},
		bothChannelsID:    {"--model", "fable", "--append-system-prompt", hostileClaudeArgValueForFoldTest},
		passthroughOnlyID: {"--add-dir", "/one"},
		unknownAndEmptyID: nil,
		neitherID:         nil,
	}
	for missionID, expected := range expectations {
		got, err := db.GetMission(missionID)
		if err != nil {
			t.Fatalf("failed to get mission '%s': %v", missionID, err)
		}
		if !reflect.DeepEqual(got.ClaudePassthroughArgs, expected) {
			t.Errorf("mission '%s': expected %q, got %q", missionID, expected, got.ClaudePassthroughArgs)
		}
	}

	columns, err := getColumnNames(db.conn)
	if err != nil {
		t.Fatalf("failed to read columns: %v", err)
	}
	if columns["claude_args"] {
		t.Fatalf("expected the claude_args column to be dropped")
	}
}

// The migration runs on every database open, so a second run must change
// nothing — in particular it must not prepend the keyed args a second time.
func TestFoldClaudeArgsMigrationIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	seedLegacyClaudeArgsColumn(t, db)
	missionID := createLegacyMission(t, db, stringPointer(`{"model":"opus"}`), []string{"--chrome"})

	for run := 1; run <= 2; run++ {
		if err := migrateFoldClaudeArgsIntoPassthroughArgs(db.conn); err != nil {
			t.Fatalf("fold migration run %d failed: %v", run, err)
		}
	}

	got, err := db.GetMission(missionID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	expected := []string{"--model", "opus", "--chrome"}
	if !reflect.DeepEqual(got.ClaudePassthroughArgs, expected) {
		t.Fatalf("expected %q after two runs, got %q", expected, got.ClaudePassthroughArgs)
	}
}

// A malformed claude_args value must fail the migration and leave the data as
// it was, rather than half-folding the table.
func TestFoldClaudeArgsMigrationRollsBackOnMalformedRow(t *testing.T) {
	db := openTestDB(t)
	seedLegacyClaudeArgsColumn(t, db)
	goodID := createLegacyMission(t, db, stringPointer(`{"model":"opus"}`), nil)
	createLegacyMission(t, db, stringPointer(`not json`), nil)

	if err := migrateFoldClaudeArgsIntoPassthroughArgs(db.conn); err == nil {
		t.Fatalf("expected the fold migration to fail on a malformed claude_args value")
	}

	columns, err := getColumnNames(db.conn)
	if err != nil {
		t.Fatalf("failed to read columns: %v", err)
	}
	if !columns["claude_args"] {
		t.Fatalf("expected the claude_args column to survive a failed fold")
	}
	got, err := db.GetMission(goodID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if len(got.ClaudePassthroughArgs) != 0 {
		t.Fatalf("expected no partial fold, got %q", got.ClaudePassthroughArgs)
	}
}

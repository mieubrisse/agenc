package database

import (
	"database/sql"

	"github.com/mieubrisse/stacktrace"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB connection to the agenc SQLite database.
type DB struct {
	conn *sql.DB
}

// migrationStep pairs a migration function with a human-readable description
// used in error messages.
type migrationStep struct {
	fn   func(*sql.DB) error
	desc string
}

// getMigrationSteps returns the ordered list of database migrations to run after
// the initial table creation.
func getMigrationSteps() []migrationStep {
	return []migrationStep{
		{migrateWorktreeSourceToGitRepo, "migrate worktree_source to git_repo column"},
		{migrateAddLastHeartbeat, "add last_heartbeat column"},
		{migrateAddShortID, "add short_id column"},
		{migrateAddSessionName, "add session_name columns"},
		{migrateAddCronColumns, "add cron columns"},
		{migrateDropAgentTemplate, "drop agent_template column"},
		{migrateAddConfigCommit, "add config_commit column"},
		{migrateAddTmuxPane, "add tmux_pane column"},
		{migrateAddAISummary, "add AI summary columns"},
		{migrateAddTmuxWindowTitle, "add tmux_window_title column"},
		{migrateClearTmuxWindowTitle, "clear tmux_window_title column"},
		{migrateAddQueryIndices, "add query performance indices"},
		{migrateCreateSessionsTable, "create sessions table"},
		{migrateDropLastActive, "drop last_active column"},
		{migrateAddAgencCustomTitle, "add agenc_custom_title column to sessions"},
		{migrateAddSessionShortID, "add short_id column to sessions"},
		{migrateCleanOrphanedSessions, "clean orphaned sessions"},
		{migrateAddLastUserPromptAt, "add last_user_prompt_at column"},
		{migrateAddSourceColumns, "add source columns"},
		{migrateSearchIndex, "create FTS5 search index and session processing columns"},
		{migrateCreateNotificationsTable, "create notifications table"},
		{migrateCreateWriteableCopyPausesTable, "create writeable_copy_pauses table"},
	}
}

// Open opens or creates the SQLite database at the given filepath
// and runs auto-migration.
func Open(dbFilepath string) (*DB, error) {
	dsn := dbFilepath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open database at '%s'", dbFilepath)
	}

	// SQLite only supports a single writer, so limit the pool to one connection
	// to avoid unnecessary contention between connections in the same process.
	conn.SetMaxOpenConns(1)

	// Initial table creation
	if _, err := conn.Exec(createMissionsTableSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to auto-migrate database")
	}

	// Run all migration steps
	for _, step := range getMigrationSteps() {
		if err := step.fn(conn); err != nil {
			conn.Close()
			return nil, stacktrace.Propagate(err, "failed to %s", step.desc)
		}
	}

	// Drop legacy mission_descriptions table
	if _, err := conn.Exec(dropMissionDescriptionsTableSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to drop mission_descriptions table")
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

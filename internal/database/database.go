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

// Open opens or creates the SQLite database at the given filepath
// and runs auto-migration.
func Open(dbFilepath string) (*DB, error) {
	dsn := dbFilepath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open database at '%s'", dbFilepath)
	}

	// SQLite only supports a single writer, so limit the pool to one connection
	// to avoid unnecessary contention between connections in the same process.
	conn.SetMaxOpenConns(1)

	migrations := []string{createMissionsTableSQL}
	for _, migrationSQL := range migrations {
		if _, err := conn.Exec(migrationSQL); err != nil {
			conn.Close()
			return nil, stacktrace.Propagate(err, "failed to auto-migrate database")
		}
	}

	if err := migrateWorktreeSourceToGitRepo(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to migrate worktree_source to git_repo column")
	}

	if err := migrateAddLastHeartbeat(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add last_heartbeat column")
	}

	if err := migrateAddShortID(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add short_id column")
	}

	if err := migrateAddSessionName(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add session_name columns")
	}

	if err := migrateAddCronColumns(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add cron columns")
	}

	if err := migrateDropAgentTemplate(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to drop agent_template column")
	}

	if err := migrateAddConfigCommit(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add config_commit column")
	}

	if err := migrateAddTmuxPane(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add tmux_pane column")
	}

	if err := migrateAddAISummary(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add AI summary columns")
	}

	if err := migrateAddTmuxWindowTitle(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add tmux_window_title column")
	}

	if err := migrateAddQueryIndices(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to add query performance indices")
	}

	if err := migrateCreateSessionsTable(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to create sessions table")
	}

	if err := migrateDropLastActive(conn); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to drop last_active column")
	}

	// Backfill: strip "%" prefix from tmux_pane values stored by older builds
	if _, err := conn.Exec(stripTmuxPanePercentSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to strip percent prefix from tmux_pane values")
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

package database

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/mieubrisse/stacktrace"

	_ "modernc.org/sqlite"
)

const createMissionsTableSQL = `
CREATE TABLE IF NOT EXISTS missions (
	id TEXT PRIMARY KEY,
	agent_template TEXT NOT NULL DEFAULT '',
	prompt TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

const dropMissionDescriptionsTableSQL = `DROP TABLE IF EXISTS mission_descriptions;`

const addGitRepoColumnSQL = `ALTER TABLE missions ADD COLUMN git_repo TEXT NOT NULL DEFAULT '';`
const addLastHeartbeatColumnSQL = `ALTER TABLE missions ADD COLUMN last_heartbeat TEXT;`
// Mission represents a row in the missions table.
type Mission struct {
	ID            string
	AgentTemplate string
	Prompt        string
	Status        string
	GitRepo       string
	LastHeartbeat *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// DB wraps a sql.DB connection to the agenc SQLite database.
type DB struct {
	conn *sql.DB
}

// Open opens or creates the SQLite database at the given filepath
// and runs auto-migration.
func Open(dbFilepath string) (*DB, error) {
	dsn := dbFilepath + "?_busy_timeout=5000"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open database at '%s'", dbFilepath)
	}

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

	// Drop legacy mission_descriptions table
	if _, err := conn.Exec(dropMissionDescriptionsTableSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to drop mission_descriptions table")
	}

	return &DB{conn: conn}, nil
}

// migrateWorktreeSourceToGitRepo ensures the missions table has a git_repo
// column. Handles three states idempotently:
//   - Neither column exists (new DB): adds git_repo
//   - worktree_source exists (old DB): renames it to git_repo
//   - git_repo already exists (already migrated): no-op
func migrateWorktreeSourceToGitRepo(conn *sql.DB) error {
	hasWorktreeSource := false
	hasGitRepo := false

	rows, err := conn.Query("PRAGMA table_info(missions)")
	if err != nil {
		return stacktrace.Propagate(err, "failed to read missions table info")
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return stacktrace.Propagate(err, "failed to scan table_info row")
		}
		switch name {
		case "worktree_source":
			hasWorktreeSource = true
		case "git_repo":
			hasGitRepo = true
		}
	}
	if err := rows.Err(); err != nil {
		return stacktrace.Propagate(err, "error iterating table_info rows")
	}

	switch {
	case hasGitRepo:
		return nil // already migrated
	case hasWorktreeSource:
		_, err := conn.Exec("ALTER TABLE missions RENAME COLUMN worktree_source TO git_repo")
		return err
	default:
		_, err := conn.Exec(addGitRepoColumnSQL)
		return err
	}
}

// migrateAddLastHeartbeat idempotently adds the last_heartbeat column.
func migrateAddLastHeartbeat(conn *sql.DB) error {
	rows, err := conn.Query("PRAGMA table_info(missions)")
	if err != nil {
		return stacktrace.Propagate(err, "failed to read missions table info")
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return stacktrace.Propagate(err, "failed to scan table_info row")
		}
		if name == "last_heartbeat" {
			return nil // already exists
		}
	}
	if err := rows.Err(); err != nil {
		return stacktrace.Propagate(err, "error iterating table_info rows")
	}

	_, err = conn.Exec(addLastHeartbeatColumnSQL)
	return err
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// CreateMission inserts a new mission and returns it.
func (db *DB) CreateMission(agentTemplate string, prompt string, gitRepo string) (*Mission, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, agent_template, prompt, git_repo, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?)",
		id, agentTemplate, prompt, gitRepo, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert mission")
	}

	return &Mission{
		ID:            id,
		AgentTemplate: agentTemplate,
		Prompt:        prompt,
		GitRepo:       gitRepo,
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

// ListMissions returns missions ordered by last_heartbeat DESC (most recently
// active first), with missions that have never sent a heartbeat sorted to the
// end by created_at DESC.
// If includeArchived is true, all missions are returned; otherwise archived missions are excluded.
func (db *DB) ListMissions(includeArchived bool) ([]*Mission, error) {
	query := "SELECT id, agent_template, prompt, status, git_repo, last_heartbeat, created_at, updated_at FROM missions"
	if !includeArchived {
		query += " WHERE status != 'archived'"
	}
	query += " ORDER BY last_heartbeat IS NULL, last_heartbeat DESC, created_at DESC"

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query missions")
	}
	defer rows.Close()

	return scanMissions(rows)
}

// GetMission returns a single mission by ID.
func (db *DB) GetMission(id string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, agent_template, prompt, status, git_repo, last_heartbeat, created_at, updated_at FROM missions WHERE id = ?",
		id,
	)

	mission, err := scanMission(row)
	if err == sql.ErrNoRows {
		return nil, stacktrace.NewError("mission '%s' not found", id)
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get mission '%s'", id)
	}
	return mission, nil
}

// ArchiveMission sets the mission status to 'archived'.
func (db *DB) ArchiveMission(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.conn.Exec(
		"UPDATE missions SET status = 'archived', updated_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to archive mission '%s'", id)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return stacktrace.Propagate(err, "failed to check rows affected")
	}
	if rowsAffected == 0 {
		return stacktrace.NewError("mission '%s' not found", id)
	}
	return nil
}

// UnarchiveMission sets the mission status back to 'active'.
func (db *DB) UnarchiveMission(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.conn.Exec(
		"UPDATE missions SET status = 'active', updated_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to unarchive mission '%s'", id)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return stacktrace.Propagate(err, "failed to check rows affected")
	}
	if rowsAffected == 0 {
		return stacktrace.NewError("mission '%s' not found", id)
	}
	return nil
}

// DeleteMission permanently removes a mission from the database.
func (db *DB) DeleteMission(id string) error {
	result, err := db.conn.Exec("DELETE FROM missions WHERE id = ?", id)
	if err != nil {
		return stacktrace.Propagate(err, "failed to delete mission '%s'", id)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return stacktrace.Propagate(err, "failed to check rows affected")
	}
	if rowsAffected == 0 {
		return stacktrace.NewError("mission '%s' not found", id)
	}
	return nil
}

// UpdateHeartbeat sets the last_heartbeat timestamp to the current time for
// the given mission. Called periodically by the wrapper to signal liveness.
func (db *DB) UpdateHeartbeat(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE missions SET last_heartbeat = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update heartbeat for mission '%s'", id)
	}
	return nil
}

func scanMissions(rows *sql.Rows) ([]*Mission, error) {
	var missions []*Mission
	for rows.Next() {
		var m Mission
		var lastHeartbeat sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.AgentTemplate, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission row")
		}
		if lastHeartbeat.Valid {
			t, _ := time.Parse(time.RFC3339, lastHeartbeat.String)
			m.LastHeartbeat = &t
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		missions = append(missions, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating mission rows")
	}
	return missions, nil
}

func scanMission(row *sql.Row) (*Mission, error) {
	var m Mission
	var lastHeartbeat sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&m.ID, &m.AgentTemplate, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if lastHeartbeat.Valid {
		t, _ := time.Parse(time.RFC3339, lastHeartbeat.String)
		m.LastHeartbeat = &t
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &m, nil
}

package database

import (
	"database/sql"
	"strings"
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

const createMissionDescriptionsTableSQL = `
CREATE TABLE IF NOT EXISTS mission_descriptions (
	mission_id TEXT PRIMARY KEY,
	description TEXT NOT NULL,
	created_at TEXT NOT NULL
);
`

const addWorktreeSourceColumnSQL = `ALTER TABLE missions ADD COLUMN worktree_source TEXT NOT NULL DEFAULT '';`

// Mission represents a row in the missions table.
type Mission struct {
	ID             string
	AgentTemplate  string
	Prompt         string
	Status         string
	WorktreeSource string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MissionDescription represents a row in the mission_descriptions table.
type MissionDescription struct {
	MissionID   string
	Description string
	CreatedAt   time.Time
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

	migrations := []string{createMissionsTableSQL, createMissionDescriptionsTableSQL}
	for _, migrationSQL := range migrations {
		if _, err := conn.Exec(migrationSQL); err != nil {
			conn.Close()
			return nil, stacktrace.Propagate(err, "failed to auto-migrate database")
		}
	}

	if err := runMigrationIgnoreDuplicate(conn, addWorktreeSourceColumnSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to run worktree_source migration")
	}

	return &DB{conn: conn}, nil
}

// runMigrationIgnoreDuplicate executes a migration SQL statement, ignoring
// "duplicate column" errors for idempotent ALTER TABLE ADD COLUMN migrations.
func runMigrationIgnoreDuplicate(conn *sql.DB, migrationSQL string) error {
	_, err := conn.Exec(migrationSQL)
	if err != nil && strings.Contains(err.Error(), "duplicate column") {
		return nil
	}
	return err
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// CreateMission inserts a new mission and returns it.
func (db *DB) CreateMission(agentTemplate string, prompt string, worktreeSource string) (*Mission, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, agent_template, prompt, worktree_source, status, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?)",
		id, agentTemplate, prompt, worktreeSource, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert mission")
	}

	return &Mission{
		ID:             id,
		AgentTemplate:  agentTemplate,
		Prompt:         prompt,
		WorktreeSource: worktreeSource,
		Status:         "active",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}, nil
}

// ListMissions returns missions ordered by created_at DESC.
// If includeArchived is true, all missions are returned; otherwise archived missions are excluded.
func (db *DB) ListMissions(includeArchived bool) ([]*Mission, error) {
	query := "SELECT id, agent_template, prompt, status, worktree_source, created_at, updated_at FROM missions"
	if !includeArchived {
		query += " WHERE status != 'archived'"
	}
	query += " ORDER BY created_at DESC"

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
		"SELECT id, agent_template, prompt, status, worktree_source, created_at, updated_at FROM missions WHERE id = ?",
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

// DeleteMission permanently removes a mission and its description from the database.
func (db *DB) DeleteMission(id string) error {
	// Delete description first (child record)
	if _, err := db.conn.Exec("DELETE FROM mission_descriptions WHERE mission_id = ?", id); err != nil {
		return stacktrace.Propagate(err, "failed to delete description for mission '%s'", id)
	}

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

// CreateMissionDescription inserts a description for a mission.
func (db *DB) CreateMissionDescription(missionID string, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"INSERT OR REPLACE INTO mission_descriptions (mission_id, description, created_at) VALUES (?, ?, ?)",
		missionID, description, now,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to insert mission description for '%s'", missionID)
	}
	return nil
}

// GetMissionDescription returns the description for a mission, or nil if none exists.
func (db *DB) GetMissionDescription(missionID string) (*MissionDescription, error) {
	row := db.conn.QueryRow(
		"SELECT mission_id, description, created_at FROM mission_descriptions WHERE mission_id = ?",
		missionID,
	)
	var md MissionDescription
	var createdAt string
	if err := row.Scan(&md.MissionID, &md.Description, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "failed to get mission description for '%s'", missionID)
	}
	md.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &md, nil
}

// ListMissionsWithoutDescription returns active missions that have no description
// and were created more than 10 seconds ago.
func (db *DB) ListMissionsWithoutDescription() ([]*Mission, error) {
	cutoff := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339)
	rows, err := db.conn.Query(
		`SELECT m.id, m.agent_template, m.prompt, m.status, m.worktree_source, m.created_at, m.updated_at
		FROM missions m
		LEFT JOIN mission_descriptions md ON m.id = md.mission_id
		WHERE m.status = 'active' AND md.mission_id IS NULL AND m.created_at <= ?
		ORDER BY m.created_at ASC`,
		cutoff,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query missions without descriptions")
	}
	defer rows.Close()

	return scanMissions(rows)
}

// GetDescriptionsForMissions returns a map of mission_id -> description for the given IDs.
func (db *DB) GetDescriptionsForMissions(missionIDs []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(missionIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(missionIDs))
	queryArgs := make([]any, len(missionIDs))
	for i, id := range missionIDs {
		placeholders[i] = "?"
		queryArgs[i] = id
	}

	query := "SELECT mission_id, description FROM mission_descriptions WHERE mission_id IN (" +
		joinStrings(placeholders, ",") + ")"

	rows, err := db.conn.Query(query, queryArgs...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to batch-fetch mission descriptions")
	}
	defer rows.Close()

	for rows.Next() {
		var missionID, description string
		if err := rows.Scan(&missionID, &description); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission description row")
		}
		result[missionID] = description
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating mission description rows")
	}

	return result, nil
}

// CountDescriptionStats returns the number of active missions with and without descriptions.
func (db *DB) CountDescriptionStats() (described int, pending int, err error) {
	row := db.conn.QueryRow(`
		SELECT
			COUNT(md.mission_id),
			COUNT(m.id) - COUNT(md.mission_id)
		FROM missions m
		LEFT JOIN mission_descriptions md ON m.id = md.mission_id
		WHERE m.status = 'active'
	`)
	if err := row.Scan(&described, &pending); err != nil {
		return 0, 0, stacktrace.Propagate(err, "failed to count description stats")
	}
	return described, pending, nil
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}

func scanMissions(rows *sql.Rows) ([]*Mission, error) {
	var missions []*Mission
	for rows.Next() {
		var m Mission
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.AgentTemplate, &m.Prompt, &m.Status, &m.WorktreeSource, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission row")
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
	var createdAt, updatedAt string
	if err := row.Scan(&m.ID, &m.AgentTemplate, &m.Prompt, &m.Status, &m.WorktreeSource, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &m, nil
}

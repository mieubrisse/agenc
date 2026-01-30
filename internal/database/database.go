package database

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/kurtosis-tech/stacktrace"

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

// Mission represents a row in the missions table.
type Mission struct {
	ID            string
	AgentTemplate string
	Prompt        string
	Status        string
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
	conn, err := sql.Open("sqlite", dbFilepath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open database at '%s'", dbFilepath)
	}

	if _, err := conn.Exec(createMissionsTableSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to auto-migrate database")
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// CreateMission inserts a new mission and returns it.
func (db *DB) CreateMission(agentTemplate string, prompt string) (*Mission, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, agent_template, prompt, status, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?)",
		id, agentTemplate, prompt, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert mission")
	}

	return &Mission{
		ID:            id,
		AgentTemplate: agentTemplate,
		Prompt:        prompt,
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

// ListActiveMissions returns all missions that are not archived.
func (db *DB) ListActiveMissions() ([]*Mission, error) {
	rows, err := db.conn.Query(
		"SELECT id, agent_template, prompt, status, created_at, updated_at FROM missions WHERE status != 'archived' ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query active missions")
	}
	defer rows.Close()

	return scanMissions(rows)
}

// GetMission returns a single mission by ID.
func (db *DB) GetMission(id string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, agent_template, prompt, status, created_at, updated_at FROM missions WHERE id = ?",
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

func scanMissions(rows *sql.Rows) ([]*Mission, error) {
	var missions []*Mission
	for rows.Next() {
		var m Mission
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.AgentTemplate, &m.Prompt, &m.Status, &createdAt, &updatedAt); err != nil {
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
	if err := row.Scan(&m.ID, &m.AgentTemplate, &m.Prompt, &m.Status, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &m, nil
}

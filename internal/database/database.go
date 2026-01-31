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

const createAgentTemplatesTableSQL = `
CREATE TABLE IF NOT EXISTS agent_templates (
	repo TEXT PRIMARY KEY,
	nickname TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);
`

const dropMissionDescriptionsTableSQL = `DROP TABLE IF EXISTS mission_descriptions;`

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

	migrations := []string{createMissionsTableSQL, createAgentTemplatesTableSQL}
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

	// Drop legacy mission_descriptions table
	if _, err := conn.Exec(dropMissionDescriptionsTableSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to drop mission_descriptions table")
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

// AgentTemplate represents a row in the agent_templates table.
type AgentTemplate struct {
	Repo      string
	Nickname  string
	CreatedAt time.Time
}

// CreateAgentTemplate inserts a new agent template record.
func (db *DB) CreateAgentTemplate(repo string) (*AgentTemplate, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		"INSERT INTO agent_templates (repo, nickname, created_at) VALUES (?, '', ?)",
		repo, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert agent template '%s'", repo)
	}

	return &AgentTemplate{
		Repo:      repo,
		Nickname:  "",
		CreatedAt: time.Now().UTC(),
	}, nil
}

// ListAgentTemplates returns all agent templates ordered by repo name.
func (db *DB) ListAgentTemplates() ([]*AgentTemplate, error) {
	rows, err := db.conn.Query("SELECT repo, nickname, created_at FROM agent_templates ORDER BY repo")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query agent templates")
	}
	defer rows.Close()

	var templates []*AgentTemplate
	for rows.Next() {
		var t AgentTemplate
		var createdAt string
		if err := rows.Scan(&t.Repo, &t.Nickname, &createdAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan agent template row")
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		templates = append(templates, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating agent template rows")
	}
	return templates, nil
}

// GetAgentTemplate returns a single agent template by repo name.
func (db *DB) GetAgentTemplate(repo string) (*AgentTemplate, error) {
	row := db.conn.QueryRow("SELECT repo, nickname, created_at FROM agent_templates WHERE repo = ?", repo)

	var t AgentTemplate
	var createdAt string
	if err := row.Scan(&t.Repo, &t.Nickname, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, stacktrace.NewError("agent template '%s' not found", repo)
		}
		return nil, stacktrace.Propagate(err, "failed to get agent template '%s'", repo)
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &t, nil
}

// DeleteAgentTemplate removes an agent template record by repo name.
func (db *DB) DeleteAgentTemplate(repo string) error {
	result, err := db.conn.Exec("DELETE FROM agent_templates WHERE repo = ?", repo)
	if err != nil {
		return stacktrace.Propagate(err, "failed to delete agent template '%s'", repo)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return stacktrace.Propagate(err, "failed to check rows affected")
	}
	if rowsAffected == 0 {
		return stacktrace.NewError("agent template '%s' not found", repo)
	}
	return nil
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

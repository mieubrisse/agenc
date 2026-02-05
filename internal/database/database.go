package database

import (
	"database/sql"
	"fmt"
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

const dropMissionDescriptionsTableSQL = `DROP TABLE IF EXISTS mission_descriptions;`

const addGitRepoColumnSQL = `ALTER TABLE missions ADD COLUMN git_repo TEXT NOT NULL DEFAULT '';`
const addLastHeartbeatColumnSQL = `ALTER TABLE missions ADD COLUMN last_heartbeat TEXT;`
const addShortIDColumnSQL = `ALTER TABLE missions ADD COLUMN short_id TEXT NOT NULL DEFAULT '';`
const backfillShortIDSQL = `UPDATE missions SET short_id = SUBSTR(id, 1, 8) WHERE short_id = '';`
const createShortIDIndexSQL = `CREATE INDEX IF NOT EXISTS idx_missions_short_id ON missions(short_id);`
const addSessionNameColumnSQL = `ALTER TABLE missions ADD COLUMN session_name TEXT NOT NULL DEFAULT '';`
const addSessionNameUpdatedAtColumnSQL = `ALTER TABLE missions ADD COLUMN session_name_updated_at TEXT;`
const addCronIDColumnSQL = `ALTER TABLE missions ADD COLUMN cron_id TEXT;`
const addCronNameColumnSQL = `ALTER TABLE missions ADD COLUMN cron_name TEXT;`

// Mission represents a row in the missions table.
type Mission struct {
	ID                   string
	ShortID              string
	AgentTemplate        string
	Prompt               string
	Status               string
	GitRepo              string
	LastHeartbeat        *time.Time
	SessionName          string
	SessionNameUpdatedAt *time.Time
	CronID               *string
	CronName             *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

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

	// Drop legacy mission_descriptions table
	if _, err := conn.Exec(dropMissionDescriptionsTableSQL); err != nil {
		conn.Close()
		return nil, stacktrace.Propagate(err, "failed to drop mission_descriptions table")
	}

	return &DB{conn: conn}, nil
}

// getColumnNames returns a set of column names present in the missions table.
func getColumnNames(conn *sql.DB) (map[string]bool, error) {
	rows, err := conn.Query("PRAGMA table_info(missions)")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read missions table info")
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan table_info row")
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating table_info rows")
	}
	return columns, nil
}

// migrateWorktreeSourceToGitRepo ensures the missions table has a git_repo
// column. Handles three states idempotently:
//   - Neither column exists (new DB): adds git_repo
//   - worktree_source exists (old DB): renames it to git_repo
//   - git_repo already exists (already migrated): no-op
func migrateWorktreeSourceToGitRepo(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	switch {
	case columns["git_repo"]:
		return nil // already migrated
	case columns["worktree_source"]:
		_, err := conn.Exec("ALTER TABLE missions RENAME COLUMN worktree_source TO git_repo")
		return err
	default:
		_, err := conn.Exec(addGitRepoColumnSQL)
		return err
	}
}

// migrateAddLastHeartbeat idempotently adds the last_heartbeat column.
func migrateAddLastHeartbeat(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if columns["last_heartbeat"] {
		return nil
	}

	_, err = conn.Exec(addLastHeartbeatColumnSQL)
	return err
}

// migrateAddShortID idempotently adds the short_id column, backfills it from
// existing IDs, and creates an index.
func migrateAddShortID(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if columns["short_id"] {
		return nil
	}

	if _, err := conn.Exec(addShortIDColumnSQL); err != nil {
		return stacktrace.Propagate(err, "failed to add short_id column")
	}
	if _, err := conn.Exec(backfillShortIDSQL); err != nil {
		return stacktrace.Propagate(err, "failed to backfill short_id column")
	}
	if _, err := conn.Exec(createShortIDIndexSQL); err != nil {
		return stacktrace.Propagate(err, "failed to create short_id index")
	}
	return nil
}

// migrateAddSessionName idempotently adds the session_name and
// session_name_updated_at columns for caching resolved session names.
func migrateAddSessionName(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if !columns["session_name"] {
		if _, err := conn.Exec(addSessionNameColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add session_name column")
		}
	}

	if !columns["session_name_updated_at"] {
		if _, err := conn.Exec(addSessionNameUpdatedAtColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add session_name_updated_at column")
		}
	}

	return nil
}

// migrateAddCronColumns idempotently adds the cron_id and cron_name columns
// for tracking cron-spawned missions.
func migrateAddCronColumns(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if !columns["cron_id"] {
		if _, err := conn.Exec(addCronIDColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add cron_id column")
		}
	}

	if !columns["cron_name"] {
		if _, err := conn.Exec(addCronNameColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add cron_name column")
		}
	}

	return nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// CreateMissionParams holds optional parameters for creating a mission.
type CreateMissionParams struct {
	CronID   *string
	CronName *string
}

// CreateMission inserts a new mission and returns it.
func (db *DB) CreateMission(agentTemplate string, gitRepo string, params *CreateMissionParams) (*Mission, error) {
	id := uuid.New().String()
	shortID := ShortID(id)
	now := time.Now().UTC().Format(time.RFC3339)

	var cronID, cronName *string
	if params != nil {
		cronID = params.CronID
		cronName = params.CronName
	}

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, short_id, agent_template, git_repo, status, cron_id, cron_name, created_at, updated_at) VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?)",
		id, shortID, agentTemplate, gitRepo, cronID, cronName, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert mission")
	}

	return &Mission{
		ID:            id,
		ShortID:       shortID,
		AgentTemplate: agentTemplate,
		GitRepo:       gitRepo,
		Status:        "active",
		CronID:        cronID,
		CronName:      cronName,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}, nil
}

// ListMissionsParams holds optional parameters for filtering missions.
type ListMissionsParams struct {
	IncludeArchived bool
	CronID          *string // If set, filter to missions with this cron_id
}

// ListMissions returns missions ordered by last_heartbeat DESC (most recently
// active first), with missions that have never sent a heartbeat sorted to the
// end by created_at DESC.
// If params.IncludeArchived is true, all missions are returned; otherwise archived missions are excluded.
// If params.CronID is set, only missions with that cron_id are returned.
func (db *DB) ListMissions(params ListMissionsParams) ([]*Mission, error) {
	query := "SELECT id, short_id, agent_template, prompt, status, git_repo, last_heartbeat, session_name, session_name_updated_at, cron_id, cron_name, created_at, updated_at FROM missions"

	var conditions []string
	var args []interface{}

	if !params.IncludeArchived {
		conditions = append(conditions, "status != 'archived'")
	}
	if params.CronID != nil {
		conditions = append(conditions, "cron_id = ?")
		args = append(args, *params.CronID)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY last_heartbeat IS NULL, last_heartbeat DESC, created_at DESC"

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query missions")
	}
	defer rows.Close()

	return scanMissions(rows)
}

// GetMission returns a single mission by ID.
func (db *DB) GetMission(id string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, short_id, agent_template, prompt, status, git_repo, last_heartbeat, session_name, session_name_updated_at, cron_id, cron_name, created_at, updated_at FROM missions WHERE id = ?",
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

// GetMostRecentMissionForCron returns the most recent mission for a cron job,
// or nil if no mission exists for the cron.
func (db *DB) GetMostRecentMissionForCron(cronID string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, short_id, agent_template, prompt, status, git_repo, last_heartbeat, session_name, session_name_updated_at, cron_id, cron_name, created_at, updated_at FROM missions WHERE cron_id = ? ORDER BY created_at DESC LIMIT 1",
		cronID,
	)

	mission, err := scanMission(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get most recent mission for cron '%s'", cronID)
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

// UpdateMissionPrompt sets the cached first-user-prompt for a mission.
func (db *DB) UpdateMissionPrompt(id string, prompt string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE missions SET prompt = ?, updated_at = ? WHERE id = ?",
		prompt, now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update prompt for mission '%s'", id)
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

// UpdateMissionSessionName caches the resolved session name for a mission and
// sets session_name_updated_at to the current time. This is an internal cache
// update, so it does not touch updated_at.
func (db *DB) UpdateMissionSessionName(id string, sessionName string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE missions SET session_name = ?, session_name_updated_at = ? WHERE id = ?",
		sessionName, now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update session name for mission '%s'", id)
	}
	return nil
}

func scanMissions(rows *sql.Rows) ([]*Mission, error) {
	var missions []*Mission
	for rows.Next() {
		var m Mission
		var lastHeartbeat, sessionNameUpdatedAt, cronID, cronName sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.ShortID, &m.AgentTemplate, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission row")
		}
		if lastHeartbeat.Valid {
			t, _ := time.Parse(time.RFC3339, lastHeartbeat.String)
			m.LastHeartbeat = &t
		}
		if sessionNameUpdatedAt.Valid {
			t, _ := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
			m.SessionNameUpdatedAt = &t
		}
		if cronID.Valid {
			m.CronID = &cronID.String
		}
		if cronName.Valid {
			m.CronName = &cronName.String
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
	var lastHeartbeat, sessionNameUpdatedAt, cronID, cronName sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&m.ID, &m.ShortID, &m.AgentTemplate, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if lastHeartbeat.Valid {
		t, _ := time.Parse(time.RFC3339, lastHeartbeat.String)
		m.LastHeartbeat = &t
	}
	if sessionNameUpdatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
		m.SessionNameUpdatedAt = &t
	}
	if cronID.Valid {
		m.CronID = &cronID.String
	}
	if cronName.Valid {
		m.CronName = &cronName.String
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &m, nil
}

// ShortID returns the first 8 characters of a full UUID.
func ShortID(fullID string) string {
	return fullID[:8]
}

// ResolveMissionID resolves a user-provided mission identifier (either a full
// UUID or an 8-character short ID) to the full mission UUID. Returns an error
// if the identifier matches zero or multiple missions.
func (db *DB) ResolveMissionID(userInput string) (string, error) {
	// Try exact match on full ID first (O(1) via primary key)
	var fullID string
	err := db.conn.QueryRow("SELECT id FROM missions WHERE id = ?", userInput).Scan(&fullID)
	if err == nil {
		return fullID, nil
	}
	if err != sql.ErrNoRows {
		return "", stacktrace.Propagate(err, "failed to query mission by full ID")
	}

	// Try match on short_id (O(1) via index)
	rows, err := db.conn.Query("SELECT id, prompt FROM missions WHERE short_id = ?", userInput)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to query mission by short ID")
	}
	defer rows.Close()

	type match struct {
		id     string
		prompt string
	}
	var matches []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.prompt); err != nil {
			return "", stacktrace.Propagate(err, "failed to scan mission row")
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return "", stacktrace.Propagate(err, "error iterating mission rows")
	}

	switch len(matches) {
	case 0:
		return "", stacktrace.NewError("mission '%s' not found", userInput)
	case 1:
		return matches[0].id, nil
	default:
		var lines []string
		for _, m := range matches {
			snippet := m.prompt
			if len(snippet) > 60 {
				snippet = snippet[:57] + "..."
			}
			lines = append(lines, fmt.Sprintf("  %s  %s", m.id, snippet))
		}
		return "", stacktrace.NewError(
			"short ID '%s' is ambiguous; matches %d missions:\n%s\nPlease provide more of the UUID to disambiguate.",
			userInput, len(matches), strings.Join(lines, "\n"),
		)
	}
}

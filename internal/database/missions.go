package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mieubrisse/stacktrace"
)

// Mission represents a row in the missions table.
type Mission struct {
	ID                     string
	ShortID                string
	Prompt                 string
	Status                 string
	GitRepo                string
	LastHeartbeat          *time.Time
	LastActive             *time.Time
	SessionName            string
	SessionNameUpdatedAt   *time.Time
	CronID                 *string
	CronName               *string
	ConfigCommit           *string
	TmuxPane               *string
	PromptCount            int
	LastSummaryPromptCount int
	AISummary              string
	TmuxWindowTitle        string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

// CreateMissionParams holds optional parameters for creating a mission.
type CreateMissionParams struct {
	CronID       *string
	CronName     *string
	ConfigCommit *string
}

// ListMissionsParams holds optional parameters for filtering missions.
type ListMissionsParams struct {
	IncludeArchived bool
	CronID          *string // If set, filter to missions with this cron_id
}

// CreateMission inserts a new mission and returns it.
func (db *DB) CreateMission(gitRepo string, params *CreateMissionParams) (*Mission, error) {
	id := uuid.New().String()
	shortID := ShortID(id)
	now := time.Now().UTC().Format(time.RFC3339)

	var cronID, cronName, configCommit *string
	if params != nil {
		cronID = params.CronID
		cronName = params.CronName
		configCommit = params.ConfigCommit
	}

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, short_id, git_repo, status, cron_id, cron_name, config_commit, created_at, updated_at) VALUES (?, ?, ?, 'active', ?, ?, ?, ?, ?)",
		id, shortID, gitRepo, cronID, cronName, configCommit, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert mission")
	}

	return &Mission{
		ID:           id,
		ShortID:      shortID,
		GitRepo:      gitRepo,
		Status:       "active",
		CronID:       cronID,
		CronName:     cronName,
		ConfigCommit: configCommit,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

// ListMissions returns missions ordered by the most recent activity timestamp
// (newest of last_active, last_heartbeat, or created_at) descending.
// If params.IncludeArchived is true, all missions are returned; otherwise archived missions are excluded.
// If params.CronID is set, only missions with that cron_id are returned.
func (db *DB) ListMissions(params ListMissionsParams) ([]*Mission, error) {
	query, args := buildListMissionsQuery(params)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query missions")
	}
	defer rows.Close()

	return scanMissions(rows)
}

// GetMission returns a single mission by ID.
// Returns (nil, nil) if the mission is not found.
// Returns (nil, error) only for actual database failures.
func (db *DB) GetMission(id string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, short_id, prompt, status, git_repo, last_heartbeat, last_active, session_name, session_name_updated_at, cron_id, cron_name, config_commit, tmux_pane, prompt_count, last_summary_prompt_count, ai_summary, tmux_window_title, created_at, updated_at FROM missions WHERE id = ?",
		id,
	)

	mission, err := scanMission(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get mission '%s'", id)
	}
	return mission, nil
}

// GetMostRecentMissionForCron returns the most recent mission for a cron job,
// or nil if no mission exists for the cron. This function queries by cron_name
// to check if there is a running mission for the cron (for double-fire prevention).
func (db *DB) GetMostRecentMissionForCron(cronName string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, short_id, prompt, status, git_repo, last_heartbeat, last_active, session_name, session_name_updated_at, cron_id, cron_name, config_commit, tmux_pane, prompt_count, last_summary_prompt_count, ai_summary, tmux_window_title, created_at, updated_at FROM missions WHERE cron_name = ? ORDER BY created_at DESC LIMIT 1",
		cronName,
	)

	mission, err := scanMission(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get most recent mission for cron '%s'", cronName)
	}
	return mission, nil
}

// GetMissionByTmuxPane returns the active mission associated with the given
// tmux pane ID, or nil if no active mission is running in that pane.
func (db *DB) GetMissionByTmuxPane(paneID string) (*Mission, error) {
	row := db.conn.QueryRow(
		"SELECT id, short_id, prompt, status, git_repo, last_heartbeat, last_active, session_name, session_name_updated_at, cron_id, cron_name, config_commit, tmux_pane, prompt_count, last_summary_prompt_count, ai_summary, tmux_window_title, created_at, updated_at FROM missions WHERE tmux_pane = ? AND status = 'active' LIMIT 1",
		paneID,
	)

	mission, err := scanMission(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get mission by tmux pane '%s'", paneID)
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

// UpdateLastActive sets the last_active timestamp to the current time for
// the given mission. Called by the wrapper when the user submits a prompt.
func (db *DB) UpdateLastActive(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE missions SET last_active = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update last_active for mission '%s'", id)
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

// UpdateMissionConfigCommit updates the config_commit column for a mission.
func (db *DB) UpdateMissionConfigCommit(id string, configCommit string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE missions SET config_commit = ?, updated_at = ? WHERE id = ?",
		configCommit, now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update config_commit for mission '%s'", id)
	}
	return nil
}

// SetTmuxPane records the tmux pane ID for a mission's wrapper process.
func (db *DB) SetTmuxPane(id string, paneID string) error {
	_, err := db.conn.Exec(
		"UPDATE missions SET tmux_pane = ? WHERE id = ?",
		paneID, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to set tmux pane for mission '%s'", id)
	}
	return nil
}

// ClearTmuxPane removes the tmux pane association for a mission.
func (db *DB) ClearTmuxPane(id string) error {
	_, err := db.conn.Exec(
		"UPDATE missions SET tmux_pane = NULL WHERE id = ?",
		id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to clear tmux pane for mission '%s'", id)
	}
	return nil
}

// IncrementPromptCount atomically increments the prompt_count for a mission.
// Called by the wrapper on each UserPromptSubmit hook event.
func (db *DB) IncrementPromptCount(id string) error {
	_, err := db.conn.Exec(
		"UPDATE missions SET prompt_count = prompt_count + 1 WHERE id = ?",
		id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to increment prompt count for mission '%s'", id)
	}
	return nil
}

// UpdateAISummary updates the AI-generated summary and sets
// last_summary_prompt_count to the current prompt_count so the mission won't
// be re-summarized until enough new prompts accumulate.
func (db *DB) UpdateAISummary(id string, summary string) error {
	_, err := db.conn.Exec(
		"UPDATE missions SET ai_summary = ?, last_summary_prompt_count = prompt_count WHERE id = ?",
		summary, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update AI summary for mission '%s'", id)
	}
	return nil
}

// GetMissionAISummary returns the AI-generated summary for a mission.
func (db *DB) GetMissionAISummary(id string) (string, error) {
	var summary string
	err := db.conn.QueryRow("SELECT ai_summary FROM missions WHERE id = ?", id).Scan(&summary)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to get AI summary for mission '%s'", id)
	}
	return summary, nil
}

// GetMissionTmuxWindowTitle returns the tmux window title that AgenC last set
// for this mission. Returns "" if never set. Used to detect user renames
// before applying automatic title updates.
func (db *DB) GetMissionTmuxWindowTitle(id string) (string, error) {
	var title string
	err := db.conn.QueryRow("SELECT tmux_window_title FROM missions WHERE id = ?", id).Scan(&title)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to get tmux window title for mission '%s'", id)
	}
	return title, nil
}

// SetMissionTmuxWindowTitle records the exact string AgenC sent to
// tmux rename-window for this mission. Called after every successful rename
// so the wrapper can detect if the user has changed the title themselves.
func (db *DB) SetMissionTmuxWindowTitle(id string, title string) error {
	_, err := db.conn.Exec(
		"UPDATE missions SET tmux_window_title = ? WHERE id = ?",
		title, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to set tmux window title for mission '%s'", id)
	}
	return nil
}

// ListMissionsNeedingSummary returns active missions where the number of new
// prompts since the last summarization meets or exceeds the given threshold.
func (db *DB) ListMissionsNeedingSummary(threshold int) ([]*Mission, error) {
	query := `SELECT id, short_id, prompt, status, git_repo, last_heartbeat, last_active,
		session_name, session_name_updated_at, cron_id, cron_name, config_commit, tmux_pane,
		prompt_count, last_summary_prompt_count, ai_summary, tmux_window_title, created_at, updated_at
		FROM missions WHERE status = 'active' AND (prompt_count - last_summary_prompt_count) >= ?`
	rows, err := db.conn.Query(query, threshold)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query missions needing summary")
	}
	defer rows.Close()
	return scanMissions(rows)
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

package database

import (
	"database/sql"

	"github.com/mieubrisse/stacktrace"
)

// SQL migration statements
const (
	createMissionsTableSQL             = `CREATE TABLE IF NOT EXISTS missions (id TEXT PRIMARY KEY, prompt TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL);`
	dropMissionDescriptionsTableSQL    = `DROP TABLE IF EXISTS mission_descriptions;`
	addGitRepoColumnSQL                = `ALTER TABLE missions ADD COLUMN git_repo TEXT NOT NULL DEFAULT '';`
	addLastHeartbeatColumnSQL          = `ALTER TABLE missions ADD COLUMN last_heartbeat TEXT;`
	addShortIDColumnSQL                = `ALTER TABLE missions ADD COLUMN short_id TEXT NOT NULL DEFAULT '';`
	backfillShortIDSQL                 = `UPDATE missions SET short_id = SUBSTR(id, 1, 8) WHERE short_id = '';`
	createShortIDIndexSQL              = `CREATE INDEX IF NOT EXISTS idx_missions_short_id ON missions(short_id);`
	addSessionNameColumnSQL            = `ALTER TABLE missions ADD COLUMN session_name TEXT NOT NULL DEFAULT '';`
	addSessionNameUpdatedAtColumnSQL   = `ALTER TABLE missions ADD COLUMN session_name_updated_at TEXT;`
	addCronIDColumnSQL                 = `ALTER TABLE missions ADD COLUMN cron_id TEXT;`
	addCronNameColumnSQL               = `ALTER TABLE missions ADD COLUMN cron_name TEXT;`
	addConfigCommitColumnSQL           = `ALTER TABLE missions ADD COLUMN config_commit TEXT;`
	addTmuxPaneColumnSQL               = `ALTER TABLE missions ADD COLUMN tmux_pane TEXT;`
	addLastActiveColumnSQL             = `ALTER TABLE missions ADD COLUMN last_active TEXT;`
	addPromptCountColumnSQL            = `ALTER TABLE missions ADD COLUMN prompt_count INTEGER NOT NULL DEFAULT 0;`
	addLastSummaryPromptCountColumnSQL = `ALTER TABLE missions ADD COLUMN last_summary_prompt_count INTEGER NOT NULL DEFAULT 0;`
	addAISummaryColumnSQL              = `ALTER TABLE missions ADD COLUMN ai_summary TEXT NOT NULL DEFAULT '';`
)

// stripTmuxPanePercentSQL removes the leading "%" from tmux_pane values that
// were stored with the $TMUX_PANE format (%42) rather than the canonical
// number-only format (42) used by tmux's #{pane_id} format variable.
// REPLACE is idempotent — values already without "%" are unaffected.
const stripTmuxPanePercentSQL = `UPDATE missions SET tmux_pane = REPLACE(tmux_pane, '%', '') WHERE tmux_pane IS NOT NULL;`

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

// migrateAddConfigCommit idempotently adds the config_commit column
// for tracking which config source commit was used when the mission was created.
func migrateAddConfigCommit(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if columns["config_commit"] {
		return nil
	}

	_, err = conn.Exec(addConfigCommitColumnSQL)
	return err
}

// migrateAddTmuxPane idempotently adds the tmux_pane column for tracking
// which tmux pane a mission's wrapper is running in.
func migrateAddTmuxPane(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if columns["tmux_pane"] {
		return nil
	}

	_, err = conn.Exec(addTmuxPaneColumnSQL)
	return err
}

// migrateAddLastActive idempotently adds the last_active column for tracking
// when a user last submitted a prompt to a mission's Claude session.
func migrateAddLastActive(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if columns["last_active"] {
		return nil
	}

	_, err = conn.Exec(addLastActiveColumnSQL)
	return err
}

// migrateAddAISummary idempotently adds the prompt_count,
// last_summary_prompt_count, and ai_summary columns for daemon-driven
// AI mission summarization.
func migrateAddAISummary(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if !columns["prompt_count"] {
		if _, err := conn.Exec(addPromptCountColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add prompt_count column")
		}
	}

	if !columns["last_summary_prompt_count"] {
		if _, err := conn.Exec(addLastSummaryPromptCountColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add last_summary_prompt_count column")
		}
	}

	if !columns["ai_summary"] {
		if _, err := conn.Exec(addAISummaryColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add ai_summary column")
		}
	}

	return nil
}

// migrateAddQueryIndices idempotently adds database indices to improve query
// performance for common operations: activity-based sorting, tmux pane lookup,
// and summary eligibility checks.
func migrateAddQueryIndices(conn *sql.DB) error {
	indices := []string{
		"CREATE INDEX IF NOT EXISTS idx_missions_activity ON missions(last_active DESC, last_heartbeat DESC)",
		"CREATE INDEX IF NOT EXISTS idx_missions_tmux_pane ON missions(tmux_pane) WHERE tmux_pane IS NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_missions_summary ON missions(status, prompt_count, last_summary_prompt_count)",
	}

	for _, indexSQL := range indices {
		if _, err := conn.Exec(indexSQL); err != nil {
			return stacktrace.Propagate(err, "failed to create index")
		}
	}

	return nil
}

// migrateDropAgentTemplate idempotently drops the agent_template column
// from the missions table. Agent templates have been removed from AgenC.
func migrateDropAgentTemplate(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if !columns["agent_template"] {
		return nil
	}

	_, err = conn.Exec("ALTER TABLE missions DROP COLUMN agent_template")
	return err
}

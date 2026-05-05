package database

import (
	"strings"
	"time"
)

// buildListMissionsQuery constructs the SQL query and arguments for ListMissions.
// Returns the query string and a slice of arguments to be used with db.Query.
func buildListMissionsQuery(params ListMissionsParams) (string, []interface{}) {
	query := "SELECT id, short_id, prompt, status, git_repo, last_heartbeat, last_user_prompt_at, session_name, session_name_updated_at, cron_id, cron_name, config_commit, tmux_pane, prompt_count, created_at, updated_at, source, source_id, source_metadata FROM missions"

	var conditions []string
	var args []interface{}

	if !params.IncludeArchived {
		conditions = append(conditions, "status != 'archived'")
	}
	if params.Source != nil {
		conditions = append(conditions, "source = ?")
		args = append(args, *params.Source)
	}
	if params.SourceID != nil {
		conditions = append(conditions, "source_id = ?")
		args = append(args, *params.SourceID)
	}
	if params.Since != nil {
		conditions = append(conditions, "created_at >= ?")
		args = append(args, params.Since.UTC().Format(time.RFC3339))
	}
	if params.Until != nil {
		conditions = append(conditions, "created_at <= ?")
		args = append(args, params.Until.UTC().Format(time.RFC3339))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY COALESCE(last_heartbeat, created_at) DESC"

	return query, args
}

// buildListNotificationsQuery constructs the SQL query and arguments for
// ListNotifications. Returns the query string and a slice of arguments to be
// used with db.Query.
func buildListNotificationsQuery(params ListNotificationsParams) (string, []interface{}) {
	query := "SELECT id, kind, source_repo, title, body_markdown, created_at, read_at FROM notifications"

	var conditions []string
	var args []interface{}

	if params.UnreadOnly {
		conditions = append(conditions, "read_at IS NULL")
	}
	if params.SourceRepo != "" {
		conditions = append(conditions, "source_repo = ?")
		args = append(args, params.SourceRepo)
	}
	if params.Kind != "" {
		conditions = append(conditions, "kind = ?")
		args = append(args, params.Kind)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC"

	return query, args
}

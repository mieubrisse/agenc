package database

import (
	"strings"
)

// buildListMissionsQuery constructs the SQL query and arguments for ListMissions.
// Returns the query string and a slice of arguments to be used with db.Query.
func buildListMissionsQuery(params ListMissionsParams) (string, []interface{}) {
	query := "SELECT id, short_id, prompt, status, git_repo, last_heartbeat, last_active, session_name, session_name_updated_at, cron_id, cron_name, config_commit, tmux_pane, prompt_count, last_summary_prompt_count, ai_summary, tmux_window_title, created_at, updated_at FROM missions"

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
	query += " ORDER BY COALESCE(last_active, last_heartbeat, created_at) DESC"

	return query, args
}

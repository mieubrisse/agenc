Cron Logs Print Feature
=======================

Print cron job logs via a server endpoint so the CLI doesn't read the filesystem directly.

Motivation
----------

Users currently have to filesystem-hunt for cron logs at `~/.agenc/logs/crons/<id>.log`.
This feature exposes cron logs through the server API, consistent with the broader goal
of making the server the single owner of `~/.agenc/` file access (see agenc-4ex2, agenc-p2op).

Endpoints
---------

### `GET /crons`

Returns the list of configured cron jobs with their name-to-ID mapping.

Response: `application/json`

```json
[
  {
    "name": "daily-report",
    "id": "abc-123",
    "schedule": "0 9 * * *",
    "repo": "mieubrisse/my-repo"
  }
]
```

Response type:

```go
type CronInfo struct {
    Name     string `json:"name"`
    ID       string `json:"id"`
    Schedule string `json:"schedule"`
    Repo     string `json:"repo,omitempty"`
}
```

### `GET /crons/{id}/logs?mode=tail|all`

Returns the log file content for a cron job.

- `mode=tail` (default): last 200 lines
- `mode=all`: entire file
- 404 if log file doesn't exist yet

Response: `text/plain`

CLI Commands
------------

### `agenc cron logs print <name-or-id> [--all]`

- Calls `GET /crons` to resolve name → ID
- If the arg doesn't match any name, treats it as a UUID
- Calls `GET /crons/{id}/logs` and writes output to stdout
- Friendly error messages for missing crons and missing log files

### `agenc cron history` (updated)

- Switches from `readConfig()` to `client.ListCrons()` for name-to-ID resolution
- No behavior change for the user

New Files
---------

- `internal/server/handle_crons.go` — `GET /crons` handler
- `internal/server/handle_cron_logs.go` — `GET /crons/{id}/logs` handler
- `internal/server/handle_cron_logs_test.go` — tail/all/404 tests
- `internal/server/handle_crons_test.go` — response shape tests
- `cmd/cron_logs.go` — parent `cron logs` subcommand
- `cmd/cron_logs_print.go` — `agenc cron logs print` command

Modified Files
--------------

- `internal/server/server.go` — register two new routes
- `internal/server/client.go` — add `ListCrons()` and `GetCronLogs()` methods
- `cmd/cron_history.go` — switch from `readConfig()` to `client.ListCrons()`

Error Handling
--------------

| Scenario | Location | Behavior |
|----------|----------|----------|
| Name not found, arg not a valid UUID | CLI | `cron job 'foo' not found` |
| Cron has no ID | CLI | `cron job 'foo' has no ID` |
| Log file missing | Server 404 | `no logs found — cron may not have run yet` |
| ID not in config but log file exists | Server serves it | Allows viewing logs for deleted crons |

Out of Scope
------------

- `--follow` / streaming (no existing machinery)
- Per-run log isolation (use the mission for that)
- Full cron CRUD via server (tracked in agenc-p2op)

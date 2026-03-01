Rename Session
==============

Problem
-------

Users have no way to rename their mission's tmux window while Claude is working.
Claude's `/rename` command sets `custom_title` on the session via JSONL metadata,
but this requires Claude to be idle and accept input. The user needs a way to set
a window title at any time, independent of the Claude process.

Design
------

Add an `agenc_custom_title` column to the `sessions` table. This is a
user-controlled title set via the CLI, distinct from the Claude-set
`custom_title`. The window title reconciliation prioritizes Claude's
`custom_title` over `agenc_custom_title`, so Claude's `/rename` always wins if
both are set.

### Title Priority (updated)

```
1. custom_title         (Claude /rename via JSONL)     ← highest, unchanged
2. agenc_custom_title   (user rename via CLI)          ← NEW
3. auto_summary         (scanner / summarizer)         ← unchanged
4. repo short name      (from git_repo)                ← unchanged
5. mission short ID     (fallback)                     ← unchanged
```

### Database

Add migration to `sessions` table:

```sql
ALTER TABLE sessions ADD COLUMN agenc_custom_title TEXT NOT NULL DEFAULT '';
```

Add `AgencCustomTitle string` to the `Session` struct. Update all scan functions
(`scanSession`, `scanSessions`) and queries to include the new column.

New DB function:

- `UpdateSessionAgencCustomTitle(sessionID, title string) error` — sets
  `agenc_custom_title` and `updated_at`

### Server Endpoints

Two new routes on the HTTP server:

**`GET /sessions?mission_id={id}`** — List sessions for a mission.

- `mission_id` is required (400 if missing)
- Resolves mission ID via `ResolveMissionID` (supports short IDs)
- Returns `[]SessionResponse` ordered by `updated_at` descending

**`PATCH /sessions/{id}`** — Update a session.

- Accepts `UpdateSessionRequest`:
  ```json
  {"agenc_custom_title": "my title"}
  ```
- Updates `agenc_custom_title` in the database (empty string clears it)
- Looks up the session's `mission_id` to trigger `reconcileTmuxWindowTitle`
- Returns `{"status": "updated"}`

New types:

```go
type SessionResponse struct {
    ID               string    `json:"id"`
    MissionID        string    `json:"mission_id"`
    CustomTitle      string    `json:"custom_title"`
    AgencCustomTitle string    `json:"agenc_custom_title"`
    AutoSummary      string    `json:"auto_summary"`
    CreatedAt        time.Time `json:"created_at"`
    UpdatedAt        time.Time `json:"updated_at"`
}

type UpdateSessionRequest struct {
    AgencCustomTitle *string `json:"agenc_custom_title,omitempty"`
}
```

New client methods:

- `ListMissionSessions(missionID string) ([]*database.Session, error)`
- `UpdateSession(sessionID string, req UpdateSessionRequest) error`

### CLI Commands

**`agenc session rename [session-id] [title]`**

- If `session-id` not provided: error with usage
- If `title` not provided: prompt via stdin (`bufio.NewReader` + `ReadString('\n')`)
- Empty input clears `agenc_custom_title`
- Calls `PATCH /sessions/{id}` with the title

**`agenc mission rename [mission-id] [title]`**

- Sugar that resolves the active session and delegates to session rename logic
- If `mission-id` not provided: reads `$AGENC_CALLING_MISSION_UUID` env var;
  errors if also empty
- Resolves active session via `GET /sessions?mission_id={id}` (first result)
- If no sessions found: error "no sessions found for mission"
- If `title` not provided: prompt via stdin
- Calls `PATCH /sessions/{active-session-id}`

### Palette Command

New builtin command in `BuiltinPaletteCommands`:

```go
"renameSession": {
    Title:       "✨  Rename Session",
    Description: "Rename the focused mission's window",
    Command:     "agenc mission rename $AGENC_CALLING_MISSION_UUID",
},
```

Insert into `builtinPaletteCommandOrder` after `copyMissionUuid` (grouping it
with other mission-context commands).

The palette executes this in a tmux popup. The `mission rename` command prompts
for the title since no title argument is given. The popup closes when the command
exits.

### Title Reconciliation

Update `determineBestTitle()` in `internal/server/tmux.go` to check
`agenc_custom_title` at priority 2:

```go
func determineBestTitle(activeSession *database.Session, mission *database.Mission) string {
    if activeSession != nil && activeSession.CustomTitle != "" {
        return activeSession.CustomTitle
    }
    if activeSession != nil && activeSession.AgencCustomTitle != "" {
        return activeSession.AgencCustomTitle
    }
    if activeSession != nil && activeSession.AutoSummary != "" {
        return activeSession.AutoSummary
    }
    // ... repo short name, mission short ID
}
```

Files Changed
-------------

- `internal/database/sessions.go` — New column, struct field, scan functions, new DB function
- `internal/database/migrations.go` — New migration SQL
- `internal/server/tmux.go` — Updated `determineBestTitle()` priority chain
- `internal/server/server.go` — Register two new routes
- `internal/server/sessions.go` — New file: session HTTP handlers, types
- `internal/server/client.go` — New client methods for session endpoints
- `internal/config/agenc_config.go` — New palette command in builtins
- `cmd/session.go` — New `session` command group
- `cmd/session_rename.go` — New `session rename` subcommand
- `cmd/mission_rename.go` — New `mission rename` subcommand
- `docs/system-architecture.md` — Update route table, session API docs

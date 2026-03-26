Sleep Mode
==========

Problem
-------

AgenC is always available. There is no mechanism to discourage the user from starting new work during hours they should be sleeping. Unlike YappBlocker (which kills distracting apps on a schedule), AgenC has no concept of a block window.

Decision
--------

Add a **sleep mode** feature: a configurable schedule of time windows during which the server refuses to create new missions or new cron jobs. Cron-triggered missions are exempt — existing scheduled work continues, but the user cannot start new work or create new scheduled work.

The schedule format matches YappBlocker's: a list of windows, each with days of the week and start/end times in HH:MM format. Overnight windows are supported.

There is no escape hatch. To disable sleep mode, the user removes all windows.

Design
------

### Config

New `sleepMode` section in `config.yml`:

```yaml
sleepMode:
  windows:
    - days: [mon, tue, wed, thu]
      start: "22:00"
      end: "06:00"
    - days: [fri, sat]
      start: "23:00"
      end: "07:00"
```

Empty or missing `sleepMode` (or empty `windows` list) means sleep mode is disabled. No separate enable/disable flag.

### New package: `internal/sleep/`

Pure library with no server dependency. Contains:

- `WindowDef` struct: `Days []string`, `Start string`, `End string`
- `IsActive(windows []WindowDef, now time.Time) bool` — returns true if the current time falls within any window
- `ValidateDays(days []string) error` — validates day name abbreviations
- `ValidateTime(t string) error` — validates HH:MM format, range checks

Time-matching logic is ported from YappBlocker's `internal/schedule/schedule.go`:
- Same-day windows: active if today is a listed day and time is within [start, end)
- Overnight windows (start >= end): active if (today is listed and time >= start) OR (yesterday is listed and time < end)
- `start == end` is rejected — a 24h block is not useful for sleep mode

### Config type changes in `internal/config/agenc_config.go`

New field on `AgencConfig`:

```go
type SleepModeConfig struct {
    Windows []sleep.WindowDef `yaml:"windows"`
}

type AgencConfig struct {
    // ... existing fields ...
    SleepMode *SleepModeConfig `yaml:"sleepMode"`
}
```

Validated during `ReadAgencConfig` via `validateSleepMode()` — calls into `sleep.ValidateDays` and `sleep.ValidateTime` for each window.

### Server endpoints for sleep config

All validation is server-side. The CLI is a thin HTTP client.

- **`POST /config/sleep/windows`** — Add a window. Body: `{"days":["mon","tue"],"start":"22:00","end":"06:00"}`. Server validates, acquires config lock, reads config.yml, appends window, writes config.yml, releases lock, updates `cachedConfig`. Returns 201 with the updated windows list.
- **`DELETE /config/sleep/windows/{index}`** — Remove a window by zero-based index. Server validates index bounds, acquires config lock, reads config.yml, removes window, writes config.yml, releases lock, updates `cachedConfig`. Returns 200 with the updated windows list.
- **`GET /config/sleep/windows`** — List current windows. Reads from `cachedConfig` (no disk I/O). Returns 200 with the windows list.

Error responses follow existing `newHTTPError` pattern:
- Invalid day name → 400
- Bad time format → 400
- start == end → 400
- Index out of bounds → 404

### `sleepGuard` middleware in `internal/server/middleware.go`

Follows the `stashGuard` pattern:

```go
func (s *Server) sleepGuard(fn appHandlerFunc) appHandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) error {
        if s.isSleepModeActive(r) {
            return newHTTPError(http.StatusForbidden,
                "Sleep mode active until HH:MM — go to bed!")
        }
        return fn(w, r)
    }
}
```

`isSleepModeActive` reads `s.getConfig().SleepMode`, calls `sleep.IsActive(windows, time.Now())`. For `POST /missions`, it also checks the request body for `source == "cron"` and exempts cron-triggered missions.

The "until HH:MM" in the error message is computed from the active window's `end` time.

### Guarded endpoints

- `POST /missions` (exempt if `source == "cron"`)
- `POST /crons` (from agenc-wpkh, the cron CRUD refactor)

### Not guarded

- All GET/read endpoints
- `PATCH /crons`, `DELETE /crons` — editing or removing existing crons is fine
- Cron-triggered mission creation (exempt by source)
- Running missions — they keep running
- Sleep config endpoints — the user can always modify the schedule

### CLI commands

- `agenc config sleep add --days mon,tue,wed,thu --start 22:00 --end 06:00` — sends `POST /config/sleep/windows`
- `agenc config sleep rm <index>` — sends `DELETE /config/sleep/windows/{index}`
- `agenc config sleep ls` — sends `GET /config/sleep/windows`

CLI confirms actions with feedback:
- Add: `Added sleep window: mon,tue,wed,thu 22:00–06:00 (window 1)`
- Remove: `Removed sleep window 1`
- List with no windows: `No sleep windows configured`

When a guarded endpoint returns 403 during sleep mode, the CLI prints the message and exits non-zero.

### Hot-reload

No new infrastructure. The existing fsnotify config watcher picks up changes to config.yml and updates `cachedConfig`. The `sleepGuard` reads from `s.getConfig()` on every request, so changes take effect immediately.

### Dependencies

This feature depends on **agenc-wpkh** (Refactor cron CLI commands to use server CRUD endpoints). The sleep guard needs to wrap `POST /crons`, which does not exist until that refactor is complete.

The `internal/sleep/` package and the sleep config endpoints can be built independently. The `sleepGuard` wrapping of `POST /crons` is the only piece that requires agenc-wpkh to land first.

Testing
-------

- **`internal/sleep/` unit tests** — Same-day window active/inactive, overnight window with yesterday carry-over, boundary conditions (exactly at start = active, exactly at end = not active), unlisted day not active, `start == end` rejected, invalid day names rejected, bad time formats rejected.
- **`internal/config/` validation tests** — Invalid sleep mode configs rejected during `ReadAgencConfig`, valid configs round-trip through YAML.
- **Server middleware tests** — `sleepGuard` with injected `time.Time`: passes through when no sleep config, passes through outside window, blocks during window with 403, exempts cron source during window, error message includes correct end time.
- **Server endpoint tests** — `POST /config/sleep/windows` with valid/invalid bodies, `DELETE /config/sleep/windows/{index}` with valid/out-of-bounds index, `GET /config/sleep/windows` returns current state.
- **E2E test** — Add a sleep window covering "now", attempt `POST /missions`, verify 403. Remove window, verify 201.

Clock injection: `sleep.IsActive` accepts `time.Time` as a parameter. The `sleepGuard` passes `time.Now()` at the call site. Tests inject fixed times.

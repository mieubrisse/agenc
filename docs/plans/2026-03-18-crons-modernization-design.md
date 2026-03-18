Crons Modernization
====================

Status: approved
Date: 2026-03-18

Problem
-------

The crons feature is non-functional. It was built around assumptions that no longer
hold: `claude --print` mode, synchronous `agenc mission new`, and a pre-tmux-pool
architecture. The core infrastructure (launchd plist generation, CronSyncer, CLI
commands) exists but the end-to-end pipeline is broken because headless missions
never actually worked — the old code predates the tmux pool model.

The system also has unnecessary complexity: double-fire prevention that can't
reliably determine "Claude finished," cron-specific timeout enforcement, and
hook-based idle detection that's flaky (e.g., Ctrl-C/ESC during agent work doesn't
fire the Stop hook).

Design Principles
-----------------

1. **Cron missions are normal missions.** No special lifecycle, no special idle
   timeout, no special cleanup. A cron mission gets a tmux pool window like any
   other mission. The user can attach, detach, or ignore it.

2. **JSONL ModTime is the canonical idle signal.** The existing 30-minute idle
   timeout based on session JSONL file modification time applies uniformly to all
   missions. Hook-based `claudeIdle` is used for UI (window coloring) only, not
   lifecycle decisions.

3. **Remove complexity, don't add it.** No double-fire prevention, no cron-specific
   timeouts, no cron-specific idle policies.

Design
------

### What a cron run looks like

```
launchd fires at scheduled time (from plist in ~/Library/LaunchAgents/)
  → agenc mission new --headless --source cron --source-id <cronUUID> \
      --source-metadata '{"cron_name":"daily-sync"}' \
      --prompt "..." [repo]
    → ensureServerRunning() auto-starts server if needed
    → POST /missions to server
    → Server creates DB record with source/source_id/source_metadata
    → Server calls createPoolWindow() (no linked tmux session)
    → Wrapper starts in pool, runs Claude with prompt
    → Claude does work... JSONL gets written...
    → Claude finishes → JSONL stops updating → 30-min idle timeout → wrapper stopped
    → User can attach at any point via `mission attach`
```

### Database: generic mission source columns

Replace the cron-specific `cron_id` and `cron_name` columns with generic source
tracking:

```sql
ALTER TABLE missions ADD COLUMN source TEXT;
ALTER TABLE missions ADD COLUMN source_id TEXT;
ALTER TABLE missions ADD COLUMN source_metadata TEXT;  -- JSON

CREATE INDEX idx_missions_source ON missions(source, source_id, created_at DESC);
```

- `source`: the type of entity that created this mission (e.g., `"cron"`, and
  later `"user"`)
- `source_id`: indexed identifier for lookups (e.g., cron UUID)
- `source_metadata`: JSON blob for display/debugging (e.g.,
  `{"cron_name":"daily-sync"}` or `{"cron_name":"daily-sync","trigger":"manual"}`)

The old `cron_id` and `cron_name` columns are left in place but unused. A follow-up
bead tracks their removal.

Migration: any existing rows with `cron_id IS NOT NULL` get migrated to the new
columns. In practice there are no such rows.

### `mission new` flag changes

Remove:
- `--cron-id`
- `--cron-name`
- `--cron-trigger`

Add:
- `--source` (hidden, internal)
- `--source-id` (hidden, internal)
- `--source-metadata` (hidden, internal)

Remove double-fire prevention logic (`shouldSkipCronTrigger` and related code).

Remove `--timeout` flag and timeout enforcement.

### CronSyncer changes

`internal/server/cron_syncer.go` — update plist ProgramArguments to use new flags:

```
agenc mission new --headless \
  --source cron \
  --source-id <cronUUID> \
  --source-metadata '{"cron_name":"<name>"}' \
  --prompt "<prompt>" \
  [repo]
```

Add validation: refuse to sync crons that have no `id` field in config.yml. Log a
warning and skip.

### Plist log paths

Change from `$AGENC_DIRPATH/logs/crons/{cronName}-{stdout,stderr}.log` to:

```
$AGENC_DIRPATH/logs/crons/{cronID}.log
```

Single appending file per cron (launchd uses `O_APPEND`). Captures the minimal
`agenc mission new` stdout/stderr — useful for diagnosing launch failures. The
actual Claude work is visible via `mission attach`.

Update `config.GetCronLogFilepaths()` to use cron ID. The plist sets both
`StandardOutPath` and `StandardErrorPath` to the same file.

### CLI commands

| Command | Status | Notes |
|---------|--------|-------|
| `cron new` | Keep | Creates cron definition in config.yml. Generates UUID. |
| `cron ls` | Keep | Lists cron definitions with schedule, enabled, next run. |
| `cron rm <name>` | Keep | Removes cron definition, syncer removes plist. |
| `cron enable <name>` | Keep | Sets enabled in config.yml, syncer loads plist. |
| `cron disable <name>` | Keep | Sets disabled in config.yml, syncer unloads plist. |
| `cron run <name>` | Keep | Manual trigger. Tags mission with source=cron, source_metadata includes `"trigger":"manual"`. |
| `cron logs <name>` | **Remove** | No useful target — Claude output lives in tmux pane (use `mission attach`). Plist logs are for debugging only. |
| `cron history <name>` | Keep | Sugar for `mission ls` filtered by source=cron + source_id=cronUUID. |

### What does NOT change

- Wrapper lifecycle, idle timeout logic, heartbeats, hook handling
- Config.yml format for cron definitions (except `id` field is now validated)
- Attach/detach mechanics
- Launchd plist generation (`internal/launchd/plist.go`)
- Launchd manager (`internal/launchd/manager.go`)
- `ensurePoolSession()` auto-creates tmux pool session

### What gets deleted

- `shouldSkipCronTrigger()` and double-fire prevention logic in `mission_new.go`
- `--cron-id`, `--cron-name`, `--cron-trigger` flags
- `--timeout` flag and timeout enforcement
- `cmd/cron_logs.go`
- `GetMostRecentMissionForCron()` database function (if it exists)
- References to `cron_id`/`cron_name` columns in Go code (columns stay in DB)

Testing
-------

### Unit tests

- CronSyncer generates correct plist ProgramArguments with `--source`/`--source-id`/
  `--source-metadata` flags
- CronSyncer skips crons without UUID, logs warning
- Database migration populates source columns from legacy cron columns
- `cron history` correctly filters by source + source_id

### Integration / manual tests

- Add cron to config.yml → plist appears in ~/Library/LaunchAgents/ and is loaded
- Wait for scheduled fire → mission created with correct source columns, visible
  in tmux pool
- `mission attach` on cron mission → normal attach behavior
- `cron history <name>` → shows missions for this cron
- `cron run <name>` → creates mission tagged with source=cron + trigger=manual
- Disable cron → plist unloaded
- Remove cron → plist deleted
- Cron without UUID in config.yml → warning logged, cron skipped
- Server restart → plists re-synced on startup

### Hardest to test

The launchd → `agenc mission new` → server → tmux pool window pipeline. Best
validated with a fast-firing cron (every minute) during development.

Risks
-----

### ParseCronExpression limitations

launchd's `StartCalendarInterval` doesn't support `*/N`, ranges, or lists. The
current `ParseCronExpression` only handles literal values and `*`. Unsupported
expressions are logged and skipped. This is a known limitation documented in the
existing migration spec. Future enhancement: generate multiple
`StartCalendarInterval` dicts for simple intervals.

### launchd fires without tmux

If the user's machine reboots and launchd fires before a terminal is open,
`ensurePoolSession()` creates the agenc-pool tmux session automatically. This is
already implemented in `internal/server/pool.go`.

### Log file growth

Plist logs append indefinitely. For crons that fire frequently, logs will grow.
Acceptable for now — add newsyslog rotation later if needed.

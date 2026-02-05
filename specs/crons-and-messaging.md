Crons and Messaging
===================

Overview
--------

This spec describes three features that make agenc more autonomous, reducing the amount of hand-holding required from the user:

1. **Crons** — scheduled, headless missions that the daemon launches automatically on a crontab schedule.
2. **Messaging** — a mechanism for agents to send messages to the user during (or at the end of) a mission, and for the user to reply.
3. **Inbox** — an interactive CLI interface for the user to read agent messages, reply, and triage.

Together, these features enable a workflow where agents do work in the background on a schedule, report results or ask for help via messages, and the user processes those messages asynchronously through an inbox — much like email.


Prerequisites
-------------

### Verify `--print` Resumability

Before implementing crons, verify that headless Claude sessions can be resumed:

```bash
claude --print -p "Say hello and remember the number 42"
claude -c -p "What number did I ask you to remember?"
```

If `-c` successfully continues the `--print` conversation, proceed with this spec. If not, investigate alternatives:

- `claude -p <prompt>` with stdin closed (may behave differently)
- A different flag combination
- Filing a feature request with Anthropic

**This is a blocker.** Do not implement crons until headless resumability is confirmed.


Crons
-----

### Definition

A cron is a named, scheduled rule that tells the daemon to create and launch a headless mission at specific times. Each cron definition contains:

| Field | Required | Description |
|-------|----------|-------------|
| `schedule` | yes | Standard 5-field crontab expression (`minute hour day-of-month month day-of-week`). |
| `agent` | yes | Agent template to use (canonical `github.com/owner/repo` format). |
| `prompt` | yes | The prompt text to pass to Claude. |
| `description` | no | Human-readable description of what this cron does. Displayed in `cron ls`. |
| `git` | no | A Git repo from the agenc repo library to copy into the mission's workspace. |
| `timeout` | no | Maximum runtime before the mission is killed. Format: `30m`, `2h`, `1h30m`. Default: `1h`. |
| `overlap` | no | Policy when previous run is still active: `skip` (default), `allow`, or `queue`. |
| `retention` | no | Number of missions to retain per cron. Older missions are auto-archived. Default: unlimited. |
| `enabled` | no | Boolean. Disabled crons are retained in config but do not fire. Default: `true`. |

### Timezone Behavior

All crontab expressions are evaluated against the **system's local time**. The daemon uses `time.Now()` truncated to the minute. Agenc does not support per-cron timezone configuration; if you need a cron to fire at a specific time in a different timezone, convert the schedule to local time.

### Storage

Cron definitions live in `config.yml` under a top-level `crons` key:

```yaml
crons:
  maxConcurrent: 5  # optional, default: 10

  daily-git-sync:
    description: "Check all repos for unpushed changes and report"
    schedule: "0 9 * * *"
    agent: github.com/mieubrisse/git-sync-agent
    prompt: "Check all repos for unpushed changes and send me a summary via agenc message send"
    timeout: 30m
    overlap: skip
    retention: 7

  weekly-inbox-zero:
    description: "Organize Todoist inbox into projects"
    schedule: "0 10 * * 1"
    agent: github.com/mieubrisse/todoist-agent
    prompt: "Clear the Todoist inbox and organize tasks into appropriate projects"
    git: github.com/mieubrisse/todoist-config
    timeout: 1h
    overlap: skip
```

### Global Settings

The `crons` section supports one global setting:

- **`maxConcurrent`** — Maximum number of headless cron missions that can run simultaneously. Default: `10`. When the limit is reached, new cron fires are skipped (not queued) and a warning is logged.

### CLI Commands

Crons are managed through `agenc cron` subcommands:

| Command | Description |
|---------|-------------|
| `agenc cron add` | Interactive wizard: prompts for name, schedule, agent template (fzf picker), prompt, and optional fields. Validates inputs and writes to config.yml. |
| `agenc cron ls` | Lists all crons with name, schedule, agent, enabled, last run status, and next fire time. |
| `agenc cron rm <name>` | Removes a cron from config.yml. Does not affect missions already created by the cron. |
| `agenc cron enable <name>` | Sets `enabled: true` for the named cron. |
| `agenc cron disable <name>` | Sets `enabled: false` for the named cron. |
| `agenc cron run <name>` | Immediately fires the cron (creates mission, launches Claude). Ignores `enabled` status. Does not update `last_run_at`. Useful for testing. |
| `agenc cron logs <name>` | Tails the `claude-output.log` of the most recent mission for this cron. Supports `--follow` flag. |
| `agenc cron history <name>` | Shows recent runs for this cron with timestamps, duration, and exit status. |

### `cron ls` Output Format

```
NAME             SCHEDULE      ENABLED  LAST RUN              STATUS    NEXT RUN
daily-git-sync   0 9 * * *     yes      2025-01-15 09:00      success   2025-01-16 09:00
weekly-review    0 10 * * 1    yes      2025-01-13 10:00      failed    2025-01-20 10:00
hourly-check     0 * * * *     yes      2025-01-15 14:00      running   2025-01-15 15:00
disabled-cron    0 6 * * *     no       2025-01-10 06:00      success   -
```

Use `--verbose` or `-v` to include description and agent columns.

### Crontab Parsing Library

Use [`adhocore/gronx`](https://github.com/adhocore/gronx) (v1.19.6+, MIT license) for crontab expression parsing and evaluation. Key API:

- `gronx.New().IsValid(expr)` — validate a cron expression.
- `gronx.New().IsDue(expr, referenceTime)` — check if an expression matches a specific time.
- `gronx.NextTickAfter(expr, refTime, false)` — find the next fire time after a reference.

### Validation

**On `cron add`:**
- Cron name must be non-empty and contain only `[a-zA-Z0-9_-]`.
- Cron name must be unique (not already defined in config).
- Schedule must be a valid 5-field crontab expression.
- Agent must exist in `agentTemplates` section of config.yml.
- Git repo (if specified) must exist in `syncedRepos` section of config.yml.
- Timeout must be a valid Go duration string (e.g., `30m`, `1h`, `2h30m`).
- Overlap must be one of: `skip`, `allow`, `queue`.
- Retention must be a positive integer.

**On daemon startup:**
- Log warnings for crons with invalid agent or git references (don't crash).
- Invalid crons are skipped during scheduling.

**On cron fire:**
- If agent template doesn't exist: skip, log error, record failed run.
- If git repo doesn't exist: skip, log error, record failed run.

### Daemon Scheduling

The daemon gains a new background goroutine: the **cron scheduler**.

**Cycle:** Every 60 seconds.

**Behavior:**

1. Read the `crons` section of `config.yml`.
2. Get the current time, truncated to the minute.
3. Count currently running headless missions (wrappers). If >= `maxConcurrent`, log a warning and skip this cycle.
4. For each enabled cron with valid references, evaluate the crontab expression using `IsDue(expr, now)`.
5. For each cron whose schedule matches:
   a. **Double-fire guard:** Check `cron_runs` table. If a run exists for this cron with `started_at` within the current minute, skip (already fired).
   b. **Overlap policy:**
      - `skip`: If a run exists with `finished_at IS NULL`, skip this fire and log it.
      - `allow`: Proceed regardless of running missions.
      - `queue`: If a run exists with `finished_at IS NULL`, mark this cron as "queued" (in-memory) and check again next cycle. Max queue depth: 1.
   c. **Concurrency check:** If running headless missions >= `maxConcurrent`, skip and log.
   d. **Create mission:** Use the same code path as `agenc mission new`, passing the cron's agent template, prompt, and optional git repo. Set `cron_name` field.
   e. **Record run start:** Insert row into `cron_runs` with `started_at = now`, `finished_at = NULL`.
   f. **Launch wrapper in headless mode:** Start the wrapper process with `--headless` flag, passing the mission ID, prompt, and timeout. The wrapper handles everything else (Claude invocation, heartbeating, output capture, secrets, timeout enforcement).
   g. **Track wrapper:** Store wrapper PID and mission ID in daemon's in-memory map for cleanup.
6. **Completion handling:** When a wrapper process exits, the daemon:
   a. Removes it from the in-memory tracking map.
   b. If `retention` is set and exceeded, archive oldest missions for this cron.
   c. If overlap policy is `queue` and a fire was queued, trigger it now.

   Note: The wrapper is responsible for updating `cron_runs` with `finished_at`, `exit_code`, and `exit_reason` before it exits.

**Missed runs:** If the daemon was not running when a cron was scheduled to fire, the run is skipped. The daemon does not backfill missed runs. This matches standard cron behavior.

### Daemon Startup: Orphan Handling

On daemon startup, handle missions that may have been orphaned by a previous daemon crash:

1. Query `cron_runs` for rows where `finished_at IS NULL`.
2. For each orphaned run:
   a. Check if the wrapper process is still running (read PID from mission's `pid` file, check if process exists).
   b. If running: adopt the wrapper (add to in-memory tracking map). The wrapper continues handling heartbeats and will update `cron_runs` when it finishes.
   c. If not running: update `cron_runs` with `finished_at = now`, `exit_code = NULL`, `exit_reason = 'orphaned'`.

### Daemon Shutdown

On `SIGTERM` or `SIGINT`:

1. Stop the cron scheduler loop.
2. For each running headless wrapper process:
   a. Send `SIGINT` to the wrapper. The wrapper will forward this to Claude and handle graceful shutdown.
   b. Wait up to 60 seconds for wrapper to exit (wrapper has its own 30-second grace period for Claude).
   c. If wrapper still running, send `SIGKILL`.
3. Exit.

Note: The wrapper is responsible for updating `cron_runs` on shutdown. If the wrapper is killed before it can update the DB, the orphan handling on next daemon startup will mark it as `orphaned`.

### Wrapper Headless Mode

Both interactive and headless missions use the same wrapper process. The wrapper accepts a `--headless` flag that changes its behavior:

| Aspect | Interactive Mode | Headless Mode (`--headless`) |
|--------|------------------|------------------------------|
| Claude command | `claude <prompt>` or `claude -c` | `claude --print -p <prompt>` |
| stdin | Wired to user's terminal | `/dev/null` |
| stdout/stderr | Wired to user's terminal | Captured to `claude-output.log` |
| Spawned by | CLI (foreground) | Daemon (background) |
| Template live-reload | Yes (fsnotify watches) | No (single-shot execution) |
| Timeout enforcement | No | Yes (kills Claude after timeout) |
| On completion | Wrapper exits, user returns to shell | Wrapper updates `cron_runs`, then exits |

**Shared behavior (both modes):**
- Heartbeat updates every 30 seconds
- Secrets handling via `op run --env-file` if `secrets.env` exists
- Sets `AGENC_MISSION_UUID` environment variable
- Writes PID to mission's `pid` file

**Headless-specific wrapper flags:**
- `--headless` — Enable headless mode
- `--prompt <text>` — The prompt to pass to Claude
- `--timeout <duration>` — Maximum runtime (e.g., `30m`, `1h`). Wrapper sends SIGTERM to Claude after timeout, SIGKILL after 30-second grace period.
- `--cron-run-id <id>` — The `cron_runs` row ID to update on completion

**On headless completion:** The wrapper updates the `cron_runs` row with `finished_at`, `exit_code`, and `exit_reason` before exiting. This ensures the DB is consistent even if the daemon crashes.

Headless missions reuse the same directory structure, DB record, and `AGENC_MISSION_UUID` as interactive missions. Users can `mission resume` a completed headless mission to continue interactively.

### Log Rotation

The wrapper (in headless mode) manages `claude-output.log` with size-based rotation:

- Maximum size: 10 MB
- On rotation: rename to `.1`, shift existing `.1` to `.2`, etc.
- Keep last 3 rotated files (`.1`, `.2`, `.3`)
- Rotation checked at mission start and periodically during execution

### Database Changes

**New table for cron run history:**

```sql
CREATE TABLE cron_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cron_name TEXT NOT NULL,
    mission_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    exit_code INTEGER,
    exit_reason TEXT,  -- 'success', 'error', 'timeout', 'killed', 'orphaned', 'daemon_shutdown'
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);
CREATE INDEX idx_cron_runs_name_started ON cron_runs(cron_name, started_at DESC);
CREATE INDEX idx_cron_runs_running ON cron_runs(cron_name) WHERE finished_at IS NULL;
```

**Add column to missions table:**

```sql
ALTER TABLE missions ADD COLUMN cron_name TEXT NOT NULL DEFAULT '';
```


Messaging
---------

### Agent-to-User Messages

Agents send messages to the user by running a CLI command via Bash:

```bash
agenc message send "I finished checking all repos. 3 have unpushed changes."
```

Or for longer messages, pipe via stdin:

```bash
agenc message send <<'EOF'
## Git Sync Report

Checked 12 repositories. Results:

- **dotfiles**: 3 unpushed commits on `main`
- **api-server**: 1 unpushed commit on `feat/auth`
- **blog**: uncommitted changes in working tree

All other repos are clean and in sync with remote.
EOF
```

**Sender identification:** The `agenc message send` command reads `AGENC_MISSION_UUID` from the environment to associate the message with the correct mission. If the variable is not set, the command exits with an error.

**Message format:** Free-text Markdown. No subject line, no size limit (filesystem-based storage).

### Message Storage

Message bodies live on the filesystem inside the mission directory:

```
$AGENC_DIRPATH/missions/<uuid>/
    messages/
        1.md
        2.md
        3.md
```

Files are numbered sequentially starting at 1. The `messages/` directory is created on first message and cleaned up automatically when the mission is deleted.

**`agenc message send` behavior:**

1. Read `AGENC_MISSION_UUID` from environment.
2. Validate mission exists in DB.
3. Create `messages/` subdirectory if needed.
4. Query DB for highest `seq` value for this mission, increment by 1.
5. Write message body to `<seq>.md`.
6. Insert row into `messages` table.

### Settings Integration

For `agenc message send` to work, agents need Bash permission. The Claude config sync adds this to the merged `settings.json`:

```json
{
  "permissions": {
    "allow": [
      "Bash(agenc message send:*)"
    ]
  }
}
```

### Environment Variable

The mission wrapper sets `AGENC_MISSION_UUID` in the Claude child process environment. This applies to both interactive and headless modes, since headless missions also run through the wrapper.

Agent templates that want agents to send messages should include guidance in their CLAUDE.md on when and how to use `agenc message send`.

### Data Model

```sql
CREATE TABLE messages (
    mission_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    sender TEXT NOT NULL,       -- 'agent' or 'user'
    is_read INTEGER NOT NULL DEFAULT 0,
    delivered INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    PRIMARY KEY (mission_id, seq),
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);
CREATE INDEX idx_messages_unread ON messages(mission_id) WHERE is_read = 0;
```

- `seq`: Sequential message number (1, 2, 3...). Maps to `<seq>.md` filename.
- `sender`: `'agent'` or `'user'`.
- `is_read`: 0 = unread, 1 = read. Agent messages default to unread; user replies default to read.
- `delivered`: Only meaningful for user replies. 0 = not yet injected into resumed mission, 1 = delivered.


Inbox
-----

### Overview

The inbox is an interactive CLI for reading and responding to agent messages.

### Command: `agenc inbox`

Launches an fzf-based thread picker showing message threads grouped by mission.

**Thread list view:**

```
[3 unread]  daily-git-sync  2025-01-15 09:02  "I finished checking all repos. 3 have unpushed..."
[1 unread]  fix-auth-bug    2025-01-15 08:45  "I'm stuck. The OAuth provider returns a 403 when..."
            weekly-review   2025-01-14 10:00  "Weekly review complete. Summary attached below..."
```

Each line shows:
- Unread count (highlighted, omitted if zero)
- Mission identifier: cron name (if cron-spawned) or truncated prompt (if manual)
- Timestamp of most recent message
- Truncated preview of most recent message body

Sorted by most recent message, descending.

**Thread detail view:**

```
─── daily-git-sync (mission abc12345) ───

[2025-01-15 09:00] Agent:
Started checking repos for unpushed changes...

[2025-01-15 09:02] Agent:
## Git Sync Report

Checked 12 repositories. Results:

- **dotfiles**: 3 unpushed commits on `main`
- **api-server**: 1 unpushed commit on `feat/auth`
- **blog**: uncommitted changes in working tree

All other repos are clean and in sync with remote.

────────────────────────────────────────

[r] Reply  [u] Mark unread  [a] Attach (resume mission)  [q] Back
```

Reading a thread marks all messages as read.

**Actions:**

- **Reply (`r`):** Opens `$EDITOR` with reply template.
- **Mark unread (`u`):** Marks all messages unread, returns to list.
- **Attach (`a`):** Resumes mission interactively (equivalent to `mission resume`).
- **Back (`q`):** Returns to thread list.

### Reply Flow

When the user presses `r`:

1. Create temporary file with template:

```

<!-- Write your reply above this line. Only text above the line will be sent. -->
<!-- ─────────────────────────────────────────────────────────────────────────── -->

[2025-01-15 09:02] Agent:

## Git Sync Report

Checked 12 repositories...


[2025-01-15 09:00] Agent:

Started checking repos for unpushed changes...
```

2. Open `$EDITOR` (default: `vim`) with cursor at line 1.
3. User writes reply above the separator.
4. On save and quit:
   a. Extract text above separator.
   b. If empty/whitespace: discard reply.
   c. Otherwise: write to next `<seq>.md`, insert DB row with `sender='user'`, `is_read=1`, `delivered=0`.

### Reply Delivery

When a mission is resumed (`mission resume` or inbox attach):

1. Query `messages` for undelivered user replies (`sender='user'`, `delivered=0`).
2. If replies exist:
   a. Read reply bodies from `<seq>.md` files.
   b. Concatenate in chronological order, separated by blank lines.
   c. Launch `claude -c -p <concatenated_reply>`.
   d. Mark replies as `delivered=1`.
3. If no replies: launch `claude -c` (standard resume).

### Headless Mission Resume

When resuming a completed headless mission, it switches to interactive mode:

1. Wrapper detects this is a resume (not cron launch).
2. Uses `claude -c` (or `claude -c -p <reply>` if queued).
3. User is now in interactive session, continuing from headless conversation.


CLI Commands Summary
--------------------

### `agenc cron`

| Command | Description |
|---------|-------------|
| `agenc cron add` | Interactive wizard for creating a cron. |
| `agenc cron ls` | List all crons with status and next fire time. |
| `agenc cron rm <name>` | Remove a cron from config. |
| `agenc cron enable <name>` | Enable a cron. |
| `agenc cron disable <name>` | Disable a cron. |
| `agenc cron run <name>` | Manually trigger a cron (for testing). |
| `agenc cron logs <name>` | View output log of most recent run. |
| `agenc cron history <name>` | Show recent runs with status. |

### `agenc message`

| Command | Description |
|---------|-------------|
| `agenc message send [body]` | Send message from agent to user. Body as argument or stdin. |

### `agenc inbox`

| Command | Description |
|---------|-------------|
| `agenc inbox` | Interactive inbox for reading/replying to messages. |

### Enhanced `agenc mission ls`

Add `--cron <name>` flag to filter missions by cron:

```
agenc mission ls --cron daily-git-sync
```

Display cron name in output when present.


Database Schema Summary
-----------------------

```sql
-- Cron run history (replaces cron_last_runs)
CREATE TABLE cron_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cron_name TEXT NOT NULL,
    mission_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    exit_code INTEGER,
    exit_reason TEXT,
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);
CREATE INDEX idx_cron_runs_name_started ON cron_runs(cron_name, started_at DESC);
CREATE INDEX idx_cron_runs_running ON cron_runs(cron_name) WHERE finished_at IS NULL;

-- Message metadata
CREATE TABLE messages (
    mission_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    sender TEXT NOT NULL,
    is_read INTEGER NOT NULL DEFAULT 0,
    delivered INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    PRIMARY KEY (mission_id, seq),
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);
CREATE INDEX idx_messages_unread ON messages(mission_id) WHERE is_read = 0;

-- Link missions to crons
ALTER TABLE missions ADD COLUMN cron_name TEXT NOT NULL DEFAULT '';
```


Changes to Existing Components
------------------------------

### Daemon

- **New goroutine:** Cron scheduler (60-second cycle). Evaluates cron expressions and spawns wrapper processes.
- **Wrapper tracking:** Maintain in-memory map of running headless wrapper PIDs.
- **Orphan handling:** On startup, adopt running wrappers or mark orphaned runs.
- **Shutdown cleanup:** Send SIGINT to running wrappers, wait for graceful exit.
- **Retention enforcement:** After wrapper exits, archive old missions if retention policy exceeded.
- **Queue management:** Track queued cron fires (for `overlap: queue` policy) and trigger when previous run completes.

### Mission Wrapper

- **New `--headless` mode:** Run in background without terminal interaction.
- **New flags:** `--headless`, `--prompt`, `--timeout`, `--cron-run-id`.
- **Timeout enforcement (headless):** Kill Claude after configured timeout.
- **Output capture (headless):** Write stdout/stderr to `claude-output.log` with rotation.
- **DB updates (headless):** Update `cron_runs` row on completion with exit status.
- **Environment variable:** Set `AGENC_MISSION_UUID` (both modes).
- **Reply injection:** On resume, check for undelivered replies and inject as prompt (both modes).

### Claude Config Sync

- **Bash permission:** Add `Bash(agenc message send:*)`.

### Mission Directory

```
$AGENC_DIRPATH/missions/<uuid>/
    pid
    claude-state
    template-commit
    wrapper.log
    claude-output.log      # headless only
    claude-output.log.1    # rotated
    claude-output.log.2
    claude-output.log.3
    messages/
        1.md
        2.md
    agent/
        ...
```

### `agenc mission ls`

- Display cron name for cron-spawned missions.
- Add `--cron <name>` filter flag.


What Does NOT Change
--------------------

- Existing mission lifecycle (new, resume, archive, stop, rm, nuke).
- Mission wrapper state machine for interactive missions.
- Template updater daemon goroutine.
- The `agenc template` and `agenc repo` command families.
- Daemon's repo update loop behavior.


Implementation Phases
---------------------

**Phase 0: Prerequisite verification**
- Verify `claude --print` conversations can be resumed with `claude -c`.
- Document findings. If it doesn't work, investigate alternatives before proceeding.

**Phase 1: Messaging foundation**
- Add `messages` table.
- Implement `agenc message send`.
- Set `AGENC_MISSION_UUID` in wrapper environment.
- Add `Bash(agenc message send:*)` permission.

**Phase 2: Inbox (read-only)**
- Implement `agenc inbox` thread list and detail views.
- Implement mark-unread.

**Phase 3: Reply flow**
- Implement reply composition in inbox.
- Implement reply delivery on `mission resume`.
- Implement inbox attach action.

**Phase 4: Cron infrastructure**
- Add `cron_name` column to missions.
- Add `cron_runs` table.
- Parse `crons` section from config.yml with validation.
- Implement `agenc cron add/ls/rm/enable/disable`.

**Phase 5: Wrapper headless mode**
- Add `--headless`, `--prompt`, `--timeout`, `--cron-run-id` flags to wrapper.
- Implement output capture to `claude-output.log` with rotation.
- Implement timeout enforcement (SIGTERM → SIGKILL).
- Implement `cron_runs` DB update on exit.
- Test wrapper headless mode independently before integrating with daemon.

**Phase 6: Cron execution**
- Implement daemon cron scheduler goroutine.
- Implement wrapper spawning for headless missions.
- Implement overlap policies.
- Implement orphan handling on daemon startup.
- Implement graceful shutdown (signal wrappers).

**Phase 7: Cron observability**
- Implement `agenc cron run <name>`.
- Implement `agenc cron logs <name>`.
- Implement `agenc cron history <name>`.
- Implement retention policy enforcement.
- Add `--cron` filter to `mission ls`.

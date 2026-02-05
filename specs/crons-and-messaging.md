Crons and Messaging
===================

Overview
--------

This spec describes three features that make agenc more autonomous, reducing the amount of hand-holding required from the user:

1. **Crons** -- scheduled, headless missions that the daemon launches automatically on a crontab schedule.
2. **Messaging** -- a mechanism for agents to send messages to the user during (or at the end of) a mission, and for the user to reply.
3. **Inbox** -- an interactive CLI interface for the user to read agent messages, reply, and triage.

Together, these features enable a workflow where agents do work in the background on a schedule, report results or ask for help via messages, and the user processes those messages asynchronously through an inbox -- much like email.


Crons
-----

### Definition

A cron is a named, scheduled rule that tells the daemon to create and launch a headless mission at specific times. Each cron definition contains:

- **name** -- unique identifier for the cron (e.g. `daily-git-sync`).
- **schedule** -- standard 5-field crontab expression (`minute hour day-of-month month day-of-week`).
- **agent** -- agent template to use (canonical `github.com/owner/repo` format).
- **prompt** -- the prompt text to pass to Claude.
- **git** _(optional)_ -- a Git repo from the agenc repo library to copy into the mission's workspace.
- **enabled** -- boolean, defaults to `true`. Disabled crons are retained in config but do not fire.

### Storage

Cron definitions live in `config.yml` under a top-level `crons` key:

```yaml
crons:
  daily-git-sync:
    schedule: "0 9 * * *"
    agent: github.com/mieubrisse/git-sync-agent
    prompt: "Check all repos for unpushed changes and report"
    enabled: true
  weekly-inbox-zero:
    schedule: "0 10 * * 1"
    agent: github.com/mieubrisse/todoist-agent
    prompt: "Clear the Todoist inbox and organize into projects"
    git: github.com/mieubrisse/todoist-config
    enabled: true
```

### CLI Commands

Crons are managed through `agenc cron` subcommands:

- `agenc cron add` -- interactive wizard that prompts for name, schedule, agent template (fzf picker), prompt text, and optional git repo. Validates the crontab expression before saving.
- `agenc cron ls` -- lists all crons with their name, schedule, agent, enabled status, and last-fired time.
- `agenc cron rm <name>` -- removes a cron definition from config.yml.
- `agenc cron enable <name>` -- sets `enabled: true` for the named cron.
- `agenc cron disable <name>` -- sets `enabled: false` for the named cron.

### Crontab Parsing Library

Use [`adhocore/gronx`](https://github.com/adhocore/gronx) (v1.19.6+, MIT license) for crontab expression parsing and evaluation. It is a zero-dependency library purpose-built for parsing and matching crontab expressions. Key API:

- `gron.IsValid(expr)` -- validate a cron expression.
- `gron.IsDue(expr, referenceTime)` -- check if an expression matches a specific time. This is the core function the scheduler uses.
- `gronx.NextTickAfter(expr, refTime, false)` -- find the next fire time after a reference (useful for `cron ls` display).

Alternatives considered:
- `robfig/cron` -- popular but primarily a job scheduler, not maintained since 2020.
- `hashicorp/cronexpr` -- maintained but GPL/Apache dual-licensed and lacks a built-in `IsDue` function.

### Daemon Scheduling

The daemon gains a new background goroutine: the **cron scheduler**.

**Cycle:** Every 60 seconds.

**Behavior:**

1. Read the `crons` section of `config.yml`.
2. Get the current time, truncated to the minute.
3. For each enabled cron, evaluate the crontab expression against the current minute using `gron.IsDue(expr, now)`.
4. For each cron whose schedule matches:
   a. Create a new mission via the same code path as `agenc mission new`, passing the cron's agent template, prompt, and optional git repo. Set the mission's `cron_name` field to the cron's name.
   b. Launch Claude in headless mode using `claude --print -p <prompt>` with the working directory set to the mission's `agent/` subdirectory. The `--print` flag runs Claude non-interactively: it processes the prompt, executes any tool calls, and exits.
   c. The headless Claude process runs as a child of the daemon. Its stdout and stderr are captured to a log file at `$AGENC_DIRPATH/missions/<uuid>/claude-output.log`.
   d. Update the mission's `last_heartbeat` in the DB periodically (every 30 seconds) while the Claude process is alive, so the daemon's repo updater knows to keep syncing repos used by active cron missions.
   e. When Claude exits, stop updating the heartbeat. The mission remains in `active` status -- it is not auto-archived.
5. Update `last_run_at` for the cron in the DB (see tracking below).

**Cron run tracking:**

A new `cron_last_runs` table tracks when each cron last fired:

```sql
CREATE TABLE cron_last_runs (
    cron_name TEXT PRIMARY KEY,
    last_run_at TEXT NOT NULL
);
```

This serves two purposes:
- `agenc cron ls` can display when each cron last fired.
- The scheduler uses it as a guard against double-firing: before launching a cron, it checks that `last_run_at` for that cron does not fall within the current minute.

**Missed runs:** If the daemon was not running when a cron was scheduled to fire, the run is skipped. The daemon does not backfill missed runs. This matches standard cron behavior.

**Concurrency:** If a cron fires while a previous mission from the same cron is still running, a new mission is created anyway. Each run is independent. The daemon logs a warning noting the overlap.

### Headless Mission Differences

Headless missions (spawned by crons) differ from interactive missions in these ways:

| Aspect | Interactive mission | Headless mission |
|---|---|---|
| Launch command | `claude <prompt>` or `claude -c` | `claude --print -p <prompt>` |
| stdin | Wired to user's terminal | Not connected (no user interaction) |
| stdout/stderr | Wired to user's terminal | Captured to `claude-output.log` |
| Process parent | agenc wrapper (foreground) | agenc daemon (background) |
| Template live-reload | Yes (wrapper watches for changes) | No (single-shot execution) |
| Wrapper state machine | Full Running/RestartPending/Restarting | Not applicable |

Headless missions reuse the same mission directory structure, DB record, and `AGENC_MISSION_UUID` environment variable as interactive missions. The user can `mission resume` a completed headless mission to interact with it (Claude picks up from the `--print` conversation via `claude -c`).

### Database Changes

Add `cron_name` column to the `missions` table:

```sql
ALTER TABLE missions ADD COLUMN cron_name TEXT NOT NULL DEFAULT '';
```

Missions created by crons have `cron_name` set to the cron's name. Manually-created missions have an empty `cron_name`. This allows `mission ls` to display which missions were cron-spawned, and enables querying mission history per cron.


Messaging
---------

### Agent-to-User Messages

Agents send messages to the user by running a CLI command via Bash:

```bash
agenc message send "I finished checking all repos. 3 have unpushed changes: dotfiles, api-server, blog."
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

**Sender identification:** The mission wrapper sets `AGENC_MISSION_UUID` in the environment when spawning Claude. The `agenc message send` command reads this variable to associate the message with the correct mission. If the variable is not set, the command exits with an error.

**Message format:** Free-text, expected to be Markdown. No subject line.

### Message Storage

Message bodies live on the filesystem inside the mission directory, not in the database. Each message is a numbered Markdown file:

```
$AGENC_DIRPATH/missions/<uuid>/
    messages/
        1.md
        2.md
        3.md
```

Files are numbered sequentially starting at 1, in chronological order. The `messages/` directory is created on first message. Since message files live inside the mission directory, they are automatically cleaned up when the mission is deleted via `agenc mission rm`.

The `agenc message send` command:

1. Reads `AGENC_MISSION_UUID` to determine the mission directory.
2. Creates the `messages/` subdirectory if it does not exist.
3. Determines the next sequence number by reading the DB for the highest existing `seq` value for this mission, then incrementing by 1.
4. Writes the message body to `<seq>.md`.
5. Inserts a row into the `messages` table with the sequence number, sender, and metadata.

This separation keeps large message bodies out of the database while still enabling efficient queries on message metadata (unread count, latest timestamp, delivery status).

### Settings Integration

For `agenc message send` to work, the agent must have Bash permission to run it. The Claude config sync component (Component 2 from the wrapper spec) adds the following to the merged `settings.json`:

```json
{
  "permissions": {
    "allow": [
      "Bash(agenc message send:*)"
    ]
  }
}
```

This is merged into the agenc Claude config directory (`$AGENC_DIRPATH/claude/settings.json`) alongside the existing hook configurations.

### Environment Variable

The mission wrapper sets `AGENC_MISSION_UUID` in the Claude child process environment. This applies to both interactive and headless missions:

- **Interactive missions:** The wrapper already spawns Claude via `os/exec.Command`. Adding an environment variable is a one-line change.
- **Headless missions:** The daemon's cron scheduler spawns Claude the same way and sets the same variable.

The agent template's CLAUDE.md should instruct agents that `agenc message send` is available for communicating with the user. Templates that want agents to send messages should include guidance on when and how to use it.

### Data Model

Message metadata is stored in a `messages` table. Message bodies live on the filesystem (see Message Storage above).

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
```

- `mission_id`: The mission this message belongs to.
- `seq`: Sequential message number within the mission (1, 2, 3, ...). Maps to the filename `<seq>.md` in the mission's `messages/` directory.
- `sender`: Either `'agent'` (sent by `agenc message send`) or `'user'` (sent via inbox reply).
- `is_read`: 0 = unread, 1 = read. New agent messages default to unread. User replies default to read.
- `delivered`: 0 = not yet delivered to the agent, 1 = delivered (or not applicable). Only meaningful for user replies (`sender = 'user'`). Agent messages always default to 1. User replies are created with 0 and marked 1 upon injection into a resumed mission.
- `created_at`: ISO 8601 timestamp.

The composite primary key `(mission_id, seq)` enforces uniqueness per mission and eliminates the need for a separate UUID. The `ON DELETE CASCADE` ensures message metadata is cleaned up when a mission is deleted (the filesystem files are removed when the mission directory is deleted).


Inbox
-----

### Overview

The inbox is an interactive CLI interface for reading and responding to agent messages. It is the user's primary touchpoint for asynchronous agent communication.

### Command: `agenc inbox`

Launches an interactive fzf-based thread picker showing message threads grouped by mission.

**Thread list view:**

Each line in the fzf picker represents one mission thread:

```
[3 unread]  daily-git-sync  2025-01-15 09:02  "I finished checking all repos. 3 have unpushed..."
[1 unread]  fix-auth-bug    2025-01-15 08:45  "I'm stuck. The OAuth provider returns a 403 when..."
            weekly-review   2025-01-14 10:00  "Weekly review complete. Summary attached below..."
```

Each line shows:
- Unread count (highlighted, omitted if zero).
- Mission identifier: the cron name (if cron-spawned) or a truncated mission prompt (if manual).
- Timestamp of the most recent message.
- Truncated preview of the most recent message body.

Threads are sorted by most recent message timestamp, descending (newest first).

**Thread detail view:**

Selecting a thread displays all messages in chronological order:

```
─── daily-git-sync (mission abc123) ───

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

Reading a thread marks all its messages as read.

**Actions from thread detail view:**

- **Reply (`r`):** Opens `$EDITOR` with a reply template (see Reply Flow below).
- **Mark unread (`u`):** Marks all messages in the thread as unread and returns to the thread list. Useful for "I'll deal with this later" triage.
- **Attach (`a`):** Resumes the mission interactively (equivalent to `agenc mission resume <id>`). If the mission has a queued reply, it is injected as the prompt.
- **Back (`q`):** Returns to the thread list.

### Reply Flow

When the user presses `r` to reply:

1. A temporary file is created with the following template. Earlier messages are included as plain Markdown (no `>` quoting) so that formatting renders correctly as the user reviews them in their editor:

```

<!-- Write your reply above this line. Only text above the line will be sent. -->
<!-- ─────────────────────────────────────────────────────────────────────── -->

[2025-01-15 09:02] Agent:

## Git Sync Report

Checked 12 repositories. Results:

- **dotfiles**: 3 unpushed commits on `main`
- **api-server**: 1 unpushed commit on `feat/auth`
- **blog**: uncommitted changes in working tree

All other repos are clean and in sync with remote.


[2025-01-15 09:00] Agent:

Started checking repos for unpushed changes...
```

The separator is the HTML comment block (two lines). Everything above is the user's reply; everything below is read-only context. The messages below the separator are rendered as plain Markdown so the user can read them naturally with full formatting intact.

2. The user's `$EDITOR` (defaulting to `vim`) opens the file with the cursor at line 1.
3. The user writes their reply above the separator. The message history below provides context, just like email.
4. On save and quit:
   a. The text above the separator is extracted.
   b. If the text is empty or whitespace-only, the reply is discarded.
   c. Otherwise, the reply body is written to the next `<seq>.md` file in the mission's `messages/` directory, and a row is inserted into the `messages` table with `sender = 'user'`, `is_read = 1`, and `delivered = 0`.
5. The reply is now queued. It will be delivered to the agent when the mission is next resumed (see Reply Delivery below).

### Reply Delivery

When a mission is resumed (via `agenc mission resume` or the inbox's attach action), the wrapper checks for queued user replies:

1. Query the `messages` table for rows where `mission_id` matches, `sender = 'user'`, and `delivered = 0`.
2. If one or more undelivered replies exist:
   a. Read each reply body from the corresponding `<seq>.md` file in the mission's `messages/` directory.
   b. Concatenate them in chronological order (in case the user replied multiple times), separated by blank lines.
   c. Use the concatenated text as the prompt for `claude -c -p <reply_text>` (continue conversation with injected prompt).
   d. Mark the replies as delivered (`delivered = 1`).
3. If no undelivered replies exist, resume normally with `claude -c` (standard resume behavior).

### Headless Reply Delivery

For headless cron missions, reply delivery works the same way but with a nuance: `--print` mode doesn't support `-c` (continue). When a completed headless mission is resumed via `mission resume` or inbox attach, it switches to interactive mode:

1. The wrapper detects that this is a resume (not a cron launch).
2. It uses `claude -c` (or `claude -c -p <reply>` if there's a queued reply), launching Claude interactively.
3. The user is now in an interactive session, continuing from where the headless run left off.

This is the same behavior as the existing `mission resume` flow -- the only addition is the optional reply injection.


New CLI Commands Summary
------------------------

### `agenc cron`

| Command | Description |
|---|---|
| `agenc cron add` | Interactive wizard: prompts for name, crontab schedule, agent template (fzf), prompt, optional git repo. Validates inputs and writes to config.yml. |
| `agenc cron ls` | Lists all crons. Columns: name, schedule, agent, enabled, last fired. |
| `agenc cron rm <name>` | Removes a cron from config.yml. Does not affect missions already created by the cron. |
| `agenc cron enable <name>` | Sets `enabled: true` for the named cron. |
| `agenc cron disable <name>` | Sets `enabled: false` for the named cron. |

### `agenc message`

| Command | Description |
|---|---|
| `agenc message send [body]` | Sends a message from the current agent to the user. Reads `AGENC_MISSION_UUID` from environment. Body is a positional argument; if omitted, reads from stdin. |

### `agenc inbox`

| Command | Description |
|---|---|
| `agenc inbox` | Opens the interactive inbox. Thread picker → thread detail → reply/mark-unread/attach. |


Database Schema Changes
-----------------------

Two new tables and one column addition:

```sql
-- Track cron execution history
CREATE TABLE cron_last_runs (
    cron_name TEXT PRIMARY KEY,
    last_run_at TEXT NOT NULL
);

-- Message metadata (bodies stored as files in mission's messages/ directory)
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

-- Link missions to crons
ALTER TABLE missions ADD COLUMN cron_name TEXT NOT NULL DEFAULT '';
```


Changes to Existing Components
------------------------------

### Daemon

- **New goroutine:** Cron scheduler (60-second cycle). Reads `config.yml`, evaluates crontab expressions, creates and launches headless missions.
- **Headless mission management:** The daemon must track running headless Claude processes (child PIDs) for cleanup on daemon shutdown. On `SIGTERM`/`SIGINT`, the daemon sends `SIGINT` to all running headless Claude processes before exiting.

### Mission Directory Structure

The mission directory gains a `messages/` subdirectory:

```
$AGENC_DIRPATH/missions/<uuid>/
    pid
    claude-state
    template-commit
    claude-output.log          # (headless missions only)
    messages/                  # created on first message
        1.md
        2.md
        3.md
    agent/
        ...
```

The `messages/` directory is outside `agent/` (alongside `pid` and `claude-state`), so it is invisible to Claude and unaffected by template syncs.

### Mission Wrapper

- **Environment variable:** Set `AGENC_MISSION_UUID` in the Claude child process environment.
- **Reply injection:** On resume, check for undelivered user replies in the `messages` table. If present, read the corresponding `<seq>.md` files, concatenate, and pass as the prompt to `claude -c -p`.

### Claude Config Sync

- **Bash permission:** Add `Bash(agenc message send:*)` to the `permissions.allow` list in the merged settings.json.

### `agenc mission ls`

- **Cron indicator:** If a mission has a non-empty `cron_name`, display it in the output so the user can distinguish cron-spawned missions from manual ones.

### Config

- **New YAML section:** Parse and validate the `crons` key in config.yml.
- **Cron validation:** Cron names must be unique, non-empty, and contain only alphanumeric characters, hyphens, and underscores. Crontab expressions must be valid 5-field expressions.


What Does NOT Change
--------------------

- The existing mission lifecycle (new, resume, archive, stop, rm, nuke).
- The mission wrapper's state machine for interactive missions (Running/RestartPending/Restarting).
- The template updater daemon goroutine.
- The Claude config sync daemon goroutine (aside from adding the new Bash permission).
- The `agenc template` and `agenc repo` command families.
- The daemon's repo update loop behavior.


Implementation Phases
---------------------

This spec covers significant surface area. A phased implementation is recommended:

**Phase 1: Messaging foundation**
- Add the `messages` table (metadata only; bodies on filesystem).
- Implement `agenc message send` (writes `<seq>.md` to mission's `messages/` directory, inserts DB row).
- Set `AGENC_MISSION_UUID` in the wrapper's Claude environment.
- Add `Bash(agenc message send:*)` to the Claude config sync.

**Phase 2: Inbox**
- Implement `agenc inbox` with the interactive thread picker and detail view.
- Implement the reply flow ($EDITOR with plain-Markdown message history, separator parsing, filesystem write, queueing).
- Implement mark-unread.

**Phase 3: Reply delivery**
- Implement reply injection on `mission resume` (read undelivered reply files, concatenate, pass as prompt).
- Implement the inbox attach action.

**Phase 4: Crons**
- Add `cron_name` column to missions.
- Parse cron definitions from config.yml.
- Implement `agenc cron add/ls/rm/enable/disable`.
- Implement the daemon cron scheduler goroutine.
- Implement headless mission launching with `--print`.
- Add `cron_last_runs` table and double-fire guard.
- Add daemon shutdown cleanup for headless Claude processes.


Open Questions
--------------

1. **Cron output viewing:** Should there be a dedicated command to view the stdout/stderr output of a headless cron mission (the `claude-output.log`), or is `mission resume` + reading the conversation sufficient?

2. **Message retention:** Should old messages be automatically cleaned up after some period, or retained indefinitely (cleaned up only when the mission is deleted)?

3. **Cron mission cleanup:** Cron missions accumulate over time. Should there be an auto-cleanup policy (e.g., keep only the last N missions per cron), or is manual `mission rm` sufficient?

4. **Concurrent cron limit:** Should there be a configurable limit on how many headless missions can run simultaneously, to prevent resource exhaustion?

5. **`--print` conversation continuability:** Verify that `claude --print` stores its conversation in a way that `claude -c` can resume. If not, an alternative headless execution strategy will be needed.

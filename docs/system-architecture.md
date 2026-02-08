System Architecture
===================

AgenC is a CLI tool that runs AI agents (Claude Code instances) in isolated sandboxes, tracks all conversations in a central database, and makes it easy to roll configuration improvements back into agent templates. When an agent template is updated, the daemon syncs the changes so new missions pick them up automatically.

Runtime Processes
-----------------

Three cooperating processes form the runtime:

### CLI

The CLI is the user-facing entry point. It parses commands, manages the database, creates mission directories, and spawns wrapper processes.

- Entry point: `main.go`
- Commands: `cmd/` (Cobra-based; one file per command or command group)
- Key command files:
  - `cmd/root.go` — root command, global flags, database/config initialization
  - `cmd/mission_new.go` — creates missions: DB record, directory structure, config build, wrapper launch
  - `cmd/mission_ls.go` — lists missions with status, heartbeat, session names
  - `cmd/mission_resume.go` — resumes a stopped mission with `claude -c`
  - `cmd/mission_stop.go` — sends SIGINT to wrapper PID
  - `cmd/daemon_start.go` — forks the daemon as a detached background process
  - `cmd/cron_*.go` — cron CRUD commands
  - `cmd/repo_*.go` — repo library management
  - `cmd/tmux_*.go` — tmux window/pane management
  - `cmd/login.go` — Claude authentication (delegates to `claude login`)
  - `cmd/first_run.go` — first-time setup flow (optional config repo clone)
  - `cmd/resolver.go` — generic fuzzy-match resolver (wraps fzf)
  - `cmd/repo_resolution.go` — repo reference parsing and resolution

### Daemon

The daemon is a long-running background process that performs periodic maintenance. It is forked by `agenc daemon start` and detaches from the parent terminal via `setsid`.

- Entry point: `internal/daemon/daemon.go` (`Daemon.Run`)
- Process management: `internal/daemon/process.go` (PID file, fork, stop)
- PID file: `$AGENC_DIRPATH/daemon/daemon.pid`
- Log file: `$AGENC_DIRPATH/daemon/daemon.log`

The daemon runs three concurrent goroutines:

**1. Repo update loop** (`internal/daemon/template_updater.go`)
- Runs every 60 seconds
- Fetches and fast-forwards all repos in the synced set: `config.yml` `syncedRepos` + `claudeConfig.repo` + repos from missions with a recent heartbeat (< 5 minutes)
- Refreshes `origin/HEAD` every 10 cycles (~10 minutes) via `git remote set-head origin --auto`

**2. Config auto-commit loop** (`internal/daemon/config_auto_commit.go`)
- Runs every 10 minutes (first cycle delayed by 10 minutes after startup)
- If `$AGENC_DIRPATH/config/` is a Git repo with uncommitted changes: stages all, commits with timestamp message, pushes (if `origin` remote exists)

**3. Cron scheduler loop** (`internal/daemon/cron_scheduler.go`)
- Runs every 60 seconds (immediate first cycle)
- Reads enabled cron jobs from `config.yml`, checks which are due
- Spawns headless missions for due crons, respecting max concurrent limit and overlap policies
- On startup, adopts orphaned headless missions from a previous daemon instance
- On shutdown, gracefully terminates all running cron missions

### Wrapper

The wrapper is a per-mission foreground process that supervises a Claude child process. One wrapper runs per active mission.

- Entry point: `internal/wrapper/wrapper.go` (`Wrapper.Run` for interactive, `Wrapper.RunHeadless` for headless)
- Tmux integration: `internal/wrapper/tmux.go`

The wrapper:

1. Writes the wrapper PID to `$AGENC_DIRPATH/missions/<uuid>/pid`
2. Writes initial claude-state as `idle`
3. Spawns Claude as a child process (with 1Password wrapping if `secrets.env` exists)
4. Sets `CLAUDE_CONFIG_DIR` to the per-mission config directory
5. Sets `AGENC_MISSION_UUID` for the child process
6. Starts three background goroutines:
   - **Heartbeat writer** — updates `last_heartbeat` in the database every 30 seconds
   - **Claude-state watcher** — watches `claude-state` file via fsnotify; tracks whether a resumable conversation exists
   - **Remote refs watcher** (if mission has a git repo) — watches `.git/refs/remotes/origin/<branch>` for pushes; when detected, force-updates the repo library clone so other missions get fresh copies (debounced at 5 seconds)
7. Main event loop waits for either Claude to exit or a signal (SIGINT/SIGTERM/SIGHUP)

**Interactive mode** (`Run`): pipes stdin/stdout/stderr directly to the terminal. On signal, forwards it to Claude and waits for exit.

**Headless mode** (`RunHeadless`): runs `claude --print -p <prompt>`, captures output to `claude-output.log` with log rotation (10MB max, 3 backups). Supports timeout and graceful shutdown (SIGTERM then SIGKILL after 30 seconds).


Directory Structure
-------------------

### Source tree

```
.
├── main.go                       # CLI entry point
├── Makefile                      # Build with version injection via ldflags
├── go.mod / go.sum
├── README.md
├── CLAUDE.md                     # Agent instructions for working on this codebase
├── cmd/                          # CLI commands (Cobra)
│   ├── root.go                   # Root command, global flags
│   ├── mission_*.go              # mission new/ls/resume/stop/archive/rm/nuke/inspect
│   ├── config_*.go               # config init/edit
│   ├── cron_*.go                 # cron new/ls/rm/enable/disable/run/history/logs
│   ├── daemon_*.go               # daemon start/stop/restart/status/version-check
│   ├── repo_*.go                 # repo add/edit/rm/ls/update
│   ├── tmux_*.go                 # tmux attach/detach/window/pane/rm
│   ├── login.go                  # Claude login flow
│   ├── first_run.go              # First-time setup
│   ├── resolver.go               # Generic fuzzy-match resolver (fzf)
│   ├── repo_resolution.go        # Repo reference parsing
│   └── gendocs/                  # CLI doc generation
├── internal/
│   ├── config/                   # Path management, YAML config
│   ├── database/                 # SQLite CRUD
│   ├── mission/                  # Mission lifecycle, Claude spawning
│   ├── claudeconfig/             # Per-mission config merging
│   ├── daemon/                   # Background loops
│   ├── wrapper/                  # Claude child process management
│   ├── history/                  # Prompt extraction from history.jsonl
│   ├── session/                  # Session name resolution
│   ├── version/                  # Build-time version string
│   └── tableprinter/             # ANSI-aware table formatting
├── docs/                         # Documentation
│   └── cli/                      # Generated CLI reference
├── specs/                        # Design specs (historical reference)
└── scripts/                      # Utility scripts
```

### Runtime tree (`$AGENC_DIRPATH`, defaults to `~/.agenc/`)

```
$AGENC_DIRPATH/
├── database.sqlite                        # SQLite: missions table
│
├── config/                                # User configuration (optionally a git repo)
│   ├── config.yml                         # Cron jobs, synced repos, Claude config source
│   └── claude-modifications/              # AgenC-specific Claude config overrides
│       ├── CLAUDE.md                      # Appended to user's CLAUDE.md during merge
│       └── settings.json                  # Deep-merged with user's settings.json
│
├── repos/                                 # Shared repo library (daemon syncs these)
│   └── github.com/owner/repo/            # One clone per repo
│
├── missions/                              # Per-mission sandboxes
│   └── <uuid>/
│       ├── agent/                         # Git repo working directory
│       ├── claude-config/                 # Per-mission CLAUDE_CONFIG_DIR
│       │   ├── CLAUDE.md                  # Merged: config-source + claude-modifications
│       │   ├── settings.json              # Merged + hooks + deny entries
│       │   ├── .claude.json               # Symlink to user's account identity
│       │   ├── .credentials.json          # Dumped from Keychain (macOS) or file (Linux)
│       │   ├── skills/                    # From config source repo
│       │   ├── hooks/                     # From config source repo
│       │   ├── commands/                  # From config source repo
│       │   ├── agents/                    # From config source repo
│       │   └── plugins/                   # From config source repo
│       ├── pid                            # Wrapper process ID
│       ├── claude-state                   # "idle" or "busy" (written by Claude hooks)
│       ├── wrapper.log                    # Wrapper lifecycle log
│       └── claude-output.log              # Headless mode output (with rotation)
│
└── daemon/
    ├── daemon.pid                         # Daemon process ID
    ├── daemon.log                         # Daemon log
    └── daemon.version                     # Last recorded daemon version
```


Core Packages
-------------

### `internal/config/`

Path management and YAML configuration. All path construction flows from `GetAgencDirpath()`, which reads `$AGENC_DIRPATH` and falls back to `~/.agenc`.

- `config.go` — path helper functions (`GetMissionDirpath`, `GetRepoDirpath`, `GetDatabaseFilepath`, etc.), directory structure initialization (`EnsureDirStructure`), constant definitions for filenames and directory names
- `agenc_config.go` — `AgencConfig` struct (YAML round-trip with comment preservation), `CronConfig` struct, validation functions for repo format, cron names, schedules, timeouts, and overlap policies. Cron expression evaluation via the `gronx` library.
- `first_run.go` — `IsFirstRun()` detection

### `internal/mission/`

Mission lifecycle: directory creation, repo copying, and Claude process spawning.

- `mission.go` — `CreateMissionDir` (sets up mission directory, copies git repo, builds per-mission config), `SpawnClaude`/`SpawnClaudeWithPrompt`/`SpawnClaudeResume` (construct and start Claude `exec.Cmd` with 1Password integration and environment variables)
- `repo.go` — git repository operations: `CopyRepo`/`CopyAgentDir` (rsync-based), `ForceUpdateRepo` (fetch + reset to remote default branch), `ParseRepoReference`/`ParseGitHubRemoteURL` (handle shorthand, canonical, SSH, and HTTPS URL formats), `EnsureRepoClone`, `DetectPreferredProtocol` (infers SSH vs HTTPS from existing repos)

### `internal/claudeconfig/`

Per-mission Claude configuration building and merging.

- `build.go` — `BuildMissionConfigDir` (copies trackable items from config source, merges CLAUDE.md and settings.json, symlinks account identity, dumps credentials), `GetMissionClaudeConfigDirpath` (falls back to global config if per-mission doesn't exist), `ResolveConfigSourceDirpath`, `ResolveConfigCommitHash`
- `merge.go` — `DeepMergeJSON` (objects merge recursively, arrays concatenate, scalars overlay), `MergeClaudeMd` (concatenation), `MergeSettings` (deep-merge user + modifications, then apply operational overrides)
- `overrides.go` — `AgencHookEntries` (Stop and UserPromptSubmit hooks for idle detection), `AgencDenyPermissionTools` (deny Read/Glob/Grep/Write/Edit on repo library), `BuildRepoLibraryDenyEntries`

### `internal/database/`

SQLite mission tracking with auto-migration.

- `database.go` — `DB` struct (wraps `sql.DB` with max connections = 1 for SQLite), `Mission` struct, CRUD operations (`CreateMission`, `ListMissions`, `GetMission`, `ResolveMissionID`, `ArchiveMission`, `DeleteMission`), heartbeat updates, session name caching, cron association tracking. Idempotent migrations handle schema evolution.

### `internal/daemon/`

Background daemon with three concurrent loops.

- `daemon.go` — `Daemon` struct, `Run` method that starts and coordinates all goroutines
- `process.go` — daemon lifecycle: `ForkDaemon` (re-executes binary as detached process via setsid), `ReadPID`, `IsProcessRunning`, `StopDaemon` (SIGTERM then SIGKILL)
- `template_updater.go` — repo update loop (60-second interval, fetches synced + active-mission repos)
- `config_auto_commit.go` — config auto-commit loop (10-minute interval, git add/commit/push)
- `cron_scheduler.go` — cron scheduler loop (60-second interval, spawns headless missions, overlap policies, orphan adoption)

### `internal/wrapper/`

Per-mission Claude child process management.

- `wrapper.go` — `Wrapper` struct, `Run` (interactive mode), `RunHeadless` (headless mode with timeout and log rotation), background goroutines (heartbeat, claude-state watcher, remote refs watcher), signal handling
- `tmux.go` — tmux window renaming when `AGENC_TMUX=1`

### Utility packages

- `internal/version/` — single `Version` string set via ldflags at build time (`version.go`)
- `internal/history/` — `FindFirstPrompt` extracts the first user prompt from Claude's `history.jsonl` for a given mission UUID (`history.go`)
- `internal/session/` — `FindSessionName` resolves a mission's session name from Claude metadata (priority: custom-title > sessions-index.json summary > JSONL summary) (`session.go`)
- `internal/tableprinter/` — ANSI-aware table formatting using `rodaine/table` with `runewidth` for wide character support (`tableprinter.go`)


Key Architectural Patterns
--------------------------

### Per-mission config merging

Each mission gets its own `claude-config/` directory, built at creation time from three sources:

1. **Config source repo** — the user's registered Claude config repo (skills, hooks, commands, agents, plugins, CLAUDE.md, settings.json)
2. **AgenC modifications** — files in `$AGENC_DIRPATH/config/claude-modifications/` that overlay the user's config
3. **AgenC operational overrides** — programmatically injected hooks and deny permissions

Merging logic (`internal/claudeconfig/merge.go`):
- CLAUDE.md: simple concatenation (user content + modifications content)
- settings.json: recursive deep merge (user as base, modifications as overlay), then append operational overrides (hooks and deny entries)
- Deep merge rules: objects merge recursively, arrays concatenate, scalars from the overlay win

Credentials are handled separately: `.claude.json` is symlinked to the user's account identity file; `.credentials.json` is dumped as a real file from the macOS Keychain (via `security find-generic-password`) or copied from `~/.claude/.credentials.json` on Linux.

### Idle detection via Claude Code hooks

The wrapper needs to know whether a resumable conversation exists. This is accomplished via Claude Code's hook system.

The config merge injects two hooks into each mission's `settings.json` (`internal/claudeconfig/overrides.go`):

- **Stop hook** — writes `idle` to the `claude-state` file when Claude finishes responding
- **UserPromptSubmit hook** — writes `busy` to `claude-state` when the user submits a prompt

The `claude-state` file lives at `$AGENC_DIRPATH/missions/<uuid>/claude-state`. The hooks reference it via `$CLAUDE_PROJECT_DIR/../claude-state`.

The wrapper watches this file using fsnotify on the parent directory (not the file itself, because shell redirects like `echo idle > file` may atomically replace the file, which would break a direct file watch). When the state becomes `busy`, the wrapper records that a resumable conversation exists (`hasConversation` flag), enabling `claude -c` on resume.

### Heartbeat system

Each wrapper writes a heartbeat to the database every 30 seconds (`internal/wrapper/wrapper.go:writeHeartbeat`). The daemon uses heartbeat staleness (> 5 minutes) to determine which missions are actively running and should have their repos included in the sync cycle (`internal/daemon/template_updater.go`).

### Repo library

All repos are cloned into a shared library at `$AGENC_DIRPATH/repos/github.com/owner/repo/`. Missions copy from this library at creation time rather than cloning directly from GitHub.

The daemon keeps the library fresh by fetching and fast-forwarding every 60 seconds. The wrapper contributes by watching `.git/refs/remotes/origin/<branch>` for push events — when a mission pushes to its repo, the wrapper immediately force-updates the corresponding library clone so other missions get the changes without waiting for the next daemon cycle (debounced at 5 seconds).

Missions are denied Read/Glob/Grep/Write/Edit access to the repo library directory via injected deny permissions in settings.json (`internal/claudeconfig/overrides.go`).

### 1Password secret injection

When a mission's `agent/.claude/secrets.env` file exists, Claude is launched via `op run --env-file secrets.env --no-masking -- claude [args]`. The 1Password CLI resolves vault references (e.g., `op://vault/item/field`) into actual secret values and injects them as environment variables. If `secrets.env` is absent, Claude launches directly without `op`.

Implemented in `internal/mission/mission.go:buildClaudeCmd` and `internal/wrapper/wrapper.go:buildHeadlessClaudeCmd`.

### Config auto-sync

The `$AGENC_DIRPATH/config/` directory can optionally be a Git repo. The daemon's config auto-commit loop (`internal/daemon/config_auto_commit.go`) checks every 10 minutes: if there are uncommitted changes, it stages all, commits with a timestamped message, and pushes (skipping push if no `origin` remote exists). This keeps agent configuration version-controlled without manual effort.

### Cron scheduling

Cron jobs are defined in `config.yml` under the `crons` key. The daemon's cron scheduler (`internal/daemon/cron_scheduler.go`) evaluates cron expressions every 60 seconds using the `gronx` library.

Key behaviors:
- **Max concurrent limit** — configurable via `cronsMaxConcurrent` (default: 10). Crons are skipped when the limit is reached.
- **Overlap policies** — `skip` (default): skips if the previous run is still active. `allow`: permits concurrent runs of the same cron.
- **Double-fire guard** — checks the database for a mission created by this cron in the current minute, preventing duplicate spawns.
- **Orphan adoption** — on daemon startup, queries the database for cron-spawned missions, checks their wrapper PIDs, and adopts any that are still running into the scheduler's tracking map.
- **Graceful shutdown** — on daemon stop, sends SIGINT to all running cron missions and waits up to 60 seconds before force-killing.

Cron missions run in headless mode (`wrapper.RunHeadless`) with configurable timeout (default: 1 hour), capturing output to `claude-output.log` with log rotation.


Data Flow: Mission Lifecycle
-----------------------------

### Creation (`agenc mission new`)

1. CLI ensures the daemon is running and a config source repo is registered
2. Resolves the git repo reference (URL, shorthand, or fzf picker) and ensures it is cloned into the repo library
3. Creates a database record (`database.CreateMission`) — generates UUID + 8-char short ID, records the git repo name, config source commit hash, and optional cron association
4. Creates the mission directory structure (`mission.CreateMissionDir`):
   - Copies the repo from the library into `missions/<uuid>/agent/` via rsync
   - Builds the per-mission Claude config directory (`claudeconfig.BuildMissionConfigDir`)
5. Creates a `Wrapper` and calls `Run(isResume=false)` or `RunHeadless` depending on flags

### Running

1. Wrapper writes PID file and initial `claude-state`
2. Wrapper spawns Claude as a child process (with 1Password wrapping if needed)
3. Background goroutines start: heartbeat, claude-state watcher, remote refs watcher
4. Main event loop blocks until Claude exits or a signal arrives
5. Daemon concurrently syncs the mission's repo if it has a recent heartbeat

### Stopping

- **User-initiated** (`agenc mission stop`): reads PID file, sends SIGINT to wrapper, wrapper forwards to Claude, waits for exit, cleans up PID file
- **Natural exit**: Claude exits on its own (e.g., user types `/exit`), wrapper detects via `cmd.Wait()`, cleans up
- **Headless timeout**: context cancellation triggers SIGTERM to Claude, then SIGKILL after 30 seconds

### Resuming (`agenc mission resume`)

1. Creates a new `Wrapper` and calls `Run(isResume=true)`
2. Wrapper spawns `claude -c` (resume last conversation)
3. `hasConversation` flag is pre-set to `true`


Database Schema
---------------

### `missions` table

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT (PK) | Full UUID |
| `short_id` | TEXT (UNIQUE) | First 8 characters of UUID, for user-friendly display |
| `git_repo` | TEXT | Canonical repo name (`github.com/owner/repo`), empty for blank missions |
| `config_commit` | TEXT | Config source repo HEAD hash at mission creation (nullable) |
| `status` | TEXT | `active` or `archived` |
| `prompt` | TEXT | First user prompt, cached for listing display |
| `last_heartbeat` | TEXT | Last wrapper heartbeat timestamp (RFC3339, nullable) |
| `session_name` | TEXT | User-assigned or auto-generated session name |
| `session_name_updated_at` | TEXT | When `session_name` was last updated (nullable) |
| `cron_id` | TEXT | UUID of the cron that spawned this mission (nullable) |
| `cron_name` | TEXT | Name of the cron job (nullable, used for orphan tracking) |
| `created_at` | TEXT | Mission creation timestamp (RFC3339) |
| `updated_at` | TEXT | Last update timestamp (RFC3339) |

SQLite is opened with max connections = 1 (`SetMaxOpenConns(1)`) due to its single-writer limitation. Migrations are idempotent and run on every database open.

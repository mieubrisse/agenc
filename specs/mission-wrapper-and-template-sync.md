Mission Wrapper and Template Sync
===================================

Overview
--------

This spec describes four cooperating components that enable live-updating of Claude Code mission configurations:

1. **Template updater** -- a background goroutine in the `agenc` daemon that keeps local agent-template Git repos in sync with their GitHub remotes.
2. **Mission updater** -- a background goroutine in the `agenc` daemon that propagates template changes into active mission directories and signals running wrappers.
3. **Claude config sync** -- a background goroutine in the `agenc` daemon that maintains agenc's Claude config directory, merging the user's Claude settings with agenc-specific hooks.
4. **Mission wrapper** -- the foreground `agenc` process (one per mission) that supervises a `claude` child process, tracking its idle/busy state and gracefully restarting it when signaled.

The template updater, mission updater, and Claude config sync all run inside the existing `agenc` daemon alongside its current description-generation functionality. The mission wrapper is a separate foreground process -- one per active mission -- invoked by the user.

Together, these components allow a user to push changes to an agent-template repo on GitHub and have all running missions using that template automatically pick up the changes without interrupting in-progress work.


Agent Templates
---------------

Each agent template is a Git repository cloned into `~/.agenc/config/agent-templates/<name>/`. Templates are the single source of truth for mission configuration files. They are hosted on GitHub and managed entirely outside of agenc -- agenc only consumes them.

A template repo contains the config files that Claude Code needs (e.g., `CLAUDE.md`, `.claude/settings.json`, `.mcp.json`). The set of files in the template repo defines what gets synced into mission directories -- there is no hardcoded file list in agenc.


Mission Directory Structure
----------------------------

Each mission lives in `~/.agenc/missions/<uuid>/`. The directory contains agenc control files at the top level, and an `agent/` subdirectory that is the Claude Code project root.

```
~/.agenc/missions/<uuid>/
    pid                          # PID of the agenc wrapper process running this mission
    claude-state                 # 'idle' or 'busy', written by Claude hooks
    agent/                       # Claude Code project root
        CLAUDE.md                # owned by template, rsynced
        .claude/
            settings.json        # owned by template, rsynced
        .mcp.json                # owned by template, rsynced
        workspace/               # owned by Claude -- never overwritten by rsync
            ... (anything Claude creates during the mission)
```

The `agent/` subdirectory is where Claude is launched. Everything inside `agent/` except `workspace/` is owned by the template and will be overwritten on sync. Claude must do all its work inside `workspace/`. The template's CLAUDE.md should enforce this rule.

The `pid` and `claude-state` files live outside `agent/` so they are invisible to Claude and unaffected by template syncs.


Component 1: Template Updater
------------------------------

**Runs as:** Background goroutine in the agenc daemon.

**Cycle:** Every 60 seconds.

**Behavior:**

For each agent-template directory under `~/.agenc/config/agent-templates/`:

1. Run `git fetch origin` in the template directory.
2. Compare the local `main` ref against `origin/main`.
3. If `origin/main` is ahead, force-pull: `git reset --hard origin/main` (or equivalent).
4. If `origin/main` has not moved, do nothing.

**Rate limit considerations:**

GitHub allows 5000 authenticated API requests per hour. Each `git fetch` counts as one request. Polling 10 templates every 60 seconds = 600 requests/hour, well within limits. The poll interval should be configurable for users with many templates.


Component 2: Mission Updater
-----------------------------

**Runs as:** Background goroutine in the agenc daemon.

**Cycle:** Every 60 seconds.

**Behavior:**

For each active (non-archived) mission in the database:

1. Look up the mission's agent template name.
2. Rsync the template directory into the mission's `agent/` subdirectory, excluding `workspace/`, using the `--itemize-changes` flag. This overwrites all template-owned files and removes any files that no longer exist in the template. The `workspace/` directory is never touched.
3. Check the rsync output: if it is empty, no files changed -- skip to the next mission.
4. If any files were updated (non-empty output):
   a. Read the `pid` file from the mission directory.
   b. If a PID exists and the process is alive, send `SIGUSR1` to it.
   c. If no PID file or the process is dead, do nothing (the mission will pick up changes on next launch).


Component 3: Claude Config Sync
---------------------------------

**Runs as:** Background goroutine in the agenc daemon.

**Cycle:** Every 60 seconds.

**Purpose:** Agenc needs Claude instances to run with agenc-specific hooks (for state tracking), but the user's own Claude config in `~/.claude/` should otherwise be preserved. This component maintains an agenc-specific Claude config directory that merges the user's config with agenc's additions.

**Config directory:** `~/.agenc/claude-config/`

**Directory structure:**

```
~/.agenc/claude-config/
    CLAUDE.md              # symlink → ~/.claude/CLAUDE.md
    skills/                # symlink → ~/.claude/skills/
    commands/              # symlink → ~/.claude/commands/
    agents/                # symlink → ~/.claude/agents/
    plugins/               # symlink → ~/.claude/plugins/
    settings.json          # generated: user's settings.json + agenc hooks
```

**Behavior:**

1. For each of `CLAUDE.md`, `skills/`, `commands/`, `agents/`, `plugins/`:
   a. If the item exists in `~/.claude/`, ensure a symlink exists in `~/.agenc/claude-config/` pointing to it.
   b. If the item does not exist in `~/.claude/`, remove the symlink from `~/.agenc/claude-config/` if present.
2. Read `~/.claude/settings.json` and merge in the agenc-specific hooks (Stop and UserPromptSubmit). Write the result to `~/.agenc/claude-config/settings.json`.

**Hook configuration merged into settings.json:**

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo idle > \"$CLAUDE_PROJECT_DIR/../claude-state\""
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo busy > \"$CLAUDE_PROJECT_DIR/../claude-state\""
          }
        ]
      }
    ]
  }
}
```

The `$CLAUDE_PROJECT_DIR` environment variable is set by Claude Code to the project root, which is the `agent/` subdirectory. `../claude-state` navigates up to the mission root where the `claude-state` file lives.

**Merging rules:** The agenc hooks are appended to any existing hook arrays in the user's settings. If the user already has Stop or UserPromptSubmit hooks, the agenc hooks are added to the end of the array -- they do not replace the user's hooks.

The mission wrapper launches Claude with `CLAUDE_CONFIG_DIR` set to `~/.agenc/claude-config/`, so Claude picks up the merged config instead of `~/.claude/`.


Component 4: Mission Wrapper
-----------------------------

**Runs as:** The `agenc` process that the user invokes via `agenc mission new` or `agenc mission resume`. One per mission, typically running inside a tmux pane.

### Lifecycle

1. Perform mission setup (DB record, directory creation, rsync template files into `agent/` subdirectory).
2. Write the wrapper's own PID to the `pid` file in the mission directory (overwrites any stale value).
3. Write `busy` to the `claude-state` file (overwrites any stale value from a previous run).
4. Start watching the `claude-state` file with fsnotify for state change notifications.
5. Spawn `claude <prompt>` (or `claude -c` for resume) as a **child process** using `os/exec.Command`, with the working directory set to the `agent/` subdirectory and `CLAUDE_CONFIG_DIR` set to `~/.agenc/claude-config/`. Wire the child's stdin/stdout/stderr directly to the terminal so the user interacts with Claude normally.
6. Enter the main loop: wait for either the child to exit or a signal to arrive.
7. On natural exit (user typed `/exit` or Claude terminated), clean up and exit the wrapper.
8. On wrapper exit, remove the `pid` file.

### Key implementation detail

The wrapper must use `os/exec.Command` to spawn Claude, **not** `syscall.Exec`. `syscall.Exec` replaces the current process with Claude (Unix `execve`), which would destroy the wrapper. `os/exec.Command` forks a child process, keeping the wrapper alive as the parent.

### State tracking via Claude hooks

Two Claude hooks write the current state to the `claude-state` file in the mission directory (above `agent/`, invisible to Claude). These hooks are configured in the agenc Claude config directory (`~/.agenc/claude-config/settings.json`) by the Claude config sync component (see Component 3).

- **`Stop` hook:** Fires when Claude finishes responding and is about to wait for user input. Writes `idle` to `$CLAUDE_PROJECT_DIR/../claude-state`.
- **`UserPromptSubmit` hook:** Fires when the user submits a prompt. Writes `busy` to `$CLAUDE_PROJECT_DIR/../claude-state`.

The wrapper watches the `claude-state` file using fsnotify (kqueue on macOS, inotify on Linux) for instant notification of state changes without polling.

### Signal handling

The wrapper listens for `SIGUSR1`. When received:

1. Set an internal `restart_pending` flag.
2. Read the current state from the `claude-state` file.
3. If idle, restart immediately.
4. If busy, wait. When fsnotify reports the `claude-state` file changed to `idle`, restart.

### Restart procedure

1. Send `SIGINT` to the Claude child process.
2. Wait for the child to exit.
3. Spawn `claude -c` as a new child process (continues from the last completed conversation turn).
4. Clear the `restart_pending` flag.

Because the wrapper only restarts when Claude is idle (finished responding, waiting for user input), no generation is interrupted and no work is lost. `claude -c` resumes with the full conversation history intact.

### Distinguishing restart-exit from natural-exit

The wrapper must distinguish between:

- **Restart-triggered exit:** The wrapper sent SIGINT because of a pending restart. Action: relaunch with `claude -c`.
- **Natural exit:** The user quit Claude (e.g., `/exit`, Ctrl-C). Action: wrapper exits.

Implementation: the wrapper sets a boolean flag before sending SIGINT. When the child process exits, check the flag to decide whether to relaunch or exit.


PID File
--------

Each mission has a `pid` file at `~/.agenc/missions/<uuid>/pid` containing the PID of the wrapper process.

- Overwritten unconditionally by the wrapper on startup (any previous value from a stale or crashed wrapper is replaced).
- Removed by the wrapper on graceful exit.
- Read by the mission updater to send SIGUSR1.
- If the process at the stored PID is dead, the mission updater ignores it.

No database schema changes are needed for PID tracking.


Trust Dialog
------------

Claude Code prompts "do you trust this directory?" when launched in a new directory. There is no clean programmatic way to skip this prompt without `--dangerously-skip-permissions`. For now, the user presses Enter once per mission to accept the trust dialog. This is a known Claude Code limitation (GitHub issues #9113, #12227, #9256) and may be resolved upstream in the future.


Config File Merging
-------------------

There are two levels of config:

- **Project-level config** (CLAUDE.md, `.claude/settings.json`, `.mcp.json` inside `agent/`): No merging. The template is the sole source of truth. The entire template directory is rsynced into the mission's `agent/` subdirectory (excluding `workspace/`).
- **Global-level config** (`~/.agenc/claude-config/`): The Claude config sync component (Component 3) merges the user's `~/.claude/settings.json` with agenc-specific hooks. All other global config items are symlinked through from `~/.claude/`.


What Does NOT Change
--------------------

- The SQLite database and its existing schema.
- The `agenc mission archive` command and its behavior.
- The `agenc mission ls` command and its behavior.
- The daemon's existing description-generation functionality.

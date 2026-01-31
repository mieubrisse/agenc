Mission Wrapper and Template Sync
===================================

Overview
--------

This spec describes three cooperating components that enable live-updating of Claude Code mission configurations:

1. **Template updater** -- a background goroutine in the `agenc` daemon that keeps local agent-template Git repos in sync with their GitHub remotes.
2. **Mission updater** -- a background goroutine in the `agenc` daemon that propagates template changes into active mission directories and signals running wrappers.
3. **Mission wrapper** -- the foreground `agenc` process (one per mission) that supervises a `claude` child process, tracking its idle/busy state and gracefully restarting it when signaled.

The template updater and mission updater both run inside the existing `agenc` daemon alongside its current description-generation functionality. The mission wrapper is a separate foreground process -- one per active mission -- invoked by the user.

Together, these components allow a user to push changes to an agent-template repo on GitHub and have all running missions using that template automatically pick up the changes without interrupting in-progress work.


Agent Templates
---------------

Each agent template is a Git repository cloned into `~/.agenc/config/agent-templates/<name>/`. Templates are the single source of truth for mission configuration files. They are hosted on GitHub and managed entirely outside of agenc -- agenc only consumes them.

A template repo contains the config files that Claude Code needs:

- `CLAUDE.md`
- `.claude/settings.json`
- `.mcp.json`

The exact set of files may evolve, but these are the known config files today.


Mission Directory Structure
----------------------------

When a mission is created, the known config files are copied from the template into the mission directory. The mission directory is a plain directory -- not a Git repo. Claude has no awareness that a template exists.

```
~/.agenc/missions/<uuid>/
    CLAUDE.md                    # copied from template
    .claude/
        settings.json            # copied from template
    .mcp.json                    # copied from template
    ... (anything Claude creates during the mission)
```


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
2. For each known config file (CLAUDE.md, `.claude/settings.json`, `.mcp.json`):
   a. Compare the file in the mission directory against the file in the template directory.
   b. If they differ, copy the template file into the mission directory (overwrite).
   c. If the template file does not exist, remove it from the mission directory if present.
3. If any files were updated:
   a. Look up `wrapper_pid` from the database for this mission.
   b. If a PID exists and the process is alive, send `SIGUSR1` to it.
   c. If no PID or the process is dead, do nothing (the mission will pick up changes on next launch).


Component 3: Mission Wrapper
-----------------------------

**Runs as:** The `agenc` process that the user invokes via `agenc mission new` or `agenc mission resume`. One per mission, typically running inside a tmux pane.

### Lifecycle

1. Perform mission setup (DB record, directory creation, config file copying from template).
2. Write the wrapper's own PID to the `wrapper_pid` column in the missions DB table.
3. Configure Claude hooks for state tracking (see below).
4. Spawn `claude <prompt>` (or `claude -c` for resume) as a **child process** using `os/exec.Command`. Wire the child's stdin/stdout/stderr directly to the terminal so the user interacts with Claude normally.
5. Enter the main loop: wait for either the child to exit or a signal to arrive.
6. On natural exit (user typed `/exit` or Claude terminated), clean up and exit the wrapper.
7. On wrapper exit, clear `wrapper_pid` in the database.

### Key implementation detail

The wrapper must use `os/exec.Command` to spawn Claude, **not** `syscall.Exec`. `syscall.Exec` replaces the current process with Claude (Unix `execve`), which would destroy the wrapper. `os/exec.Command` forks a child process, keeping the wrapper alive as the parent.

### State tracking via Claude hooks

The wrapper configures two hooks in the mission's `.claude/settings.json` (these should be included in the template's settings.json):

- **`Stop` hook:** Fires when Claude finishes responding and is about to wait for user input. The hook writes `idle` to a state file at `<mission-dir>/.agenc-state`.
- **`UserPromptSubmit` hook:** Fires when the user submits a prompt. The hook writes `busy` to the same state file.

Hook configuration in `.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo idle > \"$CLAUDE_PROJECT_DIR/.agenc-state\""
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "echo busy > \"$CLAUDE_PROJECT_DIR/.agenc-state\""
          }
        ]
      }
    ]
  }
}
```

The wrapper monitors this file to know whether Claude is idle or busy.

### Signal handling

The wrapper listens for `SIGUSR1`. When received:

1. Set an internal `restart_pending` flag.
2. Check Claude's current state by reading `.agenc-state`.
3. If idle, restart immediately.
4. If busy, wait. When the state file changes to `idle`, restart.

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


Database Changes
----------------

Add a `wrapper_pid` column (nullable integer) to the `missions` table.

- Set to the wrapper's PID when the wrapper starts.
- Cleared (set to NULL) when the wrapper exits.
- Read by the mission updater to send SIGUSR1.
- If the process at the stored PID is dead (stale entry), the mission updater ignores it. The wrapper should clear stale PIDs on startup if one exists for its mission.


Trust Dialog
------------

Claude Code prompts "do you trust this directory?" when launched in a new directory. There is no clean programmatic way to skip this prompt without `--dangerously-skip-permissions`. For now, the user presses Enter once per mission to accept the trust dialog. This is a known Claude Code limitation (GitHub issues #9113, #12227, #9256) and may be resolved upstream in the future.


Config File Merging
-------------------

The current implementation merges a global config with an agent template config when creating a mission. Under this new architecture, **there is no merging**. The template is the sole source of truth. Config files are copied directly from the template into the mission directory. Global config will be addressed separately in the future.


What Does NOT Change
--------------------

- The SQLite database and its existing schema (aside from the new `wrapper_pid` column).
- The `agenc mission archive` command and its behavior.
- The `agenc mission ls` command and its behavior.
- The daemon's existing description-generation functionality.

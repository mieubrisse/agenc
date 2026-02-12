---
name: agenc-self-usage
description: AgenC CLI quick reference for managing missions, repos, config, cron jobs, and the daemon from within a mission.
---

AgenC CLI Quick Reference
=========================

You are running inside an **AgenC mission** — an isolated sandbox managed by the `agenc` CLI. You can use `agenc` to manage the system you are running in: spawn new missions, manage repos, configure cron jobs, check status, and update config.

The `agenc` binary is in your PATH. Your current mission's UUID is in `$AGENC_MISSION_UUID`.

**Critical constraint:** Missions are ephemeral. Local filesystems do not persist after a mission ends. Always commit and push your work.

**Never use interactive commands** that open `$EDITOR` or require terminal input without arguments — they will hang. Avoid: `agenc config edit`, `agenc cron new`. Use non-interactive alternatives (`agenc config set`, direct config.yml editing).


Manage agent missions
---------------------

```
agenc mission archive [mission-id|search-terms...]        # Stop and archive one or more missions
agenc mission inspect [mission-id|search-terms...]        # Print information about a mission
agenc mission ls                                          # List active missions
agenc mission new [search-terms...]                       # Create a new mission and launch claude
agenc mission nuke                                        # Stop and permanently remove ALL missions
agenc mission resume [mission-id|search-terms...]         # Unarchive (if needed) and resume a mission with claude --continue
agenc mission rm [mission-id|search-terms...]             # Stop and permanently remove one or more missions
agenc mission send                                        # Send messages to a running mission wrapper
agenc mission stop [mission-id|search-terms...]           # Stop one or more mission wrapper processes
agenc mission update-config [mission-id|search-terms...]  # Rebuild a mission's Claude config from the shadow repo
```

Manage the repo library
-----------------------

```
agenc repo add <repo>              # Add a repository to the repo library
agenc repo edit [search-terms...]  # Edit a repo via a new mission with a repo copy
agenc repo ls                      # List repositories in the repo library
agenc repo rm [repo...]            # Remove a repository from the repo library
agenc repo update [repo...]        # Fetch and reset repos to match their remote
```

Manage agenc configuration
--------------------------

```
agenc config edit               # Open config.yml in your editor ($EDITOR)
agenc config get <key>          # Get a config value
agenc config init               # Initialize agenc configuration (interactive)
agenc config palette-command    # Manage palette commands
agenc config set <key> <value>  # Set a config value
```

Manage scheduled cron jobs
--------------------------

```
agenc cron disable <name>  # Disable a cron job
agenc cron enable <name>   # Enable a cron job
agenc cron history <name>  # Show run history for a cron job
agenc cron logs <name>     # View output logs from the most recent cron run
agenc cron ls              # List all cron jobs
agenc cron new [name]      # Create a new cron job (interactive wizard)
agenc cron rm <name>       # Remove a cron job from config
agenc cron run <name>      # Manually trigger a cron job (runs headless, untracked by cron_id)
```

Manage the background daemon
----------------------------

```
agenc daemon restart  # Restart the background daemon
agenc daemon start    # Start the background daemon
agenc daemon status   # Show daemon status
agenc daemon stop     # Stop the background daemon
```

Manage the AgenC tmux session
-----------------------------

```
agenc tmux attach                     # Attach to the AgenC tmux session, creating it if needed
agenc tmux detach                     # Detach from the AgenC tmux session
agenc tmux inject                     # Install AgenC tmux keybindings
agenc tmux palette                    # Open the AgenC command palette (runs inside a tmux display-popup)
agenc tmux pane                       # Manage tmux panes in the AgenC session
agenc tmux resolve-mission <pane-id>  # Resolve a tmux pane to its mission UUID
agenc tmux rm                         # Destroy the AgenC tmux session, stopping all running missions
agenc tmux window                     # Manage tmux windows in the AgenC session
```

Other commands
--------------

```
agenc doctor       # Check for common configuration issues
agenc login        # Log in to Claude and update credentials for all missions
agenc version      # Print the agenc version
```

Repo Formats
------------

All repo arguments accept these formats:

- `owner/repo` — shorthand
- `github.com/owner/repo` — canonical
- `https://github.com/owner/repo` — HTTPS URL
- `git@github.com:owner/repo.git` — SSH URL

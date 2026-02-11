Configuration
=============

Environment Variables
---------------------

| Variable | Default | Description |
|---|---|---|
| `AGENC_DIRPATH` | `~/.agenc` | Root directory for all AgenC state (configurable) |

config.yml
----------

The file at `$AGENC_DIRPATH/config/config.yml` is the central configuration file. All repo values must be in canonical format: `github.com/owner/repo`. The CLI accepts shorthand — `owner/repo`, `github.com/owner/repo`, or a full GitHub URL — and normalizes it automatically.

```yaml
# Repos to keep synced in the shared library (daemon fetches every 60s)
# Plain string format:
syncedRepos:
  - github.com/owner/repo
  # Structured format with optional windowTitle (overrides tmux window name):
  - repo: github.com/owner/other-repo
    windowTitle: "other"

# Max concurrent headless cron missions (default: 10)
cronsMaxConcurrent: 10

# Named cron jobs
crons:
  my-cron:
    schedule: "0 9 * * *"      # Cron expression (5 or 6 fields, evaluated by gronx)
    prompt: "Do something"     # Initial prompt sent to Claude
    description: ""            # Human-readable description (optional)
    git: github.com/owner/repo # Git repo for the mission workspace (optional)
    timeout: "1h"              # Max runtime as Go duration (default: 1h)
    overlap: skip              # "skip" (default) or "allow"
    enabled: true              # Defaults to true if omitted

# Palette commands — customize the tmux command palette and keybindings
paletteCommands:
  # Override a builtin's keybinding
  startMission:
    tmuxKeybinding: "C-n"

  # Disable a builtin entirely (no palette entry, no keybinding)
  nukeMissions: {}

  # Custom command with palette entry + tmux keybinding
  dotfiles:
    title: "Open dotfiles"
    description: "Start a dotfiles mission"
    command: "agenc tmux window new -- agenc mission new mieubrisse/dotfiles"
    tmuxKeybinding: "f"

  # Custom command, palette only (no keybinding)
  logs:
    title: "Daemon logs"
    command: "agenc tmux window new -- agenc daemon logs"

# Override the command palette keybinding (default: "-T agenc k")
# The value is inserted verbatim after "bind-key" in the tmux config.
# paletteTmuxKeybinding: "-T agenc p"    # still in agenc table, different key
# paletteTmuxKeybinding: "C-k"           # bind directly on prefix

# Override the agenc binary path used in tmux keybindings and palette commands
# tmuxAgencFilepath: /usr/local/bin/agenc-dev
```

syncedRepos
-----------

A list of repositories the daemon keeps continuously up-to-date (fetched and fast-forwarded every 60 seconds). Use `syncedRepos` for repos you want kept fresh in the shared library.

Each entry can be a plain string or a structured object:

```yaml
syncedRepos:
  - github.com/owner/repo                    # plain string
  - repo: github.com/owner/other-repo        # structured with optional fields
    windowTitle: "other"                      # custom tmux window title
```

When `windowTitle` is set, missions using that repo will display the custom title in the tmux window tab instead of the default repo name.

Manage the list via the CLI:

```
agenc repo add owner/repo --sync   # clone and add to syncedRepos
agenc repo rm owner/repo           # remove from disk and syncedRepos
```

crons
-----

Cron jobs spawn headless missions on a schedule. Each cron needs at minimum a `schedule` (cron expression) and a `prompt` (what to tell Claude). The daemon evaluates cron expressions every 60 seconds.

Key behaviors:
- **Overlap policy:** `skip` (default) prevents a new run if the previous one is still active. `allow` permits concurrent runs.
- **Timeout:** Defaults to 1 hour. After timeout, the mission receives SIGTERM then SIGKILL after 30 seconds.
- **Max concurrent:** Controlled by `cronsMaxConcurrent` (default: 10). Crons are skipped when the limit is reached.

Manage crons via the CLI:

```
agenc cron new           # create a new cron job
agenc cron ls            # list all cron jobs
agenc cron enable <name> # enable a disabled cron
agenc cron run <name>    # trigger a cron immediately
agenc cron logs <name>   # view output from the latest run
```

paletteCommands
---------------

Palette commands control what appears in the tmux command palette (prefix + a, k) and which tmux keybindings are generated. AgenC ships with built-in palette commands for common operations.

Each entry supports four fields:
- **title** — label shown in the palette picker (entries without a title are keybinding-only)
- **description** — context shown alongside the title
- **command** — full shell command to execute (e.g. `agenc tmux window new -- agenc mission new`)
- **tmuxKeybinding** — tmux key bound in the agenc key table (e.g. `"f"`, `"C-n"`)

**Merge rules for builtins:**
- Key absent from config: full defaults
- Key present with `{}` (all fields empty): disabled entirely
- Key present with some fields set: non-empty fields override defaults, empty fields keep defaults

The palette keybinding defaults to `-T agenc k` (prefix + a, k). The value is inserted verbatim after `bind-key` in the generated tmux config, so you control the full binding:

```
agenc config set paletteTmuxKeybinding "-T agenc p"  # still in agenc table, different key
agenc config set paletteTmuxKeybinding "C-k"         # bind directly on prefix (no agenc table)
agenc config get paletteTmuxKeybinding               # check current value
```

Manage palette commands via the CLI:

```
agenc config palette-command ls                                    # list all (builtin + custom)
agenc config palette-command add myCmd --title="Test" --command="agenc do" --keybinding="t"
agenc config palette-command update startMission --keybinding="C-n"  # override builtin
agenc config palette-command rm myCmd                              # remove custom
agenc config palette-command rm startMission                       # restore builtin defaults
```

Config Auto-Sync
----------------

The `$AGENC_DIRPATH/config/` directory can be a Git repository. When it is, the daemon automatically commits and pushes any uncommitted changes every 10 minutes, using a commit message of the form:

```
2026-02-04T15:30:00Z agenc auto-commit
```

This keeps your agenc configuration version-controlled without manual effort. Changes to `config.yml`, `claude-modifications/`, or any other files in the config directory are captured automatically.

The push step is skipped if the repository has no `origin` remote — so a purely local Git repo (e.g. `git init` with no remote) will still get periodic commits for local history without push errors.

### First-run setup

On the very first run, if stdin is a TTY, agenc prompts you to optionally clone an existing config repo:

```
Welcome to agenc! Setting up for the first time.

Do you have an existing agenc config repo to clone? [y/N]
```

Answering **yes** lets you provide a repo reference (`owner/repo`, `github.com/owner/repo`, or a GitHub URL), which agenc clones into `$AGENC_DIRPATH/config/`. This lets you restore your agenc configuration on a new machine or share it across machines. Answering **no** (or running non-interactively) proceeds with the default empty config.

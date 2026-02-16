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
# Per-repo configuration (keyed by canonical repo name)
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true                # daemon fetches every 60s (optional, default: false)
    windowTitle: "my-repo"            # custom tmux window name (optional)

<!--
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
-->

# Palette commands — customize the tmux command palette and keybindings
paletteCommands:
  # Override a builtin's keybinding
  newMission:
    tmuxKeybinding: "C-n"

  # Disable a builtin entirely (no palette entry, no keybinding)
  nukeMissions: {}

  # Custom command with keybinding (in agenc table: prefix + a, f)
  dotfiles:
    title: "Open dotfiles"
    description: "Start a dotfiles mission"
    command: "agenc tmux window new -- agenc mission new mieubrisse/dotfiles"
    tmuxKeybinding: "f"

  # Global keybinding (root table, no prefix needed: Ctrl-s)
  stopThisMission:
    title: "Stop Mission"
    command: "agenc mission stop $AGENC_CALLING_MISSION_UUID"
    tmuxKeybinding: "-n C-s"

  # Custom command, palette only (no keybinding)
  logs:
    title: "Daemon logs"
    command: "agenc tmux window new -- agenc daemon logs"

# Override the command palette keybinding (default: "-T agenc k")
# The value is inserted verbatim after "bind-key" in the tmux config.
# paletteTmuxKeybinding: "-T agenc p"    # still in agenc table, different key
# paletteTmuxKeybinding: "C-k"           # bind directly on prefix

# Tmux window tab coloring — visual feedback for Claude state
# tmuxWindowTitle:
#   busyBackgroundColor: "colour018"        # background when Claude is working (default: colour018; empty = disable)
#   busyForegroundColor: ""                 # foreground when Claude is working (default: ""; empty = disable)
#   attentionBackgroundColor: "colour136"   # background when Claude needs attention (default: colour136; empty = disable)
#   attentionForegroundColor: ""            # foreground when Claude needs attention (default: ""; empty = disable)

```

repoConfig
----------

Per-repo configuration, keyed by canonical repo name (`github.com/owner/repo`). Each entry supports two optional settings:

- **alwaysSynced** — when `true`, the daemon keeps the repo continuously fetched and fast-forwarded (every 60 seconds). Defaults to `false`.
- **windowTitle** — custom tmux window name for missions using this repo. When set, missions display this title instead of the default repo name.

```yaml
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true
  github.com/owner/other-repo:
    alwaysSynced: true
    windowTitle: "other"
```

Manage via the CLI:

```
agenc config repoConfig ls                                                  # list all repo configs
agenc config repoConfig set github.com/owner/repo --always-synced=true      # enable auto-sync
agenc config repoConfig set github.com/owner/repo --window-title="my-repo"  # set window title
agenc config repoConfig rm github.com/owner/repo                            # remove config entry
agenc repo add owner/repo --always-synced                                    # clone and enable sync
agenc repo rm owner/repo                                                     # remove from disk and config
```

<!--
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
-->

paletteCommands
---------------

Palette commands control what appears in the tmux command palette (prefix + a, k) and which tmux keybindings are generated. AgenC ships with built-in palette commands for common operations.

Each entry supports four fields:
- **title** — label shown in the palette picker (entries without a title are keybinding-only)
- **description** — context shown alongside the title
- **command** — full shell command to execute (e.g. `agenc tmux window new -- agenc mission new`)
- **tmuxKeybinding** — tmux keybinding. By default, a bare key like `"f"` or `"C-n"` is bound in the agenc key table (prefix + a, key). To make a global binding in the root table (no prefix needed), use `"-n C-s"` syntax — the value is passed through to tmux's `bind-key` command

**Merge rules for builtins:**
- Key absent from config: full defaults
- Key present with `{}` (all fields empty): disabled entirely
- Key present with some fields set: non-empty fields override defaults, empty fields keep defaults

The palette keybinding defaults to `-T agenc k` (prefix + a, k). The value is inserted verbatim after `bind-key` in the generated tmux config, so you control the full binding:

```
agenc config set paletteTmuxKeybinding "-T agenc p"  # still in agenc table, different key
agenc config set paletteTmuxKeybinding "C-k"         # bind directly on prefix (no agenc table)
agenc config get paletteTmuxKeybinding               # check current value
agenc config unset paletteTmuxKeybinding             # revert to default
```

Manage palette commands via the CLI:

```
agenc config paletteCommand ls                                    # list all (builtin + custom)
agenc config paletteCommand add myCmd --title="Test" --command="echo hello" --keybinding="t"
agenc config paletteCommand update newMission --keybinding="C-n"  # override builtin
agenc config paletteCommand rm myCmd                              # remove custom
agenc config paletteCommand rm newMission                         # restore builtin defaults
```

Tmux Window Coloring
--------------------

When running missions inside the AgenC tmux session, window tabs change color to provide visual feedback about Claude's state. Each state supports independent foreground and background colors:

- **Busy** — displayed when Claude is actively processing
  - Background (default: `colour018`, dark blue)
  - Foreground (default: none)
- **Attention** — displayed when Claude is idle, waiting for user input, or needs permission
  - Background (default: `colour136`, orange)
  - Foreground (default: none)

Setting any color to empty string disables that color component. If both foreground and background are empty for a state, no color override is applied.

```
# Set background and foreground for busy state
agenc config set tmuxWindowTitle.busyBackgroundColor red
agenc config set tmuxWindowTitle.busyForegroundColor white

# Use tmux color numbers (see 'tmux list-colors')
agenc config set tmuxWindowTitle.attentionBackgroundColor colour220

# Disable busy background coloring
agenc config set tmuxWindowTitle.busyBackgroundColor ""

# Check current settings
agenc config get tmuxWindowTitle.busyBackgroundColor
agenc config get tmuxWindowTitle.attentionForegroundColor

# Revert to defaults
agenc config unset tmuxWindowTitle.busyBackgroundColor
```

In `config.yml`, these live under the `tmuxWindowTitle` key:

```yaml
tmuxWindowTitle:
  busyBackgroundColor: "colour018"
  busyForegroundColor: "white"
  attentionBackgroundColor: "colour136"
  attentionForegroundColor: ""
```

**Note:** Color changes take effect for new missions. Existing missions retain the colors they started with until they're stopped and resumed.

Git Protocol Preference
-----------------------

When cloning repos, AgenC determines the protocol (SSH vs HTTPS) using this priority order:

1. **gh config get git_protocol** — if the GitHub CLI is installed and `git_protocol` is set, AgenC uses that setting
2. **Existing repos** — if you have repos in your library, AgenC infers the protocol from their remotes
3. **User prompt** — if neither applies, AgenC asks you to choose

To set a persistent default:

```
gh config set git_protocol ssh     # use SSH (git@github.com:...)
gh config set git_protocol https   # use HTTPS (https://github.com/...)
```

This affects all future `agenc repo add` and `agenc mission new` operations.
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

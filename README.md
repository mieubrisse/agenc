![](./image.png)

AgenC
=====

Multi-agent development is plate-spinning. You're building a feature in one Claude session, spot a prompt that needs tweaking, pop open another session to fix it, switch back to approve a permissions dialog, realize your other session just pushed to the same branch - and suddenly you're not coding anymore, you're just managing Claude sessions.

AgenC (pronounced "agency") is a CLI built for that chaos. Every Claude Code session runs in an isolated sandbox called a **mission** - its own repo copy, its own config, no collision risk. Need to fork off and fix something? Open a side mission from the command palette, handle it, close it, pick up where you left off. The gap between "I need to do this thing" and "I'm doing this thing" stays near zero, no matter how many plates you're spinning.

Quick Start
-----------

### Prerequisites

- **macOS** (Linux support planned)
- **Claude Code** installed and in your PATH ([installation guide](https://docs.anthropic.com/en/docs/claude-code/getting-started))

### Install

```
brew tap mieubrisse/agenc
brew install agenc
```

This automatically installs required dependencies (`gh`, `fzf`).

### Launch a mission

Start a mission on any GitHub repo:

```
agenc mission new owner/repo
```

AgenC accepts multiple formats:

```
agenc mission new owner/repo                          # shorthand
agenc mission new github.com/owner/repo               # canonical
agenc mission new https://github.com/owner/repo       # HTTPS URL
agenc mission new git@github.com:owner/repo.git       # SSH URL
```

The repo is cloned into an isolated sandbox and Claude launches ready to work. Each mission gets its own copy of the repo, so multiple missions can run against the same repo without interfering with each other.

To launch a mission without a repo (e.g., for general-purpose tasks):

```
agenc mission new --blank
```

Or run `agenc mission new` with no arguments to get an interactive picker.

CLI Reference
-------------

Run `agenc --help` for available commands, or see [docs/cli/](docs/cli/) for complete documentation.

<!--- TODO Debora feedback - why use AgenC? There are a million AIs out there; why do we need this one? -->

Authentication
--------------

Each mission runs Claude with its own isolated config directory (`CLAUDE_CONFIG_DIR`). This means each mission gets its own macOS Keychain entry for credentials, separate from the global `Claude Code-credentials` entry that Claude Code uses by default.

AgenC handles the plumbing so you rarely need to think about this — but it helps to understand what's happening.

### How credentials flow

When you create a mission, AgenC clones the global Keychain credentials into a per-mission Keychain entry (named `Claude Code-credentials-<hash>`, where the hash is derived from the mission's config directory path). Claude inside the mission reads from that per-mission entry instead of the global one.

When the mission's Claude process exits, AgenC merges any new credentials back into the global entry. This means if Claude acquires an OAuth token during a mission (e.g. authenticating with an MCP server like Todoist), that token propagates to the global entry and becomes available to every future mission — no re-authentication needed.

The merge is per-server and timestamp-aware: for each MCP server, the token with the newest `expiresAt` wins. Tokens that exist only on one side are always preserved.

```
Global Keychain ──clone──▶ Per-mission Keychain
       ▲                          │
       │                     Claude runs,
       │                     may acquire
       │                     MCP OAuth tokens
       │                          │
       └────merge back────────────┘
            (on exit)
```

### `agenc login`

Run `agenc login` when:

- **First-time setup** — you haven't run `claude login` yet, or you're setting up a new machine
- **Credentials expired** — Claude sessions fail to authenticate
- **After re-authenticating with Claude** — so existing missions pick up the new credentials

The command opens an interactive Claude shell where you run `/login`, authorize in the browser, and exit. AgenC then propagates the updated credentials to all existing missions.

```
agenc login
```

You do **not** need to run `agenc login` for MCP OAuth tokens — those propagate automatically when you authenticate inside any mission.

### MCP OAuth tokens

MCP servers that use OAuth (like Todoist) prompt for authentication the first time you use them. Once you authenticate inside any mission:

1. The OAuth token is stored in that mission's Keychain entry
2. When the Claude process exits, AgenC merges the token back to the global entry
3. The next mission you create inherits the token — no re-auth prompt

If a token expires and Claude refreshes it during a mission, the refreshed token (with a newer `expiresAt`) replaces the stale one in the global entry on exit.

Tips
----

- **Use tmux.** Run `agenc tmux attach` to enter the AgenC tmux session. The command palette, window management, and keybindings all live here - it's the intended way to use AgenC.
- **Rename missions when you stop them.** When you run `agenc mission stop`, give the mission a descriptive name so you can find it later with `agenc mission resume`. A wall of unnamed missions is hard to navigate.
- **Open a shell pane with prefix + %.** Inside the AgenC tmux session, the standard tmux split (`prefix + %`) opens a shell in the mission's workspace directory. Handy for running tests, checking git status, or poking around while Claude works.

How It Works
------------

Most people treat their Claude sessions like pets - nursing context, afraid to close the window. AgenC makes them cheap. Stop a mission, fork off to handle something else, come back to the original later. The durable context lives in your Claude config, not in any single session.

AgenC is built on one principle: **[Inputs, Not Outputs](https://mieubrisse.substack.com/p/inputs-not-outputs)**. Instead of correcting an agent's output after the fact, you fix the input (its configuration) so the problem never recurs.

1. You launch a **mission** from a repo. AgenC clones the repo into an isolated sandbox and starts a Claude session.
2. When something goes wrong — bad output, missing permissions, wrong approach — you roll that lesson back into your Claude configuration (CLAUDE.md, settings.json, skills, etc.).
3. Every future mission benefits from the fix. Over time, your agents compound in capability.

Sandboxing and session management let you run dozens of missions simultaneously, each isolated from the others. For a detailed technical overview, see [System Architecture](docs/system-architecture.md).

<!--
> ⚠️ Addiction Warning
>
> Like other agentic work factories, AgenC makes thought -> implemented reality nearly instantaneous.
>
> This is breathtaking, like going from Minecraft Survival -> Creative Mode. But there's a real danger to watch out for.
>
> The system goes as fast as you can tell it what to do, so suddenly the limiting factor is your ability to make decisions.
>
> Meaning, your head is going to be buzzing with a dozen threads at once, constantly deciding, constantly building with no downtime. It's like the deepest flow state you've ever had.
>
> This leaves you really activated, always wanting to implement one more thing. And it's really bad for sleep.
>
> This isn't just AgenC. [Across the board, agentic work factories seem to have this effect](https://steve-yegge.medium.com/steveys-birthday-blog-34f437139cb5#:~:text=This%20week%20the,Even%20for%20him.).
>
> So please stop for breaks, and remember to make some wind-down time for sleep!
-->

Workflows
---------

AgenC currently supports:

- **Human-in-the-loop coding:** Launch a mission on a repo, work with Claude interactively, commit and push from within the sandbox.
- **Human-in-the-loop assistant work with MCP:** Connect Claude to external services (Todoist, Notion, email) via MCP servers for inbox processing, calendar management, and similar tasks.
- **Scheduled autonomous work:** Define cron jobs that spawn headless missions on a schedule — daily reports, weekly cleanups, recurring maintenance.

The core loop is: launch missions, observe what works and what doesn't, refine your Claude config, and repeat. Basically, [painting with your mind](https://mieubrisse.substack.com/p/be-rembrandt).

Troubleshooting
---------------

### "Command Line Tools are too outdated"

If you see this error during installation:

```
Error: Your Command Line Tools are too outdated.
```

This is a [Homebrew requirement](https://docs.brew.sh/Common-Issues#homebrew-is-slow), not an AgenC issue. Homebrew requires up-to-date Xcode Command Line Tools to function, even when installing pre-built binaries.

To fix, update your Command Line Tools:

```
xcode-select --install
```

If that doesn't work, remove and reinstall:

```
sudo rm -rf /Library/Developer/CommandLineTools
xcode-select --install
```

Then retry `brew install agenc`.

Configuration
-------------

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `AGENC_DIRPATH` | `~/.agenc` | Root directory for all AgenC state (configurable) |

### config.yml

The file at `$AGENC_DIRPATH/config/config.yml` is the central configuration file. All repo values must be in canonical format: `github.com/owner/repo`. The CLI accepts shorthand — `owner/repo`, `github.com/owner/repo`, or a full GitHub URL — and normalizes it automatically.

```yaml
# Repos to keep synced in the shared library (daemon fetches every 60s)
syncedRepos:
  - github.com/owner/repo

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

# Custom entries for the tmux command palette
customCommands:
  my-shortcut:
    paletteName: "📝 Open my project"   # Label shown in the palette picker
    args: "mission new owner/repo"      # Arguments passed to agenc
```

#### syncedRepos

A list of repositories the daemon keeps continuously up-to-date (fetched and fast-forwarded every 60 seconds). Use `syncedRepos` for repos you want kept fresh in the shared library.

Manage the list via the CLI:

```
agenc repo add owner/repo --sync   # clone and add to syncedRepos
agenc repo rm owner/repo           # remove from disk and syncedRepos
```

#### crons

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

#### customCommands

Custom commands add entries to the tmux command palette (opened with the palette keybinding). Each entry needs a `paletteName` (what you see in the picker) and `args` (the agenc subcommand to run).

Manage custom commands via the CLI:

```
agenc config custom-command add my-shortcut --palette-name "📝 Open my project" --args "mission new owner/repo"
agenc config custom-command ls
agenc config custom-command rm my-shortcut
```

### Config Auto-Sync

The `$AGENC_DIRPATH/config/` directory can be a Git repository. When it is, the daemon automatically commits and pushes any uncommitted changes every 10 minutes, using a commit message of the form:

```
2026-02-04T15:30:00Z agenc auto-commit
```

This keeps your agenc configuration version-controlled without manual effort. Changes to `config.yml`, `claude-modifications/`, or any other files in the config directory are captured automatically.

The push step is skipped if the repository has no `origin` remote — so a purely local Git repo (e.g. `git init` with no remote) will still get periodic commits for local history without push errors.

#### First-run setup

On the very first run, if stdin is a TTY, agenc prompts you to optionally clone an existing config repo:

```
Welcome to agenc! Setting up for the first time.

Do you have an existing agenc config repo to clone? [y/N]
```

Answering **yes** lets you provide a repo reference (`owner/repo`, `github.com/owner/repo`, or a GitHub URL), which agenc clones into `$AGENC_DIRPATH/config/`. This lets you restore your agenc configuration on a new machine or share it across machines. Answering **no** (or running non-interactively) proceeds with the default empty config.

1Password Secret Injection
--------------------------

Repos often need secrets — API tokens, database credentials, etc. AgenC integrates with [1Password CLI](https://developer.1password.com/docs/cli/) (`op`) to inject secrets at runtime without storing them on disk.

### Setup

Create a `.claude/secrets.env` file in your repo. Each line maps an environment variable to a [1Password secret reference](https://developer.1password.com/docs/cli/secret-references/):

```
NOTION_TOKEN=op://Personal/Notion API Token/credential
TODOIST_API_KEY=op://Personal/Todoist/api_key
```

When AgenC detects this file, it automatically wraps the Claude invocation with `op run`, which resolves secret references and injects the values as environment variables.

### Example: MCP servers with secrets

This is particularly useful for MCP servers that need API tokens. For example, a `.mcp.json` that connects to Todoist and a custom Notion server:

```json
{
    "mcpServers": {
        "todoist": {
            "type": "http",
            "url": "https://ai.todoist.net/mcp"
        },
        "notion": {
            "command": "npx",
            "args": [
                "-y",
                "@mieubrisse/notion-mcp-server"
            ],
            "env": {
                "NOTION_TOKEN": "${NOTION_TOKEN}"
            }
        }
    }
}
```

The `${NOTION_TOKEN}` reference is resolved from the environment, which `op run` populates from your `.claude/secrets.env`. The secret never touches disk — it flows from 1Password → environment → MCP server process.

### Requirements

- [1Password CLI](https://developer.1password.com/docs/cli/) (`op`) must be installed and in your PATH
- You must be signed in to 1Password (`op signin`)
- The `.claude/secrets.env` file is only needed in the repo; AgenC handles the rest

If `.claude/secrets.env` does not exist, AgenC launches Claude directly with no `op` dependency.

Theory
------

An AI agent is a probabilistic function. It takes input - context, instructions, tools - and produces a good output some percentage of the time. Not 100%. Never 100%. That's the fundamental constraint of the medium.

This is what makes AI agents different from traditional software. A well-written function returns the correct result every time. An AI agent returns a *useful* result most of the time, and the exact threshold depends on how well you've tuned it.

Your organization is a function too - composed of these agent functions. You have a coding agent, an email agent, a writing agent. Each is a probabilistic function with its own success rate. The org's overall capability is bounded by its weakest agents and degraded by uncertainty compounding across them.

This is what it means to "program an organization." The industrial capitalists could only approximate it - writing policies, training workers, hoping the message got through. You can do it precisely: adjust a prompt, add a permission, provide a better example. The agent updates immediately. The org function improves.

The key insight is that refining the outer function means refining the inner functions. Every time an agent misbehaves, that's signal. Capture it in the agent's config, and you've permanently raised its success rate. Do this systematically across all your agents, and the organization compounds in capability rather than in error.

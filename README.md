![](./image.png)

AgenC
=====

Managing AI agents is tedious. Every time an agent misbehaves, you have to find the right config file, update it, restart the agent, and hope you remember the fix next time. Multiply that across multiple agents and the overhead becomes significant.

AgenC (pronounced "agency") solves this. It's a CLI tool that runs agents in isolated sandboxes, tracks all conversations, and makes it trivial to roll lessons back into agent config.

Quick Start
-----------

### Prerequisites

- **macOS** (Linux support planned)
- **Claude CLI** installed and in your PATH ([installation guide](https://docs.anthropic.com/en/docs/claude-code/getting-started))

### Install

```
brew tap mieubrisse/agenc
brew install agenc
```

### Create your first agent template

Agent templates define how your agents behave — their instructions, permissions, MCP servers, and skills. Create one:

```
agenc template new --nickname "Software Engineer" --default repo
```

You'll be prompted for a repo name (e.g., `your-username/software-engineer`). AgenC creates a private GitHub repo, initializes it with template files, and launches a mission to help you configure it. The `--default repo` flag makes this template auto-selected when you open repositories.

### Open a repo

Now put your agent to work. Open any GitHub repo:

```
agenc mission new owner/repo
```

AgenC accepts multiple formats — use whichever is convenient:

```
agenc mission new owner/repo                          # shorthand
agenc mission new github.com/owner/repo               # canonical
agenc mission new https://github.com/owner/repo       # HTTPS URL
agenc mission new git@github.com:owner/repo.git       # SSH URL
```

The repo is cloned into an isolated sandbox, your Software Engineer template is applied, and Claude launches ready to work.

CLI Reference
-------------

Run `agenc --help` for available commands, or see [docs/cli/](docs/cli/) for complete documentation.

<!--- TODO Debora feedback - why use AgenC? There are a million AIs out there; why do we need this one? -->

How it works
------------

1. Any time you have a negative interaction with an agent (bad output, missing permissions), it's trivial to roll the lesson back into the agent's config so you never hit it again ([Inputs, Not Outputs principle](https://mieubrisse.substack.com/p/inputs-not-outputs)).
2. Sandboxing and session management let you run dozens of agents simultaneously, constantly rolling lesson "exhaust" back into your agents' configs. They become a super team who understand your every whim.

For a detailed technical overview of the system's components and how they interact, see [System Architecture](docs/system-architecture.md).

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

<!--
Why AgenC?
----------
You want a swarm of infinite obedient robots, making your every whim a reality.

This requires a LOT of alignment work.

Every time the agents don't get it right, you need to capture the lesson back into your agent config: the [Inputs, Not Outputs](https://mieubrisse.substack.com/p/inputs-not-outputs) doctrine.

But vanilla Claude is really bad at this.

Every time an agent gets it wrong, it's on you to find the config file that contributed to the problem.

Then you have to update it (usually requiring another Claude window).

Then, you have to find all your prompts using that config and restart them.

And if you want to do any sort of longitudinal retrospective on how well your prompts are performing overall... good luck.

AgenC solves this:

- All agent conversations are collected & navigable
- All agent config is version-controlled & deployed
- At any point when using an agent, you can fork a new Claude code window editing the agent itself
- When an agent's config is updated, all agents using that config restart with the config the next time they're idle
-->

Workflows
-------------
### AgenC is currently really good at these workflows

- **Human-in-the-loop assistant work with MCP:** Examples: email/Todoist inbox processing, calendar management.
- **Human-in-the-loop editing a repo:** Examples: coding, editing AgenC agents, writing

Basically, [painting with your mind](https://mieubrisse.substack.com/p/be-rembrandt).

It works like this:

1. You create or install **agent template** repos ([example](https://github.com/mieubrisse/agenc-agent-template_agenc-engineer)) containing the agent config you want (CLAUDE.md, skills, MCP, and even 1Password secrets to inject)
1. Agent templates get instantiated into an **agent** on a **mission**, with its own sandbox and repo copies where the agent can write files, commit, etc. without interfering with other missions
1. When an agent doesn't do the right thing, you fire off a new mission to refine the agent's prompt 
1. All work is tracked and accessible, so you can run agents to analyze inefficiencies and roll improvements back into your AgenC

### AgenC doesn't currently handle these workflows, but will soon

- **Completely autonomous work:** Example: instruct the agent to do a thing without you being connected to the Claude TUI.
- **Dockerized:** Running agents in Docker so they can do `--dangerously-skip-permissions`
- **Cron:** Example: every Wednesday, summarize HackerNews and let me know what you found.
- **Automated lesson capture:** Identifying lessons that need to be rolled back into config proactively, rather than waiting for you.
- **Inter-agent communication:** Exmaple: the Code Writer agent hands off its work to the Code Reviewer agent who hands off to the PR Coordinator agent.

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

# Claude config source repo — provides CLAUDE.md, settings.json, skills, etc.
claudeConfig:
  repo: github.com/owner/config-repo
  subdirectory: ""              # Optional subdirectory within the repo

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
```

#### syncedRepos

A list of repositories the daemon keeps continuously up-to-date (fetched and fast-forwarded every 60 seconds). Use `syncedRepos` for repos you want kept fresh in the shared library.

Manage the list via the CLI:

```
agenc repo add owner/repo --sync   # clone and add to syncedRepos
agenc repo rm owner/repo           # remove from disk and syncedRepos
```

#### claudeConfig

The Claude config source repo provides the base CLAUDE.md, settings.json, skills, hooks, commands, agents, and plugins for every mission. When you create a mission, AgenC copies these from the config source and merges them with AgenC-specific modifications (from `$AGENC_DIRPATH/config/claude-modifications/`).

The optional `subdirectory` field lets you point to a subdirectory within the repo if your Claude config files are not at the root.

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

Agent templates often need secrets — API tokens, database credentials, etc. AgenC integrates with [1Password CLI](https://developer.1password.com/docs/cli/) (`op`) to inject secrets at runtime without storing them on disk.

### Setup

Create a `.claude/secrets.env` file in your agent template repo. Each line maps an environment variable to a [1Password secret reference](https://developer.1password.com/docs/cli/secret-references/):

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
- The `.claude/secrets.env` file is only needed in the agent template repo; AgenC handles the rest

If `.claude/secrets.env` does not exist, AgenC launches Claude directly with no `op` dependency.

Theory
------

An AI agent is a probabilistic function. It takes input - context, instructions, tools - and produces a good output some percentage of the time. Not 100%. Never 100%. That's the fundamental constraint of the medium.

This is what makes AI agents different from traditional software. A well-written function returns the correct result every time. An AI agent returns a *useful* result most of the time, and the exact threshold depends on how well you've tuned it.

Your organization is a function too - composed of these agent functions. You have a coding agent, an email agent, a writing agent. Each is a probabilistic function with its own success rate. The org's overall capability is bounded by its weakest agents and degraded by uncertainty compounding across them.

This is what it means to "program an organization." The industrial capitalists could only approximate it - writing policies, training workers, hoping the message got through. You can do it precisely: adjust a prompt, add a permission, provide a better example. The agent updates immediately. The org function improves.

The key insight is that refining the outer function means refining the inner functions. Every time an agent misbehaves, that's signal. Capture it in the agent's config, and you've permanently raised its success rate. Do this systematically across all your agents, and the organization compounds in capability rather than in error.

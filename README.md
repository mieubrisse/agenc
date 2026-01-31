The AgenC
=========

_Pronounced "agency"._

An orchestrator for running many Claude Code agents in parallel, each working independently on assigned missions. Feed it work via the CLI, and the AgenC spins up agents to get it done.

Why AgenC?
----------
TODO emphasizing Inputs Not Outputs

People like me want to use Claude Code as their Everything Agent, including for non-coding things like Todoist. This has the following benefits:

- Use my beloved Vim & tmux config for everything
- **Ease-of-plumbing:** No copy-pasting to and from the Desktop app. Everything is read to and written from files, which Claude Code can work with without user intervention.
- Scriptable (e.g. I can automate tmux to create workspaces open with the agents I use the most)
- Granular control over MCP servers
- Integrate secrets with 1Password via 1Password CLI

However, this has several problems:

- To do this securely (without `--dangerously-skip-permissions`), you have to do a BUNCH of settings.json management
- It's on you to create the directory & version-control structure of agents
- Often, these sorts of tasks are "do a bit of work for a bit, end", which works really well with sandbox directories. but...
    - You have to set all the sandboxing up yourself
    - Claude's really oriented around long-lived Git repos, and every time you add a new directory you have to tell Claude:
        - to trust the directory
        - set up the settings.json (which is painful given Claude's permission system is a bit wonky)
        - optionally set up any .mcp.json
- On top of all this, you need to wire up the 1Password stuff manually
- During the course of the session, you'll often hit times where the agent doesn't behave the way you want (often, CLAUDE.md or settings.json need refining). Per the doctrine of [Inputs, Not Outputs](https://mieubrisse.substack.com/p/inputs-not-outputs) you'd like to roll fixes back into the agent so the entire agency gets better, but you then have to stop what you're doing, find the agent's CLAUDE.md, open a new claude window, update it, and then restart the original conversation to pick up the changes (especially with settings.json).
- Whenever you upgrade the CLAUDE.md, or a new version of `claude` is released, you have to find and restart all your windows

AgenC solves all these:

- You define **agent templates** inside a version-controlled AgenC config repo, with the Claude config you want (`CLAUDE.md` and `settings.json` and `.mcp.json` and `skills`)
- You send agents on **missions**, which execute inside a sandbox directory created from the agent template
- Agent templates can be registered with secrets from 1Password, which get automatically injected upon agent creation
- When talking to any agent on a mission, with a simple hotkey you can switch to editing the template that created the agent to update the Inputs, making your AgenC better forever
- Missions can be quickly and easily archived to keep things tidy
- The Claude for a mission is automatically restarted when its settings change (when it's idle; generation isn't interrupted)

In the future, AgenC will also:

- Let you analyze mission results and proactively suggest fixes to agents
- Analyze how effective each agent is
- Execute missions inside Docker containers so `--dangerously-skip-permissions` is allowed

Architecture
------------

The AgenC is a Go CLI tool built with [Cobra](https://github.com/spf13/cobra). It manages all state in a single root directory and uses SQLite to track missions.

### Root Directory

All AgenC state lives under a single root directory, configured by the `AGENC_DIRPATH` environment variable. It defaults to `~/.agenc`.

```
$AGENC_DIRPATH/
├── agent-templates/  # One subdirectory per agent template
├── claude/           # CLAUDE_CONFIG_DIR for all Claude instances run by AgenC
├── missions/         # One subdirectory per mission (keyed by UUID)
└── database.sqlite   # Tracks missions and their state
```

### agent-templates

The `agent-templates` directory contains one subdirectory per agent template. Each template defines the Claude configuration for a specific type of agent:

```
agent-templates/
├── agent1/
│   ├── CLAUDE.md              # Instructions specific to agent1
│   ├── .mcp.json              # (optional) Agent-specific MCP config
│   └── .claude/
│       ├── settings.json      # (optional) Agent-specific settings
│       ├── secrets.env        # (optional) Secrets injected via 1Password
│       └── skills/            # (optional) Agent-specific skills
└── agent2/
    └── ...
```

**Agent templates** define agent-specific overrides. Each template can add its own `CLAUDE.md` instructions, MCP servers, secrets, and skills on top of the global config. When a mission launches, the global and agent-specific configs are merged to produce the mission's environment.

### missions

The `missions` directory contains workspaces for each mission. Each mission is identified by a UUID:

```
missions/
├── 0f4edd01-c480-462d-a44e-c1bd48aaa5a6/
│   ├── CLAUDE.md              # Built by cat'ing global + agent-specific CLAUDE.md
│   ├── .mcp.json              # (optional) Built by merging global + agent-specific mcp.json
│   ├── .claude/
│   │   └── settings.json      # Built by merging global + agent-specific settings.json
│   └── workspace/
│       └── ...                # All files the agent creates or modifies
└── ARCHIVE/                   # Archived missions (moved here by `agenc mission archive`)
    └── ...
```

The `workspace/` subdirectory is where the agent does its actual work — creating files, cloning Git repos, writing output, etc.

### claude

All `claude` instances launched by the AgenC have their `CLAUDE_CONFIG_DIR` environment variable set to `$AGENC_DIRPATH/claude`. This makes the AgenC fully self-contained and prevents it from interfering with any preexisting Claude Code installation on the machine.

### database.sqlite

The SQLite database currently tracks mission IDs. The schema will expand over time as needed.

CLI
---

The binary is called `agenc` and follows the `noun verb` pattern (similar to Kubernetes/Docker):

```
agenc <noun> <verb> [args...]
```

### agenc mission new

Creates a new mission and drops the user into a Claude Code session.

**Interactive mode** (no arguments):

1. The user is dropped into `fzf` to pick an agent template. The default option is `NONE` (no specific agent template).
2. The user is dropped into `vim` to write the mission prompt — what they want the agent to accomplish.
3. The AgenC creates the mission: generates a UUID, records it in the SQLite database, and constructs a `missions/<uuid>/` directory by merging the global and agent-specific config.
4. The AgenC execs into `claude` in the mission directory (foreground), sending the prompt as the first message.

**Non-interactive mode** (for scripting):

```
agenc mission new --agent <template-name> "<prompt>"
```

Both `--agent` and the prompt are optional. If either is missing, the interactive flow fills in the gaps (e.g. omitting `--agent` triggers `fzf`, omitting the prompt triggers `vim`).

### agenc mission ls

Lists all active missions.

### agenc mission resume \<mission-id\>

Resumes an existing mission by running `claude -c` in the mission's directory. Since each mission is its own project directory, all conversations are scoped to that mission.

### agenc mission archive \<mission-id\>

Archives a mission by moving it to the `missions/ARCHIVE/` subdirectory.

Example Workflows
-----------------

The AgenC is general-purpose. Any task you could give to a Claude Code session, you can give to the AgenC. Some examples:

- **Code changes** — "Clone github.com/myorg/api, add rate limiting to all public endpoints, and open a PR."
- **Research** — "Research the top 5 Golang ORMs and write a comparison."
- **Writing** — "In the substack repo, write a post about the future of AI agents and commit it."
- **Calendar management** — "Add a weekly team sync every Tuesday at 10am to my Google Calendar."

Configuration
-------------

| Variable | Default | Description |
|---|---|---|
| `AGENC_DIRPATH` | `~/.agenc` | Root directory for all AgenC state |

Design Goals
------------

- **Mission management** — Create, track, and organize missions with a simple CLI.
- **Mission isolation** — Each mission operates in its own directory with a merged config tailored to its agent.
- **Self-contained** — The AgenC uses its own `CLAUDE_CONFIG_DIR` and never touches the user's existing Claude Code setup.
- **Configurable agents** — Agent templates let you define specialized agents with their own instructions, MCP servers, secrets, and skills.
- **Observable** — Clear logging and SQLite tracking for all missions.
- **Simple interface** — Submit a mission via the CLI. The AgenC handles the rest.

Status
------

This project is in early development.

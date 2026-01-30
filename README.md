The AgenC
=========

_Pronounced "agency"._

An orchestrator for running many Claude Code agents in parallel, each working independently on assigned missions. Feed it work via the CLI, and the AgenC spins up agents to get it done.

Why
---

A single Claude Code session is powerful, but some workloads benefit from parallelism — large refactors, bulk migrations, research across multiple topics, or just knocking out a backlog of unrelated tasks. The AgenC manages a pool of Claude Code agents, distributes missions across them, and collects results.

Architecture
------------

The AgenC is a Go CLI tool built with [Cobra](https://github.com/spf13/cobra). It manages all state in a single root directory and uses SQLite to track missions.

### Root Directory

All AgenC state lives under a single root directory, configured by the `AGENC_DIRPATH` environment variable. It defaults to `~/.agenc`.

```
$AGENC_DIRPATH/
├── config/           # Git repo containing all AgenC configuration
├── claude/           # CLAUDE_CONFIG_DIR for all Claude instances run by AgenC
├── missions/         # One subdirectory per mission (keyed by UUID)
└── database.sqlite   # Tracks missions and their state
```

### config

The `config` directory is a Git repo containing the configuration that governs the AgenC and its agents. It has the following structure:

```
config/
├── claude/
│   ├── CLAUDE.md              # Global CLAUDE.md included by all agents
│   ├── mcp.json               # (optional) Global MCP server config
│   ├── settings.json          # Global settings.json included by all agents
│   └── skills/                # (optional) Skills applied to all agents
│       ├── skill1/
│       │   └── SKILL.md
│       └── skill2/
│           └── SKILL.md
└── agent-templates/
    ├── agent1/
    │   ├── CLAUDE.md          # Instructions specific to agent1
    │   ├── mcp.json           # (optional) Agent-specific MCP config
    │   └── claude/
    │       ├── secrets.env    # (optional) Secrets injected via 1Password
    │       └── skills/        # (optional) Agent-specific skills
    └── agent2/
        └── ...
```

**Global config** (`config/claude/`) is shared across all agents. It defines the baseline behavior, MCP servers, settings, and skills that every agent gets.

**Agent templates** (`config/agent-templates/`) define agent-specific overrides. Each template can add its own `CLAUDE.md` instructions, MCP servers, secrets, and skills on top of the global config. When a mission launches, the global and agent-specific configs are merged to produce the mission's environment.

### missions

The `missions` directory contains workspaces for each mission. Each mission is identified by a UUID:

```
missions/
└── 0f4edd01-c480-462d-a44e-c1bd48aaa5a6/
    ├── CLAUDE.md              # Built by cat'ing global + agent-specific CLAUDE.md
    ├── .mcp.json              # (optional) Built by merging global + agent-specific mcp.json
    ├── .claude/
    │   └── settings.json      # Built by merging global + agent-specific settings.json
    └── workspace/
        └── ...                # All files the agent creates or modifies
```

The `workspace/` subdirectory is where the agent does its actual work — creating files, cloning Git repos, writing output, etc.

### claude

All `claude` instances launched by the AgenC have their `CLAUDE_CONFIG_DIR` environment variable set to `$AGENC_DIRPATH/claude`. This makes the AgenC fully self-contained and prevents it from interfering with any preexisting Claude Code installation on the machine.

### database.sqlite

The SQLite database currently tracks mission IDs. The schema will expand over time as needed.

Doing Work
----------

Launching a mission follows this flow:

1. The user tells the AgenC to launch a mission and, optionally, specifies which agent template to use.
2. The AgenC creates a new mission: generates a UUID, records it in the SQLite database, and constructs a `missions/<uuid>/` directory by merging the global and agent-specific config from `config/`.
3. The mission directory is ready for a Claude Code session to operate in.

CLI Usage
---------

Launch a mission:

```
agenc run "Refactor the authentication module in github.com/myorg/myapp"
```

Launch a mission with a specific agent template:

```
agenc run --agent code-reviewer "Review the open PRs on github.com/myorg/myapp"
```

Check status of all missions:

```
agenc status
```

View details for a specific mission:

```
agenc status <mission-id>
```

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

The AgenC
=========
AgenC (pronounced "agency") is:
- An agent orchestrator for doing agentic work
- A personal LLMOps system for refining & deploying your agents
- A plugin system for sharing agents

Why AgenC?
----------
You want a swarm of infinite obedient agents, implementing your every thought.

This requires a LOT of work, telling agents how you like things so they ever more read your mind.

Every time the agents don't get it right, you need to roll the lesson back into the agent config: the [Inputs, Not Outputs](TODO link to factory) doctrine.

But vanilla Claude is really bad at this.

Every time you notice suboptimal output, it's on you to find the config file that contributed to the problem.

Then you have to update it (usually requiring another Claude window).

Then, you have to find all your prompts using that config and restart them.

And if you want to do any sort of retrospective on how well your prompts are performing overall... good luck.

AgenC solves this:

- All agent config is version-controlled
- When using any agent, it's trivial to start a new Claude window editing the config
- When an agent's config is updated, all agents using that config restart with the config the next time they're idle
- All work is tracked for analysis: how well are the agents performing?

How it works
------------
1. You create or install **agent template** repos containing the Claude config you want (CLAUDE.md, skills, MCP, and even 1Password secrets to inject)
1. You send agents on **missions**, which execute inside a sandbox directory created from the agent template
1. When talking to any agent on a mission, with a simple hotkey you can switch to editing the template, making your AgenC better forever
1. When an agent's config changes, the agent is restarted the next time it's idle to use the new config
1. All work is tracked and accessible, so you can run agents to analyze inefficiencies and roll improvements back into your AgenC

Getting started
---------------
TODO brew installation instructions

Future work
-----------
- Let you analyze mission results and proactively suggest fixes to agents
- Analyze how effective each agent is
- Execute missions inside Docker containers so `--dangerously-skip-permissions` is allowed
- Build settings.json files for you with AI (even out in your filesystem)
    - E.g. when you're starting a task, it will suggest settings.json's for you, so you don't have to give a bunch of "yes"s

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

**Agent templates** define the full Claude configuration for an agent. Each template can include its own `CLAUDE.md` instructions, MCP servers, settings, secrets, and skills. When a mission launches, the template's config files are copied as-is into the mission directory.

### missions

The `missions` directory contains workspaces for each mission. Each mission is identified by a UUID:

```
missions/
├── 0f4edd01-c480-462d-a44e-c1bd48aaa5a6/
│   ├── CLAUDE.md              # Copied from agent template
│   ├── .mcp.json              # (optional) Copied from agent template
│   ├── .claude/
│   │   └── settings.json      # Copied from agent template
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
3. The AgenC creates the mission: generates a UUID, records it in the SQLite database, and constructs a `missions/<uuid>/` directory by copying config files from the agent template.
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
- **Mission isolation** — Each mission operates in its own directory with config copied from its agent template.
- **Self-contained** — The AgenC uses its own `CLAUDE_CONFIG_DIR` and never touches the user's existing Claude Code setup.
- **Configurable agents** — Agent templates let you define specialized agents with their own instructions, MCP servers, secrets, and skills.
- **Observable** — Clear logging and SQLite tracking for all missions.
- **Simple interface** — Submit a mission via the CLI. The AgenC handles the rest.

Status
------

This project is in early development.

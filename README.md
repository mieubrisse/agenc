Agent Factory
=============

An orchestrator for running many Claude Code agents in parallel, each working independently on assigned tasks. Feed it work via the CLI, and the factory spins up agents to get it done.

Why
---

A single Claude Code session is powerful, but some workloads benefit from parallelism — large refactors, bulk migrations, research across multiple topics, or just knocking out a backlog of unrelated tasks. Agent Factory manages a pool of Claude Code agents, distributes tasks across them, and collects results.

Architecture
------------

Agent Factory is a Go CLI tool built with [Cobra](https://github.com/spf13/cobra). It manages all state in a single working directory and uses SQLite to track agent work.

### Working Directory

All Agent Factory state lives under a single root directory, configured by the `AGENT_FACTORY_DIRPATH` environment variable. It defaults to `~/.agent-factory`.

```
~/.agent-factory/
├── agent-factory.db        # SQLite database tracking tasks and agent state
└── workspaces/             # One subdirectory per agent
    ├── agent-abc123/
    ├── agent-def456/
    └── ...
```

### Agent Lifecycle

1. A task is submitted via the CLI and recorded in the SQLite database.
2. The factory assigns the task to a new agent and creates a workspace directory for it.
3. A Claude Code session launches inside that workspace and executes the task.
4. On completion (or failure), the agent's status is updated in the database.

### Workspaces

Each agent gets its own directory under `workspaces/`. This provides isolation — agents don't step on each other's files. For tasks that operate on external repositories, the agent clones or checks out the repo inside its workspace.

### SQLite Database

The `agent-factory.db` file tracks:

- **Tasks** — what was requested, current status, result summary
- **Agents** — which agent is working on what, when it started, when it finished
- **Logs** — output and errors from each agent session

CLI Usage
---------

Submit a task:

```
agent-factory run "Refactor the authentication module in github.com/myorg/myapp"
```

Check status of all tasks:

```
agent-factory status
```

View details for a specific task:

```
agent-factory status <task-id>
```

Example Workflows
-----------------

Agent Factory is general-purpose. Any task you could give to a Claude Code session, you can give to the factory. Some examples:

- **Code changes** — "Clone github.com/myorg/api, add rate limiting to all public endpoints, and open a PR."
- **Research** — "Research the top 5 Golang ORMs and write a comparison to /tmp/orm-comparison.md."
- **Writing** — "In the substack repo, write a post about the future of AI agents and commit it."
- **Calendar management** — "Add a weekly team sync every Tuesday at 10am to my Google Calendar."

Configuration
-------------

| Variable | Default | Description |
|---|---|---|
| `AGENT_FACTORY_DIRPATH` | `~/.agent-factory` | Root directory for all Agent Factory state |

Design Goals
------------

- **Parallel execution** — Run many agents concurrently, bounded by a configurable concurrency limit.
- **Task isolation** — Each agent operates in its own workspace to avoid conflicts.
- **Fault tolerance** — If an agent fails, the factory retries or reports the failure without blocking other agents.
- **Observable** — Clear logging and status reporting so you can see what each agent is doing.
- **Simple interface** — Submit a task via the CLI, get back results. Minimal ceremony.

Status
------

This project is in early development.

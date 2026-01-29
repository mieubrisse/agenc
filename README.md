Agent Factory
=============

An orchestrator for running many Claude Code agents in parallel, each working independently on assigned tasks. Feed it work, and the factory spins up agents to get it done.

Why
---

A single Claude Code session is powerful, but some workloads — large refactors, bulk migrations, multi-repo changes — benefit from parallelism. Agent Factory manages a pool of Claude Code agents, distributes tasks across them, and collects results.

How It Works
------------

1. **Define work** — Provide a list of tasks (from a file, a script, or a queue).
2. **Spin up agents** — The factory launches Claude Code agent sessions, each in its own isolated workspace.
3. **Execute independently** — Each agent works on its assigned task without interfering with others.
4. **Collect results** — The factory gathers outputs, logs, and status from each agent.

Key Design Goals
----------------

- **Parallel execution** — Run many agents concurrently, bounded by a configurable concurrency limit.
- **Task isolation** — Each agent operates in its own workspace to avoid conflicts.
- **Fault tolerance** — If an agent fails, the factory retries or reports the failure without blocking other agents.
- **Observable** — Clear logging and status reporting so you can see what each agent is doing.
- **Simple interface** — Feed it a task list, get back results. Minimal ceremony.

Status
------

This project is in early development. The codebase is being built out.

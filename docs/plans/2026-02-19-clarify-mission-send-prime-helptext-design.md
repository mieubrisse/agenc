2026-02-19 Clarify mission send / prime helptext
=================================================

Problem
-------

Agents running `agenc prime` were consistently confused by two gaps in the
quick reference output:

1. `agenc mission send` appeared as a user-facing command with the description
   "Send messages to a running mission wrapper". In practice it is internal
   plumbing called only by Claude Code hooks (`claude-update` subcommand). It
   has no `RunE` and does nothing when invoked directly.

2. The `--prompt` flag on `agenc mission new` was completely invisible in the
   helptext. Agents tried positional arguments, stdin pipes, a nonexistent
   `--message` flag, and `mission resume --message`, before giving up.

Observed failure mode: agents would create a mission without a prompt, then
spend multiple reasoning steps searching for a "send" primitive to deliver
their task — eventually falling back to Todoist rather than completing the work.

Design
------

Edit `internal/claudeconfig/agenc_usage_skill.md`:

**Remove** `agenc mission send` from the "Manage agent missions" command table.
It is internal infrastructure and should not be advertised to agents.

**Expand** the `agenc mission new` entry to surface the two flags agents need:

```
agenc mission new [repo]                                  # Create a new mission (opens fzf picker if no repo given)
agenc mission new [repo] --prompt "task"                  # Create a mission with an initial prompt
agenc mission new [repo] --headless --prompt "task"       # Run a headless mission (no terminal; logs to file)
```

**Add** a "Spawning missions (most common patterns)" section directly after the
intro paragraph, before the command reference sections:

```
Spawning missions (most common patterns)
-----------------------------------------

# Interactive mission with a starting task:
agenc mission new github.com/owner/repo --prompt "Your task here"

# Headless (unattended, outputs to log file):
agenc mission new github.com/owner/repo --headless --prompt "Your task here"
```

Scope
-----

Single file change: `internal/claudeconfig/agenc_usage_skill.md`.
No Go code changes required — `agenc prime` prints the file verbatim.

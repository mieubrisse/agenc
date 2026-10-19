## agenc mission new

Create a new mission and launch claude

### Synopsis

Create a new mission and launch claude.

Without arguments, opens an interactive fzf picker showing your repo library.
With arguments, accepts a git reference (URL, shorthand like owner/repo, or
local path).

Use --clone <mission-uuid> to create a new mission with a full copy of an
existing mission's agent directory.

Anything after a "--" separator is handed to the mission's Claude process
verbatim, so any Claude Code flag is reachable per-mission even when AgenC
has no flag of its own for it:

  agenc mission new owner/repo -- --append-system-prompt "Tonight is for reflection."

The args are stored on the mission, so they survive reload, restart and the
wrapper's own respawns, and they never touch a shell — a value containing
spaces, quotes or newlines arrives at Claude exactly as written. They apply to
that mission alone.

Claude resolves a repeated scalar flag last-wins, and these are appended after
the "claudeArgs" config chain, so a flag given here outranks the same flag from
config. Two Claude flags will disable part of AgenC if passed this way, and
creation warns when it sees them: "--settings" replaces AgenC's own operational
overlay rather than adding to it, and "--bare" skips hooks altogether. Either
one stops the mission reporting its state back to AgenC.

Use --env to put an environment variable in the mission's Claude process,
repeating the flag for each one:

  agenc mission new owner/repo --env REFLECTION_SESSION=1

This is how a session gets marked for a Claude Code hook to recognise. It is a
separate channel from the passthrough above precisely so that marking a session
does not need "--settings", which would replace AgenC's operational overlay
rather than adding to it. Like the passthrough args, these are stored on the
mission and survive reload and restart, and they apply to that mission alone --
a clone does not inherit them, so a marker cannot spread to missions nobody
declared. AgenC's own mission identity, AGENC_MISSION_UUID, cannot be
overridden this way: AgenC applies its own value last and it wins.

```
agenc mission new [repo] [flags] [-- claude-args...]
```

### Options

```
      --adjutant          create an Adjutant mission
      --blank             create a blank mission with no repo (skip picker)
      --clone string      mission UUID to clone agent directory from
      --effort string     Claude reasoning effort for this mission (low, medium, high, xhigh, max)
      --env stringArray   environment variable to set in the mission's Claude process, as NAME=VALUE (repeatable)
      --headless          run in headless mode (no terminal, outputs to log)
  -h, --help              help for new
      --model string      Claude model for this mission (e.g. "opus", "sonnet", "claude-opus-4-6"); overrides the defaultModel config
      --no-focus          don't focus the new mission's tmux window after creation
      --prompt string     initial prompt to start Claude with
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions


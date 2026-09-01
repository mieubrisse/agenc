## agenc mission new

Create a new mission and launch claude

### Synopsis

Create a new mission and launch claude.

Without arguments, opens an interactive fzf picker showing your repo library.
With arguments, accepts a git reference (URL, shorthand like owner/repo, or
local path).

Use --clone <mission-uuid> to create a new mission with a full copy of an
existing mission's agent directory.

```
agenc mission new [repo] [flags]
```

### Options

```
      --adjutant        create an Adjutant mission
      --blank           create a blank mission with no repo (skip picker)
      --clone string    mission UUID to clone agent directory from
      --effort string   Claude reasoning effort for this mission (low, medium, high, xhigh, max)
      --headless        run in headless mode (no terminal, outputs to log)
  -h, --help            help for new
      --model string    Claude model for this mission (e.g. "opus", "sonnet", "claude-opus-4-6"); overrides the defaultModel config
      --no-focus        don't focus the new mission's tmux window after creation
      --prompt string   initial prompt to start Claude with
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions


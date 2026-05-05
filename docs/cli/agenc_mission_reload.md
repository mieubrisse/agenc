## agenc mission reload

Reload a mission in-place (preserves tmux pane)

### Synopsis

Reload a mission in-place (preserves tmux pane).

Stops the mission wrapper and restarts it in the same tmux pane, preserving
window position, title, and conversation state. Useful after updating the
mission config or upgrading the agenc binary.

Without arguments, opens an interactive fzf picker showing running missions.
With an argument, accepts a mission ID (short 8-char hex or full UUID).

The --prompt flag, when set, is fed to Claude as a follow-up message that
runs immediately after the reload completes. The mission must have a live
tmux pane for --prompt to apply.

The --async flag queues the reload to fire on Claude's next idle (when its
current turn finishes) instead of bouncing immediately. AGENTS RELOADING
THEMSELVES SHOULD ALWAYS USE --async: a synchronous self-reload kills
Claude mid-tool-call, which discards the bash tool result from Claude's
conversation history. Async preserves the tool call and the prompt arrives
cleanly on the next turn. Returns 202 Accepted; if Claude is already idle,
the reload fires immediately.

```
agenc mission reload [mission-id] [flags]
```

### Options

```
      --async           queue the reload for Claude's next idle (REQUIRED when an agent reloads itself, to preserve the calling tool result)
  -h, --help            help for reload
      --prompt string   follow-up prompt to send after reload (requires a mission with a live tmux pane)
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions


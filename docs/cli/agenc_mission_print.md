## agenc mission print

Print a mission's current session transcript (human-readable text by default)

### Synopsis

Print a mission's current session transcript.

By default, outputs the entire session as human-readable text.
Use --format=jsonl for raw JSONL output.
Use --tail to limit output to the last N lines.

A mission accumulates a session per reload, /clear or fork; this prints the most
recently active one. Use --session to print a different one, and
'agenc session ls --mission <id>' to list them.

A session is not a single transcript either: every subagent it spawns writes its
own. --agents lists that tree, --agent prints one subagent's transcript, and
--expand-agents inlines them all at their spawn sites. --verbose additionally
renders hooks, turn timings, attachments, thinking blocks and successful tool
results.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --format=jsonl
  agenc mission print 2571d5d8 --tail 50
  agenc mission print 2571d5d8 --agents
  agenc mission print 2571d5d8 --agent aa8d6202
  agenc mission print 2571d5d8 --expand-agents
  agenc mission print 2571d5d8 --session 18749fb5

```
agenc mission print [mission-id] [flags]
```

### Options

```
      --agent string     print a subagent's transcript, by agent ID or unique ID prefix
      --agents           list the session's subagent transcripts instead of printing a conversation
      --expand-agents    inline each subagent's transcript at the point it was spawned
      --format string    output format: text or jsonl (default "text")
  -h, --help             help for print
      --session string   print a specific session of the mission, by session ID or short ID
      --tail int         limit output to last N lines
      --verbose          render every record class: hooks, turn timings, attachments, thinking, successful tool results
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions


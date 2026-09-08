## agenc session print

Print a Claude session transcript (human-readable text by default)

### Synopsis

Print a Claude session transcript.

Accepts a full session UUID or an 8-character short ID.

By default, outputs a human-readable text summary. Use --format=jsonl for
raw JSONL output.

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

A session is not a single transcript: every subagent it spawns writes its own,
and those subagents spawn subagents. --agents lists that tree, --agent prints
one subagent's transcript, and --expand-agents inlines them all at their spawn
sites. --verbose additionally renders hooks, turn timings, attachments,
thinking blocks and successful tool results.

Example:
  agenc session print 18749fb5
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5 --format=jsonl
  agenc session print 18749fb5 --tail 50
  agenc session print 18749fb5 --all
  agenc session print 18749fb5 --agents
  agenc session print 18749fb5 --agent aa8d6202
  agenc session print 18749fb5 --all --expand-agents

```
agenc session print <session-id> [flags]
```

### Options

```
      --agent string        print a subagent's transcript, by agent ID or unique ID prefix
      --agents              list the session's subagent transcripts instead of printing a conversation
      --all                 print entire session
      --expand-agents       inline each subagent's transcript at the point it was spawned
      --format string       output format: text or jsonl (default "text")
  -h, --help                help for print
      --json                with --agents or --workflow: emit JSON instead of a table
      --max-expand-mb int   stop --expand-agents inlining past this many MB of subagent transcripts (0 = unlimited) (default 16)
      --since string        only records at or after this time: RFC3339, '2026-09-07 14:00', '14:00' (on the transcript's last day) or '2h' (back from its last record); times without a zone are local, while rendered record timestamps are UTC
      --tail int            number of lines to print from end of session (default 20)
      --until string        only records at or before this time; same forms as --since, and a bare date means the end of that day
      --verbose             render every record class: hooks, turn timings, attachments, thinking, successful tool results
      --workflow string     describe one workflow run and list its agents, by run ID, ID prefix, task ID or workflow name
```

### SEE ALSO

* [agenc session](agenc_session.md)	 - Manage Claude Code sessions


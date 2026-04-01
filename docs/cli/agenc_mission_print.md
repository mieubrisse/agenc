## agenc mission print

Print a mission's current session transcript (human-readable text by default)

### Synopsis

Print a mission's current session transcript.

By default, outputs the entire session as human-readable text.
Use --format=jsonl for raw JSONL output.
Use --tail to limit output to the last N lines.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --format=jsonl
  agenc mission print 2571d5d8 --tail 50

```
agenc mission print [mission-id] [flags]
```

### Options

```
      --format string   output format: text or jsonl (default "text")
  -h, --help            help for print
      --tail int        limit output to last N lines
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions


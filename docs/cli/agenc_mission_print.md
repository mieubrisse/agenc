## agenc mission print

Print a mission's current session transcript (human-readable text by default)

### Synopsis

Print a mission's current session transcript.

By default, outputs a human-readable text summary. Use --format=jsonl for
raw JSONL output.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc mission print
  agenc mission print 2571d5d8
  agenc mission print 2571d5d8 --format=jsonl
  agenc mission print 2571d5d8 --tail 50
  agenc mission print 2571d5d8 --all

```
agenc mission print [mission-id] [flags]
```

### Options

```
      --all             print entire session
      --format string   output format: text or jsonl (default "text")
  -h, --help            help for print
      --tail int        number of lines to print from end of session (default 20)
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions


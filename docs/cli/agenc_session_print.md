## agenc session print

Print a Claude session transcript (human-readable text by default)

### Synopsis

Print a Claude session transcript.

By default, outputs a human-readable text summary. Use --format=jsonl for
raw JSONL output.

Outputs the last 20 lines by default. Use --tail to change the line count,
or --all to print the entire session.

Example:
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --format=jsonl
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --tail 50
  agenc session print 18749fb5-02ba-4b19-b989-4e18fbf8ea92 --all

```
agenc session print <session-uuid> [flags]
```

### Options

```
      --all             print entire session
      --format string   output format: text or jsonl (default "text")
  -h, --help            help for print
      --tail int        number of lines to print from end of session (default 20)
```

### SEE ALSO

* [agenc session](agenc_session.md)	 - Manage Claude Code sessions


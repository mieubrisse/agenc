## agenc server logs print

Print server log content

### Synopsis

Print the server operational log (default) or HTTP request log.

By default, prints the last 200 lines. Use --all for the full file.

Examples:
  agenc server logs print
  agenc server logs print --requests
  agenc server logs print --all

```
agenc server logs print [flags]
```

### Options

```
      --all        print entire log file instead of last 200 lines
  -h, --help       help for print
      --requests   show HTTP request log instead of operational log
```

### SEE ALSO

* [agenc server logs](agenc_server_logs.md)	 - View server logs


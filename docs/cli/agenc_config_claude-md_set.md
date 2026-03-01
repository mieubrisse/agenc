## agenc config claude-md set

Update the AgenC-specific CLAUDE.md content

### Synopsis

Update the AgenC-specific CLAUDE.md content. Reads new content from stdin.

Requires --content-hash from a previous 'get' to prevent overwriting concurrent
changes. If the file was modified since your last read, the update is rejected
and you must re-read before retrying.

Example:
  agenc config claude-md get                                    # note the Content-Hash
  echo "New instructions" | agenc config claude-md set --content-hash=abc123

```
agenc config claude-md set [flags]
```

### Options

```
      --content-hash string   content hash from the last get (required)
  -h, --help                  help for set
```

### SEE ALSO

* [agenc config claude-md](agenc_config_claude-md.md)	 - Manage AgenC-specific CLAUDE.md instructions


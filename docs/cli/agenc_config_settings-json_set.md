## agenc config settings-json set

Update the AgenC-specific settings.json content

### Synopsis

Update the AgenC-specific settings.json content. Reads new content from stdin.

Content must be valid JSON. Requires --content-hash from a previous 'get' to
prevent overwriting concurrent changes.

Example:
  agenc config settings-json get                                         # note the Content-Hash
  echo '{"permissions":{"allow":["Bash(npm:*)"]}}' | agenc config settings-json set --content-hash=abc123

```
agenc config settings-json set [flags]
```

### Options

```
      --content-hash string   content hash from the last get (required)
  -h, --help                  help for set
```

### SEE ALSO

* [agenc config settings-json](agenc_config_settings-json.md)	 - Manage AgenC-specific settings.json overrides


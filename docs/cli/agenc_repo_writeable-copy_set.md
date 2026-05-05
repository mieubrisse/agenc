## agenc repo writeable-copy set

Configure a writeable copy for a repo

### Synopsis

Configure a writeable copy of a repo at the given absolute path. The path
must be outside ~/.agenc/ and must not overlap with any other configured
writeable copy.

The repo must already be in the repo library (use 'agenc repo add' first).
Setting a writeable copy implies always-synced=true.

After this command writes the config, the AgenC server picks up the change,
clones the repo to the path if it doesn't exist, and starts the sync loop.

```
agenc repo writeable-copy set <repo> <path> [flags]
```

### Options

```
  -h, --help   help for set
```

### SEE ALSO

* [agenc repo writeable-copy](agenc_repo_writeable-copy.md)	 - Manage writeable copies of repos

